package forecast

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricRunSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "openedge_forecast_run_seconds",
		Help:    "Duration of one forecast run (one batch of due tags).",
		Buckets: prometheus.ExponentialBuckets(0.05, 2, 12),
	})
	metricResults = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openedge_forecast_results_total",
		Help: "Tag forecasts written, by status and engine.",
	}, []string{"status", "engine"})
	metricEngineErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "openedge_forecast_engine_errors_total",
		Help: "Batches an engine failed to forecast.",
	}, []string{"engine"})
)

// advisoryLockKey keeps two core-api replicas from forecasting the same tags.
const advisoryLockKey = 0x0E_F0_2E_CA

// Runner forecasts the tags that are due, in batches, and stores the result in
// tag_forecasts. One runs inside core-api.
type Runner struct {
	DB      *sql.DB
	Engines []Forecaster
	// Tick is how often due tags are looked for.
	Tick time.Duration
	// Batch is the most tags forecast in one run.
	Batch int
	// Now is the clock (tests).
	Now func() time.Time
	// OnRisk, when set, is told of every predicted alarm that has just become
	// likely or almost certain (see RiskNotice).
	OnRisk func(RiskNotice)
}

// NewRunner returns a runner with the statistical engine and, when url is not
// empty, the model served there.
func NewRunner(db *sql.DB, url string) *Runner {
	engines := []Forecaster{Statistical{}}
	if url != "" {
		engines = append(engines, NewRemote(url, "", 2*time.Minute))
	}
	return &Runner{DB: db, Engines: engines, Tick: time.Minute, Batch: 200, Now: time.Now}
}

// Start runs until ctx is done.
func (r *Runner) Start(ctx context.Context) {
	names := make([]string, len(r.Engines))
	for i, e := range r.Engines {
		names[i] = e.Name()
	}
	log.Printf("[FORECAST] runner started (engines: %v, every %s)", names, r.Tick)
	t := time.NewTicker(r.Tick)
	defer t.Stop()
	for {
		if n, err := r.RunDue(ctx); err != nil {
			log.Printf("[FORECAST] run failed: %v", err)
		} else if n > 0 {
			log.Printf("[FORECAST] %d tags forecast", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunDue forecasts up to Batch tags whose turn has come. It does nothing (and
// returns 0) when another replica holds the lock.
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	var got bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&got); err != nil {
		return 0, err
	}
	if !got {
		return 0, nil
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	tags, err := r.dueTags(ctx, nil)
	if err != nil {
		return 0, err
	}
	return len(tags), r.forecast(ctx, tags)
}

// RunTags forecasts the given tags now, due or not.
func (r *Runner) RunTags(ctx context.Context, ids []int) error {
	tags, err := r.dueTags(ctx, ids)
	if err != nil {
		return err
	}
	return r.forecast(ctx, tags)
}

type tagRow struct {
	id, gateway, area, site, org int
	isBool                       bool
	alias                        string
}

func (r *Runner) dueTags(ctx context.Context, ids []int) ([]tagRow, error) {
	q := `
		SELECT t.id, t.data_type = 'BOOL', t.gateway_id, g.area_id, a.site_id, s.org_id, COALESCE(t.alias, '')
		FROM tags t
		JOIN gateways g ON g.id = t.gateway_id
		JOIN areas    a ON a.id = g.area_id
		JOIN sites    s ON s.id = a.site_id
		LEFT JOIN tag_forecasts f ON f.tag_id = t.id
		WHERE t.historize = TRUE AND t.data_type IN ('INT', 'DINT', 'REAL', 'BOOL')`
	var args []any
	if ids != nil {
		q += ` AND t.id = ANY($1) ORDER BY t.id`
		args = append(args, pq.Array(ids))
	} else {
		q += ` AND (f.tag_id IS NULL OR f.next_run_at <= $2)
		ORDER BY f.next_run_at NULLS FIRST, t.id LIMIT $1`
		args = append(args, r.Batch, r.Now())
	}
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("due tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []tagRow
	for rows.Next() {
		var t tagRow
		if err := rows.Scan(&t.id, &t.isBool, &t.gateway, &t.area, &t.site, &t.org, &t.alias); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Runner) forecast(ctx context.Context, tags []tagRow) error {
	if len(tags) == 0 {
		return nil
	}
	start := time.Now()
	defer func() { metricRunSeconds.Observe(time.Since(start).Seconds()) }()

	profiles, err := LoadProfiles(ctx, r.DB, orgsOf(tags))
	if err != nil {
		return err
	}
	off, err := r.modulesOff(ctx, orgsOf(tags))
	if err != nil {
		return err
	}
	now := r.Now()
	jobs := make([]Job, 0, len(tags))
	for _, t := range tags {
		eff := profiles.Effective(t.org, t.site, t.area, t.gateway, t.id)
		if off[t.org] {
			// The organization does not use forecasts (its "forecast" page is
			// switched off): nothing is computed for it.
			eff.Enabled = false
		}
		j := Job{TagID: t.id, IsBool: t.isBool, Settings: eff}
		if eff.Enabled {
			s, err := r.history(ctx, t.id, eff, now)
			if err != nil {
				log.Printf("[FORECAST] tag %d: history: %v", t.id, err)
			}
			j.Series = s
		}
		jobs = append(jobs, j)
	}
	ids := make([]int, len(tags))
	for i, t := range tags {
		ids[i] = t.id
	}
	rules := r.thresholds(ctx, ids)
	prev := r.previousForecasts(ctx, ids)
	results := Run(ctx, jobs, r.Engines)
	var notices []RiskNotice
	for i := range results {
		t := tags[i]
		p := prev[t.id]
		assess(&results[i], &jobs[i], rules[t.id], &p)
		notices = append(notices, newNotices(t.org, t.id, t.alias, results[i].Risks, prev[t.id].risks)...)
	}
	for _, e := range r.Engines {
		for k := range results {
			if _, failed := results[k].Errors[e.Name()]; failed {
				metricEngineErrors.WithLabelValues(e.Name()).Inc()
				break
			}
		}
	}
	for i := range results {
		res := &results[i]
		if err := r.store(ctx, res, jobs[i].Series.Step, now); err != nil {
			log.Printf("[FORECAST] tag %d: store: %v", res.TagID, err)
			continue
		}
		metricResults.WithLabelValues(res.Status, res.Engine).Inc()
	}
	r.notify(notices)
	return nil
}

// modulesOff are the organizations, among these, that switched the forecast
// module off.
func (r *Runner) modulesOff(ctx context.Context, orgs []int) (map[int]bool, error) {
	off := map[int]bool{}
	rows, err := r.DB.QueryContext(ctx, `SELECT org_id FROM organization_modules
		WHERE module_key = 'forecast' AND enabled = false AND org_id = ANY($1)`, pq.Array(orgs))
	if err != nil {
		// A database without the table (tests, an old schema) has every module on.
		return off, nil //nolint:nilerr
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			off[id] = true
		}
	}
	return off, rows.Err()
}

func orgsOf(tags []tagRow) []int {
	seen := map[int]bool{}
	var out []int
	for _, t := range tags {
		if !seen[t.org] {
			seen[t.org] = true
			out = append(out, t.org)
		}
	}
	return out
}

// history reads a tag's past at the step its measurements call for, from the
// narrowest continuous aggregate that has it, or from the raw table.
func (r *Runner) history(ctx context.Context, tagID int, eff Effective, now time.Time) (Series, error) {
	var times []time.Time
	rows, err := r.DB.QueryContext(ctx, `
		SELECT time FROM tag_history
		WHERE tag_id = $1 AND value IS NOT NULL
		ORDER BY time DESC LIMIT 200`, tagID)
	if err != nil {
		return Series{}, err
	}
	for rows.Next() {
		var t time.Time
		if err = rows.Scan(&t); err != nil {
			_ = rows.Close()
			return Series{}, err
		}
		times = append(times, t)
	}
	_ = rows.Close()
	if len(times) == 0 {
		return Series{Step: time.Hour, Start: now}, nil
	}
	step := ChooseStep(times)
	n := ContextSteps(eff.ContextDays, step)
	from := now.Add(-time.Duration(n) * step)
	stride := fmt.Sprintf("%d seconds", int(step.Seconds()))

	var table string
	switch {
	case step >= 24*time.Hour:
		table = "tag_history_1d"
	case step >= time.Hour:
		table = "tag_history_1h"
	default:
		table = "tag_history_1m"
	}
	samples, err := r.buckets(ctx, fmt.Sprintf(`
		SELECT date_bin($2::interval, bucket, TIMESTAMPTZ 'epoch') AS b,
		       SUM(avg_value * sample_count) / NULLIF(SUM(sample_count), 0)
		FROM %s
		WHERE tag_id = $1 AND bucket >= $3 AND avg_value IS NOT NULL
		GROUP BY b ORDER BY b`, table), tagID, stride, from)
	if err != nil {
		// No continuous aggregates (plain Postgres): bucket the raw rows.
		samples, err = r.buckets(ctx, `
			SELECT date_bin($2::interval, time, TIMESTAMPTZ 'epoch') AS b, AVG(value)
			FROM tag_history
			WHERE tag_id = $1 AND time >= $3 AND value IS NOT NULL
			GROUP BY b ORDER BY b`, tagID, stride, from)
		if err != nil {
			return Series{}, err
		}
	}
	return Resample(samples, step, now, n), nil
}

func (r *Runner) buckets(ctx context.Context, q string, args ...any) ([]Sample, error) {
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Sample
	for rows.Next() {
		var s Sample
		var v sql.NullFloat64
		if err := rows.Scan(&s.T, &v); err != nil {
			return nil, err
		}
		if v.Valid {
			s.V = v.Float64
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// Refresh is how long until a tag is forecast again, by outcome.
func Refresh(status string, step time.Duration) time.Duration {
	switch status {
	case StatusDisabled:
		return 6 * time.Hour
	case StatusLearning:
		return time.Hour
	case StatusError:
		return 15 * time.Minute
	}
	d := 4 * step
	if d < 15*time.Minute {
		d = 15 * time.Minute
	}
	if d > 6*time.Hour {
		d = 6 * time.Hour
	}
	return d
}

// Quantiles is how a forecast's bands are stored and served.
type Quantiles struct {
	Q10 nullFloats `json:"q10"`
	Q50 nullFloats `json:"q50"`
	Q90 nullFloats `json:"q90"`
}

type details struct {
	Period  int               `json:"period,omitempty"`
	Engines map[string]Score  `json:"engines,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`
	Model   string            `json:"model,omitempty"`
}

func (r *Runner) store(ctx context.Context, res *Result, step time.Duration, now time.Time) error {
	d := details{Period: res.Period, Engines: res.Engines, Errors: res.Errors}
	for _, e := range r.Engines {
		if rm, ok := e.(*Remote); ok && res.Engine == rm.Name() {
			d.Model = rm.Model
		}
	}
	detailsJSON, err := json.Marshal(d)
	if err != nil {
		return err
	}
	var (
		startAt         any
		quantiles       any
		skill, coverage any
	)
	if res.Status == StatusOK || res.Status == StatusUnreliable {
		startAt = res.Forecast.Start
		qj, qerr := json.Marshal(Quantiles{Q10: res.Forecast.Q10, Q50: res.Forecast.Q50, Q90: res.Forecast.Q90})
		if qerr != nil {
			return qerr
		}
		quantiles = string(qj)
		if !math.IsNaN(res.Score.Skill) {
			skill = res.Score.Skill
		}
		coverage = res.Score.Coverage
	}
	next := now.Add(Refresh(res.Status, step))
	risks := res.Risks
	if risks == nil {
		risks = []Risk{}
	}
	risksJSON, _ := json.Marshal(risks)
	var anomaly any
	if res.Anomaly != nil {
		aj, _ := json.Marshal(res.Anomaly)
		anomaly = string(aj)
	}
	_, err = r.DB.ExecContext(ctx, `
		INSERT INTO tag_forecasts (tag_id, status, reason, kind, engine, step_seconds, start_at,
		                           quantiles, skill, coverage, details, generated_at, next_run_at,
		                           risks, anomaly)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11::jsonb, $12, $13, $14::jsonb, $15::jsonb)
		ON CONFLICT (tag_id) DO UPDATE SET
			status = EXCLUDED.status, reason = EXCLUDED.reason, kind = EXCLUDED.kind,
			engine = EXCLUDED.engine, step_seconds = EXCLUDED.step_seconds,
			start_at = EXCLUDED.start_at, quantiles = EXCLUDED.quantiles,
			skill = EXCLUDED.skill, coverage = EXCLUDED.coverage, details = EXCLUDED.details,
			generated_at = EXCLUDED.generated_at, next_run_at = EXCLUDED.next_run_at,
			risks = EXCLUDED.risks, anomaly = EXCLUDED.anomaly`,
		res.TagID, res.Status, res.Reason, string(res.Kind), res.Engine, int(step.Seconds()),
		startAt, quantiles, skill, coverage, string(detailsJSON), now, next, string(risksJSON), anomaly)
	return err
}
