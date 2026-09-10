package gatewayhealth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/gatewayhealth"
)

var cfg = gatewayhealth.Config{Silence: 3 * time.Minute}

func now() time.Time { return time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC) }

// healthy is a gateway that reported "online" a moment ago and about which
// nothing has been said.
func healthy() gatewayhealth.State {
	return gatewayhealth.State{
		GatewayID:  7,
		Name:       "PLC linea 1",
		Status:     gatewayhealth.StatusOnline,
		ReportedAt: now().Add(-10 * time.Second),
		SeenSince:  now().Add(-time.Hour),
		NotifiedAs: gatewayhealth.NotifiedNothing,
	}
}

func TestAHealthyGatewayProducesNothing(t *testing.T) {
	h := healthy()
	action, _ := gatewayhealth.Decide(&h, cfg, now())
	if action != gatewayhealth.ActionNone {
		t.Fatalf("action = %v, want none", action)
	}
}

func TestADriverReportingOfflineIsAnnouncedAtOnce(t *testing.T) {
	s := healthy()
	s.Status = gatewayhealth.StatusOffline

	action, reason := gatewayhealth.Decide(&s, cfg, now())
	if action != gatewayhealth.ActionNotifyDown {
		t.Fatalf("action = %v, want notify-down", action)
	}
	if !strings.Contains(reason, "offline") {
		t.Errorf("the reason should say what the driver reported, got %q", reason)
	}
}

// "error" is a driver that is running and cannot reach the PLC. It used to be
// dropped on the floor between the driver and the platform, which left the
// worst of the three states as the only invisible one.
func TestADriverReportingErrorIsAnnouncedToo(t *testing.T) {
	s := healthy()
	s.Status = gatewayhealth.StatusError

	action, reason := gatewayhealth.Decide(&s, cfg, now())
	if action != gatewayhealth.ActionNotifyDown {
		t.Fatalf("action = %v, want notify-down", action)
	}
	if !strings.Contains(reason, "errore") {
		t.Errorf("the reason should distinguish an error from a clean offline, got %q", reason)
	}
}

// The reason has to tell the two failures apart: one sends somebody to the
// PLC, the other to the machine running the driver.
func TestSilenceAndAReportedOutageDoNotReadTheSame(t *testing.T) {
	offline := healthy()
	offline.Status = gatewayhealth.StatusOffline
	_, offlineReason := gatewayhealth.Decide(&offline, cfg, now())

	silent := healthy()
	silent.ReportedAt = now().Add(-10 * time.Minute)
	_, silentReason := gatewayhealth.Decide(&silent, cfg, now())

	if offlineReason == silentReason {
		t.Fatalf("both failures produced the same sentence: %q", offlineReason)
	}
	if !strings.Contains(silentReason, "segnale di vita") {
		t.Errorf("silence should say so, got %q", silentReason)
	}
}

// A retained "online" outlives the driver that published it: the last-will only
// fires when the BROKER notices, and a broker that was itself restarted never
// notices anything. Silence is the only check that does not trust the broker.
func TestAGatewayStuckOnOnlineIsStillCaught(t *testing.T) {
	s := healthy()
	s.Status = gatewayhealth.StatusOnline
	s.ReportedAt = now().Add(-30 * time.Minute)

	action, reason := gatewayhealth.Decide(&s, cfg, now())
	if action != gatewayhealth.ActionNotifyDown {
		t.Fatalf("a gateway that says 'online' and has said nothing for half an hour "+
			"produced %v; a retained message is not a heartbeat", action)
	}
	if !strings.Contains(reason, "30 minuti") {
		t.Errorf("the reason should carry how long, got %q", reason)
	}
}

func TestSilenceShorterThanTheThresholdIsTolerated(t *testing.T) {
	s := healthy()
	s.ReportedAt = now().Add(-2 * time.Minute) // under the 3-minute threshold

	if action, _ := gatewayhealth.Decide(&s, cfg, now()); action != gatewayhealth.ActionNone {
		t.Fatalf("a gateway that spoke two minutes ago produced %v", action)
	}
}

// One message per outage. The watcher runs on a timer and must not turn a
// weekend-long outage into a message a minute.
func TestAnOutageIsAnnouncedOnce(t *testing.T) {
	s := healthy()
	s.Status = gatewayhealth.StatusOffline
	s.NotifiedAs = gatewayhealth.NotifiedDown

	if action, _ := gatewayhealth.Decide(&s, cfg, now()); action != gatewayhealth.ActionNone {
		t.Fatalf("an outage already announced produced %v again", action)
	}
}

func TestComingBackIsAnnouncedOnceToo(t *testing.T) {
	s := healthy()
	s.NotifiedAs = gatewayhealth.NotifiedDown

	action, _ := gatewayhealth.Decide(&s, cfg, now())
	if action != gatewayhealth.ActionNotifyUp {
		t.Fatalf("action = %v, want notify-up", action)
	}

	// And once the recovery has been recorded, nothing more.
	s.NotifiedAs = gatewayhealth.NotifiedNothing
	if action, _ := gatewayhealth.Decide(&s, cfg, now()); action != gatewayhealth.ActionNone {
		t.Fatalf("a gateway that recovered produced %v on the next pass", action)
	}
}

// A gateway that has never reported is not proof of a fault: core-api may have
// started thirty seconds ago and the drivers may still be coming up. It is
// measured from when the watcher first saw it, exactly like a silent one.
func TestAGatewayThatHasNeverReportedGetsTheFullWindowFirst(t *testing.T) {
	s := gatewayhealth.State{
		GatewayID: 7,
		Name:      "PLC linea 1",
		Status:    "",
		SeenSince: now().Add(-30 * time.Second),
	}
	if action, _ := gatewayhealth.Decide(&s, cfg, now()); action != gatewayhealth.ActionNone {
		t.Fatalf("a gateway seen 30 seconds ago produced %v before it could report", action)
	}

	// But a gateway that never comes up is still a fault, one window later.
	s.SeenSince = now().Add(-10 * time.Minute)
	action, reason := gatewayhealth.Decide(&s, cfg, now())
	if action != gatewayhealth.ActionNotifyDown {
		t.Fatalf("a gateway that never reported in ten minutes produced %v", action)
	}
	if !strings.Contains(reason, "segnale di vita") {
		t.Errorf("reason = %q", reason)
	}
}

// A zero threshold switches silence detection off; the explicitly reported
// states must keep working, or turning the check down would turn it off.
func TestWithSilenceDisabledTheReportedStatesStillCount(t *testing.T) {
	off := gatewayhealth.Config{Silence: 0}

	s := healthy()
	s.ReportedAt = now().Add(-90 * time.Minute)
	if action, _ := gatewayhealth.Decide(&s, off, now()); action != gatewayhealth.ActionNone {
		t.Fatalf("silence detection is off, yet silence produced %v", action)
	}

	s.Status = gatewayhealth.StatusOffline
	if action, _ := gatewayhealth.Decide(&s, off, now()); action != gatewayhealth.ActionNotifyDown {
		t.Fatalf("a reported outage produced %v with silence detection off", action)
	}
}
