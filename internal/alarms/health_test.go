package alarms_test

import (
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/alarms"
	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// commLossDef is a "the tag went quiet for 30s" rule.
func commLossDef(id int) models.AlarmDefinition {
	return models.AlarmDefinition{
		ID:           id,
		AlarmType:    alarms.AlarmCommLoss,
		DelaySeconds: 30,
		Severity:     "critical",
		Enabled:      true,
	}
}

// frozenDef is a "the value has not moved for 60s" rule.
func frozenDef(id int, deadband float64) models.AlarmDefinition {
	return models.AlarmDefinition{
		ID:           id,
		AlarmType:    alarms.AlarmFrozen,
		Deadband:     deadband,
		DelaySeconds: 60,
		Severity:     "warning",
		Enabled:      true,
	}
}

// recorder collects the alarm transitions a manager announces.
type recorder struct{ events []string }

func (r *recorder) attach(m *alarms.Manager) {
	m.OnAlarmEvent = func(_ int, _ int, _ string, def models.AlarmDefinition, _ float64, status string) {
		r.events = append(r.events, def.AlarmType+":"+status)
	}
}

func (r *recorder) got() []string { return r.events }

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// comm_loss — the PLC stopped answering
// ---------------------------------------------------------------------------

// The defect this is really about: the alarm engine used to drop every reading
// that was not of good quality on the floor and return. A PLC that went down
// therefore produced complete silence — the one event the operator most needed.
func TestATagThatGoesQuietRaisesCommLoss(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{commLossDef(1)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 20.0, 192)

	m.TickDelays()
	if len(r.got()) != 0 {
		t.Fatalf("a tag that answered a moment ago must not alarm: %v", r.got())
	}

	m.BackdateHealth(42, 31*time.Second)
	m.TickDelays()

	if want := []string{"comm_loss:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("after 31s of silence: got %v, want %v", r.got(), want)
	}
}

// Bad quality is a report of failure, not a sign of life. If it refreshed the
// clock, a driver that keeps politely reporting "I cannot read this" would
// suppress the alarm forever.
func TestABadQualityReadingIsNotASignOfLife(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{commLossDef(1)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 20.0, 192)
	m.BackdateHealth(42, 31*time.Second)

	// The driver is still calling in — but with nothing usable.
	m.EvaluateTag(42, "temp", 0.0, 0)

	m.TickDelays()
	if want := []string{"comm_loss:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("bad-quality readings must not count as contact: got %v, want %v", r.got(), want)
	}
}

func TestCommLossClearsWhenTheTagAnswersAgain(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{commLossDef(1)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 20.0, 192)
	m.BackdateHealth(42, 31*time.Second)
	m.TickDelays()

	m.EvaluateTag(42, "temp", 21.0, 192)
	m.TickDelays()

	want := []string{"comm_loss:ACTIVE", "comm_loss:CLEARED"}
	if !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
	if n := m.ActiveCount(42); n != 0 {
		t.Errorf("track left behind after the clear: %d active", n)
	}
}

// A gateway that starts up must not report every tag as lost before it has had
// a chance to read any of them. "Never heard from" is not a silence that began
// at the zero time.
func TestAFreshlyStartedGatewayDoesNotAlarmImmediately(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{commLossDef(1)})
	var r recorder
	r.attach(m)

	m.TickDelays()
	m.TickDelays()
	if len(r.got()) != 0 {
		t.Fatalf("startup must be given the full timeout: %v", r.got())
	}

	// But a gateway started next to a genuinely dead PLC still alarms, one
	// timeout later.
	m.BackdateHealth(42, 31*time.Second)
	m.TickDelays()
	if want := []string{"comm_loss:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

// One alarm per outage. tickDelays runs every second and must not re-announce
// a condition that is simply still true.
func TestCommLossIsAnnouncedOnceNotEverySecond(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{commLossDef(1)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 20.0, 192)
	m.BackdateHealth(42, 31*time.Second)
	for i := 0; i < 5; i++ {
		m.TickDelays()
	}

	if want := []string{"comm_loss:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

// ---------------------------------------------------------------------------
// frozen — the readings arrive, but the process has stopped moving
// ---------------------------------------------------------------------------

func TestAValueThatStopsMovingRaisesFrozen(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 0)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.TickDelays()
	if len(r.got()) != 0 {
		t.Fatalf("one sample is not enough to call a value frozen: %v", r.got())
	}

	// A minute goes by and the counter keeps reporting the same number.
	m.BackdateHealth(42, 61*time.Second)
	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.TickDelays()

	if want := []string{"frozen:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

func TestFrozenClearsAsSoonAsTheValueMoves(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 0)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.BackdateHealth(42, 61*time.Second)
	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.TickDelays()

	m.EvaluateTag(42, "watchdog", 8.0, 192)
	m.TickDelays()

	want := []string{"frozen:ACTIVE", "frozen:CLEARED"}
	if !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

// The deadband decides what counts as movement. Sensor noise inside the band is
// not the process moving, and a tag that only dithers is still frozen.
func TestNoiseInsideTheDeadbandIsNotMovement(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 1.0)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "level", 50.0, 192)
	m.BackdateHealth(42, 61*time.Second)
	m.EvaluateTag(42, "level", 50.5, 192) // inside the band
	m.TickDelays()

	if want := []string{"frozen:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("0.5 of noise with a 1.0 deadband is not movement: got %v, want %v", r.got(), want)
	}

	m.EvaluateTag(42, "level", 52.0, 192) // outside the band
	m.TickDelays()

	want := []string{"frozen:ACTIVE", "frozen:CLEARED"}
	if !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

// The anchor is the last accepted change, not the previous sample: a value
// creeping up by less than the deadband every poll is still moving, and must
// eventually be recognized as such.
func TestASlowDriftIsMovementNotAFreeze(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 1.0)})
	var r recorder
	r.attach(m)

	// Ten steps of 0.5 — each one alone is inside the deadband.
	v := 50.0
	for i := 0; i < 10; i++ {
		m.BackdateHealth(42, 10*time.Second)
		v += 0.5
		m.EvaluateTag(42, "level", v, 192)
		m.TickDelays()
	}

	if len(r.got()) != 0 {
		t.Fatalf("a drifting value is not frozen: %v", r.got())
	}
}

// A tag nobody is hearing from is mute, not frozen. Reporting both alarms for a
// single unplugged cable would just double the operator's work.
func TestAMuteTagIsNotReportedAsFrozen(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 0)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.BackdateHealth(42, 61*time.Second) // nothing has arrived since
	m.TickDelays()

	if len(r.got()) != 0 {
		t.Fatalf("silence must be reported as comm_loss, not as a freeze: %v", r.got())
	}
}

// ---------------------------------------------------------------------------
// Health rules must not be judged against incoming samples
// ---------------------------------------------------------------------------

// The value path knows nothing about these two types: isConditionViolated
// returns false for them, and isCleared returns true. Letting a health rule
// reach that code makes every single sample look like a recovery, so a frozen
// alarm would clear on the next poll and re-fire a minute later, forever.
func TestAFrozenAlarmSurvivesTheSamplesThatKeepArriving(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 0)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.BackdateHealth(42, 61*time.Second)
	m.EvaluateTag(42, "watchdog", 7.0, 192)
	m.TickDelays()
	if want := []string{"frozen:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("setup: got %v, want %v", r.got(), want)
	}

	// The stuck sensor keeps reporting the same number, as stuck sensors do.
	for i := 0; i < 5; i++ {
		m.EvaluateTag(42, "watchdog", 7.0, 192)
	}

	if want := []string{"frozen:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("the alarm cleared on a sample that changed nothing: got %v, want %v", r.got(), want)
	}
	if n := m.ActiveCount(42); n != 1 {
		t.Errorf("active tracks = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Rules that carry no delay, and rules that go away
// ---------------------------------------------------------------------------

// Zero means "not configured" for these two types, and taking it literally
// would alarm on the first poll that was a millisecond late.
func TestAHealthRuleWithNoDelayFallsBackToTheDefaultTimeout(t *testing.T) {
	def := models.AlarmDefinition{ID: 1, AlarmType: alarms.AlarmCommLoss, DelaySeconds: 0}
	if got := alarms.HealthTimeout(&def); got != 60*time.Second {
		t.Fatalf("healthTimeout with no delay = %v, want 60s", got)
	}

	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{def})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 20.0, 192)
	m.BackdateHealth(42, 30*time.Second)
	m.TickDelays()
	if len(r.got()) != 0 {
		t.Fatalf("30s of silence must not trip a 60s default: %v", r.got())
	}

	m.BackdateHealth(42, 31*time.Second)
	m.TickDelays()
	if want := []string{"comm_loss:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}

// A gateway whose alarms are edited often must not accumulate the state of
// rules that no longer exist.
func TestHealthStateGoesAwayWithTheRuleItBelongsTo(t *testing.T) {
	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{frozenDef(1, 0), frozenDef(2, 0)})

	m.EvaluateTag(42, "watchdog", 7.0, 192)
	if tags, anchors := m.HealthStateSize(42); tags != 1 || anchors != 2 {
		t.Fatalf("after two frozen rules: tags=%d anchors=%d, want 1 and 2", tags, anchors)
	}

	// One rule deleted.
	m.PruneHealthState(map[int][]models.AlarmDefinition{42: {frozenDef(1, 0)}})
	if tags, anchors := m.HealthStateSize(42); tags != 1 || anchors != 1 {
		t.Fatalf("after deleting one rule: tags=%d anchors=%d, want 1 and 1", tags, anchors)
	}

	// Both gone: the tag keeps no health state at all.
	m.PruneHealthState(map[int][]models.AlarmDefinition{42: {}})
	if tags, _ := m.HealthStateSize(42); tags != 0 {
		t.Fatalf("after deleting every health rule: tags=%d, want 0", tags)
	}
}

// ---------------------------------------------------------------------------
// The value rules must be left exactly as they were
// ---------------------------------------------------------------------------

func TestOnlyTheTwoHealthTypesAreTreatedAsHealthRules(t *testing.T) {
	health := []string{"comm_loss", "frozen"}
	value := []string{"high", "high_high", "low", "low_low", "bool_true", "bool_false", ""}

	for _, tp := range health {
		if !alarms.IsHealthAlarm(tp) {
			t.Errorf("%q should be a health rule", tp)
		}
	}
	for _, tp := range value {
		if alarms.IsHealthAlarm(tp) {
			t.Errorf("%q must keep going through the value path", tp)
		}
	}
}

// A value rule and a health rule on the same tag are independent: the arriving
// samples drive the first, the clock drives the second.
func TestAValueRuleAndAHealthRuleCoexistOnTheSameTag(t *testing.T) {
	threshold := 100.0
	high := models.AlarmDefinition{
		ID: 1, AlarmType: "high", Threshold: &threshold, Deadband: 5, DelaySeconds: 0, Enabled: true,
	}

	m := alarms.NewTestManager()
	m.SetDefinitions(42, []models.AlarmDefinition{high, commLossDef(2)})
	var r recorder
	r.attach(m)

	m.EvaluateTag(42, "temp", 101.0, 192)
	if want := []string{"high:ACTIVE"}; !equal(r.got(), want) {
		t.Fatalf("the threshold rule must still fire: got %v, want %v", r.got(), want)
	}

	// The PLC then dies with the value still over the threshold. The high alarm
	// stays up — nothing says it recovered — and the outage is reported too.
	m.BackdateHealth(42, 31*time.Second)
	m.TickDelays()

	want := []string{"high:ACTIVE", "comm_loss:ACTIVE"}
	if !equal(r.got(), want) {
		t.Fatalf("got %v, want %v", r.got(), want)
	}
}
