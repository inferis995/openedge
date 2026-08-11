//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// User-defined types earn their place only if editing the type actually
// reaches the instances. A "type" that stamps out tags and then forgets them
// is a CSV import with extra steps: changing an alarm threshold on fifty
// motors is still fifty edits, and the one that gets missed is discovered by
// an alarm that does not fire.
//
// So these tests are about the binding, not the CRUD. And the sharpest one is
// the refusal: tag_history cascades from tags, so removing a member from a
// type would delete every value ever recorded for it across every instance —
// silently, from a screen that gives no hint that is what just happened.

func motorMembers(highThreshold float64) []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name": "Run", "address_suffix": "+0", "data_type": "BOOL",
			"historize": true,
		},
		{
			"name": "Speed", "address_suffix": "+2", "data_type": "REAL",
			"historize":       true,
			"scaling_enabled": true,
			"scaling_raw_min": 0.0, "scaling_raw_max": 27648.0,
			"scaling_eu_min": 0.0, "scaling_eu_max": 1500.0,
			"eu_unit": "rpm",
			"alarms": []map[string]interface{}{
				{
					"alarm_type": "high", "threshold": highThreshold,
					"severity": "critical", "message": "overspeed", "enabled": true,
				},
			},
		},
	}
}

func createMotorType(t *testing.T, c *apiClient, name string, threshold float64) int {
	t.Helper()
	return createEntity(t, c, "/api/udt/types", map[string]interface{}{
		"name":        name,
		"description": "e2e motor",
		"members":     motorMembers(threshold),
	})
}

func tagsOfGateway(t *testing.T, c *apiClient, gatewayID int) []struct {
	ID       int    `json:"id"`
	Alias    string `json:"alias"`
	Code     string `json:"code"`
	DataType string `json:"data_type"`
} {
	t.Helper()
	raw := c.mustDo(http.MethodGet, fmt.Sprintf("/api/tags?gateway_id=%d", gatewayID), nil, http.StatusOK)

	var direct []struct {
		ID       int    `json:"id"`
		Alias    string `json:"alias"`
		Code     string `json:"code"`
		DataType string `json:"data_type"`
	}
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var wrapped struct {
		Items []struct {
			ID       int    `json:"id"`
			Alias    string `json:"alias"`
			Code     string `json:"code"`
			DataType string `json:"data_type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatalf("decoding the tag list: %v — %s", err, truncate(raw))
	}
	return wrapped.Items
}

// Instantiating a type generates the tags, with addresses built from the
// instance's base plus each member's suffix.
func TestInstantiatingATypeGeneratesItsTags(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	typeID := createMotorType(t, org, "Motor-"+uniqueSuffix(), 1400)

	before := len(tagsOfGateway(t, org, fx.gwID))

	org.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
		"type_id": typeID, "gateway_id": fx.gwID,
		"name": "Pump01", "base_address": "40001",
	}, http.StatusCreated)

	tags := tagsOfGateway(t, org, fx.gwID)
	if len(tags) != before+2 {
		t.Fatalf("instantiating a 2-member type produced %d new tags, want 2",
			len(tags)-before)
	}

	byAlias := map[string]string{}
	for _, tg := range tags {
		byAlias[tg.Alias] = tg.Code
	}
	for alias, wantCode := range map[string]string{
		"Pump01_Run":   "40001+0",
		"Pump01_Speed": "40001+2",
	} {
		got, ok := byAlias[alias]
		if !ok {
			t.Fatalf("no tag named %q was generated; got %v", alias, byAlias)
		}
		if got != wantCode {
			t.Errorf("%s has address %q, want %q — base_address + address_suffix",
				alias, got, wantCode)
		}
	}
}

// The whole point: edit the type, every instance follows.
func TestEditingATypeReachesEveryInstance(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	typeID := createMotorType(t, org, "Motor-"+uniqueSuffix(), 1400)

	for _, name := range []string{"P1", "P2", "P3"} {
		org.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
			"type_id": typeID, "gateway_id": fx.gwID,
			"name": name, "base_address": "40" + name,
		}, http.StatusCreated)
	}

	// Add a member to the type. Three instances must each gain a tag.
	members := motorMembers(1400)
	members = append(members, map[string]interface{}{
		"name": "Hours", "address_suffix": "+6", "data_type": "DINT", "historize": true,
	})
	raw := org.mustDo(http.MethodPut, fmt.Sprintf("/api/udt/types/%d", typeID),
		map[string]interface{}{
			"name": "Motor-updated-" + uniqueSuffix(), "description": "e2e", "members": members,
		}, http.StatusOK)

	var res struct {
		Reconciled struct {
			Created int `json:"tags_created"`
			Updated int `json:"tags_updated"`
		} `json:"reconciled"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("decoding the reconcile result: %v — %s", err, truncate(raw))
	}
	if res.Reconciled.Created != 3 {
		t.Fatalf("adding one member to a type with 3 instances created %d tags, want 3 — "+
			"the type is not reaching its instances", res.Reconciled.Created)
	}

	tags := tagsOfGateway(t, org, fx.gwID)
	for _, want := range []string{"P1_Hours", "P2_Hours", "P3_Hours"} {
		found := false
		for _, tg := range tags {
			if tg.Alias == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("instance tag %q was not generated", want)
		}
	}
}

// Changing a member's shape rewrites the existing tags rather than making new
// ones — otherwise the history would be stranded behind an abandoned tag.
func TestEditingAMemberRewritesTagsInPlace(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	typeID := createMotorType(t, org, "Motor-"+uniqueSuffix(), 1400)
	org.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
		"type_id": typeID, "gateway_id": fx.gwID,
		"name": "Fan01", "base_address": "41000",
	}, http.StatusCreated)

	var speedTagID int
	for _, tg := range tagsOfGateway(t, org, fx.gwID) {
		if tg.Alias == "Fan01_Speed" {
			speedTagID = tg.ID
		}
	}
	if speedTagID == 0 {
		t.Fatal("Fan01_Speed was not generated")
	}

	// Move the member's address and widen its engineering range.
	members := motorMembers(1400)
	members[1]["address_suffix"] = "+4"
	members[1]["scaling_eu_max"] = 3000.0

	org.mustDo(http.MethodPut, fmt.Sprintf("/api/udt/types/%d", typeID),
		map[string]interface{}{"name": "Motor-" + uniqueSuffix(), "members": members},
		http.StatusOK)

	// Same tag id, new address: the identity survived the edit.
	var found bool
	for _, tg := range tagsOfGateway(t, org, fx.gwID) {
		if tg.ID == speedTagID {
			found = true
			if tg.Code != "41000+4" {
				t.Errorf("the tag kept address %q after the member moved to +4", tg.Code)
			}
		}
	}
	if !found {
		t.Fatal("the Speed tag was replaced rather than updated — its history would be " +
			"stranded behind a tag nothing references")
	}
}

// The refusal this feature is built around.
//
// tag_history cascades from tags, so dropping a member takes every recorded
// value for it on every instance. Refusing by default, and saying how much is
// at stake, is the difference between an edit and an accident.
func TestRemovingAMemberIsRefusedWithoutConfirmation(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	typeID := createMotorType(t, org, "Motor-"+uniqueSuffix(), 1400)
	org.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
		"type_id": typeID, "gateway_id": fx.gwID,
		"name": "Mix01", "base_address": "42000",
	}, http.StatusCreated)

	// Drop the Speed member, keeping only Run.
	onlyRun := []map[string]interface{}{motorMembers(1400)[0]}

	raw := org.mustDo(http.MethodPut, fmt.Sprintf("/api/udt/types/%d", typeID),
		map[string]interface{}{"name": "Motor-" + uniqueSuffix(), "members": onlyRun},
		http.StatusConflict)

	var refusal struct {
		Error  string `json:"error"`
		Impact struct {
			Members []string `json:"members"`
			Tags    int      `json:"tags"`
		} `json:"impact"`
	}
	if err := json.Unmarshal(raw, &refusal); err != nil {
		t.Fatalf("decoding the refusal: %v — %s", err, truncate(raw))
	}
	if refusal.Impact.Tags != 1 {
		t.Errorf("the refusal reported %d tags at stake, want 1", refusal.Impact.Tags)
	}
	if len(refusal.Impact.Members) != 1 || refusal.Impact.Members[0] != "Speed" {
		t.Errorf("the refusal named %v, want [Speed] — an operator has to know what "+
			"they are about to lose", refusal.Impact.Members)
	}

	// The tag is still there: a refused edit must change nothing.
	still := false
	for _, tg := range tagsOfGateway(t, org, fx.gwID) {
		if tg.Alias == "Mix01_Speed" {
			still = true
		}
	}
	if !still {
		t.Fatal("the refused edit deleted the tag anyway")
	}

	// With the confirmation it goes through.
	org.mustDo(http.MethodPut, fmt.Sprintf("/api/udt/types/%d", typeID),
		map[string]interface{}{
			"name": "Motor-" + uniqueSuffix(), "members": onlyRun,
			"confirm_data_loss": true,
		}, http.StatusOK)

	for _, tg := range tagsOfGateway(t, org, fx.gwID) {
		if tg.Alias == "Mix01_Speed" {
			t.Fatal("the confirmed removal left the tag behind")
		}
	}
}

// A type in use cannot be deleted out from under its instances.
func TestDeletingATypeInUseIsRefused(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	org := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	typeID := createMotorType(t, org, "Motor-"+uniqueSuffix(), 1400)
	org.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
		"type_id": typeID, "gateway_id": fx.gwID, "name": "Conv01", "base_address": "43000",
	}, http.StatusCreated)

	org.mustDo(http.MethodDelete, fmt.Sprintf("/api/udt/types/%d", typeID), nil, http.StatusConflict)
}

// Types are tenant-scoped like everything else here.
func TestTypesAreOrgScoped(t *testing.T) {
	admin, _ := adminSession(t)
	a := newFixture(t, admin)
	b := newFixture(t, admin)

	orgA := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", a.org.ID)}
	orgB := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", b.org.ID)}

	typeID := createMotorType(t, orgA, "Motor-"+uniqueSuffix(), 1400)

	orgB.mustDo(http.MethodGet, fmt.Sprintf("/api/udt/types/%d", typeID), nil, http.StatusNotFound)

	// And a type from A cannot be stamped onto a gateway in B.
	orgB.mustDo(http.MethodPost, "/api/udt/instances", map[string]interface{}{
		"type_id": typeID, "gateway_id": b.gwID, "name": "X1", "base_address": "1",
	}, http.StatusNotFound)
}
