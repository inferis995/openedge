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
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/shifts"
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

// historyTarget is which oee_history rows a request reads: one profile's, or
// an organization's factory rollup.
type historyTarget struct {
	profileID *int // nil: the rollup
	org       *int // the rollup's organization; nil: every organization's
}

// historyTargetOf reads profile_id from the query. Every reader took it as
// given and read the rollup of every organization, so any user could read
// another organization's lines by id and everybody's production mixed into
// one factory figure. A profile of another organization is not found; the
// rollup is the caller's organization's (every organization's for the
// global administrator with none selected). On false the response is written.
func historyTargetOf(c *gin.Context, db *sql.DB) (historyTarget, bool) {
	raw := strings.TrimSpace(c.Query("profile_id"))
	if raw == "" || raw == "0" {
		return historyTarget{org: oeeOrg(c)}, true
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid profile_id"})
		return historyTarget{}, false
	}
	if !profileVisible(c.Request.Context(), db, id, middleware.GetOrgFilterForQuery(c)) {
		c.JSON(http.StatusNotFound, gin.H{"error": "profile not found"})
		return historyTarget{}, false
	}
	return historyTarget{profileID: &id}, true
}

// where is the predicate on the oee_history alias `h` for the target, its
// single placeholder numbered n, and the argument that goes with it.
func (t historyTarget) where(n int) (string, interface{}) {
	if t.profileID != nil {
		return fmt.Sprintf("h.profile_id = $%d", n), *t.profileID
	}
	return fmt.Sprintf("h.profile_id IS NULL AND ($%d::int IS NULL OR h.org_id = $%d)", n, n), t.org
}

// profileVisible says whether the OEE profile exists for orgFilter (nil: the
// global administrator, every organization).
func profileVisible(ctx context.Context, db *sql.DB, id int, orgFilter *int) bool {
	var one int
	return db.QueryRowContext(ctx,
		`SELECT 1 FROM oee_profiles WHERE id = $1 AND ($2::int IS NULL OR org_id = $2)`,
		id, orgFilter).Scan(&one) == nil
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
	target, ok := historyTargetOf(c, h.db)
	if !ok {
		return
	}
	pred, arg := target.where(3)

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT h.profile_id, h.shift_id, s.name,
			to_char(date_trunc('day', h.bucket_start), 'YYYY-MM-DD') AS date,
			AVG(h.oee), AVG(h.availability), AVG(h.performance), AVG(h.quality),
			COUNT(*) AS hours,
			MIN(h.bucket_start) AS bucket_start
		FROM oee_history h
		JOIN shifts s ON s.id = h.shift_id
		WHERE h.bucket_size = 'hour'
		  AND h.bucket_start >= $1 AND h.bucket_start < $2
		  AND h.shift_id IS NOT NULL
		  AND `+pred+`
		GROUP BY h.profile_id, h.shift_id, s.name, date
		ORDER BY date, h.shift_id`,
		from, to, arg,
	)
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

	// profile_id facoltativo: vuoto = rollup fabbrica dell'organizzazione.
	target, ok := historyTargetOf(c, h.db)
	if !ok {
		return
	}
	pred, arg := target.where(4)

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT h.profile_id, h.bucket_start, h.bucket_size, h.oee, h.availability,
			h.performance, h.quality, h.planned_min, h.downtime_min,
			h.pieces_produced, h.pieces_good, h.shift_id
		FROM oee_history h
		WHERE h.bucket_size = $1 AND h.bucket_start >= $2 AND h.bucket_start < $3
		  AND `+pred+`
		ORDER BY h.bucket_start ASC`,
		bucket, from, to, arg,
	)
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

// orgProfiles is one organization's enabled profiles.
type orgProfiles struct {
	orgID    int
	profiles []OEEProfile
}

// profilesByOrg groups the profiles by organization, in the order the
// organizations first appear.
func profilesByOrg(profiles []OEEProfile) []orgProfiles {
	var out []orgProfiles
	index := map[int]int{}
	for k := range profiles {
		org := profiles[k].OrgID
		i, ok := index[org]
		if !ok {
			i = len(out)
			index[org] = i
			out = append(out, orgProfiles{orgID: org})
		}
		out[i].profiles = append(out[i].profiles, profiles[k])
	}
	return out
}

// averageSnapshots is the factory rollup of some profiles' snapshots: the
// mean of OEE/A/P/Q, the sum of downtime and pieces.
func averageSnapshots(snaps []OEESnapshot, windowMin int) OEESnapshot {
	out := OEESnapshot{WindowMinutes: windowMin}
	if len(snaps) == 0 {
		return out
	}
	for i := range snaps {
		s := &snaps[i]
		out.OEE += s.OEE
		out.Availability += s.Availability
		out.Performance += s.Performance
		out.Quality += s.Quality
		out.CriticalDowntimeMin += s.CriticalDowntimeMin
		out.PiecesProduced += s.PiecesProduced
		out.PiecesGood += s.PiecesGood
	}
	n := float64(len(snaps))
	out.OEE /= n
	out.Availability /= n
	out.Performance /= n
	out.Quality /= n
	return out
}

// RecordHourlySnapshot salva uno snapshot orario per ogni profilo abilitato
// + un rollup fabbrica (profile_id=NULL) per ogni organizzazione. Chiamato
// dal cron worker all'inizio di ogni ora.
//
// L'ora "X:00" salva la finestra [X-1:00, X:00) — cioè il rollup dell'ora
// appena conclusa. Idempotente: le righe profilo via UNIQUE(profile_id,
// bucket_start, bucket_size), i rollup via gli indici unici parziali su
// org_id (vedi upsertHistory).
//
// Se il midpoint dell'ora cade dentro un turno attivo, viene stampato
// anche `shift_id` — abilita la matrice "OEE per turno × giorno" via
// /api/oee/by-shift.
//
// There was one rollup for the whole installation, the average of every
// organization's profiles, and every organization read it; and the hour went
// to whichever organization's shift happened to be running. Each organization
// now gets its own rollup, and its rows its own shift.
func RecordHourlySnapshot(db *sql.DB, oee *OEEHandler, now time.Time) error {
	ctx := context.Background()
	bucketEnd := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, time.UTC)
	bucketStart := bucketEnd.Add(-time.Hour)

	// Il turno attivo a metà dell'ora — semplificazione: se l'ora cade a
	// cavallo di due turni, il midpoint decide a chi assegnarla.
	midpoint := bucketStart.Add(30 * time.Minute)

	profiles := oee.loadEnabledProfiles()

	// Modalità legacy (0 profili): scriviamo comunque il rollup per non
	// avere buchi nello storico. It is computed from the platform's settings:
	// it is the organization's when there is only one (as the migration did
	// with the existing rows), otherwise the platform's.
	if len(profiles) == 0 {
		org := soleOrganization(ctx, db)
		cfg := oee.legacyConfig()
		cfg.WindowMin = 60
		snap := oee.computeSnapshotAt(bucketEnd, cfg)
		return upsertHistory(ctx, db, nil, org, bucketStart, "hour", &snap, findActiveShiftAt(ctx, db, org, midpoint))
	}

	for _, group := range profilesByOrg(profiles) {
		org := group.orgID
		shiftID := findActiveShiftAt(ctx, db, &org, midpoint)
		snaps := make([]OEESnapshot, 0, len(group.profiles))
		for i := range group.profiles {
			p := &group.profiles[i]
			cfg := p.config()
			cfg.WindowMin = 60 // forziamo a 1h indipendentemente dal window del profilo
			snap := oee.computeSnapshotAt(bucketEnd, cfg)
			if err := upsertHistory(ctx, db, &p.ID, &org, bucketStart, "hour", &snap, shiftID); err != nil {
				return err
			}
			snaps = append(snaps, snap)
		}
		rollup := averageSnapshots(snaps, 60)
		if err := upsertHistory(ctx, db, nil, &org, bucketStart, "hour", &rollup, shiftID); err != nil {
			return err
		}
	}
	return nil
}

// RecordDailySnapshot aggrega le 24 righe orarie del giorno precedente
// in una sola riga per profilo (+ rollup per organizzazione). Chiamato a
// mezzanotte UTC. Grouped by profile alone, every organization's hourly
// rollups were averaged into one daily rollup.
//
// Aggregazione: media aritmetica di A/P/Q/OEE; somma di pieces/downtime/planned.
// Niente "pesatura per pieces" — semplificazione accettabile.
func RecordDailySnapshot(db *sql.DB, oee *OEEHandler, now time.Time) error {
	ctx := context.Background()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-24 * time.Hour)
	dayEnd := dayStart.Add(24 * time.Hour)

	rows, err := db.QueryContext(ctx, `
		SELECT
			profile_id, org_id,
			AVG(oee), AVG(availability), AVG(performance), AVG(quality),
			SUM(planned_min), SUM(downtime_min),
			SUM(pieces_produced), SUM(pieces_good)
		FROM oee_history
		WHERE bucket_size = 'hour'
		  AND bucket_start >= $1 AND bucket_start < $2
		GROUP BY profile_id, org_id`,
		dayStart, dayEnd,
	)
	if err != nil {
		return err
	}
	type dayRow struct {
		profileID, orgID *int
		snap             OEESnapshot
	}
	var days []dayRow
	for rows.Next() {
		var pid, org sql.NullInt64
		var s OEESnapshot
		if err := rows.Scan(
			&pid, &org, &s.OEE, &s.Availability, &s.Performance, &s.Quality,
			// planned/downtime/pieces vanno nei campi "diagnostica"
			new(float64), &s.CriticalDowntimeMin,
			&s.PiecesProduced, &s.PiecesGood,
		); err != nil {
			continue
		}
		s.WindowMinutes = 24 * 60
		days = append(days, dayRow{profileID: nullIntPtr(pid), orgID: nullIntPtr(org), snap: s})
	}
	_ = rows.Close()

	for i := range days {
		d := &days[i]
		if err := upsertHistory(ctx, db, d.profileID, d.orgID, dayStart, "day", &d.snap, nil); err != nil {
			return err
		}
	}
	return nil
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

// historyConflict is the ON CONFLICT target of an oee_history row. A profile
// row is unique by profile; a rollup (profile_id NULL) never matched that
// constraint and was inserted again on every run, so it has its own partial
// unique indexes, one for an organization's rollup and one for the
// platform's (org_id NULL).
func historyConflict(profileID, orgID *int) string {
	switch {
	case profileID != nil:
		return `(profile_id, bucket_start, bucket_size)`
	case orgID != nil:
		return `(org_id, bucket_start, bucket_size) WHERE profile_id IS NULL AND org_id IS NOT NULL`
	default:
		return `(bucket_start, bucket_size) WHERE profile_id IS NULL AND org_id IS NULL`
	}
}

// upsertHistory è il helper INSERT...ON CONFLICT DO UPDATE per garantire
// idempotenza del cron (se gira due volte per la stessa ora, sovrascrive
// con l'ultimo calcolo).
func upsertHistory(ctx context.Context, db *sql.DB, profileID, orgID *int, bucketStart time.Time, bucketSize string, s *OEESnapshot, shiftID *int) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO oee_history (
			profile_id, org_id, bucket_start, bucket_size,
			oee, availability, performance, quality,
			planned_min, downtime_min, pieces_produced, pieces_good, shift_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT `+historyConflict(profileID, orgID)+` DO UPDATE SET
			oee = EXCLUDED.oee,
			availability = EXCLUDED.availability,
			performance = EXCLUDED.performance,
			quality = EXCLUDED.quality,
			planned_min = EXCLUDED.planned_min,
			downtime_min = EXCLUDED.downtime_min,
			pieces_produced = EXCLUDED.pieces_produced,
			pieces_good = EXCLUDED.pieces_good,
			shift_id = EXCLUDED.shift_id`,
		profileID, orgID, bucketStart, bucketSize,
		s.OEE, s.Availability, s.Performance, s.Quality,
		float64(s.WindowMinutes), s.CriticalDowntimeMin,
		s.PiecesProduced, s.PiecesGood, shiftID,
	)
	return err
}

// soleOrganization is the installation's organization when there is exactly
// one, nil otherwise.
func soleOrganization(ctx context.Context, db *sql.DB) *int {
	var id sql.NullInt64
	var n int
	if err := db.QueryRowContext(ctx, `SELECT MIN(id), COUNT(*) FROM organizations`).Scan(&id, &n); err != nil || n != 1 {
		return nil
	}
	return nullIntPtr(id)
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

// findActiveShiftAt is the id of the shift running at `at`, on the plant's
// clock (internal/shifts), for the hourly rollup. It was computed in UTC, so
// an hour was attributed to the shift before or after it; and it read every
// organization's shifts, so an hour went to whichever organization's shift
// came first. org is the organization whose shifts count (nil: every one,
// for the platform's legacy rollup).
func findActiveShiftAt(ctx context.Context, db *sql.DB, org *int, at time.Time) *int {
	scope := shifts.Scope{All: true}
	if org != nil {
		scope = shifts.Scope{OrgID: *org}
	}
	list, err := shifts.Load(ctx, db, scope)
	if err != nil {
		return nil
	}
	if sh, _, _ := shifts.ActiveAt(list, at); sh != nil {
		id := sh.ID
		return &id
	}
	return nil
}
