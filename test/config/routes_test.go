package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every path the browser calls must be a path the server answers.
//
// The trend page called POST /api/history/batch for as long as it existed and
// nothing ever served it: selecting a tag produced a 404 and an empty chart.
// Nothing failed loudly, because a chart with no data looks exactly like a
// plant with no data. tagsApi carried a client for POST /api/tags/batch-current
// on the same terms.
//
// Neither could be caught by a unit test on either side — each half was
// internally consistent — and the acceptance suite only exercises the paths
// somebody thought to write a test for. This one derives the list from the
// frontend itself, so a call added tomorrow is checked tomorrow.

var (
	// Matches api.get('/x'), api.post(`/x/${id}`), api.delete("/x").
	callRe = regexp.MustCompile(
		"\\bapi\\.(get|post|put|patch|delete)\\s*\\(\\s*[`'\"]([^`'\"]+)[`'\"]")
	// Matches someGroup := parent.Group("/prefix").
	groupRe = regexp.MustCompile(`(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"`)
	// Matches group.GET("/path", handler).
	routeRe = regexp.MustCompile(`(\w+)\.(GET|POST|PUT|PATCH|DELETE|Any)\(\s*"([^"]*)"`)
	// Matches ${...} in a template literal, and :id / *filepath in a Gin route.
	interpRe = regexp.MustCompile(`\$\{[^}]*\}`)
	paramRe  = regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_]*`)
)

type call struct {
	method, path, file string
}

// frontendCalls collects every API path the web UI asks for.
func frontendCalls(t *testing.T, root string) []call {
	t.Helper()
	var calls []call
	seen := map[string]bool{}

	src := filepath.Join(root, "services", "web-ui", "src")
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range callRe.FindAllStringSubmatch(string(body), -1) {
			p := interpRe.ReplaceAllString(m[2], ":p")
			if i := strings.Index(p, "?"); i >= 0 {
				p = p[:i]
			}
			if !strings.HasPrefix(p, "/") {
				continue // a computed path; nothing to compare against
			}
			rel, _ := filepath.Rel(root, path)
			key := m[1] + " " + p
			if seen[key] {
				continue
			}
			seen[key] = true
			calls = append(calls, call{strings.ToUpper(m[1]), "/api" + p, rel})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the web UI sources: %v", err)
	}
	return calls
}

// backendRoutes resolves the routes main.go registers, following group nesting.
func backendRoutes(t *testing.T, root string) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "services", "core-api", "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	body := string(raw)

	prefix := map[string]string{"router": "", "r": ""}
	// Groups are declared before they are used, and a group may nest in
	// another; two passes settle the chain without needing to model scope.
	for range 3 {
		for _, m := range groupRe.FindAllStringSubmatch(body, -1) {
			if p, ok := prefix[m[2]]; ok {
				prefix[m[1]] = p + m[3]
			}
		}
	}

	routes := map[string][]string{}
	for _, m := range routeRe.FindAllStringSubmatch(body, -1) {
		base, ok := prefix[m[1]]
		if !ok {
			continue // not a router group we resolved: a helper, or a mock
		}
		full := base + m[3]
		if full == "" {
			full = "/"
		}
		method := strings.ToUpper(m[2])
		routes[method] = append(routes[method], full)
		if method == "ANY" {
			for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
				routes[verb] = append(routes[verb], full)
			}
		}
	}
	return routes
}

// matches reports whether a concrete path is served by a registered route,
// treating :params and *wildcards the way Gin does.
func matches(path string, route string) bool {
	got := strings.Split(strings.Trim(path, "/"), "/")
	want := strings.Split(strings.Trim(route, "/"), "/")

	if n := len(want); n > 0 && strings.HasPrefix(want[n-1], "*") {
		if len(got) < n-1 {
			return false
		}
		want, got = want[:n-1], got[:n-1]
	} else if len(got) != len(want) {
		return false
	}

	for i := range want {
		if want[i] == got[i] || paramRe.MatchString(want[i]) || got[i] == ":p" {
			continue
		}
		return false
	}
	return true
}

func TestEveryFrontendCallHasARoute(t *testing.T) {
	root := repoRoot(t)

	calls := frontendCalls(t, root)
	if len(calls) < 50 {
		t.Fatalf("only found %d API calls in the web UI — the extractor is broken, "+
			"and a broken extractor passes this test forever", len(calls))
	}

	routes := backendRoutes(t, root)
	if len(routes["GET"]) < 50 {
		t.Fatalf("only resolved %d GET routes from main.go — the parser is broken", len(routes["GET"]))
	}

	var orphans []string
	for _, c := range calls {
		found := false
		for _, r := range routes[c.method] {
			if matches(c.path, r) {
				found = true
				break
			}
		}
		if !found {
			orphans = append(orphans, c.method+" "+c.path+"   ("+c.file+")")
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("%d call(s) from the web UI reach no route in core-api — each one is a 404 "+
			"the user sees as a page that does not work:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

// The CLI calls the same API, and nothing was checking it.
//
// TestEveryFrontendCallHasARoute above scans services/web-ui only. The
// command-line client — which is also the MCP server the AI tooling drives —
// speaks to the identical backend and had no such check at all. That is the
// exact gap that produced the defect this file exists for: POST
// /api/history/batch was called for as long as it existed and served by
// nothing, and a chart with no data looks like a plant with no data.
//
// WHAT THIS CATCHES, AND WHAT IT CANNOT — both measured, not assumed.
//
// It catches a path whose SHAPE the server does not serve at all:
// "/api/compliance/assets" fails here today, which is the check earning its
// keep, because those endpoints existed until the NIS2 module was removed and
// nothing else would have noticed a CLI command left pointing at one.
//
// It does NOT catch a typo inside a segment a parameter route accepts.
// "/api/tags/shadows-does-not-exist" passes, because "/api/tags/:id" is
// registered and :id absorbs any single segment. That is not a gap in the
// implementation, it is inherent: "/api/tags/123" and "/api/tags/nonsense" are
// the same path to a router, and no reading of the source can separate them.
//
// It also says nothing about the HTTP METHOD. The CLI builds most paths into a
// variable before passing them to client.Get/Post/Put, so the verb cannot be
// tied to the path by reading the source; a path served only under the wrong
// verb slips through.
//
// Narrow, then — and still the check that would have caught the defect this
// file was written for.
func cliCalls(t *testing.T, root string) []call {
	t.Helper()

	// Literals and format strings alike: fmt.Sprintf("/api/tags/%d", id).
	pathRe := regexp.MustCompile(`"(/api/[^"]*)"`)
	verbRe := regexp.MustCompile(`%[sdvq]`)

	var calls []call
	seen := map[string]bool{}

	cli := filepath.Join(root, "services", "cli")
	err := filepath.Walk(cli, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		// Test files describe what the server should do, not what the CLI calls.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range pathRe.FindAllStringSubmatch(string(body), -1) {
			p := verbRe.ReplaceAllString(m[1], ":p")
			if i := strings.Index(p, "?"); i >= 0 {
				p = p[:i]
			}
			// A path ending in "/" or "-" is a concatenation prefix —
			// "/api/tags/" + id — so the id is the segment that follows.
			if strings.HasSuffix(p, "/") || strings.HasSuffix(p, "-") {
				p += ":p"
			}
			rel, _ := filepath.Rel(root, path)
			if seen[p] {
				continue
			}
			seen[p] = true
			calls = append(calls, call{"ANY", p, rel})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the CLI sources: %v", err)
	}
	return calls
}

func TestEveryCLICallHasARoute(t *testing.T) {
	root := repoRoot(t)

	calls := cliCalls(t, root)
	if len(calls) < 20 {
		t.Fatalf("only found %d API paths in the CLI — the extractor is broken, and a broken "+
			"extractor passes this test forever", len(calls))
	}

	routes := backendRoutes(t, root)
	var all []string
	for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		all = append(all, routes[verb]...)
	}
	if len(all) < 100 {
		t.Fatalf("only resolved %d routes from main.go — the parser is broken", len(all))
	}

	var orphans []string
	for _, c := range calls {
		found := false
		for _, r := range all {
			if matches(c.path, r) {
				found = true
				break
			}
		}
		if !found {
			orphans = append(orphans, c.path+"   ("+c.file+")")
		}
	}

	if len(orphans) > 0 {
		sort.Strings(orphans)
		t.Fatalf("%d path(s) the CLI calls reach no route in core-api — each one is a command "+
			"that fails, or an MCP tool that hands the model a 404:\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}
