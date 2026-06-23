package connectors

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// sanitizeTag
// ---------------------------------------------------------------------------

func TestSanitizeTag_NoChange(t *testing.T) {
	inputs := []string{
		"Portata_Ingresso",
		"temperature",
		"sensor01",
		"my-tag",
		"abc123",
		"",
	}
	for _, in := range inputs {
		got := sanitizeTag(in)
		if got != in {
			t.Errorf("sanitizeTag(%q) = %q, want %q (no change expected)", in, got, in)
		}
	}
}

func TestSanitizeTag_ReplacesSpace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"hello world", "hello_world"},
		{"a b c", "a_b_c"},
		{" leading", "_leading"},
		{"trailing ", "trailing_"},
	}
	for _, tc := range cases {
		got := sanitizeTag(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeTag_ReplacesComma(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a,b", "a_b"},
		{"tag,id=1", "tag_id_1"},
		{",start", "_start"},
		{"end,", "end_"},
	}
	for _, tc := range cases {
		got := sanitizeTag(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeTag_ReplacesEquals(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a=b", "a_b"},
		{"key=value", "key_value"},
		{"=start", "_start"},
		{"end=", "end_"},
	}
	for _, tc := range cases {
		got := sanitizeTag(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeTag_MixedSpecialChars(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"a b,c=d", "a_b_c_d"},
		{"flow rate,unit=m3/s", "flow_rate_unit_m3/s"},
		{"tag id=1,org=2", "tag_id_1_org_2"},
	}
	for _, tc := range cases {
		got := sanitizeTag(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line-protocol format helpers (mirrors the logic in Write)
// ---------------------------------------------------------------------------

// buildLineProtocol replicates the line-building logic from Write so we can
// test the formatting in isolation without a live DB.
func buildLineProtocol(tagID, orgID int, tagAlias string, value interface{}, quality int, tsMs int64) (string, bool) {
	alias := sanitizeTag(tagAlias)
	tsNs := tsMs * 1_000_000

	var valuePart string
	switch v := value.(type) {
	case float64:
		valuePart = fmt.Sprintf("value=%g,quality=%di", v, quality)
	case int64:
		valuePart = fmt.Sprintf("value=%di,quality=%di", v, quality)
	case bool:
		if v {
			valuePart = fmt.Sprintf("value=true,quality=%di", quality)
		} else {
			valuePart = fmt.Sprintf("value=false,quality=%di", quality)
		}
	default:
		return "", false
	}

	line := fmt.Sprintf("openedge_tag,tag_id=%d,org_id=%d,alias=%s %s %d",
		tagID, orgID, alias, valuePart, tsNs)
	return line, true
}

func TestLineProtocol_Float64(t *testing.T) {
	line, ok := buildLineProtocol(42, 1, "Portata_Ingresso", float64(41.2), 0, 1700000000000)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	// Check measurement name
	if !strings.HasPrefix(line, "openedge_tag,") {
		t.Errorf("line should start with measurement name, got: %s", line)
	}
	// Check tags
	if !strings.Contains(line, "tag_id=42") {
		t.Errorf("missing tag_id=42 in: %s", line)
	}
	if !strings.Contains(line, "org_id=1") {
		t.Errorf("missing org_id=1 in: %s", line)
	}
	if !strings.Contains(line, "alias=Portata_Ingresso") {
		t.Errorf("missing alias in: %s", line)
	}
	// Check field set
	if !strings.Contains(line, "value=41.2") {
		t.Errorf("missing float value in: %s", line)
	}
	if !strings.Contains(line, "quality=0i") {
		t.Errorf("missing quality field in: %s", line)
	}
	// Check timestamp (ms * 1e6)
	expectedTs := fmt.Sprintf("%d", int64(1700000000000)*1_000_000)
	if !strings.HasSuffix(line, expectedTs) {
		t.Errorf("line should end with timestamp %s, got: %s", expectedTs, line)
	}
}

func TestLineProtocol_Int64(t *testing.T) {
	line, ok := buildLineProtocol(7, 3, "counter", int64(99), 1, 1000)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	if !strings.Contains(line, "value=99i") {
		t.Errorf("integer value should have 'i' suffix, got: %s", line)
	}
	if !strings.Contains(line, "quality=1i") {
		t.Errorf("missing quality=1i in: %s", line)
	}
}

func TestLineProtocol_BoolTrue(t *testing.T) {
	line, ok := buildLineProtocol(1, 1, "valve_open", true, 0, 500)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	if !strings.Contains(line, "value=true") {
		t.Errorf("expected value=true in: %s", line)
	}
}

func TestLineProtocol_BoolFalse(t *testing.T) {
	line, ok := buildLineProtocol(1, 1, "valve_open", false, 0, 500)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	if !strings.Contains(line, "value=false") {
		t.Errorf("expected value=false in: %s", line)
	}
}

func TestLineProtocol_UnsupportedType(t *testing.T) {
	_, ok := buildLineProtocol(1, 1, "tag", "a_string", 0, 500)
	if ok {
		t.Error("expected unsupported type to return ok=false")
	}
}

func TestLineProtocol_AliasWithSpaceAndComma(t *testing.T) {
	line, ok := buildLineProtocol(5, 2, "flow rate,m3/s", float64(10.0), 0, 1000)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	// Space and comma in alias must be sanitized to underscores.
	// Verify the raw alias chars do not appear in the alias tag value portion.
	if strings.Contains(line, "alias=flow rate") {
		t.Errorf("space in alias should have been sanitized, got: %s", line)
	}
	if strings.Contains(line, "alias=flow rate,m3/s") {
		t.Errorf("comma in alias should have been sanitized, got: %s", line)
	}
	if !strings.Contains(line, "alias=flow_rate_m3/s") {
		t.Errorf("expected sanitized alias 'flow_rate_m3/s', got: %s", line)
	}
}

func TestLineProtocol_NegativeFloat(t *testing.T) {
	line, ok := buildLineProtocol(3, 1, "temp", float64(-5.5), 0, 2000)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	if !strings.Contains(line, "value=-5.5") {
		t.Errorf("expected negative float value, got: %s", line)
	}
}

func TestLineProtocol_ZeroTimestamp(t *testing.T) {
	line, ok := buildLineProtocol(1, 1, "x", float64(0), 0, 0)
	if !ok {
		t.Fatal("expected line to be produced")
	}
	if !strings.HasSuffix(line, " 0") {
		t.Errorf("expected trailing 0 timestamp, got: %s", line)
	}
}

// ---------------------------------------------------------------------------
// Config parsing helpers (mirrors reload logic)
// ---------------------------------------------------------------------------

// parseInfluxCfg replicates the config parsing from reload() without needing a DB.
func parseInfluxCfg(raw map[string]string) influxCfg {
	batchSize := 500
	if n, err := fmt.Sscanf(raw["influx_batch_size"], "%d", &batchSize); n == 0 || err != nil {
		batchSize = 500
	}
	flushSec := 10
	if n, err := fmt.Sscanf(raw["influx_flush_interval"], "%d", &flushSec); n == 0 || err != nil {
		flushSec = 10
	}
	return influxCfg{
		enabled:       strings.EqualFold(raw["influx_enabled"], "true"),
		url:           raw["influx_url"],
		token:         raw["influx_token"],
		org:           raw["influx_org"],
		bucket:        raw["influx_bucket"],
		batchSize:     batchSize,
		flushInterval: time.Duration(flushSec) * time.Second,
	}
}

func TestParseInfluxCfg_Enabled(t *testing.T) {
	cases := []struct {
		val     string
		enabled bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"false", false},
		{"", false},
		{"yes", false},
		{"1", false},
	}
	for _, tc := range cases {
		cfg := parseInfluxCfg(map[string]string{"influx_enabled": tc.val})
		if cfg.enabled != tc.enabled {
			t.Errorf("influx_enabled=%q: got enabled=%v, want %v", tc.val, cfg.enabled, tc.enabled)
		}
	}
}

func TestParseInfluxCfg_BatchSizeDefault(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{})
	if cfg.batchSize != 500 {
		t.Errorf("default batch size should be 500, got %d", cfg.batchSize)
	}
}

func TestParseInfluxCfg_BatchSizeCustom(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{"influx_batch_size": "250"})
	if cfg.batchSize != 250 {
		t.Errorf("expected batch size 250, got %d", cfg.batchSize)
	}
}

func TestParseInfluxCfg_BatchSizeInvalidFallsBack(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{"influx_batch_size": "notanumber"})
	if cfg.batchSize != 500 {
		t.Errorf("invalid batch size should fall back to 500, got %d", cfg.batchSize)
	}
}

func TestParseInfluxCfg_FlushIntervalDefault(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{})
	if cfg.flushInterval != 10*time.Second {
		t.Errorf("default flush interval should be 10s, got %v", cfg.flushInterval)
	}
}

func TestParseInfluxCfg_FlushIntervalCustom(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{"influx_flush_interval": "30"})
	if cfg.flushInterval != 30*time.Second {
		t.Errorf("expected flush interval 30s, got %v", cfg.flushInterval)
	}
}

func TestParseInfluxCfg_FlushIntervalInvalidFallsBack(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{"influx_flush_interval": "abc"})
	if cfg.flushInterval != 10*time.Second {
		t.Errorf("invalid flush interval should fall back to 10s, got %v", cfg.flushInterval)
	}
}

func TestParseInfluxCfg_URLAndCredentials(t *testing.T) {
	raw := map[string]string{
		"influx_enabled": "true",
		"influx_url":     "http://influx:8086",
		"influx_token":   "my-token",
		"influx_org":     "my-org",
		"influx_bucket":  "my-bucket",
	}
	cfg := parseInfluxCfg(raw)
	if cfg.url != "http://influx:8086" {
		t.Errorf("unexpected url: %s", cfg.url)
	}
	if cfg.token != "my-token" {
		t.Errorf("unexpected token: %s", cfg.token)
	}
	if cfg.org != "my-org" {
		t.Errorf("unexpected org: %s", cfg.org)
	}
	if cfg.bucket != "my-bucket" {
		t.Errorf("unexpected bucket: %s", cfg.bucket)
	}
}

func TestParseInfluxCfg_EmptyRaw(t *testing.T) {
	cfg := parseInfluxCfg(map[string]string{})
	if cfg.enabled {
		t.Error("empty config should not be enabled")
	}
	if cfg.url != "" || cfg.token != "" || cfg.org != "" || cfg.bucket != "" {
		t.Error("empty config should have empty string fields")
	}
}

// ---------------------------------------------------------------------------
// Write URL construction (mirrors flush logic)
// ---------------------------------------------------------------------------

func buildWriteURL(cfg influxCfg) string {
	return fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns",
		strings.TrimRight(cfg.url, "/"), cfg.org, cfg.bucket)
}

func TestBuildWriteURL_TrailingSlashStripped(t *testing.T) {
	cfg := influxCfg{url: "http://influx:8086/", org: "myorg", bucket: "mybucket"}
	got := buildWriteURL(cfg)
	want := "http://influx:8086/api/v2/write?org=myorg&bucket=mybucket&precision=ns"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildWriteURL_NoTrailingSlash(t *testing.T) {
	cfg := influxCfg{url: "http://influx:8086", org: "myorg", bucket: "mybucket"}
	got := buildWriteURL(cfg)
	want := "http://influx:8086/api/v2/write?org=myorg&bucket=mybucket&precision=ns"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildWriteURL_MultipleTrailingSlashes(t *testing.T) {
	cfg := influxCfg{url: "http://influx:8086///", org: "o", bucket: "b"}
	got := buildWriteURL(cfg)
	// TrimRight strips ALL trailing slashes
	if !strings.HasPrefix(got, "http://influx:8086/api/v2/write") {
		t.Errorf("unexpected url: %s", got)
	}
}

func TestBuildWriteURL_PrecisionIsNs(t *testing.T) {
	cfg := influxCfg{url: "http://host", org: "o", bucket: "b"}
	got := buildWriteURL(cfg)
	if !strings.Contains(got, "precision=ns") {
		t.Errorf("URL must include precision=ns, got: %s", got)
	}
}
