// export_test.go exposes unexported alarm helpers for white-box testing.
// Only compiled during `go test`.
package alarms

import "github.com/ralph/industrial-edge-middleware/internal/models"

// IsConditionViolated exposes the unexported isConditionViolated for testing.
func IsConditionViolated(def models.AlarmDefinition, val float64) bool {
	return isConditionViolated(def, val)
}

// IsCleared exposes the unexported isCleared for testing.
func IsCleared(def models.AlarmDefinition, val float64) bool {
	return isCleared(def, val)
}

// ToFloat exposes the unexported toFloat for testing.
func ToFloat(val interface{}) (float64, bool) {
	return toFloat(val)
}
