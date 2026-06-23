package mqtt

import (
	"strings"
	"testing"
)

// topicMatch is a method on *Client, but uses no Client state.
// Use a zero-value Client so no broker connection is needed.
var testClient = &Client{}

// ---------------------------------------------------------------------------
// topicMatch — exact
// ---------------------------------------------------------------------------

func TestTopicMatch_Exact(t *testing.T) {
	cases := []struct {
		sub, recv string
		want      bool
	}{
		{"data/1/tag", "data/1/tag", true},
		{"data/1/tag", "data/1/other", false},
		{"a", "a", true},
		{"a", "b", false},
	}
	for _, c := range cases {
		if got := testClient.topicMatch(c.sub, c.recv); got != c.want {
			t.Errorf("topicMatch(%q, %q) = %v, want %v", c.sub, c.recv, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// topicMatch — single-level wildcard +
// ---------------------------------------------------------------------------

func TestTopicMatch_SingleLevelWildcard(t *testing.T) {
	cases := []struct {
		sub, recv string
		want      bool
	}{
		{"data/+/tag", "data/1/tag", true},
		{"data/+/tag", "data/abc/tag", true},
		{"data/+/tag", "data/1/other", false},
		{"+/tag", "foo/tag", true},
		{"+/tag", "foo/bar/tag", false}, // depth mismatch
		{"data/+/+", "data/1/2", true},
		{"data/+/+", "data/1/2/3", false}, // extra level
	}
	for _, c := range cases {
		if got := testClient.topicMatch(c.sub, c.recv); got != c.want {
			t.Errorf("topicMatch(%q, %q) = %v, want %v", c.sub, c.recv, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// topicMatch — multi-level wildcard #
// ---------------------------------------------------------------------------

func TestTopicMatch_MultiLevelWildcard(t *testing.T) {
	cases := []struct {
		sub, recv string
		want      bool
	}{
		{"data/#", "data/1", true},
		{"data/#", "data/1/tag", true},
		{"data/#", "data/1/tag/sub", true},
		{"data/#", "other/1", false},
		{"#", "anything", true},
		{"#", "a/b/c/d", true},
		{"sys/alarms/#", "sys/alarms/org1", true},
		{"sys/alarms/#", "sys/health/org1", false},
		// # with fewer parts than received topic (sub has more fixed segments than recv)
		{"a/b/c/#", "a/b", false},
	}
	for _, c := range cases {
		if got := testClient.topicMatch(c.sub, c.recv); got != c.want {
			t.Errorf("topicMatch(%q, %q) = %v, want %v", c.sub, c.recv, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// GeneratePassword
// ---------------------------------------------------------------------------

func TestGeneratePassword_Length(t *testing.T) {
	pwd, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword() error: %v", err)
	}
	// 16 bytes → 32 hex chars
	if len(pwd) != 32 {
		t.Errorf("GeneratePassword() len = %d, want 32", len(pwd))
	}
}

func TestGeneratePassword_HexCharsOnly(t *testing.T) {
	pwd, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword() error: %v", err)
	}
	for _, ch := range pwd {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			t.Errorf("GeneratePassword() contains non-hex char: %q", ch)
		}
	}
}

func TestGeneratePassword_Unique(t *testing.T) {
	a, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GeneratePassword()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two GeneratePassword() calls returned the same password")
	}
}
