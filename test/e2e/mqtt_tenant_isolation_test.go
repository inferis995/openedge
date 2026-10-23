//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// One tenant's broker login receives nothing of another tenant's, on the
// running broker.
//
// The unit tests in internal/mqtt check the grant we build against a model of
// the broker. This checks the broker itself: organization A's web UI login
// subscribes with the same wide filters the UI uses, the broker admin
// publishes on organization B's topics — legacy data, Sparkplug, alarms,
// gateway health — and nothing may arrive. Then the same publish on A's own
// topic, which must arrive: "nothing arrived" is also what a broken
// subscription looks like.
func TestOneTenantsLoginReceivesNothingOfAnothers(t *testing.T) {
	admin, _ := adminSession(t)
	a := newFixture(t, admin)
	b := newFixture(t, admin)

	orgA := &apiClient{t: t, token: admin.token, orgID: fmt.Sprintf("%d", a.org.ID)}
	raw := orgA.mustDo(http.MethodGet, "/api/mqtt/ui-credentials", nil, http.StatusOK)
	var creds struct{ Username, Password string }
	if err := json.Unmarshal(raw, &creds); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}

	got := make(chan string, 64)
	ui := paho.NewClient(wsClientOptions(t, "e2e-iso-"+uniqueSuffix()).
		SetUsername(creds.Username).SetPassword(creds.Password))
	if tok := ui.Connect(); !tok.WaitTimeout(20*time.Second) || tok.Error() != nil {
		t.Fatalf("connect with org A's UI login: %v", tok.Error())
	}
	defer ui.Disconnect(100)
	for _, f := range []string{"data/#", "spBv1.0/#", "sys/alarms/#"} {
		sub := ui.Subscribe(f, 0, func(_ paho.Client, m paho.Message) { got <- m.Topic() })
		if !sub.WaitTimeout(10*time.Second) || sub.Error() != nil {
			t.Fatalf("org A's UI login cannot subscribe to %s: %v", f, sub.Error())
		}
	}

	bGroup := slug(b.org.Name) + "-" + slug(b.siteName)
	others := []string{
		b.dataTopic,
		fmt.Sprintf("spBv1.0/%s/DDATA/node/dev", bGroup),
		fmt.Sprintf("spBv1.0/%s/NBIRTH/node", bGroup),
		fmt.Sprintf("sys/alarms/%d/site/area/gw/tag", b.org.ID),
	}
	pub := mqttConnect(t, "e2e-iso-pub-"+uniqueSuffix())
	for _, topic := range others {
		if tok := pub.Publish(topic, 1, false, `{"value":1}`); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
			t.Fatalf("publish on %s: %v", topic, tok.Error())
		}
	}
	// The control, on A's own legacy topic.
	if tok := pub.Publish(a.dataTopic, 1, false, `{"value":1}`); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("publish on %s: %v", a.dataTopic, tok.Error())
	}

	sawOwn := false
	deadline := time.After(5 * time.Second)
	for !sawOwn {
		select {
		case topic := <-got:
			if topic == a.dataTopic {
				sawOwn = true
				continue
			}
			for _, o := range others {
				if topic == o {
					t.Errorf("organization %d's UI login received %q, organization %d's", a.org.ID, topic, b.org.ID)
				}
			}
		case <-deadline:
			t.Fatalf("organization A's login did not receive its own %s either — the subscription is broken, "+
				"so this test proves nothing", a.dataTopic)
		}
	}
	// Anything of B's that was still on its way.
	drain := time.After(2 * time.Second)
	for {
		select {
		case topic := <-got:
			for _, o := range others {
				if topic == o {
					t.Errorf("organization %d's UI login received %q, organization %d's", a.org.ID, topic, b.org.ID)
				}
			}
		case <-drain:
			return
		}
	}
}
