package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAssetsAPI(t *testing.T) {
	db := forecastDB(t) // two organizations, tag 1 (org 1, forecast ok) and tag 2 (org 2)
	ctx := context.Background()
	for _, q := range []string{
		`DROP TABLE IF EXISTS asset_tags, assets CASCADE`,
		`CREATE TABLE assets (id SERIAL PRIMARY KEY, org_id INT NOT NULL REFERENCES organizations(id),
			name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', state_tag_id INT REFERENCES tags(id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (org_id, name))`,
		`CREATE TABLE asset_tags (asset_id INT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
			tag_id INT NOT NULL REFERENCES tags(id), PRIMARY KEY (asset_id, tag_id))`,
		`UPDATE tag_forecasts SET risks = '[{"rule_id":9,"type":"high","threshold":5,"likelihood":"likely","at":"2026-10-22T18:00:00Z"}]' WHERE tag_id = 1`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	h := NewForecastHandler(db, nil)

	// Another organization's tag cannot be put in a machine.
	if w := forecastCall(t, h.CreateAsset, http.MethodPost, "/api/forecast/assets", nil, 1,
		map[string]any{"name": "Pompa", "tag_ids": []int{1, 2}}); w.Code != http.StatusBadRequest {
		t.Errorf("foreign tag: %d %s", w.Code, w.Body)
	}
	w := forecastCall(t, h.CreateAsset, http.MethodPost, "/api/forecast/assets", nil, 1,
		map[string]any{"name": "Pompa", "tag_ids": []int{1}, "state_tag_id": 1})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var a Asset
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	if a.Name != "Pompa" || len(a.Members) != 1 || a.Health.Score == nil || *a.Health.Score != 70 || a.Health.Level != "watch" {
		t.Fatalf("asset %+v: one member, a likely risk → 70", a)
	}
	if dup := forecastCall(t, h.CreateAsset, http.MethodPost, "/api/forecast/assets", nil, 1,
		map[string]any{"name": "Pompa"}); dup.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d", dup.Code)
	}

	// Organization 2 sees none of it, and cannot touch it.
	w = forecastCall(t, h.ListAssets, http.MethodGet, "/api/forecast/assets", nil, 2, nil)
	var list struct{ Assets []Asset }
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Assets) != 0 {
		t.Errorf("organization 2 sees %d machines", len(list.Assets))
	}
	id := gin.Params{{Key: "id", Value: "1"}}
	if del := forecastCall(t, h.DeleteAsset, http.MethodDelete, "/api/forecast/assets/1", id, 2, nil); del.Code != http.StatusNotFound {
		t.Errorf("organization 2 deleting: %d", del.Code)
	}

	// One machine per gateway: org 1 has gateway GA (already none by that name).
	w = forecastCall(t, h.AssetsFromGateways, http.MethodPost, "/api/forecast/assets/from-gateways", nil, 1, nil)
	if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
		t.Fatalf("from gateways: %d %s", w.Code, w.Body)
	}
	var made struct{ Created int }
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	if made.Created != 1 {
		t.Errorf("created %d, want 1 (GA)", made.Created)
	}
	w = forecastCall(t, h.AssetsFromGateways, http.MethodPost, "/api/forecast/assets/from-gateways", nil, 1, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	if made.Created != 0 {
		t.Errorf("second run created %d, want 0", made.Created)
	}
	if w := forecastCall(t, h.DeleteAsset, http.MethodDelete, "/api/forecast/assets/1", id, 1, nil); w.Code != http.StatusNoContent {
		t.Errorf("delete: %d", w.Code)
	}
}
