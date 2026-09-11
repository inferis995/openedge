//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// The service report aggregates a whole plant: how many devices, how long they
// were down, which tags alarmed. Like the inventory, a leak across tenants would
// not look like an authorization failure — it would look like somebody else's
// factory in the customer's report.
func TestTheServiceReportIsScopedToTheCallersOrganization(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	victim := createOrg(t, admin, "rep-victim-"+suffix)
	attackerOrg := createOrg(t, admin, "rep-attacker-"+suffix)

	seedInventoryGateway(t, db, victim.ID, "victim-plc-"+suffix)
	seedInventoryGateway(t, db, attackerOrg.ID, "own-plc-"+suffix)

	attacker := createOrgAdmin(t, admin, attackerOrg.ID,
		"rep-attacker-"+suffix, "e2e-Password-"+suffix)

	status, body := attacker.do("GET", "/api/reports/service-report", nil)
	if status != 200 {
		t.Fatalf("GET /api/reports/service-report returned %d: %s", status, truncate(body))
	}

	var report struct {
		Organization string `json:"organization"`
		Plant        struct {
			Devices int `json:"devices"`
		} `json:"plant"`
		Backups *struct{} `json:"backups"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("the report is not JSON: %v — %s", err, truncate(body))
	}

	if !strings.Contains(report.Organization, "rep-attacker-"+suffix) {
		t.Errorf("the report names %q, not the caller's own organization", report.Organization)
	}
	// The victim's gateway exists, so a report that counted every gateway on
	// the platform would say two.
	if report.Plant.Devices != 1 {
		t.Errorf("the report counts %d devices; the caller owns exactly one",
			report.Plant.Devices)
	}
	// backup_audit carries no organization, so a tenant must not be shown the
	// platform's backup log.
	if report.Backups != nil {
		t.Error("a tenant's report carries the platform-wide backup section")
	}
}

// The downloadable document is the deliverable. It has to render, carry the
// organization's name, and be a complete HTML file rather than a fragment.
func TestTheServiceReportDocumentRenders(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "repdoc-"+suffix)
	seedInventoryGateway(t, db, org.ID, "plc-"+suffix)

	member := createOrgAdmin(t, admin, org.ID, "repdoc-"+suffix, "e2e-Password-"+suffix)

	status, body := member.do("GET", "/api/reports/service-report.html?month=2026-08", nil)
	if status != 200 {
		t.Fatalf("downloading the report returned %d: %s", status, truncate(body))
	}
	doc := string(body)

	for _, want := range []string{
		"<!DOCTYPE html>",
		"Rapporto di servizio",
		"repdoc-" + suffix,
		"Continuità di servizio",
		"dal 01/08/2026 al 31/08/2026",
		"</html>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("%q is missing from the document", want)
		}
	}

	// Self-contained: it is saved, emailed and opened on a machine that has
	// never heard of OpenEdge, possibly with no network.
	for _, forbidden := range []string{"<script", "src=\"http", "href=\"http"} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("the document is not self-contained, it carries %q", forbidden)
		}
	}
}

// A month nobody has any data for is a legitimate thing to ask for, and the
// answer is a report saying nothing happened — not an error.
func TestAMonthWithNoDataStillProducesAReport(t *testing.T) {
	admin, _ := adminSession(t)

	status, body := admin.do("GET", "/api/reports/service-report.html?month=2019-01", nil)
	if status != 200 {
		t.Fatalf("a quiet month returned %d: %s", status, truncate(body))
	}
	if !strings.Contains(string(body), "Rapporto di servizio") {
		t.Error("no document was produced for a month with no data")
	}
}

func TestAMalformedMonthIsRefused(t *testing.T) {
	admin, _ := adminSession(t)

	status, body := admin.do("GET", "/api/reports/service-report?month=agosto", nil)
	if status != 400 {
		t.Errorf("a malformed month returned %d, want 400: %s", status, truncate(body))
	}
}
