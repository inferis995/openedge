package handlers

import (
	"strings"
	"testing"
)

func TestOnlyTrendViewsAreSaved(t *testing.T) {
	ok := []string{
		"pens=7%3A3b82f6&range=24h",
		"pens=7%3A3b82f6%2C9%3Aef4444%3Ah&range=24h&agg=max&live=1&cmp=7d&y=bar%3Aeu",
		"pens=3%3A00ff00&from=2026-10-01T00%3A00%3A00.000Z&to=2026-10-02T00%3A00%3A00.000Z",
	}
	for _, q := range ok {
		if !validTrendViewQuery(q) {
			t.Errorf("refused a trend view: %s", q)
		}
	}
	bad := []string{
		"",
		"range=24h",                             // no pens
		"pens=1&range=24h&script=alert(1)",      // unknown parameter
		"pens=1&pens=2",                         // twice
		"pens=1&y=" + strings.Repeat("a", 5000), // too long
		"%zz",
	}
	for _, q := range bad {
		if validTrendViewQuery(q) {
			t.Errorf("accepted: %.60s", q)
		}
	}
}
