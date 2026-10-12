package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestMFASetupToken_RoundTrip(t *testing.T) {
	tok, err := generateMFASetupToken(42, "op.site")
	if err != nil {
		t.Fatal(err)
	}
	id, name, err := ParseMFASetupToken(tok)
	if err != nil || id != 42 || name != "op.site" {
		t.Fatalf("got %d %q %v", id, name, err)
	}
}

func TestMFASetupToken_RejectsOtherTokens(t *testing.T) {
	// The MFA step token (password proven, second factor pending) is not an
	// enrollment token, and neither is an expired one.
	step, _ := (&Service{}).generateMFAToken(42)
	if _, _, err := ParseMFASetupToken(step); err == nil {
		t.Error("an MFA step token was accepted for enrollment")
	}
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(42), "mfa_setup": true, "exp": time.Now().Add(-time.Minute).Unix(),
	}).SignedString(SecretKey)
	if _, _, err := ParseMFASetupToken(expired); err == nil {
		t.Error("an expired setup token was accepted")
	}
	if _, _, err := ParseMFASetupToken("not-a-token"); err == nil {
		t.Error("garbage was accepted")
	}
}
