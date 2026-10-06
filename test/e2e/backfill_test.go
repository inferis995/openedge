//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// A data logger sends hours of quarter-hourly readings in one message (a
// MiDo frame holds 16–24). They reach the historian within the same second,
// and were taken fifteen minutes apart. Every one must be written at its own
// time — the historian's periodic floor used to be measured on the server
// clock, dropping all but the first unchanged reading — and none may replace
// the newer current value.
func TestABatchOfPastReadingsIsHistorisedAtItsOwnTimes(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}
	mq := mqttConnect(t, "e2e-backfill-"+uniqueSuffix())

	nowMs := time.Now().UnixMilli()
	publish(t, mq, fx.dataTopic, map[string]interface{}{
		"tag_id": fx.tagID, "org_id": fx.org.ID, "v": 99.0, "ts": nowMs, "q": 0,
	})
	waitForCurrent(t, orgScoped, fx.tagID, 99.0)

	start := time.Now().Add(-4 * time.Hour).Truncate(time.Minute)
	const n = 16
	for i := 0; i < n; i++ {
		publish(t, mq, fx.dataTopic, map[string]interface{}{
			"tag_id": fx.tagID, "org_id": fx.org.ID,
			"v":  10.0, // unchanged on purpose: the case the old floor dropped
			"ts": start.Add(time.Duration(i) * 15 * time.Minute).UnixMilli(), "q": 0,
		})
	}

	query := fmt.Sprintf("/api/history?tag_id=%d&start=%s&end=%s&raw=true", fx.tagID,
		start.Add(-time.Minute).UTC().Format(time.RFC3339),
		start.Add(n*15*time.Minute).UTC().Format(time.RFC3339))
	var got int
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, raw := orgScoped.do(http.MethodGet, query, nil)
		if status == http.StatusOK {
			var resp struct {
				Data []struct {
					Value *float64 `json:"value"`
				} `json:"data"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				got = 0
				for _, p := range resp.Data {
					if p.Value != nil && *p.Value == 10.0 {
						got++
					}
				}
				if got == n {
					break
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d past readings reached tag_history", got, n)
		}
		time.Sleep(time.Second)
	}

	// The current value is still the newest reading, not the last one received.
	status, raw := orgScoped.do(http.MethodGet, fmt.Sprintf("/api/tags/%d/current", fx.tagID), nil)
	if status != http.StatusOK {
		t.Fatalf("current value: %d %s", status, truncate(raw))
	}
	var cur struct {
		Value interface{} `json:"value"`
		V     interface{} `json:"v"`
	}
	_ = json.Unmarshal(raw, &cur)
	v := cur.Value
	if v == nil {
		v = cur.V
	}
	if f, ok := v.(float64); !ok || f != 99.0 {
		t.Errorf("the current value is %v after a batch of past readings, want the newer 99", v)
	}
}

// waitForCurrent polls the tag's current value until it reads want.
func waitForCurrent(t *testing.T, c *apiClient, tagID int, want float64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		status, raw := c.do(http.MethodGet, fmt.Sprintf("/api/tags/%d/current", tagID), nil)
		if status == http.StatusOK {
			var cur struct {
				Value interface{} `json:"value"`
				V     interface{} `json:"v"`
			}
			if json.Unmarshal(raw, &cur) == nil {
				v := cur.Value
				if v == nil {
					v = cur.V
				}
				if f, ok := v.(float64); ok && f == want {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("tag %d never showed the current value %v", tagID, want)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
