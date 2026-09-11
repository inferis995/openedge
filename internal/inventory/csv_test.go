package inventory_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/inventory"
)

func aDevice() inventory.Device {
	seen := time.Date(2026, 9, 11, 6, 30, 0, 0, time.UTC)
	return inventory.Device{
		GatewayID: 1, Name: "PLC linea 1", Site: "Stabilimento Nord", Area: "Reparto A",
		OrgName: "Acme", Protocol: "MODBUS_TCP", Endpoint: "192.168.1.50:502, slave 3",
		ScanRateMs: 1000, Enabled: true, Health: "online", HealthSeenAt: &seen,
		Tags: 40, HistorizedTags: 35, AlarmedTags: 6, AgentVersion: "3.0.0",
		FirstSeen: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
}

func csvOf(t *testing.T, devices ...inventory.Device) string {
	t.Helper()
	var b bytes.Buffer
	if err := inventory.WriteCSV(&b, devices); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	return b.String()
}

// This file is opened in Excel on an Italian machine. Without a byte order mark
// every accented letter becomes mojibake, and "Città" in a document handed to a
// customer is the sort of detail that decides whether the rest is trusted.
func TestTheExportOpensCorrectlyInExcel(t *testing.T) {
	out := csvOf(t, aDevice())

	if !strings.HasPrefix(out, "\ufeff") {
		t.Error("the file has no UTF-8 byte order mark")
	}
	// A comma is the decimal separator in that locale, so a comma-separated
	// file lands in a single column.
	header := strings.Split(strings.TrimPrefix(out, "\ufeff"), "\n")[0]
	if !strings.Contains(header, ";") {
		t.Errorf("the file is not semicolon-separated: %q", header)
	}
}

func TestEveryDeviceBecomesARow(t *testing.T) {
	a, b := aDevice(), aDevice()
	b.GatewayID, b.Name = 2, "PLC linea 2"

	out := csvOf(t, a, b)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 { // header + two devices
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "PLC linea 1") || !strings.Contains(out, "PLC linea 2") {
		t.Errorf("a device is missing from the export:\n%s", out)
	}
}

// A gateway that has never been heard from must say so. An empty cell is read
// as "fine" by whoever is skimming the page.
func TestADeviceNeverHeardFromSaysSo(t *testing.T) {
	d := aDevice()
	d.Health = ""
	d.HealthSeenAt = nil

	out := csvOf(t, d)
	if !strings.Contains(out, "mai contattato") || !strings.Contains(out, "mai") {
		t.Errorf("a device never contacted produced:\n%s", out)
	}
}

// The header and the row are built next to each other so they cannot drift; if
// they ever do, every column after the drift is labeled wrong and the document
// is confidently incorrect.
func TestTheHeaderAndTheRowsHaveTheSameShape(t *testing.T) {
	out := strings.TrimPrefix(csvOf(t, aDevice()), "\ufeff")
	lines := strings.Split(strings.TrimSpace(out), "\n")

	header := strings.Split(lines[0], ";")
	row := strings.Split(lines[1], ";")
	if len(header) != len(row) {
		t.Fatalf("the header has %d columns and the row has %d", len(header), len(row))
	}
}

func TestAnEmptyInventoryIsStillAValidFile(t *testing.T) {
	out := csvOf(t)
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(out, "\ufeff")), "\n")
	if len(lines) != 1 {
		t.Fatalf("an empty inventory produced %d lines:\n%s", len(lines), out)
	}
}

// ---------------------------------------------------------------------------
// Summarize
// ---------------------------------------------------------------------------

// A disabled gateway is not a fault: nobody is asking it anything. Counting it
// among the offline is how a summary stops being read.
func TestADisabledGatewayIsNotCountedAsOffline(t *testing.T) {
	d := aDevice()
	d.Enabled = false
	d.Health = "offline"

	s := inventory.Summarize([]inventory.Device{d})
	if s.Offline != 0 {
		t.Errorf("offline = %d, want 0", s.Offline)
	}
	if s.Disabled != 1 {
		t.Errorf("disabled = %d, want 1", s.Disabled)
	}
}

func TestTheSummaryCountsWhatIsThere(t *testing.T) {
	online, down, erroring, never := aDevice(), aDevice(), aDevice(), aDevice()
	down.Health = "offline"
	erroring.Health = "error"
	never.Health = ""
	never.Protocol = "S7"

	s := inventory.Summarize([]inventory.Device{online, down, erroring, never})

	if s.Devices != 4 {
		t.Errorf("devices = %d, want 4", s.Devices)
	}
	if s.Online != 1 {
		t.Errorf("online = %d, want 1", s.Online)
	}
	// "error" is a driver that is running and cannot reach the PLC. For a
	// summary that is the same answer as offline: the data is not arriving.
	if s.Offline != 2 {
		t.Errorf("offline = %d, want 2 (offline + error)", s.Offline)
	}
	if s.Unknown != 1 {
		t.Errorf("unknown = %d, want 1", s.Unknown)
	}
	if s.Tags != 160 {
		t.Errorf("tags = %d, want 160", s.Tags)
	}
	if s.ByProtocol["MODBUS_TCP"] != 3 || s.ByProtocol["S7"] != 1 {
		t.Errorf("by protocol = %v", s.ByProtocol)
	}
}
