package servicereport_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/servicereport"
)

func aReport() *servicereport.Report {
	last := time.Date(2026, 8, 31, 3, 0, 0, 0, time.UTC)
	return &servicereport.Report{
		Organization: "Acme Manifattura",
		From:         from,
		To:           to,
		GeneratedAt:  time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		Plant: servicereport.Plant{
			Devices: 12, Online: 10, Offline: 1, Unknown: 0, Disabled: 1, Tags: 480,
			ByProtocol: map[string]int{"MODBUS_TCP": 8, "S7": 4},
		},
		Service: servicereport.Service{
			FleetAvailability: 0.9987, Interruptions: 3, Downtime: 92 * time.Minute,
			Gateways: []servicereport.GatewayUptime{
				{GatewayID: 1, GatewayName: "PLC linea 1", Interruptions: 3,
					Downtime: 92 * time.Minute, Availability: 0.9979},
			},
		},
		Alarms: servicereport.Alarms{
			Total: 57, BySeverity: map[string]int{"warning": 50, "critical": 7},
			StillOpen: 2, Acknowledged: 41, MeanAck: 14 * time.Minute,
			TopTags: []servicereport.TagAlarms{{Alias: "temperatura_forno", Count: 19}},
		},
		History: servicereport.History{Samples: 4_182_330, Tags: 455},
		Backups: &servicereport.Backups{Taken: 31, Failed: 0, LastTaken: &last},
	}
}

func render(t *testing.T, r *servicereport.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := servicereport.Render(&b, r); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b.String()
}

func TestTheDocumentCarriesTheNumbers(t *testing.T) {
	out := render(t, aReport())

	for _, want := range []string{
		"Acme Manifattura",
		"99.87%",            // fleet availability
		"1 h 32 min",        // downtime
		"temperatura_forno", // noisiest tag
		"4182330",           // samples
		"14 min",            // mean acknowledgement
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from the document", want)
		}
	}
}

// The range is half-open — 1 August to 1 September excluded — and a heading that
// prints both ends raw reads as if a day were counted twice.
func TestThePeriodReadsAsAMonthNotAsAMonthAndADay(t *testing.T) {
	out := render(t, aReport())

	if !strings.Contains(out, "dal 01/08/2026 al 31/08/2026") {
		t.Errorf("the period heading is wrong; looked for 01/08 to 31/08 in:\n%s",
			firstLines(out, 40))
	}
}

// Every name in this document is typed by a user, and the document is opened in
// a browser. text/template instead of html/template is one import's difference
// and the whole difference between an escaped document and an injected one.
func TestNamesAreEscaped(t *testing.T) {
	r := aReport()
	r.Organization = `<script>alert('xss')</script>`
	r.Alarms.TopTags = []servicereport.TagAlarms{{Alias: `"><img src=x onerror=alert(1)>`, Count: 1}}
	r.Service.Gateways[0].GatewayName = `<b>grassetto</b>`

	out := render(t, r)

	for _, forbidden := range []string{
		"<script>alert",
		"<img src=x",
		"<b>grassetto</b>",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("unescaped user input reached the document: %q", forbidden)
		}
	}
	// And the text is still there, escaped, rather than silently dropped.
	if !strings.Contains(out, "alert") {
		t.Error("the name was dropped instead of escaped")
	}
}

// A tenant's report carries no backup section, because backup_audit has no
// organization and showing one tenant the platform's backup log would be
// showing them somebody else's.
func TestATenantReportHasNoBackupSection(t *testing.T) {
	r := aReport()
	r.Backups = nil

	out := render(t, r)
	if strings.Contains(out, "Copie di sicurezza") {
		t.Error("the backup section appeared in a report that carries no backup data")
	}
}

// A month in which nothing happened is a legitimate month, and the document has
// to say so rather than fail or print blanks.
func TestAQuietMonthStillProducesADocument(t *testing.T) {
	r := &servicereport.Report{
		Organization: "Acme", From: from, To: to,
		GeneratedAt: time.Now().UTC(),
		Service:     servicereport.Service{FleetAvailability: 1},
	}

	out := render(t, r)
	if !strings.Contains(out, "100.00%") {
		t.Errorf("a month with no interruptions did not report full availability:\n%s",
			firstLines(out, 60))
	}
	if !strings.Contains(out, "Rapporto di servizio") {
		t.Error("the document has no heading")
	}
}

// Zero is not "—" everywhere: a duration of zero reads better as a dash, but a
// count of zero has to be the digit, or a reader cannot tell "none" from "not
// measured".
func TestAZeroDurationReadsAsADashAndAZeroCountAsZero(t *testing.T) {
	r := aReport()
	r.Service.Downtime = 0
	r.Alarms.Total = 0
	r.Alarms.MeanAck = 0

	out := render(t, r)
	if !strings.Contains(out, `<div class="k">Totali</div><div class="v">0</div>`) {
		t.Error("a count of zero was not printed as 0")
	}
	if !strings.Contains(out, `<div class="k">Fermo totale</div><div class="v">—</div>`) {
		t.Error("a duration of zero was not printed as a dash")
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
