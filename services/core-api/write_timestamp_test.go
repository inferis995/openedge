package main

import "testing"

// External callers of sys/write send their timestamp in seconds or in
// milliseconds. Read as milliseconds, a seconds value dates the command to
// January 1970 and it is refused as expired every single time.
func TestExternalTimestampsInSecondsOrMillisecondsBothWork(t *testing.T) {
	cases := map[int64]int64{
		1_790_000_000:     1_790_000_000_000, // seconds
		1_790_000_000_000: 1_790_000_000_000, // milliseconds
	}
	for in, want := range cases {
		if got := normalizeMillis(in); got != want {
			t.Errorf("normalizeMillis(%d) = %d, want %d", in, got, want)
		}
	}
}
