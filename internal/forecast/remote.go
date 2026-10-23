package forecast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Remote is a forecasting model served over HTTP (services/forecaster). The
// wire format is model-agnostic: any server that answers POST /v1/forecast
// this way can be plugged in.
//
//	→ {"quantiles":[0.1,0.5,0.9],"series":[{"values":[1.2,null,…],"horizon":24}]}
//	← {"model":"mistralai/Timestral-28M","forecasts":[{"q10":[…],"q50":[…],"q90":[…]}]}
type Remote struct {
	URL    string
	name   string
	client *http.Client
	// Batch is the most series sent in one request.
	Batch int
	// Model is the model the server last said it used.
	Model string
}

// NewRemote returns a client for the server at url (e.g. http://forecaster:8000).
func NewRemote(url, name string, timeout time.Duration) *Remote {
	if name == "" {
		name = "timestral"
	}
	return &Remote{
		URL:    strings.TrimRight(url, "/"),
		name:   name,
		client: &http.Client{Timeout: timeout},
		Batch:  64,
	}
}

// Name implements Forecaster.
func (r *Remote) Name() string { return r.name }

type wireSeries struct {
	Values  nullFloats `json:"values"`
	Horizon int        `json:"horizon"`
}

type wireRequest struct {
	Quantiles []float64    `json:"quantiles"`
	Series    []wireSeries `json:"series"`
}

type wireForecast struct {
	Q10 nullFloats `json:"q10"`
	Q50 nullFloats `json:"q50"`
	Q90 nullFloats `json:"q90"`
}

type wireResponse struct {
	Model     string         `json:"model"`
	Forecasts []wireForecast `json:"forecasts"`
}

// Forecast implements Forecaster.
func (r *Remote) Forecast(ctx context.Context, reqs []Request) ([]Forecast, error) {
	out := make([]Forecast, 0, len(reqs))
	batch := r.Batch
	if batch <= 0 {
		batch = 64
	}
	for i := 0; i < len(reqs); i += batch {
		j := i + batch
		if j > len(reqs) {
			j = len(reqs)
		}
		got, err := r.call(ctx, reqs[i:j])
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func (r *Remote) call(ctx context.Context, reqs []Request) ([]Forecast, error) {
	body := wireRequest{Quantiles: []float64{0.1, 0.5, 0.9}, Series: make([]wireSeries, len(reqs))}
	for i, q := range reqs {
		body.Series[i] = wireSeries{Values: q.Series.Values, Horizon: q.Horizon}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL+"/v1/forecast", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("forecaster: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("forecaster: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var wr wireResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&wr); err != nil {
		return nil, fmt.Errorf("forecaster: bad response: %w", err)
	}
	if len(wr.Forecasts) != len(reqs) {
		return nil, fmt.Errorf("forecaster: %d forecasts for %d series", len(wr.Forecasts), len(reqs))
	}
	if wr.Model != "" {
		r.Model = wr.Model
	}
	out := make([]Forecast, len(reqs))
	for i, q := range reqs {
		f := newForecast(q)
		w := wr.Forecasts[i]
		copy(f.Q10, w.Q10)
		copy(f.Q50, w.Q50)
		copy(f.Q90, w.Q90)
		out[i] = f
	}
	return out, nil
}

// nullFloats is a float slice whose NaN travels as JSON null.
type nullFloats []float64

func (n nullFloats) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, x := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
			continue
		}
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func (n *nullFloats) UnmarshalJSON(data []byte) error {
	var raw []*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make([]float64, len(raw))
	for i, p := range raw {
		if p == nil {
			out[i] = math.NaN()
		} else {
			out[i] = *p
		}
	}
	*n = out
	return nil
}
