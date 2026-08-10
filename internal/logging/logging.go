// Package logging provides the one thing the standard logger lacks here: a way
// to keep per-message diagnostics in the source without paying for them in
// production.
//
// Why this exists. Several statements sit in genuinely hot paths — one per
// inbound MQTT message (with the full payload), one per tag publication, one
// per alarm evaluation. On a developer's laptop with three simulated tags that
// is a useful trace. On a plant gateway with a thousand tags at 1 Hz it is
// upwards of a thousand lines a second: it burns CPU formatting strings nobody
// reads, and with the compose log rotation set to 10 MB × 3 it evicts the
// startup errors and stack traces that actually matter within minutes. The
// diagnostic that is on by default is the one that hides the incident.
//
// So they stay, gated. LOG_LEVEL=debug brings them back for troubleshooting.
package logging

import (
	"log"
	"os"
	"strings"
)

// Level ordering, lowest to highest verbosity.
type Level int

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

// current is read once at startup. It is deliberately not reconfigurable at
// runtime: a level that can change under a running poll loop would need a mutex
// on the hottest path in the process, which costs more than the log line.
var current = parseLevel(os.Getenv("LOG_LEVEL"))

func parseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		// Unset or unrecognised means info: verbose enough to explain a
		// start-up failure, quiet enough to survive a production shift.
		return LevelInfo
	}
}

// Enabled reports whether lvl would be emitted. Use it to skip building an
// expensive argument — a payload dump, a JSON re-encode — that the call would
// otherwise evaluate before discovering the level is off.
func Enabled(lvl Level) bool { return lvl <= current }

// Debugf logs only when LOG_LEVEL=debug.
func Debugf(format string, v ...interface{}) {
	if current >= LevelDebug {
		log.Printf(format, v...)
	}
}

// Infof logs at the default level.
func Infof(format string, v ...interface{}) {
	if current >= LevelInfo {
		log.Printf(format, v...)
	}
}

// Warnf logs unless the level is error-only.
func Warnf(format string, v ...interface{}) {
	if current >= LevelWarn {
		log.Printf(format, v...)
	}
}

// Errorf always logs.
func Errorf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// Level reports the configured level, for a start-up banner.
func CurrentLevel() string {
	switch current {
	case LevelDebug:
		return "debug"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}
