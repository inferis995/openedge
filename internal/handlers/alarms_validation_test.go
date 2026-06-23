package handlers

import (
	"math"
	"testing"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

func ptr(f float64) *float64 { return &f }

func TestValidateAlarmDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		alarms  []models.AlarmDefinition
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid high alarm",
			alarms: []models.AlarmDefinition{
				{AlarmType: "high", Severity: "critical", Threshold: ptr(100.0), Deadband: 2.0},
			},
		},
		{
			name: "valid bool alarm no threshold",
			alarms: []models.AlarmDefinition{
				{AlarmType: "bool_true", Severity: "warning", Deadband: 0},
			},
		},
		{
			name:    "invalid alarm_type",
			alarms:  []models.AlarmDefinition{{AlarmType: "bad_type", Severity: "info"}},
			wantErr: true,
			errMsg:  "Invalid alarm_type",
		},
		{
			name:    "invalid severity",
			alarms:  []models.AlarmDefinition{{AlarmType: "high", Severity: "fatal"}},
			wantErr: true,
			errMsg:  "Invalid severity",
		},
		{
			name: "NaN threshold rejected",
			alarms: []models.AlarmDefinition{
				{AlarmType: "high", Severity: "info", Threshold: ptr(math.NaN())},
			},
			wantErr: true,
			errMsg:  "threshold must be a finite number",
		},
		{
			name: "+Inf threshold rejected",
			alarms: []models.AlarmDefinition{
				{AlarmType: "high", Severity: "info", Threshold: ptr(math.Inf(1))},
			},
			wantErr: true,
			errMsg:  "threshold must be a finite number",
		},
		{
			name: "negative deadband rejected",
			alarms: []models.AlarmDefinition{
				{AlarmType: "high", Severity: "info", Deadband: -1.0},
			},
			wantErr: true,
			errMsg:  "deadband must be a non-negative finite number",
		},
		{
			name: "Inf deadband rejected",
			alarms: []models.AlarmDefinition{
				{AlarmType: "high", Severity: "info", Deadband: math.Inf(-1)},
			},
			wantErr: true,
			errMsg:  "deadband must be a non-negative finite number",
		},
		{
			name:   "nil threshold allowed",
			alarms: []models.AlarmDefinition{{AlarmType: "bool_true", Severity: "info"}},
		},
		{
			name:   "empty slice is valid",
			alarms: []models.AlarmDefinition{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateAlarmDefinitions(tt.alarms)
			if tt.wantErr {
				if got == "" {
					t.Errorf("expected error containing %q, got no error", tt.errMsg)
				} else if tt.errMsg != "" && len(got) > 0 {
					if !containsStr(got, tt.errMsg) {
						t.Errorf("error = %q, want to contain %q", got, tt.errMsg)
					}
				}
			} else {
				if got != "" {
					t.Errorf("expected no error, got %q", got)
				}
			}
		})
	}
}

func containsStr(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
