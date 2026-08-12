// Package config checks the deployment configuration this repository ships.
//
// These are static checks — no database, no containers, no network — so they
// belong in the ordinary test run rather than in the acceptance suite. Every
// defect they cover was found late precisely because the only test that looked
// at an env template lived behind the e2e build tag and ran ten minutes after
// the change.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from this package to the repository root.
func repoRoot(t *testing.T) string {
	t.Helper()
	if r := os.Getenv("E2E_REPO_ROOT"); r != "" {
		return r
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// EVERY env template, not just the one that was broken first.
//
// .env.example was fixed for exactly this — an unquoted cron expression aborts
// backup.sh — and .env.cloud.example, which is what a VPS install copies, kept
// the same line unquoted for as long as the test only looked at one file. A
// test narrower than the defect class it describes is how the same bug ships
// twice.
func TestEnvTemplatesAreValidShell(t *testing.T) {
	root := repoRoot(t)

	templates, err := filepath.Glob(filepath.Join(root, ".env*.example"))
	if err != nil {
		t.Fatalf("globbing env templates: %v", err)
	}
	if len(templates) < 2 {
		t.Fatalf("expected at least .env.example and .env.cloud.example, found %v", templates)
	}

	for _, envFile := range templates {
		name := filepath.Base(envFile)
		out, err := exec.Command("sh", "-c",
			fmt.Sprintf("set -a; . %q; set +a", envFile)).CombinedOutput()
		if err != nil {
			t.Errorf("%s cannot be sourced by a shell: %v\n%s\n"+
				"scripts/backup.sh sources it under set -e, so this aborts the backup "+
				"before it dumps anything. Quote any value containing spaces.",
				name, err, out)
			continue
		}
		if len(out) > 0 {
			t.Errorf("%s produced output when sourced, which means the shell "+
				"tried to execute part of it:\n%s", name, out)
		}
	}
}

// A template that omits a secret is worse than one with a placeholder: the
// placeholder gets caught, the omission gets a default. ENCRYPTION_KEY defaults
// to empty, and core-api then stores every tenant's broker password in
// cleartext behind a single log line.
func TestCloudTemplateDeclaresEverySecret(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".env.cloud.example"))
	if err != nil {
		t.Fatalf("reading .env.cloud.example: %v", err)
	}
	body := string(raw)

	for _, key := range []string{
		"JWT_SECRET", "POSTGRES_PASSWORD", "MQTT_ADMIN_PASSWORD", "ENCRYPTION_KEY",
		"GRAFANA_ADMIN_PASSWORD", "OPENEDGE_INITIAL_ADMIN_PASSWORD",
		"PUBLIC_HOST", "ACME_EMAIL",
	} {
		if !strings.Contains(body, "\n"+key+"=") {
			t.Errorf("%s is not declared in .env.cloud.example — a VPS install would "+
				"take whatever the compose file defaults it to", key)
		}
	}
}

// The gate itself. It has one job: refuse a configuration that would come up
// insecure, and let a real one through.
func TestPreflightRefusesThePlaceholderTemplate(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "preflight.sh")

	out, err := exec.Command("bash", script, filepath.Join(root, ".env.cloud.example")).CombinedOutput()
	if err == nil {
		t.Fatalf("preflight accepted the unfilled template:\n%s", out)
	}
	// It has to name what is wrong, not merely refuse.
	for _, want := range []string{"ENCRYPTION_KEY", "GRAFANA_ADMIN_PASSWORD", "PUBLIC_HOST"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("preflight did not mention %s:\n%s", want, out)
		}
	}
}

func TestPreflightAcceptsAFilledConfiguration(t *testing.T) {
	root := repoRoot(t)

	filled := strings.Join([]string{
		"PUBLIC_HOST=app.example.com",
		"ACME_EMAIL=ops@example.com",
		"ALLOWED_ORIGINS=https://app.example.com",
		"JWT_SECRET=" + strings.Repeat("a", 64),
		"POSTGRES_PASSWORD=" + strings.Repeat("b", 32),
		"MQTT_ADMIN_PASSWORD=" + strings.Repeat("c", 32),
		"ENCRYPTION_KEY=" + strings.Repeat("d", 32),
		"GRAFANA_ADMIN_PASSWORD=" + strings.Repeat("e", 24),
		"OPENEDGE_INITIAL_ADMIN_PASSWORD=" + strings.Repeat("f", 20),
		"SWAGGER_ENABLED=false",
		`BACKUP_SCHEDULE="0 3 * * *"`,
		"",
	}, "\n")

	path := filepath.Join(t.TempDir(), "prod.env")
	if err := os.WriteFile(path, []byte(filled), 0o600); err != nil {
		t.Fatalf("writing env: %v", err)
	}

	out, err := exec.Command("bash", filepath.Join(root, "scripts", "preflight.sh"), path).CombinedOutput()
	if err != nil {
		t.Fatalf("preflight refused a correctly filled configuration:\n%s", out)
	}
}

// Each class on its own, so a future change cannot quietly drop one check while
// the others keep the test green.
func TestPreflightCatchesEachDefectClass(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "preflight.sh")

	base := map[string]string{
		"PUBLIC_HOST": "app.example.com", "ACME_EMAIL": "ops@example.com",
		"ALLOWED_ORIGINS": "https://app.example.com",
		"JWT_SECRET":      strings.Repeat("a", 64), "POSTGRES_PASSWORD": strings.Repeat("b", 32),
		"MQTT_ADMIN_PASSWORD": strings.Repeat("c", 32), "ENCRYPTION_KEY": strings.Repeat("d", 32),
		"GRAFANA_ADMIN_PASSWORD": strings.Repeat("e", 24),
		"OPENEDGE_INITIAL_ADMIN_PASSWORD": strings.Repeat("f", 20),
		"SWAGGER_ENABLED": "false", "BACKUP_SCHEDULE": `"0 3 * * *"`,
	}

	cases := []struct {
		name       string
		key, value string
		wantIn     string
	}{
		{"an encryption key of the wrong length", "ENCRYPTION_KEY", strings.Repeat("d", 16), "EXACTLY 32"},
		{"a short JWT secret", "JWT_SECRET", strings.Repeat("a", 20), "at least 32"},
		{"an unquoted cron expression", "BACKUP_SCHEDULE", "0 3 * * *", "BACKUP_SCHEDULE"},
		{"the example domain", "PUBLIC_HOST", "app.yourcompany.com", "PUBLIC_HOST"},
		{"a placeholder secret", "GRAFANA_ADMIN_PASSWORD", "CHANGE_ME_STRONG", "GRAFANA_ADMIN_PASSWORD"},
		{"a short admin password", "OPENEDGE_INITIAL_ADMIN_PASSWORD", "short", "global"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for k, v := range base {
				if k == tc.key {
					continue
				}
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
			fmt.Fprintf(&b, "%s=%s\n", tc.key, tc.value)

			path := filepath.Join(t.TempDir(), "broken.env")
			if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
				t.Fatalf("writing env: %v", err)
			}

			out, err := exec.Command("bash", script, path).CombinedOutput()
			if err == nil {
				t.Fatalf("preflight accepted %s:\n%s", tc.name, out)
			}
			if !strings.Contains(string(out), tc.wantIn) {
				t.Fatalf("the refusal does not explain %s (expected %q):\n%s", tc.name, tc.wantIn, out)
			}
		})
	}
}
