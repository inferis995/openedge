package handlers

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// An edge box builds nothing: every image it runs is pulled from the registry.
// For two releases the box asked for ghcr.io/inferis995/openedge/driver-manager
// while the release published ghcr.io/inferis995/openedge-driver-manager, and
// four of the six drivers were not published at all. No test ran the
// installer against the registry, and a server built with `make start` compiles
// its images locally, so nothing noticed. These tests read build.yml, the
// installer and driver-manager side by side, because the property only exists
// between them.

func source(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// publishedServices returns the services build.yml pushes to the registry.
func publishedServices(t *testing.T) map[string]string {
	t.Helper()
	build := source(t, "../../.github/workflows/build.yml")
	out := map[string]string{}
	re := regexp.MustCompile(`- service: ([a-z0-9-]+)\n\s+(?:dockerfile|path): (\S+)`)
	for _, m := range re.FindAllStringSubmatch(build, -1) {
		out[m[1]] = m[2]
	}
	if len(out) == 0 {
		t.Fatal("no services found in build.yml; this test no longer reads it correctly")
	}
	return out
}

func TestEveryDriverABoxCanStartIsPublished(t *testing.T) {
	published := publishedServices(t)
	manager := source(t, "../../services/driver-manager/main.go")
	block := regexp.MustCompile(`(?s)var driverServices = map\[string\]string\{(.*?)\n\}`).FindStringSubmatch(manager)
	if block == nil {
		t.Fatal("driverServices not found in driver-manager; if it moved, update this test")
	}
	services := regexp.MustCompile(`"(driver-[a-z0-9]+)"`).FindAllStringSubmatch(block[1], -1)
	if len(services) < 6 {
		t.Fatalf("found %d driver services in driver-manager, expected at least 6", len(services))
	}
	for _, s := range append(services, []string{"", "driver-manager"}) {
		if _, ok := published[s[1]]; !ok {
			t.Errorf("%s is not published by build.yml: a box asked to run it fails to pull, "+
				"and the only symptom is on the box", s[1])
		}
	}
}

func TestEveryPublishedServiceHasTheDockerfileItNames(t *testing.T) {
	for service, path := range publishedServices(t) {
		dockerfile := path
		if !strings.HasSuffix(dockerfile, "Dockerfile") {
			dockerfile += "/Dockerfile"
		}
		if _, err := os.Stat("../../" + dockerfile); err != nil {
			// build.yml skips a service whose Dockerfile is missing, silently.
			t.Errorf("%s: %s does not exist; the release would skip it without failing", service, dockerfile)
		}
	}
}

// The name the box pulls must be the name the release pushes:
// ${REGISTRY}/${IMAGE_PREFIX}-${service}.
func TestTheBoxPullsFromWhereTheReleasePublishes(t *testing.T) {
	build := source(t, "../../.github/workflows/build.yml")
	for _, want := range []string{
		"REGISTRY: ghcr.io",
		"IMAGE_PREFIX: ${{ github.repository_owner }}/openedge",
		"images: ${{ env.REGISTRY }}/${{ env.IMAGE_PREFIX }}-${{ matrix.service }}",
	} {
		if !strings.Contains(build, want) {
			t.Fatalf("build.yml no longer has %q; the published image names changed and the "+
				"installer default (%s) must follow", want, defaultEdgeImageRegistry)
		}
	}
	if !regexp.MustCompile(`^ghcr\.io/[a-z0-9-]+/openedge-$`).MatchString(defaultEdgeImageRegistry) {
		t.Fatalf("defaultEdgeImageRegistry %q is not ghcr.io/<owner>/openedge- — the shape "+
			"build.yml publishes", defaultEdgeImageRegistry)
	}

	compose := pkg(t)["docker-compose.yml"]
	want := "image: " + defaultEdgeImageRegistry + "driver-manager:3.2.0"
	if !strings.Contains(compose, want) {
		t.Errorf("the box's compose does not contain %q", want)
	}
	if strings.Contains(compose, "openedge/driver-manager") {
		t.Error("the box's compose still names openedge/driver-manager, which does not exist")
	}
}

// OTA updates are accepted only below an allowed prefix. It has to cover the
// images the release publishes, or every update of a box is refused.
func TestTheOTAPrefixCoversThePublishedImages(t *testing.T) {
	manager := source(t, "../../services/driver-manager/main.go")
	m := regexp.MustCompile(`getEnv\("OTA_IMAGE_PREFIX", "([^"]+)"\)`).FindStringSubmatch(manager)
	if m == nil {
		t.Fatal("OTA_IMAGE_PREFIX default not found")
	}
	if !strings.HasPrefix(defaultEdgeImageRegistry, m[1]) {
		t.Errorf("OTA prefix %q does not cover %q; updates to published images would be refused",
			m[1], defaultEdgeImageRegistry)
	}
}

func TestABoxIsPinnedToTheVersionThatGeneratedIt(t *testing.T) {
	cases := []struct{ version, override, want string }{
		{"3.2.0", "", "3.2.0"},
		{"v3.2.0", "", "3.2.0"}, // the git tag, as a local build might pass it
		{"", "", "latest"},      // built from source
		{"latest", "", "latest"},
		{"3.2.0", "3.1.9", "3.1.9"}, // an explicit override wins
	}
	for _, c := range cases {
		t.Setenv("OPENEDGE_VERSION", c.version)
		t.Setenv("EDGE_IMAGE_TAG", c.override)
		if got := edgeImageTag(); got != c.want {
			t.Errorf("version %q override %q: tag %q, want %q", c.version, c.override, got, c.want)
		}
	}
}
