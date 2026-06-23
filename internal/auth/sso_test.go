package auth_test

import (
	"strings"
	"testing"
)

// sanitizeUsername mirrors the strings.Map logic in UpsertSSOUser.
// It extracts the local-part of the email (before @), lowercases it, and
// replaces any character that is not [a-z0-9_-] with an underscore.
func sanitizeUsername(email string) string {
	base := strings.Split(email, "@")[0]
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, strings.ToLower(base))
}

func TestSSOUsernameSimple(t *testing.T) {
	got := sanitizeUsername("alice@example.com")
	if got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}
}

func TestSSOUsernameUppercase(t *testing.T) {
	got := sanitizeUsername("Alice.Smith@example.com")
	if got != "alice_smith" {
		t.Errorf("got %q, want %q", got, "alice_smith")
	}
}

func TestSSOUsernameDots(t *testing.T) {
	got := sanitizeUsername("john.doe@corp.io")
	if got != "john_doe" {
		t.Errorf("got %q, want %q", got, "john_doe")
	}
}

func TestSSOUsernameSpaces(t *testing.T) {
	// Some providers encode display names as local-part; spaces must become underscores.
	got := sanitizeUsername("john doe@example.com")
	if got != "john_doe" {
		t.Errorf("got %q, want %q", got, "john_doe")
	}
}

func TestSSOUsernameSpecialChars(t *testing.T) {
	got := sanitizeUsername("user+tag!foo#bar@example.com")
	// '+', '!', '#' all become '_'
	if got != "user_tag_foo_bar" {
		t.Errorf("got %q, want %q", got, "user_tag_foo_bar")
	}
}

func TestSSOUsernameHyphenAndUnderscore(t *testing.T) {
	// Hyphens and underscores are allowed and must pass through unchanged.
	got := sanitizeUsername("my-user_name@example.com")
	if got != "my-user_name" {
		t.Errorf("got %q, want %q", got, "my-user_name")
	}
}

func TestSSOUsernameAllDigits(t *testing.T) {
	got := sanitizeUsername("12345@example.com")
	if got != "12345" {
		t.Errorf("got %q, want %q", got, "12345")
	}
}

func TestSSOUsernameEmptyLocalPart(t *testing.T) {
	// Edge case: email is just "@domain" — local part is empty string.
	got := sanitizeUsername("@example.com")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestSSOUsernameNoAtSign(t *testing.T) {
	// strings.Split with "@" on a string with no "@" returns the whole string.
	// This is not a valid email but the function should not panic.
	got := sanitizeUsername("nodomain")
	if got != "nodomain" {
		t.Errorf("got %q, want %q", got, "nodomain")
	}
}

func TestSSOUsernameUnicodeChars(t *testing.T) {
	// Non-ASCII characters should be replaced with underscores.
	got := sanitizeUsername("héllo@example.com")
	if got != "h_llo" {
		t.Errorf("got %q, want %q", got, "h_llo")
	}
}

func TestSSOUsernameAllSpecial(t *testing.T) {
	// A local part made entirely of characters that must be replaced.
	got := sanitizeUsername("!!!@example.com")
	if got != "___" {
		t.Errorf("got %q, want %q", got, "___")
	}
}
