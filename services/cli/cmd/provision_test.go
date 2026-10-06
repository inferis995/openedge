package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// seen records what the fake API received.
type seen struct {
	mu      sync.Mutex
	method  string
	path    string
	org     string
	ctype   string
	body    map[string]interface{}
	fields  map[string]string
	file    string
	fileNam string
}

func fakeAPI(t *testing.T, reply string) (*httptest.Server, *seen) {
	t.Helper()
	s := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.method, s.path, s.org, s.ctype = r.Method, r.URL.RequestURI(), r.Header.Get("X-Organization-ID"), r.Header.Get("Content-Type")
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			s.fields = map[string]string{}
			for k, v := range r.MultipartForm.Value {
				s.fields[k] = v[0]
			}
			if fh := r.MultipartForm.File["file"]; len(fh) == 1 {
				f, _ := fh[0].Open()
				b, _ := io.ReadAll(f)
				s.file, s.fileNam = string(b), fh[0].Filename
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&s.body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

func run(t *testing.T, args ...string) {
	t.Helper()
	// Flags are package globals: reset the ones a previous run may have set.
	flagJSON, tagsImportApply, flagOrg = false, false, 0
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
}

func TestTagsImportSendsTheSheetAsTheImporterReadsIt(t *testing.T) {
	srv, s := fakeAPI(t, `{"rows":[],"created":3,"updated":0,"unchanged":0,"invalid":0,"applied":true}`)
	sheet := filepath.Join(t.TempDir(), "cariati.csv")
	if err := os.WriteFile(sheet, []byte("Nome;Indirizzo;Tipo;Percorso JSON\nP1_AN1;t/1/up;REAL;in7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "tags", "import", "12", sheet, "--apply", "--url", srv.URL, "--token", "tok", "--org", "4")

	if s.method != "POST" || s.path != "/api/tags/import/file" || s.org != "4" {
		t.Errorf("sent %s %s org %q", s.method, s.path, s.org)
	}
	if s.fields["gateway_id"] != "12" || s.fields["apply"] != "true" {
		t.Errorf("form fields %v", s.fields)
	}
	if s.fileNam != "cariati.csv" || s.file == "" {
		t.Errorf("file %q (%d bytes)", s.fileNam, len(s.file))
	}
}

func TestGatewaysCreateCarriesTheConnectionConfigAsAnObject(t *testing.T) {
	srv, s := fakeAPI(t, `{"id":9,"name":"MiDo Cariati"}`)
	run(t, "gateways", "create", "--area", "3", "--name", "MiDo Cariati", "--driver", "mqtt",
		"--config", `{"broker_host":"b","broker_port":1883,"decoder_preset":"mido","alarms_on_history":true}`,
		"--url", srv.URL, "--token", "tok")
	cfg, _ := s.body["connection_config"].(map[string]interface{})
	if s.path != "/api/gateways" || s.body["driver_type"] != "MQTT" || s.body["area_id"] != float64(3) ||
		cfg["decoder_preset"] != "mido" || cfg["alarms_on_history"] != true || cfg["broker_port"] != float64(1883) {
		t.Errorf("sent %s %v", s.path, s.body)
	}
}

func TestSitesAndUsersAreCreatedInTheOrganizationGiven(t *testing.T) {
	srv, s := fakeAPI(t, `{"id":5,"name":"Sorical"}`)
	run(t, "sites", "create", "Sorical", "--url", srv.URL, "--token", "tok", "--org", "2")
	if s.body["org_id"] != float64(2) || s.body["name"] != "Sorical" || s.org != "2" {
		t.Errorf("site: %v (header %q)", s.body, s.org)
	}
	run(t, "users", "create", "--username", "mario", "--password", "a-long-password", "--url", srv.URL, "--token", "tok", "--org", "2")
	if s.body["org_id"] != float64(2) || s.body["role"] != "user" {
		t.Errorf("user: %v", s.body)
	}
}

func TestAPICommandPassesTheBodyThrough(t *testing.T) {
	srv, s := fakeAPI(t, `{"ok":true}`)
	run(t, "api", "put", "organizations/3/mfa-required", `{"mfa_required":true}`, "--url", srv.URL, "--token", "tok")
	if s.method != "PUT" || s.path != "/organizations/3/mfa-required" || s.body["mfa_required"] != true || s.ctype != "application/json" {
		t.Errorf("sent %s %s %v %q", s.method, s.path, s.body, s.ctype)
	}
}
