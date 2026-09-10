package gatewayhealth_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/gatewayhealth"
)

// spy records what the operator would have been told.
type spy struct{ said []string }

func (s *spy) GatewayDown(id int, name, reason string) {
	s.said = append(s.said, fmt.Sprintf("down:%d:%s", id, reason))
}
func (s *spy) GatewayUp(id int, name string) {
	s.said = append(s.said, fmt.Sprintf("up:%d", id))
}

func oneGateway(status string, reportedAt time.Time) *gatewayhealth.FakeStore {
	return &gatewayhealth.FakeStore{States: []gatewayhealth.State{{
		GatewayID:  7,
		Name:       "PLC linea 1",
		Status:     status,
		ReportedAt: reportedAt,
	}}}
}

// The watcher runs once a minute. An outage that lasts a weekend must produce
// one message, not one every minute for sixty hours.
func TestAnOutageIsAnnouncedOncePerOutage(t *testing.T) {
	st := oneGateway(gatewayhealth.StatusOffline, now().Add(-time.Minute))
	var s spy
	w := gatewayhealth.NewTestWatcher(st, &s, cfg)

	for i := 0; i < 10; i++ {
		at := now().Add(time.Duration(i) * time.Minute)
		st.SetStatus(7, gatewayhealth.StatusOffline, at)
		if err := w.Scan(at); err != nil {
			t.Fatal(err)
		}
	}

	if len(s.said) != 1 {
		t.Fatalf("ten scans over one outage produced %d messages: %v", len(s.said), s.said)
	}
	if s.said[0] != "down:7:il driver segnala il gateway offline" {
		t.Errorf("message = %q", s.said[0])
	}
}

func TestARecoveryIsAnnouncedOnceToo(t *testing.T) {
	st := oneGateway(gatewayhealth.StatusOffline, now().Add(-time.Minute))
	var s spy
	w := gatewayhealth.NewTestWatcher(st, &s, cfg)

	if err := w.Scan(now()); err != nil {
		t.Fatal(err)
	}

	// The driver comes back and keeps reporting, every ten seconds, as the real
	// ones do. A fake that fell silent after one message would go on to be
	// announced as lost, which is correct behavior and not what this test is
	// about.
	for i := 1; i < 5; i++ {
		at := now().Add(time.Duration(i) * time.Minute)
		st.SetStatus(7, gatewayhealth.StatusOnline, at)
		if err := w.Scan(at); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"down:7:il driver segnala il gateway offline", "up:7"}
	if len(s.said) != len(want) || s.said[0] != want[0] || s.said[1] != want[1] {
		t.Fatalf("got %v, want %v", s.said, want)
	}
}

// The write comes first on purpose. If the announcement were sent and the write
// then failed, every later scan would announce the same outage again — an
// unhappy database would become a message a minute, which is the last thing
// anybody needs during one.
func TestNothingIsAnnouncedWhenTheStateCannotBeRecorded(t *testing.T) {
	st := oneGateway(gatewayhealth.StatusOffline, now().Add(-time.Minute))
	st.FailWrites(true)
	var s spy
	w := gatewayhealth.NewTestWatcher(st, &s, cfg)

	for i := 0; i < 5; i++ {
		at := now().Add(time.Duration(i) * time.Minute)
		st.SetStatus(7, gatewayhealth.StatusOffline, at)
		if err := w.Scan(at); err != nil {
			t.Fatal(err)
		}
	}

	if len(s.said) != 0 {
		t.Fatalf("with the database refusing writes the watcher said %d things, and would "+
			"keep saying them forever: %v", len(s.said), s.said)
	}

	// And once the database recovers, the outage is announced — once. The
	// driver is still reporting "offline", so what is announced is the outage
	// it reports and not the silence of a fake that stopped talking.
	st.FailWrites(false)
	for i := 5; i < 7; i++ {
		at := now().Add(time.Duration(i) * time.Minute)
		st.SetStatus(7, gatewayhealth.StatusOffline, at)
		if err := w.Scan(at); err != nil {
			t.Fatal(err)
		}
	}
	if s.said[0] != "down:7:il driver segnala il gateway offline" {
		t.Errorf("message = %q, want the reported outage rather than silence", s.said[0])
	}
	if len(s.said) != 1 {
		t.Fatalf("after the database recovered: %d messages, want 1: %v", len(s.said), s.said)
	}
}

// A gateway that has never said anything is measured from the moment the
// watcher first saw it, so a core-api that has just started does not declare a
// whole plant lost before the drivers have finished coming up.
func TestAFreshlyStartedWatcherStaysQuietForOneWindow(t *testing.T) {
	st := &gatewayhealth.FakeStore{States: []gatewayhealth.State{
		{GatewayID: 7, Name: "PLC linea 1"}, // nothing ever reported
	}}
	var s spy
	w := gatewayhealth.NewTestWatcher(st, &s, cfg)

	if err := w.Scan(now()); err != nil {
		t.Fatal(err)
	}
	if err := w.Scan(now().Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(s.said) != 0 {
		t.Fatalf("the watcher announced an outage inside its own start-up window: %v", s.said)
	}

	// A gateway whose driver never comes up is still a fault.
	if err := w.Scan(now().Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(s.said) != 1 {
		t.Fatalf("a gateway that never reported for four minutes produced %d messages: %v",
			len(s.said), s.said)
	}
}

// The first-sight map must not grow for the life of the process.
func TestGatewaysThatGoAwayAreForgotten(t *testing.T) {
	st := &gatewayhealth.FakeStore{States: []gatewayhealth.State{
		{GatewayID: 1, Name: "a", Status: gatewayhealth.StatusOnline, ReportedAt: now()},
		{GatewayID: 2, Name: "b", Status: gatewayhealth.StatusOnline, ReportedAt: now()},
		{GatewayID: 3, Name: "c", Status: gatewayhealth.StatusOnline, ReportedAt: now()},
	}}
	w := gatewayhealth.NewTestWatcher(st, &spy{}, cfg)

	if err := w.Scan(now()); err != nil {
		t.Fatal(err)
	}
	if got := w.SeenCount(); got != 3 {
		t.Fatalf("remembered %d gateways, want 3", got)
	}

	st.Remove(2)
	if err := w.Scan(now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := w.SeenCount(); got != 2 {
		t.Fatalf("after deleting a gateway the watcher still remembers %d, want 2", got)
	}
}

// Several gateways are judged independently: one plant going down must not
// silence the rest, and must not speak for them either.
func TestGatewaysAreJudgedOneByOne(t *testing.T) {
	st := &gatewayhealth.FakeStore{States: []gatewayhealth.State{
		{GatewayID: 1, Name: "sano", Status: gatewayhealth.StatusOnline, ReportedAt: now()},
		{GatewayID: 2, Name: "giù", Status: gatewayhealth.StatusOffline, ReportedAt: now()},
		{GatewayID: 3, Name: "errore", Status: gatewayhealth.StatusError, ReportedAt: now()},
	}}
	var s spy
	w := gatewayhealth.NewTestWatcher(st, &s, cfg)

	if err := w.Scan(now()); err != nil {
		t.Fatal(err)
	}

	if len(s.said) != 2 {
		t.Fatalf("got %d messages, want 2 (one per broken gateway): %v", len(s.said), s.said)
	}
	if s.said[0] != "down:2:il driver segnala il gateway offline" {
		t.Errorf("first message = %q", s.said[0])
	}
	if s.said[1] != "down:3:il driver segnala un errore di comunicazione" {
		t.Errorf("second message = %q", s.said[1])
	}
}
