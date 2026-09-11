package handlers

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// pkg builds an installer package and returns its files by name.
func pkg(t *testing.T) map[string]string {
	t.Helper()

	buf, err := buildInstallerZIP(&installerData{
		OrgID: 7, OrgName: "Acme Manifattura", OrgSlug: "acme-manifattura",
		APIKey: "oe_abcd1234_secret", APIBaseURL: "https://openedge.example.com",
		MQTTUser: "org-7", MQTTPass: "broker-secret",
		CloudMQTTHost: "openedge.example.com", CloudMQTTPort: 8883,
		EdgeMQTTUser: "edge", EdgeMQTTPass: "local-secret",
		Generated: "2026-09-11T20:00:00Z",
	})
	if err != nil {
		t.Fatalf("building the installer: %v", err)
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the installer is not a readable ZIP: %v", err)
	}

	files := map[string]string{}
	for _, f := range r.File {
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatalf("opening %s: %v", f.Name, openErr)
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatalf("reading %s: %v", f.Name, readErr)
		}
		// The directory prefix carries the org slug and is noise here.
		name := f.Name
		if i := strings.Index(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		files[name] = string(content)
	}
	return files
}

// The defect this exists for. The installer wrote EDGE_API_URL and
// EDGE_API_KEY; driver-manager reads CORE_API_URL and CORE_API_TOKEN, and
// ORG_ID was never written at all. Every condition that switches on the box's
// contact with the platform was therefore false, and a freshly installed box
// sat there forever without ever appearing in the fleet.
//
// The names are read out of driver-manager's own source, so this cannot pass by
// agreeing with a list somebody typed twice.
func TestTheInstallerSetsEveryVariableTheAgentReads(t *testing.T) {
	source, err := os.ReadFile("../../services/driver-manager/main.go")
	if err != nil {
		t.Fatalf("reading driver-manager: %v", err)
	}
	sync, err := os.ReadFile("../../services/driver-manager/configsync.go")
	if err != nil {
		t.Fatalf("reading the config sync: %v", err)
	}

	// getEnv("NAME", ...) — the only way this agent reads its environment.
	re := regexp.MustCompile(`getEnv(?:Int)?\("([A-Z0-9_]+)"`)
	wanted := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(source)+string(sync), -1) {
		wanted[m[1]] = true
	}
	if len(wanted) == 0 {
		t.Fatal("no environment variables found in driver-manager; this test is not testing anything")
	}

	files := pkg(t)
	// Only lines that actually set something. A commented-out line still
	// contains "NAME=" and would let this test pass over a variable that is
	// documented and never set — which is the same outcome as not setting it.
	env := uncommented(files[".env"])
	compose := uncommented(files["docker-compose.yml"])

	// Variables the agent reads but that are legitimately left to their
	// defaults, or supplied by Docker rather than by the installer.
	optional := map[string]bool{
		"HEALTH_PORT": true, "LOG_FORMAT": true, "AGENT_VERSION": true,
		"DOCKER_HOST": true, "NETWORK_NAME": true, "POLL_INTERVAL": true,
		"MQTT_USERNAME": true, "MQTT_PASSWORD": true,
		"SPOOL_DIR": true, "SPOOL_MAX_BYTES": true,
		// Defaults to this deployment's own registry, which is where the
		// generated compose already points. Overriding it is for somebody
		// running their own mirror.
		"OTA_IMAGE_PREFIX": true,
	}

	for name := range wanted {
		if optional[name] {
			continue
		}
		if !definesVariable(env, compose, name) {
			t.Errorf("driver-manager reads %s, and nothing in the package gives it a value "+
				"— on a real box that variable is empty", name)
		}
	}
}

// Without a bridge the box collects data and keeps it. The generated broker had
// no bridge section at all, and the cloud credentials written into .env were
// consumed by nothing.
func TestTheBrokerForwardsToTheCentralPlatform(t *testing.T) {
	files := pkg(t)

	raw, ok := files["mosquitto/config/conf.d/bridge.conf"]
	if !ok {
		t.Fatal("the package contains no bridge configuration: data would never leave the box")
	}
	// Directives only. A commented-out "topic data/# out" still contains the
	// text, and asserting on the raw file let a bridge that forwards nothing
	// pass — the third time in this file that a comment looked like a setting.
	bridge := uncommented(raw)

	for _, want := range []string{
		"connection openedge-central",
		"address openedge.example.com:8883",
		"remote_username org-7",
		"topic data/# out",
		"topic sys/alarms/# out",
		"topic sys/health/# out",
		"topic sys/write/# in",
	} {
		if !strings.Contains(bridge, want) {
			t.Errorf("the bridge is missing %q", want)
		}
	}

	// The one line that makes an intermittent link work: with a clean session
	// the broker drops everything it could not deliver, and a plant whose
	// internet comes back after a weekend has lost the weekend.
	if !strings.Contains(bridge, "cleansession false") {
		t.Error("the bridge uses a clean session — messages published while the link is down " +
			"would be discarded rather than queued")
	}

	// And the main config has to actually read that directory.
	if !strings.Contains(uncommented(files["mosquitto/config/mosquitto.conf"]), "include_dir") {
		t.Error("mosquitto.conf does not include the directory the bridge lives in")
	}
}

// A broker on a factory network that accepts anyone can be read by anyone, and
// — where writes are enabled — used to send setpoints to the PLCs.
func TestTheBoxBrokerRefusesAnonymousConnections(t *testing.T) {
	files := pkg(t)
	conf := files["mosquitto/config/mosquitto.conf"]

	if strings.Contains(conf, "allow_anonymous true") {
		t.Fatal("the box's broker accepts anonymous connections")
	}
	if !strings.Contains(conf, "allow_anonymous false") || !strings.Contains(conf, "password_file") {
		t.Errorf("the broker does not require credentials:\n%s", conf)
	}
	if !strings.Contains(files[".env"], "EDGE_MQTT_PASS=") {
		t.Error("no password was generated for the box's own broker")
	}
	if !strings.Contains(files["install.sh"], "mosquitto_passwd") {
		t.Error("nothing in the installer turns that password into a password file")
	}
}

// The box runs its own Postgres, and the tables the drivers need exist only in
// the schema files. The edge package did not carry them, so the database came
// up empty and every driver failed its first query.
func TestTheInstallerShipsTheSchema(t *testing.T) {
	files := pkg(t)

	var shipped []string
	for name := range files {
		if strings.HasPrefix(name, "migrations/") && strings.HasSuffix(name, ".sql") {
			shipped = append(shipped, name)
		}
	}
	if len(shipped) == 0 {
		t.Fatal("the package ships no schema: the box's database would have no tables")
	}

	// The tables without which nothing works.
	var all strings.Builder
	for _, name := range shipped {
		all.WriteString(files[name])
	}
	for _, table := range []string{"organizations", "sites", "areas", "gateways", "tags", "alarm_definitions"} {
		if !strings.Contains(all.String(), "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("the shipped schema does not create %s", table)
		}
	}

	if !strings.Contains(files["docker-compose.yml"], "/docker-entrypoint-initdb.d") {
		t.Error("the schema is shipped but never mounted, so Postgres will not run it")
	}
}

// Credentials in the package are unavoidable — it is how a box authenticates —
// but they must be the ones for this organization and nothing else.
func TestThePackageCarriesOnlyThisOrganizationsCredentials(t *testing.T) {
	files := pkg(t)
	env := files[".env"]

	for _, want := range []string{
		"CORE_API_TOKEN=oe_abcd1234_secret",
		"ORG_ID=7",
		"CORE_API_URL=https://openedge.example.com",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("%q is missing from .env", want)
		}
	}

	// The README is what a customer reads; it must not repeat the secrets.
	if strings.Contains(files["README.md"], "oe_abcd1234_secret") ||
		strings.Contains(files["README.md"], "broker-secret") {
		t.Error("the README repeats a credential")
	}
}

// uncommented drops comment lines, so a documented-but-unset variable does not
// look like a set one.
func uncommented(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// definesVariable reports whether the package actually gives a variable a value.
//
// The first version of this test accepted the compose merely mentioning the
// name — but "CORE_API_URL: ${CORE_API_URL}" is a passthrough, and passing
// through a variable nobody defined yields an empty string. With that rule the
// test stayed green while .env still carried the old names, which is precisely
// the defect it exists to catch.
func definesVariable(env, compose, name string) bool {
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name+"=") {
			return true
		}
	}
	for _, line := range strings.Split(compose, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name+":") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, name+":"))
		// A bare passthrough defines nothing; a default or a literal does.
		if value != "${"+name+"}" && value != "" {
			return true
		}
	}
	return false
}
