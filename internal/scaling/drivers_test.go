package scaling_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file asserts a property of the other services, by reading their source.
// That is unusual, and it is deliberate: the property is exactly the one that
// was broken, and nothing else could have caught it.
//
// The engineering-unit columns were added to the tags table, and the six driver
// binaries went on selecting the seven columns they had always selected. They
// compiled, they ran, they published — raw counts, on a product whose every
// screen showed engineering units. There is no unit test that fails, because
// each driver is internally consistent; there is no integration test that
// fails, because reproducing it needs a PLC and a scaled tag. What it produces
// is a plausible number in the wrong units.

func driverSources(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("../../services/driver-*/main.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no driver sources found: %v", err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		src := string(b)
		// Only processes that actually read a tag list from the database are
		// bound by these rules. driver-manager matches the directory pattern
		// but is the supervisor: it starts driver containers and never touches
		// a tag value.
		if !strings.Contains(src, "FROM tags") {
			continue
		}
		out[filepath.Base(filepath.Dir(p))] = src
	}
	if len(out) < 6 {
		t.Fatalf("expected at least 6 tag-reading drivers, found %d: %v",
			len(out), out)
	}
	return out
}

// Every driver must load a tag through the one shared column list. A driver
// that writes its own SELECT gets whichever columns existed the day it was
// written, and the fields it does not select read as their zero values —
// ScalingEnabled false, which is silently "no conversion".
func TestEveryDriverLoadsTagsThroughTheSharedColumnList(t *testing.T) {
	for name, src := range driverSources(t) {
		if !strings.Contains(src, "models.DriverTagColumns") {
			t.Errorf("%s builds its own tag SELECT instead of using "+
				"models.DriverTagColumns; columns added to the table will not "+
				"reach it and will read as zero values", name)
		}
		if !strings.Contains(src, "models.ScanDriverTag") {
			t.Errorf("%s scans tag rows by hand instead of using "+
				"models.ScanDriverTag", name)
		}
	}
}

// publishEntryPoints names, per driver, the function through which a tag value
// leaves the process. Each one must convert.
//
// Naming them is the point. The first version of this test only checked that
// "scaling.Apply" appeared somewhere in the file, and it passed with the
// conversion deleted from the publish path — because every driver also
// converts on the alarm path, and one mention satisfied it. A test that cannot
// fail is worse than no test: it reports the property is held.
var publishEntryPoints = map[string][]string{
	"driver-s7":      {"publishDual"},
	"driver-modbus":  {"publishDual"},
	"driver-opcua":   {"publishTagValue"},
	"driver-mqtt":    {"publishDual"},
	"driver-redis":   {"publishDual"},
	"driver-lorawan": {"publishFields"},
}

// functionBody returns the source of one method of *Driver.
func functionBody(src, name string) string {
	re := regexp.MustCompile(`(?s)\nfunc \(d \*Driver\) ` + name + `\(.*?\n\}\n`)
	return re.FindString(src)
}

func TestEveryPublishPathConvertsToEngineeringUnits(t *testing.T) {
	srcs := driverSources(t)
	for name, fns := range publishEntryPoints {
		src, ok := srcs[name]
		if !ok {
			t.Errorf("%s is named as having a publish path but was not found", name)
			continue
		}
		for _, fn := range fns {
			body := functionBody(src, fn)
			if body == "" {
				t.Errorf("%s.%s not found; if it was renamed, update this map — "+
					"the conversion it performs is not checked by anything else", name, fn)
				continue
			}
			if !strings.Contains(body, "scaling.Apply(") {
				t.Errorf("%s.%s publishes without converting to engineering units: "+
					"every scaled tag it sends is in raw counts, and the flag on "+
					"the payload will say otherwise", name, fn)
			}
		}
	}
}

// The catch-all for a publish path nobody remembered to name above: anything
// that builds the wire payload must either convert, or be handed the answer.
func TestAnyFunctionBuildingTheWirePayloadKnowsAboutTheConversion(t *testing.T) {
	fnDecl := regexp.MustCompile(`(?s)\nfunc [^\n]*\{.*?\n\}\n`)
	for name, src := range driverSources(t) {
		for _, body := range fnDecl.FindAllString(src, -1) {
			if !strings.Contains(body, "models.TagPayload{") {
				continue
			}
			if strings.Contains(body, "scaling.Apply(") || strings.Contains(body, "euScaled") {
				continue
			}
			head := strings.SplitN(strings.TrimLeft(body, "\n"), "{", 2)[0]
			t.Errorf("%s: %s builds a wire payload but neither converts nor is told "+
				"whether the value was converted", name, strings.TrimSpace(head))
		}
	}
}

var publishDualBody = regexp.MustCompile(`(?s)func \(d \*Driver\) publishDual\(.*?\n\}\n`)

// Once, not twice. Converting an already-converted value is the failure this
// change can introduce, and it is quieter than the one it fixes: full scale
// converted twice on a 0..27648 → 0..100 transmitter reads 0.36 bar, which is
// a number a plant could genuinely produce.
func TestPublishDualConvertsExactlyOnce(t *testing.T) {
	for name, src := range driverSources(t) {
		body := publishDualBody.FindString(src)
		if body == "" {
			continue // this driver publishes by another route
		}
		if n := strings.Count(body, "scaling.Apply("); n != 1 {
			t.Errorf("%s: publishDual applies the conversion %d times, want exactly 1", name, n)
		}
	}
}

// The flag on the payload is what lets a not-yet-updated core-api tell a
// converted value from a raw one during a rollout. A driver that converts and
// does not say so gets its values converted a second time downstream.
func TestADriverThatConvertsAlsoSetsTheFlag(t *testing.T) {
	for name, src := range driverSources(t) {
		if !strings.Contains(src, "scaling.Apply(") {
			continue
		}
		if !strings.Contains(src, "EUScaled") {
			t.Errorf("%s converts to engineering units but never sets EUScaled on "+
				"what it publishes; core-api will convert it again", name)
		}
	}
}

// And nobody may keep a private copy of the wire payload: a copy without the
// flag field does not fail to compile and does not fail to marshal — it just
// publishes a payload the receiver reads as unconverted.
func TestNoDriverDeclaresItsOwnWirePayload(t *testing.T) {
	for name, src := range driverSources(t) {
		if strings.Contains(src, "type TagPayload struct") {
			t.Errorf("%s declares its own TagPayload; use models.TagPayload so the "+
				"eu flag cannot be left out of it", name)
		}
	}
}
