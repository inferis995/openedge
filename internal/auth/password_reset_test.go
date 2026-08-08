// White-box tests for password reset token handling — must be package auth so the
// unexported hashResetToken / generateResetToken helpers are reachable.
package auth

import (
	"encoding/hex"
	"testing"
)

func TestGenerateResetToken_ShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		tok, err := generateResetToken()
		if err != nil {
			t.Fatalf("generateResetToken: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token length = %d, want 64 hex chars (32 random bytes)", len(tok))
		}
		if _, err := hex.DecodeString(tok); err != nil {
			t.Fatalf("token is not hex: %v", err)
		}
		if seen[tok] {
			t.Fatal("generateResetToken produced a duplicate")
		}
		seen[tok] = true
	}
}

func TestHashResetToken_IsDeterministicDigest(t *testing.T) {
	tok, err := generateResetToken()
	if err != nil {
		t.Fatalf("generateResetToken: %v", err)
	}

	d1 := hashResetToken(tok)
	d2 := hashResetToken(tok)
	if d1 != d2 {
		t.Error("hashResetToken must be deterministic — lookups are done by digest equality")
	}
	if d1 == tok {
		t.Error("digest must differ from the token; the DB must never hold the live token")
	}
	if len(d1) != 64 {
		t.Errorf("digest length = %d, want 64 (hex SHA-256)", len(d1))
	}
	if _, err := hex.DecodeString(d1); err != nil {
		t.Errorf("digest is not hex: %v", err)
	}
}

func TestHashResetToken_DistinctTokensDistinctDigests(t *testing.T) {
	a, _ := generateResetToken()
	b, _ := generateResetToken()
	if hashResetToken(a) == hashResetToken(b) {
		t.Error("different tokens must produce different digests")
	}
}

func TestHashResetToken_EmptyToken(t *testing.T) {
	// An empty submitted token must not hash to something that could match a row
	// written from a real token.
	if got := hashResetToken(""); len(got) != 64 {
		t.Errorf("digest of empty token = %q, want a 64-char digest", got)
	}
}
