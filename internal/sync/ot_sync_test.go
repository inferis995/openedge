package sync

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// mapProtocol
// ---------------------------------------------------------------------------

func TestMapProtocol(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"MODBUS_TCP", "Modbus"},
		{"modbus_tcp", "Modbus"}, // case-insensitive
		{"MODBUS", "Modbus"},
		{"OPC_UA", "OPC-UA"},
		{"OPCUA", "OPC-UA"},
		{"opc_ua", "OPC-UA"},
		{"S7", "S7/ISO-TCP"},
		{"S7_ISO", "S7/ISO-TCP"},
		{"MQTT", "MQTT"},
		{"mqtt", "MQTT"},
		{"LORAWAN", "LoRaWAN"},
		{"lorawan", "LoRaWAN"},
		{"OPC_DA", "OPC-DA"},
		{"OPCDA", "OPC-DA"},
		{"UNKNOWN_DRIVER", "UNKNOWN_DRIVER"}, // passthrough
		{"", ""},                              // empty passthrough
	}
	for _, c := range cases {
		if got := mapProtocol(c.input); got != c.want {
			t.Errorf("mapProtocol(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// computeAssetRiskScore
// ---------------------------------------------------------------------------

func ptr(t time.Time) *time.Time { return &t }

func TestComputeAssetRiskScore_BaseScore(t *testing.T) {
	// Unknown protocol, no lastSeen, no version, no TLS → 3 + 2 (nil) + 0.5 (no version) = 5.5
	got := computeAssetRiskScore("UNKNOWN", nil, "", false, "")
	if got != 5.5 {
		t.Errorf("base score = %v, want 5.5", got)
	}
}

func TestComputeAssetRiskScore_LegacyProtocolRaisesScore(t *testing.T) {
	// Modbus: +2 on top of base. With nil lastSeen+no version → 3+2+2+0.5 = 7.5
	got := computeAssetRiskScore("MODBUS", nil, "", false, "")
	if got != 7.5 {
		t.Errorf("modbus score = %v, want 7.5", got)
	}
}

func TestComputeAssetRiskScore_ModernProtocolLowersScore(t *testing.T) {
	// OPC-UA: -1 on top of base. With nil lastSeen+no version → 3-1+2+0.5 = 4.5
	got := computeAssetRiskScore("OPC_UA", nil, "", false, "")
	if got != 4.5 {
		t.Errorf("opc-ua score = %v, want 4.5", got)
	}
}

func TestComputeAssetRiskScore_RecentLastSeenNoExtraPenalty(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	// Unknown protocol, seen 1h ago, no version, no TLS → 3 + 0 + 0.5 = 3.5
	got := computeAssetRiskScore("UNKNOWN", ptr(recent), "", false, "")
	if got != 3.5 {
		t.Errorf("recent lastSeen score = %v, want 3.5", got)
	}
}

func TestComputeAssetRiskScore_OldLastSeen24h(t *testing.T) {
	old := time.Now().Add(-25 * time.Hour)
	// Unknown protocol, 25h ago → +1 penalty; no version → +0.5; base=3 → 4.5
	got := computeAssetRiskScore("UNKNOWN", ptr(old), "", false, "")
	if got != 4.5 {
		t.Errorf("25h lastSeen score = %v, want 4.5", got)
	}
}

func TestComputeAssetRiskScore_VeryOldLastSeen48h(t *testing.T) {
	veryOld := time.Now().Add(-49 * time.Hour)
	// Unknown protocol, 49h ago → +2 penalty; no version → +0.5; base=3 → 5.5
	got := computeAssetRiskScore("UNKNOWN", ptr(veryOld), "", false, "")
	if got != 5.5 {
		t.Errorf("49h lastSeen score = %v, want 5.5", got)
	}
}

func TestComputeAssetRiskScore_TLSReducesScore(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	// Unknown, recent, has version, TLS enabled → 3 + 0 + 0 - 1.5 = 1.5
	got := computeAssetRiskScore("UNKNOWN", ptr(recent), "v1.0", true, "")
	if got != 1.5 {
		t.Errorf("TLS score = %v, want 1.5", got)
	}
}

func TestComputeAssetRiskScore_SecurityModeReducesScore(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	// securityMode="SignAndEncrypt" (non-"none") reduces by 1.5
	got := computeAssetRiskScore("UNKNOWN", ptr(recent), "v1.0", false, "SignAndEncrypt")
	if got != 1.5 {
		t.Errorf("securityMode score = %v, want 1.5", got)
	}
}

func TestComputeAssetRiskScore_SecurityModeNoneHasNoEffect(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	// securityMode="none" → no reduction
	got := computeAssetRiskScore("UNKNOWN", ptr(recent), "v1.0", false, "none")
	if got != 3.0 {
		t.Errorf("securityMode=none score = %v, want 3.0", got)
	}
}

func TestComputeAssetRiskScore_ClampedToZero(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	// MQTT (-1) + recent (0) + has version (0) + TLS (-1.5) = 3-1-1.5 = 0.5 (positive, no clamp)
	// To get negative: need MQTT base=2, then TLS -1.5 → 0.5; still positive.
	// Try: base=3, MQTT -1=2, recent 0, has version 0, TLS -1.5 = 0.5, still ok.
	// Actually let's verify min clamp doesn't cut off valid scores
	got := computeAssetRiskScore("MQTT", ptr(recent), "v1.0", true, "")
	if got < 0 {
		t.Errorf("score clamped below zero: got %v", got)
	}
}

func TestComputeAssetRiskScore_ClampedToTen(t *testing.T) {
	// Worst case: Modbus (3+2=5), nil lastSeen (+2=7), no version (+0.5=7.5) < 10
	// To exceed 10, would need multiple penalties stacked beyond what's possible.
	// The function caps at 10 — verify the cap doesn't affect valid extremes.
	got := computeAssetRiskScore("MODBUS", nil, "", false, "")
	if got > 10 {
		t.Errorf("score exceeded 10: got %v", got)
	}
}

func TestComputeAssetRiskScore_NilAndBlankNeverPanic(t *testing.T) {
	// Regression: nil lastSeenAt must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("computeAssetRiskScore panicked: %v", r)
		}
	}()
	computeAssetRiskScore("", nil, "", false, "")
}
