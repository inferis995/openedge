// Package handlers — OEE history (snapshot persistiti).
//
// Il cron worker (services/core-api/main.go) chiama RecordHourly /
// RecordDaily a intervalli regolari per popolare la tabella oee_history.
// Senza questa persistenza il calcolo "OEE ultime 4 settimane per turno"
// dovrebbe scannerare tag_history ogni volta → non sostenibile.
//
// La granularità è 3-livelli:
//   - "hour"  → 1 riga per profilo per ora (24/giorno)
//   - "day"   → 1 riga aggregata per profilo per giorno
//   - "shift" → 1 riga per profilo per turno (popolato a fine turno)
//
// L'aggregazione hour → day è fatta come media aritmetica delle 4 metriche
// orarie. Per ora pesata da pieces sarebbe più accurata ma richiede
// piecesgià conosciuti — semplificazione accettabile per uno stato "buono".
package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// OEEHistoryHandler espone la lettura del rollup storico.
// La scrittura è interna (cron worker).
type OEEHistoryHandler struct {
	db  *sql.DB
	oee *OEEHandler
}

func NewOEEHistoryHandler(db *sql.DB, oee *OEEHandler) *OEEHistoryHandler {
	return &OEEHistoryHandler{db: db, oee: oee}
}

// HistoryRow è una riga del rollup persistito.
type HistoryRow struct {
	ProfileID      *int      `json:"profile_id,omitempty"`
	BucketStart    time.Time `json:"bucket_start"`
	BucketSize     string    `json:"bucket_size"`
	OEE            float64   `json:"oee"`
	Availability   float64   `json:"availability"`
	Performance    float64   `json:"performance"`
	Quality        float64   `json:"quality"`
	PlannedMin     float64   `json:"planned_min"`
	DowntimeMin    float64   `json:"downtime_min"`
	PiecesProduced float64   `json:"pieces_produced"`
	PiecesGood     float64   `json:"pieces_good"`
	ShiftID        *int      `json:"shift_id,omitempty"`
}

// History GET /api/oee/history-v2?profile_id=X&from=...&to=...&bucket=hour|day|shift
//
// Sostituisce gradualmente il legacy History() di oee.go (che ricalcolava
// 7 ricalcoli on-the-fly). Quando i dati persistiti coprono il range
// richiesto, si usa quelli; altrimenti fallback al ricalcolo "ad-hoc"
// del legacy endpoint.
func (h *OEEHistoryHandler) History(c *gin.Context) {
	bucket := c.DefaultQuery("bucket", "hour")
	if bucket != "hour" && bucket != "day" && bucket != "shift" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bucket must be hour|day|shift"})
		return
	}
	from, err := parseHistoryTime(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from': " + err.Error()})
		return
	}
	to, err := parseHistoryTime(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to': " + err.Error()})
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "'to' must be after 'from'"})
		return
	}

	// profile_id facoltativo: vuoto = NULL = rollup fabbrica.
	profileFilter := strings.TrimSpace(c.Query("profile_id"))

	var rows *sql.Rows
	if profileFilter == "" || profileFilter == "0" {
		rows, err = h.db.Query(`
			SELECT profile_id, bucket_start, bucket_size, oee, availability,
				performance, quality, planned_min, downtime_min,
				pieces_produced, pieces_good, shift_id
			FROM oee_history
			WHERE profile_id IS NULL
			  AND bucket_size = $1 AND bucket_start >= $2 AND bucket_start < $3
			ORDER BY bucket_start ASC`,
			bucket, from, to,
		)
	} else {
		rows, err = h.db.Query(`
			SELECT profile_id, bucket_start, bucket_size, oee, availability,
				performance, quality, planned_min, downtime_min,
				pieces_produced, pieces_good, shift_id
			FROM oee_history
			WHERE profile_id = $1
			  AND bucket_size = $2 AND bucket_start >= $3 AND bucket_start < $4
			ORDER BY bucket_start ASC`,
			profileFilter, bucket, from, to,
		)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	out := []HistoryRow{}
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(
			&r.ProfileID, &r.BucketStart, &r.BucketSize, &r.OEE, &r.Availability,
			&r.Performance, &r.Quality, &r.PlannedMin, &r.DowntimeMin,
			&r.PiecesProduced, &r.PiecesGood, &r.ShiftID,
		); err == nil {
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
}

// RecordHourlySnapshot salva uno snapshot orario per ogni profilo abilitato
// + un rollup fabbrica (profile_id=NULL). Chiamato dal cron worker
// all'inizio di ogni ora.
//
// L'ora "X:00" salva la finestra [X-1:00, X:00) — cioè il rollup dell'ora
// appena conclusa. Idempotente via UNIQUE(profile_id, bucket_start, bucket_size).
func RecordHourlySnapshot(db *sql.DB, oee *OEEHandler, now time.Time) error {
	bucketEnd := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	bucketStart := bucketEnd.Add(-time.Hour)

	profiles := oee.loadEnabledProfiles()

	// Snapshot per profilo
	rollupSum := struct{ a, p, q, oee, planned, downtime, prod, good float64 }{}
	count := 0
	for _, p := range profiles {
		cfg := p.config()
		cfg.WindowMin = 60 // forziamo a 1h indipendentemente dal window del profilo
		snap := oee.computeSnapshotAt(bucketEnd, cfg)
		if err := upsertHistory(db, &p.ID, bucketStart, "hour", snap, nil); err != nil {
			return err
		}
		rollupSum.a += snap.Availability
		rollupSum.p += snap.Performance
		rollupSum.q += snap.Quality
		rollupSum.oee += snap.OEE
		rollupSum.downtime += snap.CriticalDowntimeMin
		rollupSum.prod += snap.PiecesProduced
		rollupSum.good += snap.PiecesGood
		count++
	}

	// Rollup fabbrica (profile_id=NULL). Solo se ci sono profili.
	if count > 0 {
		fn := func(s float64) float64 { return s / float64(count) }
		rollup := OEESnapshot{
			OEE:                 fn(rollupSum.oee),
			Availability:        fn(rollupSum.a),
			Performance:         fn(rollupSum.p),
			Quality:             fn(rollupSum.q),
			WindowMinutes:       60,
			CriticalDowntimeMin: rollupSum.downtime,
			PiecesProduced:      rollupSum.prod,
			PiecesGood:          rollupSum.good,
		}
		if err := upsertHistory(db, nil, bucketStart, "hour", rollup, nil); err != nil {
			return err
		}
	}

	// Modalità legacy (0 profili): scriviamo comunque il rollup per non
	// avere buchi nello storico.
	if count == 0 {
		cfg := oee.legacyConfig()
		cfg.WindowMin = 60
		snap := oee.computeSnapshotAt(bucketEnd, cfg)
		if err := upsertHistory(db, nil, bucketStart, "hour", snap, nil); err != nil {
			return err
		}
	}
	return nil
}

// RecordDailySnapshot aggrega le 24 righe orarie del giorno precedente
// in una sola riga per profilo (+ rollup). Chiamato a mezzanotte UTC.
//
// Aggregazione: media aritmetica di A/P/Q/OEE; somma di pieces/downtime/planned.
// Niente "pesatura per pieces" — semplificazione accettabile.
func RecordDailySnapshot(db *sql.DB, oee *OEEHandler, now time.Time) error {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := db.Query(`
		SELECT
			profile_id,
			AVG(oee), AVG(availability), AVG(performance), AVG(quality),
			SUM(planned_min), SUM(downtime_min),
			SUM(pieces_produced), SUM(pieces_good)
		FROM oee_history
		WHERE bucket_size = 'hour'
		  AND bucket_start >= $1 AND bucket_start < $2
		GROUP BY profile_id`,
		dayStart, dayEnd,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var pid sql.NullInt64
		var s OEESnapshot
		if err := rows.Scan(
			&pid, &s.OEE, &s.Availability, &s.Performance, &s.Quality,
			// planned/downtime/pieces vanno nei campi "diagnostica"
			new(float64), &s.CriticalDowntimeMin,
			&s.PiecesProduced, &s.PiecesGood,
		); err != nil {
			continue
		}
		s.WindowMinutes = 24 * 60
		var pidPtr *int
		if pid.Valid {
			v := int(pid.Int64)
			pidPtr = &v
		}
		if err := upsertHistory(db, pidPtr, dayStart, "day", s, nil); err != nil {
			return err
		}
	}
	return nil
}

// upsertHistory è il helper INSERT...ON CONFLICT DO UPDATE per garantire
// idempotenza del cron (se gira due volte per la stessa ora, sovrascrive
// con l'ultimo calcolo).
func upsertHistory(db *sql.DB, profileID *int, bucketStart time.Time, bucketSize string, s OEESnapshot, shiftID *int) error {
	_, err := db.Exec(`
		INSERT INTO oee_history (
			profile_id, bucket_start, bucket_size,
			oee, availability, performance, quality,
			planned_min, downtime_min, pieces_produced, pieces_good, shift_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (profile_id, bucket_start, bucket_size) DO UPDATE SET
			oee = EXCLUDED.oee,
			availability = EXCLUDED.availability,
			performance = EXCLUDED.performance,
			quality = EXCLUDED.quality,
			planned_min = EXCLUDED.planned_min,
			downtime_min = EXCLUDED.downtime_min,
			pieces_produced = EXCLUDED.pieces_produced,
			pieces_good = EXCLUDED.pieces_good`,
		profileID, bucketStart, bucketSize,
		s.OEE, s.Availability, s.Performance, s.Quality,
		float64(s.WindowMinutes), s.CriticalDowntimeMin,
		s.PiecesProduced, s.PiecesGood, shiftID,
	)
	return err
}

// parseHistoryTime accetta sia RFC3339 ("2026-06-02T10:00:00Z") che
// formato data sola ("2026-06-02" → mezzanotte UTC). Comodo per la UI
// che spesso passa solo date.
func parseHistoryTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errInvalidTime
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, errInvalidTime
}

var errInvalidTime = errMsg("timestamp must be RFC3339 or YYYY-MM-DD")
