package notifications

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToMax(t *testing.T) {
	r := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !r.allow() {
			t.Fatalf("event %d rejected; the first %d must always pass", i, 3)
		}
	}
	if r.allow() {
		t.Fatalf("event 4 must be rejected (over budget)")
	}
}

func TestRateLimiterReleasesAfterWindow(t *testing.T) {
	// 25 ms window — short enough for a fast unit test, long enough that
	// scheduling jitter on slow CI doesn't make the test flake.
	r := newRateLimiter(1, 25*time.Millisecond)
	if !r.allow() {
		t.Fatal("first event rejected")
	}
	if r.allow() {
		t.Fatal("second event in window must be rejected")
	}
	time.Sleep(35 * time.Millisecond)
	if !r.allow() {
		t.Fatal("event after window must be allowed")
	}
}

func TestSeverityRankUnknownDefaultsToZero(t *testing.T) {
	// Unknown severities should pass through the minSeverity gate
	// (return 0) — better to over-notify than to silence a real alarm
	// because the driver typo'd "criitcal".
	cases := map[string]int{
		"low": 0, "LOW": 0, " low ": 0,
		"medium": 1, "Medium": 1,
		"high": 2, "critical": 3,
		"": 0, "unknown-future-value": 0,
	}
	for in, want := range cases {
		if got := severityRank(in); got != want {
			t.Errorf("severityRank(%q) = %d, want %d", in, got, want)
		}
	}
}
