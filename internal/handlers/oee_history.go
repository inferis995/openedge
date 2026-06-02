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
	"github.com/lib/pq"
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

// ShiftRollupRow è una riga del rollup OEE-per-turno: snapshot orari
// aggregati per (turno, data) — la classica matrice "OEE turno mattina
// ultimi 30 giorni" della direzione di produzione.
type ShiftRollupRow struct {
	ProfileID    *int      `json:"profile_id,omitempty"`
	ShiftID      int       `json:"shift_id"`
	ShiftName    string    `json:"shift_name"`
	Date         string    `json:"date"` // YYYY-MM-DD
	OEE          float64   `json:"oee"`
	Availability float64   `json:"availability"`
	Performance  float64   `json:"performance"`
	Quality      float64   `json:"quality"`
	Hours        int       `json:"hours"` // n. ore di snapshot aggregate (max ~ durata turno)
	BucketStart  time.Time `json:"bucket_start"`
}

// ByShift GET /api/oee/by-shift?profile_id=X&from=Y&to=Z
//
// Aggrega oee_history (bucket=hour) per (shift_id, date) per produrre la
// matrice "OEE per turno × giorno" che la direzione vuole vedere.
// Funziona solo dopo che il cron worker ha popolato oee_history e
// stampato shift_id sulle righe (vedi RecordHourlySnapshot).
func (h *OEEHistoryHandler) ByShift(c *gin.Context) {
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

	profileFilter := strings.TrimSpace(c.Query("profile_id"))
	var rows *sql.Rows

	baseSQL := `
		SELECT h.profile_id, h.shift_id, s.name,
			to_char(date_trunc('day', h.bucket_start), 'YYYY-MM-DD') AS date,
			AVG(h.oee), AVG(h.availability), AVG(h.performance), AVG(h.quality),
			COUNT(*) AS hours,
			MIN(h.bucket_start) AS bucket_start
		FROM oee_history h
		JOIN shifts s ON s.id = h.shift_id
		WHERE h.bucket_size = 'hour'
		  AND h.bucket_start >= $1 AND h.bucket_start < $2
		  AND h.shift_id IS NOT NULL`

	if profileFilter == "" || profileFilter == "0" {
		rows, err = h.db.Query(baseSQL+`
			  AND h.profile_id IS NULL
			GROUP BY h.profile_id, h.shift_id, s.name, date
			ORDER BY date, h.shift_id`,
			from, to,
		)
	} else {
		rows, err = h.db.Query(baseSQL+`
			  AND h.profile_id = $3
			GROUP BY h.profile_id, h.shift_id, s.name, date
			ORDER BY date, h.shift_id`,
			from, to, profileFilter,
		)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	out := []ShiftRollupRow{}
	for rows.Next() {
		var r ShiftRollupRow
		if err := rows.Scan(
			&r.ProfileID, &r.ShiftID, &r.ShiftName, &r.Date,
			&r.OEE, &r.Availability, &r.Performance, &r.Quality,
			&r.Hours, &r.BucketStart,
		); err == nil {
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
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
//
// Se il midpoint dell'ora cade dentro un turno attivo, viene stampato
// anche `shift_id` — abilita la matrice "OEE per turno × giorno" via
// /api/oee/by-shift.
func RecordHourlySnapshot(db *sql.DB, oee *OEEHandler, now time.Time) error {
	bucketEnd := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	bucketStart := bucketEnd.Add(-time.Hour)

	// Determina il turno attivo a metà dell'ora — semplificazione: se
	// l'ora cade a cavallo di due turni, il midpoint decide a chi assegnarla.
	midpoint := bucketStart.Add(30 * time.Minute)
	shiftID := findActiveShiftAt(db, midpoint)

	profiles := oee.loadEnabledProfiles()

	// Snapshot per profilo
	rollupSum := struct{ a, p, q, oee, planned, downtime, prod, good float64 }{}
	count := 0
	for _, p := range profiles {
		cfg := p.config()
		cfg.WindowMin = 60 // forziamo a 1h indipendentemente dal window del profilo
		snap := oee.computeSnapshotAt(bucketEnd, cfg)
		if err := upsertHistory(db, &p.ID, bucketStart, "hour", snap, shiftID); err != nil {
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
		if err := upsertHistory(db, nil, bucketStart, "hour", rollup, shiftID); err != nil {
			return err
		}
	}

	// Modalità legacy (0 profili): scriviamo comunque il rollup per non
	// avere buchi nello storico.
	if count == 0 {
		cfg := oee.legacyConfig()
		cfg.WindowMin = 60
		snap := oee.computeSnapshotAt(bucketEnd, cfg)
		if err := upsertHistory(db, nil, bucketStart, "hour", snap, shiftID); err != nil {
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

// findActiveShiftAt ritorna l'id del turno attivo al timestamp `at`,
// oppure nil se nessun turno copre quel momento (es. weekend o pause
// non coperte). Usato dal cron worker per stampare oee_history.shift_id.
//
// Replica la logica di ShiftsHandler.Current ma su un timestamp arbitrario
// (non solo "adesso") — necessaria per backfill o se il cron parte in
// ritardo rispetto all'ora esatta.
func findActiveShiftAt(db *sql.DB, at time.Time) *int {
	weekday := int(at.Weekday())
	prevWeekday := (weekday + 6) % 7
	nowMin := at.Hour()*60 + at.Minute()

	rows, err := db.Query(`
		SELECT id, start_time::text, end_time::text, weekdays
		FROM shifts WHERE active = true`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var startS, endS string
		var weekdays pq.Int64Array
		if err := rows.Scan(&id, &startS, &endS, &weekdays); err != nil {
			continue
		}
		startS = trimSeconds(startS)
		endS = trimSeconds(endS)
		sMin := minutesOfDay(startS)
		eMin := minutesOfDay(endS)
		wraps := wrapsMidnight(startS, endS)
		wd := int64sToInts(weekdays)

		if !wraps {
			if contains(wd, weekday) && nowMin >= sMin && nowMin < eMin {
				v := id
				return &v
			}
			continue
		}
		// Wrap midnight: due casi.
		if contains(wd, weekday) && nowMin >= sMin {
			v := id
			return &v
		}
		if contains(wd, prevWeekday) && nowMin < eMin {
			v := id
			return &v
		}
	}
	return nil
}
