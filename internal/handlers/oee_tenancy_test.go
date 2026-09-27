package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

func oeeTestContext(query string, claims jwt.MapClaims, org *int) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/oee/history-v2?"+query, nil)
	if claims != nil {
		c.Set(middleware.UserKey, claims)
	}
	if org != nil {
		c.Set(middleware.ContextKeyOrganizationID, *org)
	}
	return c, w
}

// The rollup a user reads is their organization's: it was every
// organization's lines averaged into one row.
func TestTheRollupIsTheCallersOrganizations(t *testing.T) {
	org := 7
	c, _ := oeeTestContext("", jwt.MapClaims{"role": "viewer", "org_id": float64(org)}, &org)
	target, ok := historyTargetOf(c, nil)
	if !ok {
		t.Fatal("the rollup was refused")
	}
	if target.profileID != nil || target.org == nil || *target.org != org {
		t.Fatalf("target = %+v, want organization %d's rollup", target, org)
	}
	pred, arg := target.where(4)
	if !strings.Contains(pred, "h.profile_id IS NULL") || !strings.Contains(pred, "h.org_id = $4") {
		t.Errorf("predicate %q does not select the organization's rollup", pred)
	}
	if p, ok := arg.(*int); !ok || p == nil || *p != org {
		t.Errorf("argument = %v, want %d", arg, org)
	}
}

// The global administrator with no organization selected keeps seeing
// every rollup; one who selected an organization sees that one's.
func TestTheGlobalAdministratorsRollup(t *testing.T) {
	global := jwt.MapClaims{"role": "admin"}
	c, _ := oeeTestContext("profile_id=0", global, nil)
	target, ok := historyTargetOf(c, nil)
	if !ok || target.org != nil || target.profileID != nil {
		t.Fatalf("global admin target = %+v, %v; want every organization's rollup", target, ok)
	}

	org := 3
	c, _ = oeeTestContext("", global, &org)
	target, _ = historyTargetOf(c, nil)
	if target.org == nil || *target.org != org {
		t.Fatalf("global admin with an organization selected: target = %+v", target)
	}
}

// Without an organization and not the global administrator, the filter
// matches nothing rather than everything.
func TestNoOrganizationReadsNoRollup(t *testing.T) {
	c, _ := oeeTestContext("", jwt.MapClaims{"role": "viewer"}, nil)
	target, _ := historyTargetOf(c, nil)
	if target.org == nil || *target.org != -1 {
		t.Fatalf("target = %+v, want the organization that matches nothing", target)
	}
}

func TestAnInvalidProfileIsRefusedBeforeTheDatabase(t *testing.T) {
	org := 1
	c, w := oeeTestContext("profile_id=abc", jwt.MapClaims{"role": "viewer", "org_id": float64(org)}, &org)
	if _, ok := historyTargetOf(c, nil); ok {
		t.Fatal("a non-numeric profile_id was accepted")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAProfileTargetFiltersByProfileOnly(t *testing.T) {
	id := 12
	pred, arg := historyTarget{profileID: &id}.where(3)
	if pred != "h.profile_id = $3" || arg != 12 {
		t.Errorf("where = %q, %v", pred, arg)
	}
}

// A rollup never matched UNIQUE (profile_id, ...) and was inserted again on
// every run; each kind of row conflicts on its own unique index.
func TestEachHistoryRowUpsertsOnItsOwnUniqueIndex(t *testing.T) {
	pid, org := 1, 2
	cases := []struct {
		profileID, orgID *int
		want             string
	}{
		{&pid, &org, "(profile_id, bucket_start, bucket_size)"},
		{nil, &org, "(org_id, bucket_start, bucket_size) WHERE profile_id IS NULL AND org_id IS NOT NULL"},
		{nil, nil, "(bucket_start, bucket_size) WHERE profile_id IS NULL AND org_id IS NULL"},
	}
	for _, tc := range cases {
		if got := historyConflict(tc.profileID, tc.orgID); got != tc.want {
			t.Errorf("historyConflict(%v, %v) = %q, want %q", tc.profileID, tc.orgID, got, tc.want)
		}
	}
}

func TestProfilesAreGroupedByOrganization(t *testing.T) {
	profiles := []OEEProfile{
		{ID: 1, OrgID: 10}, {ID: 2, OrgID: 20}, {ID: 3, OrgID: 10}, {ID: 4, OrgID: 30},
	}
	groups := profilesByOrg(profiles)
	if len(groups) != 3 {
		t.Fatalf("%d groups, want 3", len(groups))
	}
	want := map[int][]int{10: {1, 3}, 20: {2}, 30: {4}}
	for i, g := range groups {
		if wantOrg := []int{10, 20, 30}[i]; g.orgID != wantOrg {
			t.Errorf("group %d is organization %d, want %d", i, g.orgID, wantOrg)
		}
		var ids []int
		for _, p := range g.profiles {
			if p.OrgID != g.orgID {
				t.Errorf("profile %d of organization %d in organization %d's group", p.ID, p.OrgID, g.orgID)
			}
			ids = append(ids, p.ID)
		}
		if len(ids) != len(want[g.orgID]) {
			t.Errorf("organization %d has profiles %v, want %v", g.orgID, ids, want[g.orgID])
		}
	}
	if profilesByOrg(nil) != nil {
		t.Error("no profiles should give no groups")
	}
}

// Each organization's rollup averages its own profiles only.
func TestTheRollupAveragesOneOrganizationsProfiles(t *testing.T) {
	a := []OEESnapshot{
		{OEE: 80, Availability: 90, Performance: 95, Quality: 99, PiecesProduced: 100, PiecesGood: 98, CriticalDowntimeMin: 5},
		{OEE: 60, Availability: 70, Performance: 85, Quality: 97, PiecesProduced: 50, PiecesGood: 45, CriticalDowntimeMin: 15},
	}
	r := averageSnapshots(a, 60)
	if r.OEE != 70 || r.Availability != 80 || r.Performance != 90 || r.Quality != 98 {
		t.Errorf("averages = %.1f/%.1f/%.1f/%.1f, want 70/80/90/98", r.OEE, r.Availability, r.Performance, r.Quality)
	}
	if r.PiecesProduced != 150 || r.PiecesGood != 143 || r.CriticalDowntimeMin != 20 {
		t.Errorf("sums = %.0f/%.0f/%.0f, want 150/143/20", r.PiecesProduced, r.PiecesGood, r.CriticalDowntimeMin)
	}
	if r.WindowMinutes != 60 {
		t.Errorf("window = %d, want 60", r.WindowMinutes)
	}
	if empty := averageSnapshots(nil, 60); empty.OEE != 0 {
		t.Errorf("empty rollup OEE = %v", empty.OEE)
	}
}

func TestAnAlertIsNotifiedWithItsOrganization(t *testing.T) {
	org := 4
	if orgOrZero(&org) != 4 || orgOrZero(nil) != 0 {
		t.Error("orgOrZero")
	}
}

func TestALossRequestNeedsAValidProfile(t *testing.T) {
	org := 1
	for _, raw := range []string{"", "0", "x", "-3"} {
		c, w := oeeTestContext("", jwt.MapClaims{"role": "viewer", "org_id": float64(org)}, &org)
		if _, ok := lossProfileOf(c, nil, raw); ok {
			t.Errorf("profile %q was accepted", raw)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("profile %q: status %d, want 400", raw, w.Code)
		}
	}
}
