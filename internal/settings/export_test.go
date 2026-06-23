// export_test.go exposes internal helpers for white-box testing.
// Only compiled during `go test`.
package settings

// ValuesEqual exposes the unexported valuesEqual function for testing.
func ValuesEqual(a, b interface{}, deadbandPercent float64) bool {
	return valuesEqual(a, b, deadbandPercent)
}

// ParseInt exposes parseInt for testing.
func ParseInt(s string, def int) int {
	return parseInt(s, def)
}

// ParseFloat exposes parseFloat for testing.
func ParseFloat(s string, def float64) float64 {
	return parseFloat(s, def)
}
