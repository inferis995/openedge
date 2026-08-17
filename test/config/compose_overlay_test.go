package config

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// What the cloud overlay actually publishes, as Compose resolves it.
//
// docker-compose.vps.yml said, in its own header, that it "removes all host
// port bindings from internal services" and that "only ports 80, 443 and 8883
// are exposed to the internet". It did this by writing `ports: []` under each
// service — which does nothing. Compose CONCATENATES ports across overlay
// files, so an empty list merges with the base one and leaves it whole.
//
// The result on a real VPS was 0.0.0.0:3000 — the web UI's nginx, and through
// it the entire API, over plaintext HTTP with none of the security headers
// Traefik adds — plus 0.0.0.0:18830 and 0.0.0.0:9001, unencrypted MQTT and
// MQTT-over-WebSocket, on a public address. Postgres, Redis and core-api
// escaped only because the BASE file binds them to 127.0.0.1 by default.
//
// Neither compose file is wrong when read on its own, which is why this could
// not be caught by reading them: the defect exists only in the merge. So the
// test asks Compose for the merge, the way `make vps-up` does.

type composePort struct {
	Mode      string `json:"mode"`
	HostIP    string `json:"host_ip"`
	Target    int    `json:"target"`
	Published string `json:"published"`
	Protocol  string `json:"protocol"`
}

type composeConfig struct {
	Services map[string]struct {
		Ports []composePort `json:"ports"`
	} `json:"services"`
}

// mergedCompose resolves the given overlay files into the configuration Compose
// would actually run.
func mergedCompose(t *testing.T, root string, files ...string) composeConfig {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH — this check reads `docker compose config`, no daemon needed")
	}

	// Every value the overlay marks required, so resolution reaches the ports
	// rather than stopping at a missing variable.
	envFile := filepath.Join(t.TempDir(), "resolve.env")
	if err := os.WriteFile(envFile, []byte(strings.Join([]string{
		"PUBLIC_HOST=app.example.com",
		"ACME_EMAIL=ops@example.com",
		"POSTGRES_PASSWORD=resolve-only",
		"MQTT_ADMIN_PASSWORD=resolve-only",
		"GRAFANA_ADMIN_PASSWORD=resolve-only",
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("writing the resolution env: %v", err)
	}

	args := []string{"compose", "--env-file", envFile}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, "config", "--format", "json")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = root
	// An inherited COMPOSE_FILE would silently replace the files under test.
	cmd.Env = append(os.Environ(), "COMPOSE_FILE=")

	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("docker compose config on %v: %v\n%s", files, err, stderr)
	}

	var cfg composeConfig
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("decoding the merged configuration: %v", err)
	}
	if len(cfg.Services) < 5 {
		t.Fatalf("only %d services resolved — the merge did not happen, and a merge that "+
			"did not happen passes every assertion below", len(cfg.Services))
	}
	return cfg
}

func TestCloudOverlayPublishesOnlyTheProxy(t *testing.T) {
	root := repoRoot(t)
	cfg := mergedCompose(t, root, "docker-compose.yml", "docker-compose.vps.yml")

	// The overlay's entire claim, as a list. 80 redirects to 443; 443 is the
	// web UI and the API behind it; 8883 is MQTT over TLS for edge gateways.
	allowed := map[string]string{
		"80":   "traefik",
		"443":  "traefik",
		"8883": "traefik",
	}

	var published []string
	for name, svc := range cfg.Services {
		for _, p := range svc.Ports {
			published = append(published, name+" "+p.HostIP+":"+p.Published)
			owner, ok := allowed[p.Published]
			if !ok {
				t.Errorf("%s publishes %s:%s to the host. This overlay is what puts the "+
					"application on the public internet: anything but 80, 443 and 8883 is "+
					"a service reachable without Traefik, without TLS and without the "+
					"security headers. `ports: []` does not remove a base binding — use "+
					"`ports: !override []`.", name, p.HostIP, p.Published)
				continue
			}
			if name != owner {
				t.Errorf("%s publishes %s, which belongs to %s", name, p.Published, owner)
			}
		}
	}

	sort.Strings(published)
	t.Logf("published by the cloud overlay: %s", strings.Join(published, ", "))

	// Traefik has to be there at all: an overlay that published nothing would
	// satisfy every assertion above and serve no one.
	if len(cfg.Services["traefik"].Ports) != 3 {
		t.Errorf("traefik publishes %d ports, want 3 (80, 443, 8883)",
			len(cfg.Services["traefik"].Ports))
	}
}

// The counterpart, so the test above cannot pass by the overlay being broken:
// on-prem is supposed to publish the web UI and the broker, because there is no
// proxy in front and the gateways are on the same LAN.
func TestOnPremStillPublishesWhatTheLANNeeds(t *testing.T) {
	root := repoRoot(t)
	cfg := mergedCompose(t, root, "docker-compose.yml")

	for _, want := range []struct{ service, port string }{
		{"web-ui", "3000"},
		{"mosquitto", "18830"},
		{"core-api", "8081"},
		{"postgres", "5432"},
	} {
		found := false
		for _, p := range cfg.Services[want.service].Ports {
			if p.Published == want.port {
				found = true
			}
		}
		if !found {
			t.Errorf("the on-prem stack no longer publishes %s on %s — nothing on the "+
				"machine can reach it", want.service, want.port)
		}
	}
}
