// Package commands decides whether a write command may still be executed.
//
// A command to a PLC is only meaningful in the moment it is given. "Set the
// oven to 180" or "open V3" issued at 10:00 is an operator's decision about the
// plant as it was at 10:00; executed at 14:00 it is nobody's decision at all.
//
// That delay is not hypothetical. An edge box reaches the central broker
// through a bridge with a persistent session, which is what lets readings queue
// across a dropped link instead of being lost — and the same session queues
// the commands flowing the other way. Without an expiry, every setpoint an
// operator typed while the link was down, and every retry of it because
// "nothing happened", would be written to the machine the moment the link came
// back.
//
// So a command carries the moment it was issued, and a driver refuses one that
// is older than the configured validity. A command arrives in time or not at
// all, and when it does not the operator is told why.
package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxAge is the validity used when none is configured. Long enough for
// a command crossing a slow link, short enough that nothing an operator could
// have forgotten about gets executed.
const DefaultMaxAge = 30 * time.Second

// MinMaxAge is the shortest validity accepted from configuration. Below it,
// ordinary network latency plus a second of clock difference would start
// refusing commands that are perfectly fresh.
const MinMaxAge = 5 * time.Second

// MaxAgeFromSeconds turns a configured value into a validity, falling back to
// the default for zero or nonsense and never going below MinMaxAge. There is
// deliberately no way to switch the check off.
func MaxAgeFromSeconds(s int) time.Duration {
	if s <= 0 {
		return DefaultMaxAge
	}
	d := time.Duration(s) * time.Second
	if d < MinMaxAge {
		return MinMaxAge
	}
	return d
}

// Fresh reports whether a command issued at issuedAtMs (Unix milliseconds) may
// be executed at now.
//
// A command with no timestamp is accepted. It can only come from a publisher
// older than this check — and refusing it would stop every write during a
// rolling upgrade, when core-api and the drivers are briefly on different
// versions. Every publisher in this repository stamps its commands.
//
// A command dated in the future by more than the validity is refused too. That
// is not a command from the future, it is a clock that is wrong — on this
// machine or on the one that issued it — and a clock that is wrong can make a
// stale command look fresh just as easily. Refusing surfaces the problem the
// first time somebody writes, instead of hiding it until the day it matters.
func Fresh(issuedAtMs int64, now time.Time, maxAge time.Duration) (bool, time.Duration) {
	if issuedAtMs <= 0 {
		return true, 0
	}
	age := now.Sub(time.UnixMilli(issuedAtMs))
	if age > maxAge || age < -maxAge {
		return false, age
	}
	return true, age
}

// RefusalMessage is what the operator reads when a command is refused. It says
// which of the two cases it was, because they are fixed in different places.
func RefusalMessage(age, maxAge time.Duration) string {
	if age < 0 {
		return fmt.Sprintf("command refused: issued %s in the future — the clocks of the "+
			"server and of this machine disagree; check NTP on both", (-age).Round(time.Second))
	}
	return fmt.Sprintf("command expired: issued %s ago, valid for %s — not executed, "+
		"send it again if it is still wanted", age.Round(time.Second), maxAge)
}

// MaxAgeFromDB reads the configured validity from global_settings.
//
// Read at the moment a command arrives rather than cached, because writes are
// rare and a validity changed from the web UI should apply to the next command
// without restarting every driver. Any failure falls back to the default: the
// check is never skipped because the setting could not be read.
func MaxAgeFromDB(ctx context.Context, db *sql.DB) time.Duration {
	if db == nil {
		return DefaultMaxAge
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var v string
	if err := db.QueryRowContext(ctx,
		`SELECT value FROM global_settings WHERE key = 'write_command_max_age_seconds'`).Scan(&v); err != nil {
		return DefaultMaxAge
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return DefaultMaxAge
	}
	return MaxAgeFromSeconds(n)
}
