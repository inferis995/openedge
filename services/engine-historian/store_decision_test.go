package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// shouldStoreValue decides whether a reading from a plant is written down or
// thrown away. It is the most consequential piece of pure logic in this
// service, and it had no tests at all.
//
// What makes it worth this many: when it is wrong it does not produce an error.
// It produces an ABSENCE — a gap in the historian, which on a chart reads as a
// quiet process rather than as a fault. Nobody goes looking for data that was
// never written.

// fakeCache stands in for the Redis keys the decision depends on.
type fakeCache struct{ values map[string]string }

func (f *fakeCache) Get(key string) (string, error) {
	v, ok := f.values[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

// historian builds a service whose only live part is the cache.
func historian(t *testing.T, entries map[string]string) *HistorianService {
	t.Helper()
	if entries == nil {
		entries = map[string]string{}
	}
	return &HistorianService{cache: &fakeCache{values: entries}}
}

// previously records a tag's last stored value and quality, as
// storePreviousValue writes them.
func previously(tagID int, value interface{}, quality int) (string, string) {
	b, _ := json.Marshal(PreviousValue{Value: value, Quality: quality})
	return fmt.Sprintf("prev_value:%d", tagID), string(b)
}

func lastStored(tagID int, ago time.Duration) (string, string) {
	return fmt.Sprintf("last_store:%d", tagID),
		fmt.Sprintf("%d", time.Now().Add(-ago).UnixMilli())
}

func tag(deadband float64) *TagInfo {
	return &TagInfo{ID: 42, HistorizeDeadband: deadband}
}

// ---------------------------------------------------------------------------
// The first sample, and quality
// ---------------------------------------------------------------------------

// A tag nobody has heard from has nothing to compare against. Dropping its
// first reading would leave a tag that looks dead until it happens to change.
func TestTheFirstReadingOfATagIsAlwaysKept(t *testing.T) {
	s := historian(t, nil) // nothing cached at all
	if !s.shouldStoreValue(tag(5), 20.0, 0) {
		t.Fatal("the first reading of a tag was thrown away")
	}
}

// Bad and uncertain readings are kept so a chart shows a visible gap rather
// than a straight line across a period when nothing was actually known.
// Every other rule in the function is deliberately made inert here: the cached
// quality equals the incoming one (so the transition rule cannot fire), the
// value is unchanged (so the change rule cannot fire), the tag has no deadband
// and its last write was a second ago (so the 30-second floor cannot fire).
// Only the quality guard on the first line can return true.
func TestABadReadingIsAlwaysKept(t *testing.T) {
	for _, quality := range []int{1, 2} {
		k, v := previously(42, 20.0, quality)
		last, lv := lastStored(42, time.Second)
		s := historian(t, map[string]string{k: v, last: lv})

		if !s.shouldStoreValue(tag(0), 20.0, quality) {
			t.Errorf("a reading of quality %d was thrown away; the chart would draw a "+
				"straight line across a period when nothing was known", quality)
		}
	}
}

// The moment a tag recovers is the most interesting sample it will produce all
// day, and it is the one the deadband would discard: the value has not moved,
// only its trustworthiness has.
func TestARecoveryIsKeptEvenWhenTheValueHasNotMoved(t *testing.T) {
	k, v := previously(42, 20.0, 2) // was bad
	last, lv := lastStored(42, time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if !s.shouldStoreValue(tag(5), 20.0, 0) { // same value, now good
		t.Fatal("a tag coming back to good quality was thrown away because its value " +
			"had not changed")
	}
}

// ---------------------------------------------------------------------------
// On-change storage, with no deadband configured
// ---------------------------------------------------------------------------

func TestWithNoDeadbandAnUnchangedValueIsDropped(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if s.shouldStoreValue(tag(0), 20.0, 0) {
		t.Fatal("an unchanged value was written; report-by-exception does nothing")
	}
}

func TestWithNoDeadbandAChangedValueIsKept(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if !s.shouldStoreValue(tag(0), 20.1, 0) {
		t.Fatal("a changed value was thrown away")
	}
}

// ---------------------------------------------------------------------------
// The configured deadband
// ---------------------------------------------------------------------------

func TestAMovementInsideTheDeadbandIsDropped(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if s.shouldStoreValue(tag(5), 22.0, 0) {
		t.Fatal("a movement of 2.0 was written despite a deadband of 5.0")
	}
}

func TestAMovementBeyondTheDeadbandIsKept(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if !s.shouldStoreValue(tag(5), 26.0, 0) {
		t.Fatal("a movement of 6.0 was dropped despite a deadband of 5.0")
	}
}

// ---------------------------------------------------------------------------
// The thirty-second floor
// ---------------------------------------------------------------------------

// Every historised tag is written at least this often, whatever the deadband
// says. It is deliberate — the code calls it a production fix — and it is
// pinned here because it is also what makes historize_deadband weaker than an
// operator reading that field would expect: a motionless tag still produces
// 2,880 rows a day.
func TestEveryTagIsWrittenAtLeastEveryThirtySeconds(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, 31*time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if !s.shouldStoreValue(tag(5), 20.0, 0) {
		t.Error("with a deadband: an unchanged tag was not written after 31 seconds")
	}
	if !s.shouldStoreValue(tag(0), 20.0, 0) {
		t.Error("with no deadband: an unchanged tag was not written after 31 seconds")
	}
}

// And a tag written a moment ago is not written again, or the floor would
// become a ceiling and every sample would be stored.
func TestATagWrittenAMomentAgoIsNotWrittenAgain(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	last, lv := lastStored(42, 2*time.Second)
	s := historian(t, map[string]string{k: v, last: lv})

	if s.shouldStoreValue(tag(5), 20.0, 0) {
		t.Fatal("an unchanged tag written two seconds ago was written again")
	}
}

// A tag with a previous value but no record of when it was last written is in
// an unknown state, and the safe answer is to write: losing a sample is worse
// than writing one too many.
func TestWithNoRecordOfTheLastWriteTheSampleIsKept(t *testing.T) {
	k, v := previously(42, 20.0, 0)
	s := historian(t, map[string]string{k: v}) // no last_store key

	if !s.shouldStoreValue(tag(5), 20.0, 0) {
		t.Fatal("a tag with no record of its last write was skipped")
	}
}

// Cached rubbish must not silently discard a plant's data.
func TestAnUnreadableCacheEntryDoesNotCostASample(t *testing.T) {
	s := historian(t, map[string]string{"prev_value:42": "{not json"})
	if !s.shouldStoreValue(tag(5), 20.0, 0) {
		t.Fatal("a corrupt cache entry caused the reading to be thrown away")
	}
}
