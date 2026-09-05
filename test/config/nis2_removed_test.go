package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The NIS2 module was removed in 3.0.0. Nothing was stopping it from coming
// back.
//
// Removal is a one-time act; staying removed is not. Eight Dependabot pull
// requests were open against a master that predated the removal, and the
// question they raised — "can one of these put the compliance module back?" —
// had no answer in the repository. It had an answer in my head, from having
// read the diffs by hand that afternoon, which is exactly the kind of answer
// that expires.
//
// It cannot come back by accident from a dependency bump: those change
// manifests, not handlers. It can come back from a merge of a long-lived
// branch that forked before the removal — and three such branches exist, with
// 236, 253 and 259 commits each — or from a revert, or from someone restoring
// a file from history without knowing why it left.
//
// This test is the answer that does not expire. It fails if the routes, the
// files, or the background worker return.
//
// What it deliberately does NOT assert: that the string "NIS2" is absent from
// the codebase. It is still there, on purpose. /api/security/compliance
// survived the removal and reports a security-posture self-assessment whose
// checks are labelled NIS2, and the Terms of Service mention the directive in
// order to DISCLAIM providing compliance with it. Asserting on the word would
// fail on the disclaimer — the one mention we most want to keep.

// Files that were the NIS2 module. Restoring any of them is the module coming
// back, whatever the commit message says.
var nis2Files = []string{
	"internal/handlers/compliance.go",
	"internal/handlers/compliance_report.go",
	"internal/handlers/csirt.go",
	"internal/handlers/threat_monitor.go",
	"internal/handlers/vendor_risk.go",
	"internal/sync/ot_sync.go",
	"internal/sync/ot_sync_test.go",
	"services/web-ui/src/api/compliance.ts",
	"services/web-ui/src/pages/compliance/AssetDiscoveryPage.tsx",
	"services/web-ui/src/pages/compliance/CSIRTPage.tsx",
	"services/web-ui/src/pages/compliance/ComplianceReportsPage.tsx",
	"services/web-ui/src/pages/compliance/RiskPosturePage.tsx",
	"services/web-ui/src/pages/compliance/ThreatMonitorPage.tsx",
	"services/web-ui/src/pages/compliance/VendorRiskPage.tsx",
}

func TestTheNIS2ModuleStaysRemoved(t *testing.T) {
	root := repoRoot(t)

	// 1. No route under /api/compliance. This is the one that matters to a
	//    caller: thirty-eight endpoints lived there, and their return is what
	//    "the module is back" means from outside the process.
	routes := backendRoutes(t, root)
	if len(routes["GET"]) < 50 {
		t.Fatalf("only resolved %d GET routes from main.go — the parser is broken, "+
			"and a broken parser passes this test forever", len(routes["GET"]))
	}
	var back []string
	for method, paths := range routes {
		for _, p := range paths {
			if strings.HasPrefix(p, "/api/compliance") {
				back = append(back, method+" "+p)
			}
		}
	}
	if len(back) > 0 {
		sort.Strings(back)
		t.Errorf("the NIS2 compliance routes are registered again in core-api:\n  %s\n"+
			"They were removed in 3.0.0 and the Terms of Service now disclaim providing "+
			"NIS2 compliance. If this return is intentional, the ToS has to change with it.",
			strings.Join(back, "\n  "))
	}

	// 2. None of the module's files is back on disk.
	var restored []string
	for _, f := range nis2Files {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			restored = append(restored, f)
		}
	}
	if len(restored) > 0 {
		t.Errorf("%d file(s) of the removed NIS2 module are back:\n  %s",
			len(restored), strings.Join(restored, "\n  "))
	}

	// 3. The hourly per-organisation asset sync worker does not start.
	//
	//    This one is here because I claimed in conversation that the module had
	//    no background worker, and it did: otSync.StartAssetSyncWorker(database)
	//    ran every hour for every organisation. I had grepped for workers by a
	//    naming pattern this one does not match. A file check would not have
	//    caught the call either, had the file come back under another name.
	mainGo := filepath.Join(root, "services", "core-api", "main.go")
	body, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatalf("reading %s: %v", mainGo, err)
	}
	if strings.Contains(string(body), "StartAssetSyncWorker") {
		t.Errorf("core-api starts StartAssetSyncWorker again — the NIS2 OT asset sync " +
			"worker ran hourly for every organisation and was removed with the module")
	}
}
