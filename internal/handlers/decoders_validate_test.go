package handlers

import "testing"

func TestValidateDecoderConfigOptions(t *testing.T) {
	ok := []map[string]interface{}{
		nil,
		{"decoder_preset": "mido", "stale_after_minutes": 480.0, "alarms_on_history": true},
		{"decoder_preset": "mido", "alarms_on_history": false},
	}
	for _, cfg := range ok {
		if err := validateDecoderConfig(cfg); err != nil {
			t.Errorf("%v refused: %v", cfg, err)
		}
	}
	bad := []map[string]interface{}{
		{"decoder_preset": "nope"},
		{"decoder_preset": "mido", "stale_after_minutes": -1.0},
		{"decoder_preset": "mido", "alarms_on_history": "yes"},
	}
	for _, cfg := range bad {
		if err := validateDecoderConfig(cfg); err == nil {
			t.Errorf("%v accepted", cfg)
		}
	}
}
