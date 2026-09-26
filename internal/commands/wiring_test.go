package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests read the source of the drivers and the publishers. The property
// they check — every command is stamped, every driver refuses a stale one —
// spans six services, and a copy that forgets it compiles, runs, and executes
// a four-hour-old setpoint without an error anywhere.

var writeDrivers = []string{"driver-s7", "driver-modbus", "driver-opcua", "driver-mqtt"}

var handlerBody = regexp.MustCompile(`(?s)\nfunc \(d \*Driver\) handleWriteCommand\(.*?\n\}\n`)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func TestEveryDriverRefusesAStaleCommandBeforeActingOnIt(t *testing.T) {
	for _, d := range writeDrivers {
		body := handlerBody.FindString(readFile(t, filepath.Join("..", "..", "services", d, "main.go")))
		if body == "" {
			t.Errorf("%s: handleWriteCommand not found; if it was renamed, update this test", d)
			continue
		}
		check := strings.Index(body, "commands.Fresh(")
		if check < 0 {
			t.Errorf("%s executes write commands without checking their age: one queued "+
				"across a dropped link is written to the PLC however old it is", d)
			continue
		}
		// The check must come before the command is handed on — to the write
		// queue or to a goroutine — or it checks nothing.
		for _, act := range []string{"<- cmd", "go d.executeWrite"} {
			if i := strings.Index(body, act); i >= 0 && i < check {
				t.Errorf("%s hands the command on (%q) before checking its age", d, act)
			}
		}
	}
}

func TestNoDriverKeepsItsOwnCopyOfTheCommand(t *testing.T) {
	for _, d := range writeDrivers {
		src := readFile(t, filepath.Join("..", "..", "services", d, "main.go"))
		if strings.Contains(src, "type WriteCommand struct") {
			t.Errorf("%s declares its own WriteCommand; a copy without IssuedAt reads every "+
				"command as unstamped and accepts it", d)
		}
	}
}

// Every file that publishes on cmd/write builds the command with
// NewWriteCommand, which stamps it. An anonymous struct literal next to the
// topic is how the stamp gets forgotten.
func TestEveryPublisherStampsItsCommands(t *testing.T) {
	files, _ := filepath.Glob("../../internal/handlers/*.go")
	files = append(files, "../../services/core-api/main.go")
	publishers := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src := readFile(t, f)
		if !strings.Contains(src, `"cmd/write/%d"`) {
			continue
		}
		publishers++
		if !strings.Contains(src, "NewWriteCommand(") {
			t.Errorf("%s publishes on cmd/write without NewWriteCommand: its commands carry "+
				"no time and never expire", filepath.Base(f))
		}
		if regexp.MustCompile(`:=\s*struct \{\s*\n\s*TagID\s+int`).MatchString(src) {
			t.Errorf("%s still builds a write command as an anonymous struct", filepath.Base(f))
		}
	}
	if publishers < 4 {
		t.Fatalf("found %d publishers of cmd/write, expected at least 4 (tags, recipes, i3x, "+
			"core-api); this test is no longer looking where the commands are sent", publishers)
	}
}

// The external write path used to publish on sys/command/write, which only the
// OPC UA driver listened to.
func TestTheExternalWritePathUsesTheTopicEveryDriverHears(t *testing.T) {
	src := readFile(t, "../../services/core-api/main.go")
	if strings.Contains(src, `Sprintf("sys/command/write/%d"`) {
		t.Fatal("core-api publishes writes on sys/command/write/{gateway}; S7, Modbus and MQTT " +
			"drivers do not listen there, and the write is lost without an error")
	}
}
