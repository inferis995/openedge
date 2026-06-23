package main

import (
	"strings"
	"testing"
)

// sanitizeVersion replicates the path-traversal sanitization in checkAndApplyUpdate.
func sanitizeVersion(version string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, version)
}

func TestSanitizeVersion_PathTraversal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.2.3", "v1.2.3"},
		{"1.0.0-rc1", "1.0.0-rc1"},
		{"../etc/passwd", ".._etc_passwd"},
		{"v1.2.3; rm -rf /", "v1.2.3__rm_-rf__"},
		{"v1.2.3\x00evil", "v1.2.3_evil"},
		{"v1.2.3/../../../tmp", "v1.2.3_.._.._.._tmp"},
		{"", ""},
	}

	for _, tt := range tests {
		got := sanitizeVersion(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
		// Ensure no path separators remain
		if strings.Contains(got, "/") {
			t.Errorf("sanitizeVersion(%q) = %q still contains '/'", tt.input, got)
		}
	}
}

func TestSanitizeVersion_SafeCharsPreserved(t *testing.T) {
	safe := "v1.23.456-beta"
	got := sanitizeVersion(safe)
	if got != safe {
		t.Errorf("safe version %q was altered to %q", safe, got)
	}
}
