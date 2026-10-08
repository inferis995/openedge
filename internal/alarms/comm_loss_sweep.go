package alarms

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// StartCommLossSweep opens and closes comm_loss events from the database
// clock, every interval. The drivers judge the same rules in memory, but a
// driver restart wipes their silence clock — a tag quiet for 12 days would
// never alarm if the driver rebooted yesterday. History survives restarts,
// so the sweep re-derives the verdict from MAX(tag_history.time):
//
//   - silence >= delay and no open comm_loss event → open one (idempotent);
//   - data again and an open comm_loss event → close it.
//
// Both writers use the same open-row guard, so driver and sweep converge
// instead of duplicating.
func StartCommLossSweep(db *sql.DB, interval time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			if err := commLossSweep(db); err != nil {
				log.Printf("[ALARMS] comm_loss sweep: %v", err)
			}
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
}

func commLossSweep(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Open: every tag whose comm_loss rule is violated by the data clock.
	rows, err := db.QueryContext(ctx, `
		SELECT d.tag_id, d.id, d.severity, COALESCE(d.message, ''),
		       COALESCE(d.delay_seconds, 0)
		FROM alarm_definitions d
		WHERE d.alarm_type = 'comm_loss'`)
	if err != nil {
		return err
	}
	type rule struct {
		tagID int
		defID int
		sev   string
		msg   string
		delay int
	}
	var rules []rule
	for rows.Next() {
		var r rule
		if err := rows.Scan(&r.tagID, &r.defID, &r.sev, &r.msg, &r.delay); err == nil && r.delay > 0 {
			rules = append(rules, r)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, r := range rules {
		var last sql.NullTime
		if err := db.QueryRowContext(ctx,
			`SELECT MAX(time) FROM tag_history WHERE tag_id = $1 AND value IS NOT NULL`,
			r.tagID).Scan(&last); err != nil {
			continue
		}
		if !last.Valid {
			// Never heard from: silence counts from the tag's creation,
			// so a device mute since installation still alarms.
			if err := db.QueryRowContext(ctx,
				`SELECT created_at FROM tags WHERE id = $1`, r.tagID).Scan(&last); err != nil || !last.Valid {
				continue
			}
		}
		silence := now.Sub(last.Time.UTC())
		if silence < time.Duration(r.delay)*time.Second {
			// Data again: close any open comm_loss row for this definition.
			_, _ = db.ExecContext(ctx, `
				UPDATE alarm_events SET status = 'CLEARED', clear_time = NOW()
				WHERE tag_id = $1 AND definition_id = $2 AND status IN ('ACTIVE','ACKNOWLEDGED')
				  AND clear_time IS NULL`, r.tagID, r.defID)
			continue
		}
		msg := r.msg
		if msg == "" {
			msg = "no data received within the configured interval"
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO alarm_events (tag_id, definition_id, status, alarm_type, severity, message, value_at_trigger, trigger_time)
			SELECT $1, $2, 'ACTIVE', 'comm_loss', $3, $4, NULL, $5
			WHERE NOT EXISTS (
				SELECT 1 FROM alarm_events
				WHERE tag_id = $1 AND definition_id = $2 AND status IN ('ACTIVE','ACKNOWLEDGED') AND clear_time IS NULL
			)`, r.tagID, r.defID, r.sev, msg, now)
	}
	return nil
}
