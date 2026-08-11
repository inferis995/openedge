//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// Commanding a physical output is the most consequential thing this API does,
// and it was the one mutating tag route with no check at all. Create, Update and
// Delete each required admin; POST /tags/:id/write accepted any authenticated
// user in the organization.
//
// The control existed the whole time. can_write_tags is stored per user, is
// editable from the Users page, and middleware.RequirePermission was written to
// enforce it — and that middleware was mounted on no route anywhere in the
// codebase. Unchecking the box saved, redisplayed as unchecked, and changed
// nothing about what the account could do.
//
// Loading a recipe is the same act with more of the plant behind it: it
// publishes a whole batch of setpoints. It had no check either.

// createOrgUser makes a plain (non-admin) account in an organization.
func createOrgUser(t *testing.T, admin *apiClient, orgID int, username, password string) (*apiClient, int) {
	t.Helper()
	admin.mustDo(http.MethodPost, "/api/users", map[string]interface{}{
		"username":  username,
		"password":  password,
		"role":      "user",
		"full_name": username,
		"org_id":    orgID,
	}, http.StatusCreated)

	c, lr := login(t, username, password)
	if lr.User.Role != "user" {
		t.Fatalf("account %q has role %q, want user", username, lr.User.Role)
	}
	return c, lr.User.ID
}

// A plain user with no granted permission cannot command an output.
func TestPlainUserCannotWriteATag(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)

	suffix := uniqueSuffix()
	operator, _ := createOrgUser(t, admin, fx.org.ID, "op-"+suffix, "operator-password-1")
	operator.orgID = fmt.Sprintf("%d", fx.org.ID)

	// No row in role_permissions → denied. The value is deliberately valid, so
	// a pass here would mean the authorization failed, not the payload.
	operator.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 1}, http.StatusForbidden)
}

// An admin always can — that is the decision this deployment made, and
// RequirePermission short-circuits for role=admin before consulting the table.
func TestAdminCanAlwaysWriteATag(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	orgScoped.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 1}, http.StatusOK)
}

// Granting the permission makes the plain user able to write, which is the
// other half of the contract: the checkbox has to do something in BOTH
// directions, or an administrator cannot delegate at all.
func TestGrantingWritePermissionLetsAUserWrite(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)

	suffix := uniqueSuffix()
	username := "op-grant-" + suffix
	operator, userID := createOrgUser(t, admin, fx.org.ID, username, "operator-password-1")
	operator.orgID = fmt.Sprintf("%d", fx.org.ID)

	// Denied before the grant.
	operator.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 1}, http.StatusForbidden)

	admin.mustDo(http.MethodPut, fmt.Sprintf("/api/users/%d/permissions", userID),
		map[string]interface{}{"can_write_tags": true}, http.StatusOK)

	// Allowed after it.
	operator.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 1}, http.StatusOK)
}

// Loading a recipe writes a batch of setpoints and must be gated the same way,
// or the permission is a front door with the back door left open.
func TestPlainUserCannotLoadARecipe(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	recipeID := createEntity(t, orgScoped, "/api/recipes", map[string]interface{}{
		"name": "e2e-recipe-" + uniqueSuffix(),
		"values": []map[string]interface{}{
			{"tag_id": fx.tagID, "value": "1"},
		},
	})

	suffix := uniqueSuffix()
	operator, _ := createOrgUser(t, admin, fx.org.ID, "op-recipe-"+suffix, "operator-password-1")
	operator.orgID = fmt.Sprintf("%d", fx.org.ID)

	operator.mustDo(http.MethodPost, fmt.Sprintf("/api/recipes/%d/load", recipeID),
		nil, http.StatusForbidden)
}

// subscribeOnce subscribes before the action under test and returns a function
// that waits for the first message. Subscribing first matters: the write is
// published the moment the API returns, so a subscription set up afterwards
// races with it and fails intermittently — the kind of flake that gets a real
// test deleted.
func subscribeOnce(t *testing.T, c paho.Client, topic string) func(*testing.T, time.Duration) []byte {
	t.Helper()

	msgs := make(chan []byte, 4)
	tok := c.Subscribe(topic, 1, func(_ paho.Client, m paho.Message) {
		// Copy: paho reuses the buffer once the callback returns.
		buf := make([]byte, len(m.Payload()))
		copy(buf, m.Payload())
		select {
		case msgs <- buf:
		default:
		}
	})
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("subscribing to %s: %v", topic, tok.Error())
	}

	return func(t *testing.T, wait time.Duration) []byte {
		t.Helper()
		select {
		case m := <-msgs:
			return m
		case <-time.After(wait):
			t.Fatalf("no message on %s within %s — the API accepted the write but "+
				"nothing reached the driver topic", topic, wait)
			return nil
		}
	}
}

// A setpoint written in engineering units must reach the device as raw.
//
// The read path converts and its result replaces the raw value everywhere
// downstream, so every number a synoptic shows is in engineering units. Writes
// travelled the other way untouched: on a tag scaled 0..27648 raw to 0..100 bar
// a 50 bar setpoint reached the register as 50, about 0.18 bar.
//
// Asserting the conversion in a unit test proves the arithmetic. Only driving
// the real API proves the conversion is actually ON the write path — which is
// exactly what was missing, since scaling.Apply had been correct all along.
func TestSetpointIsConvertedToRawBeforeItLeavesTheAPI(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	// Scale the fixture tag the way an analogue input normally is.
	orgScoped.mustDo(http.MethodPut, fmt.Sprintf("/api/tags/%d", fx.tagID),
		map[string]interface{}{
			"scaling_enabled": true,
			"scaling_raw_min": 0.0, "scaling_raw_max": 27648.0,
			"scaling_eu_min": 0.0, "scaling_eu_max": 100.0,
			"eu_unit": "bar",
		}, http.StatusOK)

	// Listen for what the driver would receive.
	sub := mqttConnect(t, "e2e-write-observer-"+uniqueSuffix())
	received := subscribeOnce(t, sub, fmt.Sprintf("cmd/write/%d", fx.gwID))

	// The operator asks for 50 bar.
	orgScoped.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 50.0}, http.StatusOK)

	payload := received(t, 15*time.Second)

	var cmd struct {
		TagID int     `json:"tag_id"`
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(payload, &cmd); err != nil {
		t.Fatalf("decoding the write command: %v — %s", err, truncate(payload))
	}

	// Half of the raw span, not the number the operator typed.
	if math.Abs(cmd.Value-13824) > 1 {
		t.Fatalf("a 50 bar setpoint reached the driver as %g, want ~13824 raw.\n"+
			"At %g the device would sit at %.2f bar while the operator believes it is at 50.",
			cmd.Value, cmd.Value, cmd.Value/27648*100)
	}
}

// A setpoint the configured range cannot express is refused, not clamped:
// silently turning an operator's 500 into 100 reports success and leaves them
// believing the plant is at 500.
func TestOutOfRangeSetpointIsRejected(t *testing.T) {
	admin, _ := adminSession(t)
	fx := newFixture(t, admin)
	orgScoped := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", fx.org.ID)}

	orgScoped.mustDo(http.MethodPut, fmt.Sprintf("/api/tags/%d", fx.tagID),
		map[string]interface{}{
			"scaling_enabled": true,
			"scaling_raw_min": 0.0, "scaling_raw_max": 27648.0,
			"scaling_eu_min": 0.0, "scaling_eu_max": 100.0,
		}, http.StatusOK)

	orgScoped.mustDo(http.MethodPost, fmt.Sprintf("/api/tags/%d/write", fx.tagID),
		map[string]interface{}{"value": 500.0}, http.StatusBadRequest)
}
