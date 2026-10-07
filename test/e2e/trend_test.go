//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	// The payload shape the historian actually reads. Sending {value, quality,
	// timestamp} instead produced an empty series and looked like a broken
	// endpoint; see TestPublishedValueIsHistorised, which is the shape of
	// record here.
	want := 42.5
	publish(t, mqtt, fx.dataTopic, map[string]interface{}{
		"tag_id": fx.tagID,
		"org_id": fx.org.ID,
		"v":      want,
		"ts":     time.Now().UnixMilli(),
		"q":      0,
	})

	// Under an hour on purpose: determineAggregationLevel sends anything longer
	// to the 1-minute continuous aggregate, which materialises on its own
	// schedule and would report a fresh point as missing. The batch endpoint has
	// no raw=true, so the window is what selects raw.
	body := map[string]interface{}{
		"tag_ids": []int{fx.tagID},
		"start":   time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
		"end":     time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
		"agg":     "avg",
	}

	// Poll rather than sleep: the historian persists on its own schedule, and a
	// fixed wait is either flaky or slow.
	var status int
	var raw []byte
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, raw = batchQuery(t, org, body)
		if status == http.StatusOK && strings.Contains(string(raw), "42.5") {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
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

// The chart asks for as many values as it has pixels. With more samples than
// that, each value is a bucket carrying its min and max — a spike averaged
// away still shows in the envelope; with fewer, the samples come back as they
// are.
func TestTrendBatchSizesBucketsToTheChartAndKeepsTheExtremes(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}
	mq := mqttConnect(t, "e2e-trend-points-"+uniqueSuffix())

	// 120 readings 5 s apart over the last 10 minutes, one of them a spike.
	start := time.Now().Add(-11 * time.Minute).Truncate(time.Minute)
	for i := 0; i < 120; i++ {
		v := 10.0 + float64(i%3)*0.1 // changes every sample: the historian keeps them all
		if i == 60 {
			v = 99
		}
		publish(t, mq, fx.dataTopic, map[string]interface{}{
			"tag_id": fx.tagID, "org_id": fx.org.ID, "v": v,
			"ts": start.Add(time.Duration(i) * 5 * time.Second).UnixMilli(), "q": 0,
		})
	}

	type point struct {
		Value *float64 `json:"value"`
		Min   *float64 `json:"min"`
		Max   *float64 `json:"max"`
	}
	ask := func(points int) []point {
		body := map[string]interface{}{
			"tag_ids": []int{fx.tagID},
			"start":   start.Add(-time.Second).UTC().Format(time.RFC3339),
			"end":     start.Add(10 * time.Minute).UTC().Format(time.RFC3339),
			"agg":     "avg", "points": points,
		}
		status, raw := batchQuery(t, org, body)
		if status != http.StatusOK {
			t.Fatalf("batch with points=%d: %d %s", points, status, truncate(raw))
		}
		var out map[string][]point
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out[strconv.Itoa(fx.tagID)]
	}

	// Wait for the historian to have all of them (asking for every sample).
	deadline := time.Now().Add(30 * time.Second)
	for len(ask(1000)) < 120 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of 120 readings reached tag_history", len(ask(1000)))
		}
		time.Sleep(time.Second)
	}

	// Room for every sample: the samples, no envelope.
	all := ask(1000)
	if all[len(all)/2].Min != nil {
		t.Errorf("a window with fewer samples than points came back bucketed")
	}

	// Room for 50: buckets, fewer than the samples, the spike in a max.
	b := ask(50)
	if len(b) >= 120 || len(b) < 10 {
		t.Fatalf("%d values for 50 points over 120 samples", len(b))
	}
	spike := false
	for _, p := range b {
		if p.Min == nil || p.Max == nil {
			continue
		}
		if *p.Max == 99 {
			spike = true
		}
	}
	if !spike {
		t.Errorf("the spike is in no bucket's max — it was averaged away")
	}
}

// Named trend views belong to an organization: saved and listed there, not
// visible from another, and only trend views are stored.
func TestTrendViewsAreKeptPerOrganization(t *testing.T) {
	admin, _ := adminSession(t)
	mine := newFixture(t, admin)
	theirs := newFixture(t, admin)
	a := createOrgAdmin(t, admin, mine.org.ID, "tv-"+uniqueSuffix(), "trend-views-pass-1234")
	a.orgID = fmt.Sprintf("%d", mine.org.ID)
	b := createOrgAdmin(t, admin, theirs.org.ID, "tv-"+uniqueSuffix(), "trend-views-pass-1234")
	b.orgID = fmt.Sprintf("%d", theirs.org.ID)

	query := fmt.Sprintf("pens=%d%%3A3b82f6&range=24h", mine.tagID)
	if status, raw := a.do(http.MethodPost, "/api/trend-views", map[string]string{"name": "Pressioni", "query": query}); status != http.StatusCreated {
		t.Fatalf("save: %d %s", status, truncate(raw))
	}
	if status, raw := a.do(http.MethodPost, "/api/trend-views", map[string]string{"name": "x", "query": "pens=1&evil=1"}); status != http.StatusBadRequest {
		t.Errorf("a query that is not a trend view was answered %d %s", status, truncate(raw))
	}
	list := func(c *apiClient) []map[string]interface{} {
		status, raw := c.do(http.MethodGet, "/api/trend-views", nil)
		if status != http.StatusOK {
			t.Fatalf("list: %d %s", status, truncate(raw))
		}
		var out []map[string]interface{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if l := list(a); len(l) != 1 || l[0]["name"] != "Pressioni" || l[0]["query"] != query {
		t.Errorf("own organization lists %v", l)
	}
	if l := list(b); len(l) != 0 {
		t.Errorf("another organization sees %v", l)
	}
}
