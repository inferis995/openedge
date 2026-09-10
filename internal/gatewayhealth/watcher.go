package gatewayhealth

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// Announcer is told when a gateway goes down and when it comes back.
//
// An interface rather than the notification dispatcher itself, so this package
// can be exercised without a database, a broker or an SMTP server, and so the
// thing that decides has no opinion about the thing that delivers.
type Announcer interface {
	GatewayDown(gatewayID int, name, reason string)
	GatewayUp(gatewayID int, name string)
}

// store is the database access the watcher needs, behind an interface so the
// scan can be exercised without a Postgres — the same shape internal/alarms
// uses for the same reason.
type store interface {
	// EnabledGateways returns one State per enabled gateway, with SeenSince
	// left for the watcher to fill in.
	EnabledGateways(ctx context.Context) ([]State, error)
	// RecordNotified stores what the operator has been told.
	RecordNotified(ctx context.Context, gatewayID int, notified string) error
}

// Watcher walks the enabled gateways on a timer and announces the ones whose
// state changed.
type Watcher struct {
	store     store
	announcer Announcer
	cfg       Config

	mu sync.Mutex
	// seen is when this process first laid eyes on each gateway. It is what a
	// gateway that has never reported anything is measured against: without it
	// a restart of core-api would declare every gateway lost before its driver
	// had finished starting.
	seen map[int]time.Time
}

func NewWatcher(db *sql.DB, a Announcer, cfg Config) *Watcher {
	return newWatcher(&sqlStore{db: db}, a, cfg)
}

func newWatcher(st store, a Announcer, cfg Config) *Watcher {
	return &Watcher{store: st, announcer: a, cfg: cfg, seen: make(map[int]time.Time)}
}

// sqlStore is the real thing.
type sqlStore struct{ db *sql.DB }

func (s *sqlStore) EnabledGateways(ctx context.Context) ([]State, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name,
		       COALESCE(g.health_status, ''),
		       g.health_reported_at,
		       COALESCE(g.health_notified_state, '')
		FROM gateways g
		WHERE g.enabled = true`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []State
	for rows.Next() {
		var st State
		var reportedAt sql.NullTime
		if scanErr := rows.Scan(&st.GatewayID, &st.Name, &st.Status, &reportedAt, &st.NotifiedAs); scanErr != nil {
			log.Printf("[GW-HEALTH] skipping a row: %v", scanErr)
			continue
		}
		if reportedAt.Valid {
			st.ReportedAt = reportedAt.Time
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *sqlStore) RecordNotified(ctx context.Context, gatewayID int, notified string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE gateways SET health_notified_state = $2 WHERE id = $1`, gatewayID, notified)
	return err
}

// Run scans on a timer until the context is canceled.
func (w *Watcher) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	log.Printf("[GW-HEALTH] watching gateways every %s, silence threshold %s", every, w.cfg.Silence)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Scan(time.Now()); err != nil {
				log.Printf("[GW-HEALTH] scan failed: %v", err)
			}
		}
	}
}

// pending is one decision taken during a scan, to be acted on afterwards.
type pending struct {
	state  State
	action Action
	reason string
}

// Scan judges every enabled gateway once.
//
// Every row is read before anything is announced or written. Writing through an
// open cursor on the same connection is how a scan deadlocks against itself
// under a busy pool, and announcing inside the loop would put an SMTP timeout
// in the middle of a query.
func (w *Watcher) Scan(now time.Time) error {
	states, err := w.store.EnabledGateways(context.Background())
	if err != nil {
		return err
	}

	var decisions []pending
	existing := make(map[int]bool, len(states))
	for i := range states {
		s := &states[i]
		existing[s.GatewayID] = true
		s.SeenSince = w.firstSight(s.GatewayID, now)

		if action, reason := Decide(s, w.cfg, now); action != ActionNone {
			decisions = append(decisions, pending{state: *s, action: action, reason: reason})
		}
	}

	// A gateway that has been deleted, or disabled, stops being watched; its
	// first-sight record would otherwise live as long as the process. Being
	// re-enabled therefore starts a fresh window, which is the right answer:
	// nobody wants an outage announced for the seconds between switching a
	// gateway on and its driver coming up.
	w.Forget(existing)

	for i := range decisions {
		w.apply(&decisions[i])
	}
	return nil
}

// apply records the new state first and announces second.
//
// That order is deliberate. If the announcement is what fails, the operator has
// lost one message; if the write is what fails after a successful announcement,
// the next scan announces the same outage again, and again, for as long as the
// database is unhappy — which is exactly when nobody needs a second problem.
func (w *Watcher) apply(d *pending) {
	notified := NotifiedNothing
	if d.action == ActionNotifyDown {
		notified = NotifiedDown
	}

	if err := w.store.RecordNotified(context.Background(), d.state.GatewayID, notified); err != nil {
		log.Printf("[GW-HEALTH] could not record the state of gateway %d, staying quiet rather than "+
			"repeating myself every scan: %v", d.state.GatewayID, err)
		return
	}

	if w.announcer == nil {
		return
	}
	switch d.action {
	case ActionNotifyDown:
		log.Printf("[GW-HEALTH] gateway %d (%s) is down: %s", d.state.GatewayID, d.state.Name, d.reason)
		w.announcer.GatewayDown(d.state.GatewayID, d.state.Name, d.reason)
	case ActionNotifyUp:
		log.Printf("[GW-HEALTH] gateway %d (%s) is back", d.state.GatewayID, d.state.Name)
		w.announcer.GatewayUp(d.state.GatewayID, d.state.Name)
	case ActionNone:
	}
}

// firstSight records, once, when this process first saw a gateway.
func (w *Watcher) firstSight(gatewayID int, now time.Time) time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.seen[gatewayID]; ok {
		return t
	}
	w.seen[gatewayID] = now
	return now
}

// Forget drops the first-sight record of every gateway not in the given set.
func (w *Watcher) Forget(existing map[int]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id := range w.seen {
		if !existing[id] {
			delete(w.seen, id)
		}
	}
}
