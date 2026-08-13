//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// The trend chart's own endpoint.
//
// TrendPage has always called POST /api/history/batch, and until now nothing
// answered it: every attempt to chart a tag returned 404 and left an empty
// graph. It never looked like a failure, because a chart with no data is
// indistinguishable from a plant with no data — which is why it survived so
// long and why the check belongs here, against the assembled stack, rather
// than in a unit test on either side of the gap.

type batchSeries map[string][]struct {
	Timestamp int64    `json:"timestamp"`
	Value     *float64 `json:"value"`
	Quality   int      `json:"quality"`
}

func batchQuery(t *testing.T, c *apiClient, body interface{}) (int, []byte) {
	t.Helper()
	return c.do(http.MethodPost, "/api/history/batch", body)
}

// The path the page actually walks: publish a value, then ask for it the way
// the chart does.
func TestTrendBatchReturnsTheSeries(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	mqtt := mqttConnect(t, "e2e-trend-"+uniqueSuffix())
	publish(t, mqtt, fx.dataTopic, map[string]interface{}{
		"value": 42.5, "quality": 0, "timestamp": time.Now().UnixMilli(),
	})
	// Give the historian time to persist it.
	time.Sleep(3 * time.Second)

	status, raw := batchQuery(t, org, map[string]interface{}{
		"tag_ids": []int{fx.tagID},
		"start":   time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"end":     time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		"agg":     "avg",
	})
	if status != http.StatusOK {
		t.Fatalf("the endpoint the trend page calls answered %d — %s", status, truncate(raw))
	}

	var series batchSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		t.Fatalf("decode batch response: %v — %s", err, truncate(raw))
	}
	points, ok := series[strconv.Itoa(fx.tagID)]
	if !ok {
		t.Fatalf("no entry for tag %d; the chart indexes by tag id — got %s", fx.tagID, truncate(raw))
	}
	if len(points) == 0 {
		t.Fatal("the series came back empty for a tag that has a value")
	}
	found := false
	for _, p := range points {
		if p.Value != nil && *p.Value > 42 && *p.Value < 43 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the published value is not in the series: %s", truncate(raw))
	}
}

// A batch is not a reason to trust a list of ids. One tag from another tenant
// among your own must be refused like any other.
func TestTrendBatchRefusesAnotherTenantsTag(t *testing.T) {
	admin, _ := adminSession(t)
	mine := newFixture(t, admin)
	theirs := newFixture(t, admin)

	orgAdmin := createOrgAdmin(t, admin, mine.org.ID,
		"trend-"+uniqueSuffix(), "trend-password-1234")
	orgAdmin.orgID = fmt.Sprintf("%d", mine.org.ID)

	// The org admin can read their own tag.
	if status, raw := batchQuery(t, orgAdmin, map[string]interface{}{
		"tag_ids": []int{mine.tagID},
		"start":   time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"end":     time.Now().UTC().Format(time.RFC3339),
	}); status != http.StatusOK {
		t.Fatalf("an org admin was refused their own tag: %d — %s", status, truncate(raw))
	}

	// The same request with one foreign id mixed in must be refused entirely,
	// not answered with the tags that happen to be theirs.
	status, raw := batchQuery(t, orgAdmin, map[string]interface{}{
		"tag_ids": []int{mine.tagID, theirs.tagID},
		"start":   time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"end":     time.Now().UTC().Format(time.RFC3339),
	})
	if status == http.StatusOK {
		t.Fatalf("a batch containing another organisation's tag was answered: %s", truncate(raw))
	}
	if status != http.StatusForbidden && status != http.StatusNotFound {
		t.Errorf("want 403 or 404 for a foreign tag, got %d — %s", status, truncate(raw))
	}
}

func TestTrendBatchValidatesItsInput(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	now := time.Now().UTC().Format(time.RFC3339)
	hourAgo := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	// A huge id list would become a query per id.
	many := make([]int, 200)
	for i := range many {
		many[i] = fx.tagID
	}

	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"no tags", map[string]interface{}{"tag_ids": []int{}, "start": hourAgo, "end": now}},
		{"too many tags", map[string]interface{}{"tag_ids": many, "start": hourAgo, "end": now}},
		{"end before start", map[string]interface{}{"tag_ids": []int{fx.tagID}, "start": now, "end": hourAgo}},
		{"unparseable start", map[string]interface{}{"tag_ids": []int{fx.tagID}, "start": "yesterday", "end": now}},
		// agg is interpolated into SQL by the query builders, so anything
		// outside the whitelist has to be refused before it gets there.
		{"an aggregate that is not one", map[string]interface{}{
			"tag_ids": []int{fx.tagID}, "start": hourAgo, "end": now, "agg": "count(*); DROP TABLE tag_history"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, raw := batchQuery(t, org, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("want 400, got %d — %s", status, truncate(raw))
			}
		})
	}
}
