package mqtt

import "testing"

func TestSchemeForPort(t *testing.T) {
	for port, want := range map[int]string{8883: "ssl", 1883: "tcp", 18830: "tcp", 0: "tcp"} {
		if got := SchemeForPort(port); got != want {
			t.Errorf("SchemeForPort(%d) = %q, want %q", port, got, want)
		}
	}
}
