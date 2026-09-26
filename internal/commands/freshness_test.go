package commands

import (
	"strings"
	"testing"
	"time"
)

var now = time.UnixMilli(1_790_000_000_000)

func ms(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

// The scenario the whole package exists for: a setpoint typed while the link
// to an edge box was down, delivered hours later when it came back.
func TestACommandFromHoursAgoIsRefused(t *testing.T) {
	if ok, _ := Fresh(ms(4*time.Hour), now, DefaultMaxAge); ok {
		t.Fatal("a command issued four hours ago was accepted; it would be written to " +
			"the machine now, as if an operator had just decided it")
	}
}

func TestAFreshCommandIsAccepted(t *testing.T) {
	for _, age := range []time.Duration{0, time.Second, 29 * time.Second} {
		if ok, _ := Fresh(ms(age), now, DefaultMaxAge); !ok {
			t.Errorf("a command %s old was refused with a %s validity", age, DefaultMaxAge)
		}
	}
}

func TestTheValidityIsTheBoundary(t *testing.T) {
	if ok, _ := Fresh(ms(30*time.Second), now, 30*time.Second); !ok {
		t.Error("a command exactly at the validity was refused")
	}
	if ok, _ := Fresh(ms(31*time.Second), now, 30*time.Second); ok {
		t.Error("a command one second past the validity was accepted")
	}
}

// A clock far ahead can make a stale command look fresh just as easily as a
// clock behind makes a fresh one look stale. Refuse, and say which it was.
func TestACommandDatedInTheFutureIsRefused(t *testing.T) {
	ok, age := Fresh(now.Add(10*time.Minute).UnixMilli(), now, DefaultMaxAge)
	if ok {
		t.Fatal("a command dated ten minutes in the future was accepted")
	}
	if !strings.Contains(RefusalMessage(age, DefaultMaxAge), "clock") {
		t.Errorf("the message for a future command does not point at the clocks: %q",
			RefusalMessage(age, DefaultMaxAge))
	}
}

// A little skew is normal and must not refuse good commands.
func TestSmallClockSkewIsTolerated(t *testing.T) {
	if ok, _ := Fresh(now.Add(2*time.Second).UnixMilli(), now, DefaultMaxAge); !ok {
		t.Fatal("a command two seconds 'in the future' was refused; ordinary clock " +
			"skew would stop every write")
	}
}

// Rolling upgrade: an old core-api stamps nothing.
func TestAnUnstampedCommandIsAccepted(t *testing.T) {
	if ok, _ := Fresh(0, now, DefaultMaxAge); !ok {
		t.Fatal("an unstamped command was refused; every write would stop while " +
			"core-api and the drivers are on different versions")
	}
}

func TestTheValidityCannotBeSwitchedOffOrMadeUseless(t *testing.T) {
	cases := map[int]time.Duration{
		0:   DefaultMaxAge,
		-1:  DefaultMaxAge,
		1:   MinMaxAge,
		60:  60 * time.Second,
		600: 600 * time.Second,
	}
	for in, want := range cases {
		if got := MaxAgeFromSeconds(in); got != want {
			t.Errorf("MaxAgeFromSeconds(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestTheOperatorIsToldWhatHappened(t *testing.T) {
	msg := RefusalMessage(4*time.Hour, DefaultMaxAge)
	for _, want := range []string{"expired", "not executed", "4h0m0s"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message %q does not say %q", msg, want)
		}
	}
}

func TestMaxAgeFromANilDatabaseIsTheDefault(t *testing.T) {
	if got := MaxAgeFromDB(t.Context(), nil); got != DefaultMaxAge {
		t.Fatalf("got %s with no database, want the default %s — the check must "+
			"never be skipped because the setting could not be read", got, DefaultMaxAge)
	}
}
