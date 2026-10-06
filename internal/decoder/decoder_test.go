package decoder

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"
)

// Frames received from a MiDo logger in the field (serial 202400444).
const (
	midoUP = "DBBivAPxacp1pzaoAAVpyj8UCQAA4AAAAAAAP4AAAOEAAAAAAD+AAADiAAAAAAA/gAAAAwABxAAAAAAAxQAAAAAAxgAAAAAAxwAAAAAAyAAAAAAAacpCmAkAAOAAAAAAAD+AAADhAAAAAAA/gAAA4gAAAAAAP4AAAAMAAcQAAAAAAMUAAAAAAMYAAAAAAMcAAAAAAMgAAAAAAGnKRhwJAADgAAAAAAA/gAAA4QAAAAAAP4AAAOIAAAAAAD+AAAADAAHEAAAAAADFAAAAAADGAAAAAADHAAAAAADIAAAAAABpykmgCQAA4AAAAAAAP4AAAOEAAAAAAD+AAADiAAAAAAA/gAAAAwABxAAAAAAAxQAAAAAAxgAAAAAAxwAAAAAAyAAAAAAAacpNJAkAAOAAAAAAAD+AAADhAAAAAAA/gAAA4gAAAAAAP4AAAAMAAcQAAAAAAMUAAAAAAMYAAAAAAMcAAAAAAMgAAAAAAA=="
	midoAN = "DBBivAPyacp1pTaoADg2MzI2NjA1MzU1ODUxOQAAODkzOTAxMDAwMDI2ODU2NzgyMTUAAAANSAEAAE1pRG9fU1UxAAngAAEADwBJbmcxAAAAAOEAAQAPAEluZzIAAAAA4gABAA8ASW5nMwAAAAADAAQADwBTdGF0bwAAAMQAQAAPAFBvcnQxAAAAxQBAAA8AUG9ydDIAAADGAEAADwBQb3J0MwAAAMcKCAAPAEFOMQAAAAAAyAoIAA8AQU4yAAAAAAA="
)

var now = time.Date(2026, 3, 30, 13, 10, 0, 0, time.UTC)

func mido(t *testing.T) *Decoder {
	t.Helper()
	src, ok := PresetScript("mido")
	if !ok {
		t.Fatal("the mido preset does not ship")
	}
	d, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func num(t *testing.T, values map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := Lookup(values, key)
	if !ok {
		t.Fatalf("%s missing", key)
	}
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	t.Fatalf("not a number: %T %v", v, v)
	return 0
}

func TestMidoUPFrame(t *testing.T) {
	res, err := mido(t).Decode([]byte(midoUP), "mido/202400444/up", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != 6 {
		t.Fatalf("%d samples, want 6 (device + 5 readings)", len(res.Samples))
	}
	// Oldest first: the five readings 09:15 … 10:15 UTC, then the device
	// sample at the time the frame was sent.
	want := []string{"09:15", "09:30", "09:45", "10:00", "10:15", "13:07"}
	for i, s := range res.Samples {
		if got := s.Time.UTC().Format("15:04"); got != want[i] {
			t.Errorf("sample %d at %s, want %s", i, got, want[i])
		}
	}
	dev := res.Samples[5].Values
	if b := num(t, dev, "batteria"); b != 3.66 {
		t.Errorf("batteria %v, want 3.66", b)
	}
	if r := num(t, dev, "rssi"); r != -88 {
		t.Errorf("rssi %v, want -88", r)
	}
	if s := num(t, dev, "seriale"); s != 202400444 {
		t.Errorf("seriale %v", s)
	}
	if v, ok := Lookup(dev, "stato_bit.batteria_bassa"); !ok || v != false {
		t.Errorf("stato_bit.batteria_bassa = %v %v", v, ok)
	}
	r := res.Samples[0].Values
	if v := num(t, r, "in3"); v != 1 {
		t.Errorf("in3 (Stato) = %v, want 1", v)
	}
	if v := num(t, r, "in7"); v != 0 {
		t.Errorf("in7 (AN1) = %v, want 0", v)
	}
	if v, ok := Lookup(r, "in7_allarmi.sopra_soglia"); !ok || v != false {
		t.Errorf("in7_allarmi.sopra_soglia = %v %v", v, ok)
	}
}

func TestMidoANFrame(t *testing.T) {
	res, err := mido(t).Decode([]byte(midoAN), "mido/202400444/an", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Samples) != 1 {
		t.Fatalf("%d samples, want 1", len(res.Samples))
	}
	v := res.Samples[0].Values
	if imei, _ := Lookup(v, "imei"); imei != "863266053558519" {
		t.Errorf("imei %v", imei)
	}
	if lbl, _ := Lookup(v, "ingressi.7.etichetta"); lbl != "AN1" {
		t.Errorf("input 7 label %v, want AN1", lbl)
	}
	if k, _ := Lookup(v, "ingressi.7.chiave"); k != "in7" {
		t.Errorf("input 7 key %v, want in7", k)
	}
}

// Without the topic saying which frame it is, the structure decides.
func TestMidoFrameTypeFromStructure(t *testing.T) {
	d := mido(t)
	up, err := d.Decode([]byte(midoUP), "mido/202400444", now)
	if err != nil || len(up.Samples) != 6 {
		t.Fatalf("UP by structure: %d samples, err %v", len(up.Samples), err)
	}
	an, err := d.Decode([]byte(midoAN), "mido/202400444", now)
	if err != nil || len(an.Samples) != 1 {
		t.Fatalf("AN by structure: %d samples, err %v", len(an.Samples), err)
	}
}

// Every data type, with values that are not zero: the field frames above
// carry only zeros, which would not catch a wrong width or byte order.
func TestMidoEveryDataType(t *testing.T) {
	var b []byte
	be := binary.BigEndian
	u32 := func(v uint32) { b = be.AppendUint32(b, v) }
	b = be.AppendUint32(b, 202400444)
	b = append(b, 3, 241) // version, battery raw → 3.66 V
	u32(1774876071)
	b = append(b, 54, byte(0xA8), 0x40) // sqi, rssi -88, status: batteria_bassa
	b = append(b, 1)                    // one reading
	u32(1774862100)                     // 09:15 UTC
	b = append(b, 8)                    // eight inputs
	b = be.AppendUint16(b, 0x0102)      // modbus status
	in := func(id, tipo, alarm byte, v []byte) { b = append(b, tipo<<5|id, alarm); b = append(b, v...) }
	in(0, 0, 0, []byte{200})
	in(1, 1, 0, []byte{0xF6}) // -10
	in(2, 2, 0, be.AppendUint16(nil, 60000))
	in(3, 3, 0, be.AppendUint16(nil, uint16(0x10000-1234))) // -1234
	in(4, 4, 0, be.AppendUint32(nil, 4000000000))
	in(5, 5, 0, be.AppendUint32(nil, uint32(0x100000000-123456))) // -123456
	in(6, 6, 0x10, be.AppendUint32(nil, math.Float32bits(12.5)))  // sopra_soglia
	in(7, 7, 0, append(be.AppendUint32(nil, 1500), be.AppendUint32(nil, math.Float32bits(0.25))...))
	frame := base64.StdEncoding.EncodeToString(b)

	res, err := mido(t).Decode([]byte(frame), "x/up", now)
	if err != nil {
		t.Fatal(err)
	}
	r := res.Samples[0].Values
	want := map[string]float64{"in0": 200, "in1": -10, "in2": 60000, "in3": -1234,
		"in4": 4000000000, "in5": -123456, "in6": 12.5, "in7": 375, "stato_modbus": 0x0102}
	for k, w := range want {
		if got := num(t, r, k); got != w {
			t.Errorf("%s = %v, want %v", k, got, w)
		}
	}
	if v, _ := Lookup(r, "in6_allarmi.sopra_soglia"); v != true {
		t.Errorf("in6 sopra_soglia = %v", v)
	}
	if v, _ := Lookup(res.Samples[1].Values, "stato_bit.batteria_bassa"); v != true {
		t.Errorf("batteria_bassa = %v", v)
	}
}

// A truncated or padded frame is an error, not a set of wrong numbers.
func TestMidoDamagedFrame(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(midoUP)
	cut := base64.StdEncoding.EncodeToString(raw[:len(raw)-3])
	if _, err := mido(t).Decode([]byte(cut), "x/up", now); err == nil {
		t.Error("a truncated UP frame decoded without error")
	}
}

func TestGenericContract(t *testing.T) {
	cases := map[string]struct {
		src   string
		check func(t *testing.T, r Result, err error)
	}{
		"plain object at arrival time": {
			`function decode(m){ return {temp: 21.5, ok: true}; }`,
			func(t *testing.T, r Result, err error) {
				if err != nil || len(r.Samples) != 1 || !r.Samples[0].Time.Equal(now) {
					t.Fatalf("%v %+v", err, r)
				}
				if v := num(t, r.Samples[0].Values, "temp"); v != 21.5 {
					t.Errorf("temp %v", v)
				}
			}},
		"seconds and milliseconds": {
			`function decode(m){ return [{ts: 1774862100, values:{a:1}}, {ts: 1774862100000, values:{a:2}}]; }`,
			func(t *testing.T, r Result, err error) {
				if err != nil || !r.Samples[0].Time.Equal(r.Samples[1].Time) {
					t.Fatalf("%v %+v", err, r.Samples)
				}
			}},
		"far future refused": {
			`function decode(m){ return {ts: Date.now() + 864e5*30, values:{a:1}}; }`,
			func(t *testing.T, r Result, err error) {
				if err == nil {
					t.Fatal("a sample a month in the future was accepted")
				}
			}},
		"endless loop stopped": {
			`function decode(m){ for(;;){} }`,
			func(t *testing.T, r Result, err error) {
				if err == nil || !strings.Contains(err.Error(), "stopped") {
					t.Fatalf("err %v", err)
				}
			}},
		"script error reported": {
			`function decode(m){ throw new Error("bad frame"); }`,
			func(t *testing.T, r Result, err error) {
				if err == nil || !strings.Contains(err.Error(), "bad frame") {
					t.Fatalf("err %v", err)
				}
			}},
		"nothing returned is nothing": {
			`function decode(m){ console.log("skip", m.topic); }`,
			func(t *testing.T, r Result, err error) {
				if err != nil || len(r.Samples) != 0 || len(r.Logs) != 1 {
					t.Fatalf("%v %+v", err, r)
				}
			}},
		"bytes and hex helpers": {
			`function decode(m){ var h = hexToBytes("0102ff"); return {n: m.bytes.length, h: h[2]}; }`,
			func(t *testing.T, r Result, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if v := num(t, r.Samples[0].Values, "h"); v != 255 {
					t.Errorf("h %v", v)
				}
			}},
		"no host access": {
			`function decode(m){ return {r: typeof require, p: typeof process, f: typeof fetch}; }`,
			func(t *testing.T, r Result, err error) {
				for _, k := range []string{"r", "p", "f"} {
					if v, _ := Lookup(r.Samples[0].Values, k); v != "undefined" {
						t.Errorf("%s is %v inside the sandbox", k, v)
					}
				}
			}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := Compile(c.src)
			if err != nil {
				t.Fatal(err)
			}
			d.Timeout = 200 * time.Millisecond
			r, err := d.Decode([]byte("abc"), "t/x", now)
			c.check(t, r, err)
		})
	}
}

func TestCompileRefusesScriptsWithoutDecode(t *testing.T) {
	if _, err := Compile(`var x = 1;`); err == nil {
		t.Error("a script without decode() compiled")
	}
	if _, err := Compile(`function decode( {`); err == nil {
		t.Error("a syntax error compiled")
	}
}
