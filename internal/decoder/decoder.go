// Package decoder runs a gateway's payload decoder: a JavaScript function that
// turns a device's own message format — binary, base64, hex, a vendor JSON —
// into tag values, each with the time the device took it.
//
// Devices that speak MQTT do not all speak JSON. A meter that packs a day of
// readings into a base64 frame every few hours needed a separate program in
// front of OpenEdge to be read at all. The vendor usually ships the decoding
// logic as JavaScript (it is what LoRaWAN network servers run), so that is
// what a decoder is: the vendor's function, pasted, plus a few lines that say
// which value is which.
//
// The script is untrusted input. Each message runs in a fresh JavaScript
// runtime with no file, network or module access, under a time limit, and
// its output is checked before anything reaches a tag.
//
// Contract:
//
//	function decode(msg) {
//	  // msg.text  — the payload as text (e.g. the base64 string)
//	  // msg.bytes — the payload as a Uint8Array
//	  // msg.topic — the MQTT topic it arrived on
//	  return [ { ts: 1774861200000, values: { AN1: 4.2, batteria: 3.66 } }, ... ];
//	}
//
// decode may also return a single { ts, values } object, or a plain object of
// values (taken at arrival time). ts is epoch milliseconds, epoch seconds
// (anything below 1e11), a Date or an ISO-8601 string; without it the sample
// is stamped with the arrival time. Values may be nested; a tag picks its
// value with a dotted path ("in7.valore", "stato.batteria_bassa").
package decoder

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Limits on one run. A decoder handles one message; anything near these is a
// loop, not a decoder.
const (
	DefaultTimeout = 500 * time.Millisecond
	MaxPayload     = 1 << 20 // 1 MiB
	MaxSamples     = 10000
	MaxScriptSize  = 256 << 10
	maxLogLines    = 50
)

// A sample stamped more than this far in the future is a device clock gone
// wrong, not a reading: it would sit at the top of every trend for days.
const maxFutureSkew = 24 * time.Hour

// Sample is one set of values the device took at one moment.
type Sample struct {
	Time   time.Time
	Values map[string]interface{}
}

// Decoder is a compiled decode script. It is safe for concurrent use: every
// Decode builds its own runtime.
type Decoder struct {
	prog    *goja.Program
	Timeout time.Duration
}

// Compile parses a decoder script and checks that it defines decode().
func Compile(src string) (*Decoder, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errors.New("the decoder script is empty")
	}
	if len(src) > MaxScriptSize {
		return nil, fmt.Errorf("the decoder script is larger than %d KB", MaxScriptSize>>10)
	}
	prog, err := goja.Compile("decoder.js", src, false)
	if err != nil {
		return nil, fmt.Errorf("the decoder script does not compile: %w", err)
	}
	d := &Decoder{prog: prog, Timeout: DefaultTimeout}
	rt, err := d.runtime(nil)
	if err != nil {
		return nil, err
	}
	if _, ok := goja.AssertFunction(rt.Get("decode")); !ok {
		return nil, errors.New("the decoder script must define a function decode(msg)")
	}
	return d, nil
}

// Result is what a run produced, logs included (for the test page).
type Result struct {
	Samples []Sample
	Logs    []string
}

// Decode runs the script on one message. now stamps samples without a time
// and bounds the ones in the future.
func (d *Decoder) Decode(payload []byte, topic string, now time.Time) (Result, error) {
	var res Result
	if len(payload) > MaxPayload {
		return res, fmt.Errorf("payload of %d bytes is larger than the %d-byte limit", len(payload), MaxPayload)
	}
	rt, err := d.runtime(&res.Logs)
	if err != nil {
		return res, err
	}
	decode, _ := goja.AssertFunction(rt.Get("decode"))

	msg := rt.NewObject()
	_ = msg.Set("text", string(payload))
	_ = msg.Set("topic", topic)
	u8, err := rt.New(rt.Get("Uint8Array"), rt.ToValue(rt.NewArrayBuffer(append([]byte(nil), payload...))))
	if err != nil {
		return res, fmt.Errorf("decoder: %w", err)
	}
	_ = msg.Set("bytes", u8)

	timer := time.AfterFunc(d.timeout(), func() { rt.Interrupt("time limit exceeded") })
	defer timer.Stop()
	out, err := decode(goja.Undefined(), msg)
	if err != nil {
		return res, scriptError(err)
	}
	res.Samples, err = normalize(rt, out, now)
	return res, err
}

func (d *Decoder) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

// runtime builds a fresh sandbox with the script loaded. Nothing outside the
// language itself is exposed: no require, no file system, no network, no
// timers. atob/btoa and the hex helpers are what vendor decoders use.
func (d *Decoder) runtime(logs *[]string) (*goja.Runtime, error) {
	rt := goja.New()
	rt.SetFieldNameMapper(goja.UncapFieldNameMapper())
	_ = rt.Set("atob", func(s string) (string, error) {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return "", fmt.Errorf("atob: not base64: %w", err)
		}
		// Like the browser: one character per byte, codes 0..255.
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r), nil
	})
	_ = rt.Set("btoa", func(s string) (string, error) {
		b := make([]byte, 0, len(s))
		for _, c := range s {
			if c > 255 {
				return "", errors.New("btoa: character outside Latin-1")
			}
			b = append(b, byte(c))
		}
		return base64.StdEncoding.EncodeToString(b), nil
	})
	_ = rt.Set("hexToBytes", func(s string) (goja.Value, error) {
		b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
		if err != nil {
			return nil, fmt.Errorf("hexToBytes: %w", err)
		}
		return rt.New(rt.Get("Uint8Array"), rt.ToValue(rt.NewArrayBuffer(b)))
	})
	console := rt.NewObject()
	logf := func(call goja.FunctionCall) goja.Value {
		if logs != nil && len(*logs) < maxLogLines {
			parts := make([]string, len(call.Arguments))
			for i, a := range call.Arguments {
				parts[i] = a.String()
			}
			*logs = append(*logs, strings.Join(parts, " "))
		}
		return goja.Undefined()
	}
	_ = console.Set("log", logf)
	_ = console.Set("warn", logf)
	_ = console.Set("error", logf)
	_ = rt.Set("console", console)

	timer := time.AfterFunc(d.timeout(), func() { rt.Interrupt("time limit exceeded while loading the script") })
	defer timer.Stop()
	if _, err := rt.RunProgram(d.prog); err != nil {
		return nil, scriptError(err)
	}
	return rt, nil
}

func scriptError(err error) error {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		return fmt.Errorf("decoder stopped: %v", ie.Value())
	}
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return fmt.Errorf("decoder error: %s", ex.Error())
	}
	return fmt.Errorf("decoder error: %w", err)
}

// normalize turns what decode() returned into samples, sorted by time.
func normalize(rt *goja.Runtime, v goja.Value, now time.Time) ([]Sample, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, nil // nothing in this message for us (e.g. an announce frame)
	}
	var items []interface{}
	switch x := v.Export().(type) {
	case []interface{}:
		items = x
	case map[string]interface{}:
		items = []interface{}{x}
	default:
		return nil, fmt.Errorf("decode() must return an array of {ts, values} or an object, not %T", x)
	}
	if len(items) > MaxSamples {
		return nil, fmt.Errorf("decode() returned %d samples, more than the %d allowed", len(items), MaxSamples)
	}
	out := make([]Sample, 0, len(items))
	for i, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("sample %d is not an object", i)
		}
		s := Sample{Time: now}
		_, hasValues := m["values"]
		if hasValues {
			vals, ok := m["values"].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("sample %d: values must be an object", i)
			}
			s.Values = vals
		} else {
			// A plain object of values; a "ts" key in it is the time.
			s.Values = make(map[string]interface{}, len(m))
			for k, val := range m {
				if k != "ts" {
					s.Values[k] = val
				}
			}
		}
		if ts, ok := m["ts"]; ok && ts != nil {
			t, err := toTime(ts)
			if err != nil {
				return nil, fmt.Errorf("sample %d: %w", i, err)
			}
			if t.After(now.Add(maxFutureSkew)) {
				return nil, fmt.Errorf("sample %d is stamped %s, more than a day in the future: check the device clock", i, t.UTC().Format(time.RFC3339))
			}
			s.Time = t
		}
		out = append(out, s)
	}
	// Oldest first: a backlog is written in the order it was taken, so the
	// last value applied is the newest.
	sort.SliceStable(out, func(a, b int) bool { return out[a].Time.Before(out[b].Time) })
	return out, nil
}

func toTime(v interface{}) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case int64:
		return fromEpoch(float64(t))
	case float64:
		return fromEpoch(t)
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if p, err := time.Parse(layout, t); err == nil {
				return p, nil
			}
		}
		return time.Time{}, fmt.Errorf("ts %q is not an ISO-8601 time", t)
	}
	return time.Time{}, fmt.Errorf("ts of type %T is not a time", v)
}

// fromEpoch reads seconds or milliseconds: no device time in seconds is above
// 1e11 (the year 5138), and none in milliseconds is below it (1973).
func fromEpoch(f float64) (time.Time, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return time.Time{}, fmt.Errorf("ts %v is not a valid time", f)
	}
	if f < 1e11 {
		f *= 1000
	}
	return time.UnixMilli(int64(f)), nil
}

// Lookup reads a dotted path ("in7.valore", "allarmi.0") from a sample's
// values. ok is false when the path is not there.
func Lookup(values map[string]interface{}, path string) (interface{}, bool) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "$.")
	if path == "" {
		return nil, false
	}
	if v, ok := values[path]; ok {
		return v, true // a key that itself contains dots
	}
	var cur interface{} = values
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]interface{}:
			next, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = next
		case []interface{}:
			var idx int
			if _, err := fmt.Sscanf(seg, "%d", &idx); err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}
