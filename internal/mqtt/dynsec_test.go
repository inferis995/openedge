package mqtt

import (
	"testing"
	"time"
)

// The lock that stopped every organization getting an MQTT account.
//
// DynsecClient had one mutex doing two jobs: serializing commands, and guarding
// the channel the subscription callback delivers replies on. send() waited for
// the reply while holding it, so the callback carrying that very reply blocked
// until the wait it was meant to end had already timed out — five seconds,
// every time, on every installation. The broker executed the commands, the API
// recorded a failure, and the log said "dynsec command timed out".
//
// This asserts the property directly rather than the symptom: while an exchange
// is in flight, the lock that guards responseCh must be free. No broker
// required — the deadlock never needed one.
// silentBroker accepts the command and never answers, which is what a real
// broker looks like for the five seconds send() is prepared to wait.
type silentBroker struct{ published chan struct{} }

func (s *silentBroker) Publish(string, interface{}) error {
	close(s.published)
	return nil
}

func TestDeliveryLockIsFreeWhileWaitingForAReply(t *testing.T) {
	broker := &silentBroker{published: make(chan struct{})}
	d := &DynsecClient{client: broker}

	// The real path: a caller takes the command lock and send() waits.
	done := make(chan error, 1)
	go func() { done <- d.CreateOrgUser(1, "Org", "org-1", "secret") }()

	select {
	case <-broker.published:
	case <-time.After(2 * time.Second):
		t.Fatal("the command was never published")
	}

	// What the subscription callback does for every inbound response. It must
	// not have to wait for the exchange it is trying to complete.
	acquired := make(chan struct{})
	go func() {
		d.mu.Lock()
		_ = d.responseCh
		d.mu.Unlock()
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("the callback cannot reach responseCh while a command is in flight — " +
			"the reply it carries can only be delivered after the command has given up, " +
			"which is every organization getting no MQTT account")
	}

	<-done // let the exchange time out rather than leaking the goroutine
}

// A reply belongs to the exchange that asked for it. Without this, a response
// that arrives after its own command timed out is handed to whichever command
// is waiting next, which then reports somebody else's result as its own.
func TestRepliesAreMatchedToTheirExchange(t *testing.T) {
	d := &DynsecClient{}
	ch := make(chan dynsecResponse, 1)

	d.mu.Lock()
	d.responseCh, d.correlationID = ch, "current"
	d.mu.Unlock()

	deliver := func(correlation string) bool {
		resp := dynsecResponse{}
		resp.Responses = append(resp.Responses, struct {
			Command         string `json:"command"`
			CorrelationData string `json:"correlationData,omitempty"`
			Error           string `json:"error,omitempty"`
		}{Command: "createClient", CorrelationData: correlation})

		d.mu.Lock()
		target, want := d.responseCh, d.correlationID
		d.mu.Unlock()
		if target == nil {
			return false
		}
		for _, r := range resp.Responses {
			if r.CorrelationData != "" && r.CorrelationData != want {
				return false
			}
		}
		select {
		case target <- resp:
			return true
		default:
			return false
		}
	}

	if deliver("stale") {
		t.Fatal("a reply from an abandoned exchange was delivered to the one waiting now")
	}
	if !deliver("current") {
		t.Fatal("the reply for this exchange was not delivered")
	}
}
