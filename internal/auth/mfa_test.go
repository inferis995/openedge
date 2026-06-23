// Package auth white-box tests — must be package auth (not auth_test) so that
// unexported helpers validateTOTP and generateMFAToken are accessible.
package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
)

// ---------------------------------------------------------------------------
// validateTOTP
// ---------------------------------------------------------------------------

func TestValidateTOTP_ValidCode(t *testing.T) {
	// Generate a real TOTP secret and a valid code for the current time window.
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "testuser@example.com",
	})
	if err != nil {
		t.Fatalf("generate TOTP key: %v", err)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}

	if !validateTOTP(key.Secret(), code) {
		t.Error("validateTOTP returned false for a freshly generated, valid code")
	}
}

func TestValidateTOTP_WrongCode(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "testuser@example.com",
	})
	if err != nil {
		t.Fatalf("generate TOTP key: %v", err)
	}

	if validateTOTP(key.Secret(), "000000") {
		t.Error("validateTOTP returned true for an obviously wrong code")
	}
}

func TestValidateTOTP_EmptyCode(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "testuser@example.com",
	})
	if err != nil {
		t.Fatalf("generate TOTP key: %v", err)
	}

	if validateTOTP(key.Secret(), "") {
		t.Error("validateTOTP returned true for an empty code")
	}
}

func TestValidateTOTP_WrongSecret(t *testing.T) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "testuser@example.com",
	})
	if err != nil {
		t.Fatalf("generate TOTP key: %v", err)
	}

	// Generate a valid code for this key, but validate it against a different secret.
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("generate TOTP code: %v", err)
	}

	otherKey, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "test",
		AccountName: "other@example.com",
	})
	if err != nil {
		t.Fatalf("generate other TOTP key: %v", err)
	}

	if validateTOTP(otherKey.Secret(), code) {
		t.Error("validateTOTP returned true for a code that belongs to a different secret")
	}
}

// ---------------------------------------------------------------------------
// generateMFAToken
// ---------------------------------------------------------------------------

// newServiceNoDB constructs a Service with a nil DB — sufficient for
// generateMFAToken, which only uses SecretKey and never touches the DB.
func newServiceNoDB() *Service {
	return &Service{db: nil}
}

func TestGenerateMFAToken_HasMFAStepClaim(t *testing.T) {
	svc := newServiceNoDB()
	tokenStr, err := svc.generateMFAToken(99)
	if err != nil {
		t.Fatalf("generateMFAToken: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("generateMFAToken returned empty token string")
	}

	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			t.Fatalf("unexpected signing method: %v", tok.Header["alg"])
		}
		return SecretKey, nil
	})
	if err != nil {
		t.Fatalf("parse MFA token: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("MFA token is not valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected jwt.MapClaims")
	}

	// Must have mfa_step = true
	if claims["mfa_step"] != true {
		t.Errorf("mfa_step claim = %v, want true", claims["mfa_step"])
	}

	// Must carry the correct user_id
	if uid, ok := claims["user_id"].(float64); !ok || int(uid) != 99 {
		t.Errorf("user_id claim = %v, want 99", claims["user_id"])
	}
}

func TestGenerateMFAToken_ShortExpiry(t *testing.T) {
	svc := newServiceNoDB()
	before := time.Now()
	tokenStr, err := svc.generateMFAToken(1)
	if err != nil {
		t.Fatalf("generateMFAToken: %v", err)
	}

	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (interface{}, error) {
		return SecretKey, nil
	})
	if err != nil {
		t.Fatalf("parse MFA token: %v", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected jwt.MapClaims")
	}

	expFloat, ok := claims["exp"].(float64)
	if !ok {
		t.Fatal("exp claim missing or not a number")
	}
	expTime := time.Unix(int64(expFloat), 0)

	// The token must expire roughly 5 minutes from now (allow ±10 s for test latency).
	wantExp := before.Add(5 * time.Minute)
	diff := expTime.Sub(wantExp)
	if diff < -10*time.Second || diff > 10*time.Second {
		t.Errorf("token expiry %v is not ~5 minutes from now (diff=%v)", expTime, diff)
	}

	// Sanity: must not already be expired.
	if expTime.Before(time.Now()) {
		t.Error("MFA token is already expired")
	}
}

func TestGenerateMFAToken_DifferentUserIDs(t *testing.T) {
	svc := newServiceNoDB()
	tok1, err := svc.generateMFAToken(1)
	if err != nil {
		t.Fatalf("generateMFAToken(1): %v", err)
	}
	tok2, err := svc.generateMFAToken(2)
	if err != nil {
		t.Fatalf("generateMFAToken(2): %v", err)
	}
	if tok1 == tok2 {
		t.Error("tokens for different user_ids should differ")
	}
}

func TestGenerateMFAToken_NoOrgIDClaim(t *testing.T) {
	// MFA step tokens should not embed org_id — they are pre-auth and the
	// full org context is loaded in CompleteMFALogin from the DB.
	svc := newServiceNoDB()
	tokenStr, err := svc.generateMFAToken(7)
	if err != nil {
		t.Fatalf("generateMFAToken: %v", err)
	}

	parsed, _ := jwt.Parse(tokenStr, func(tok *jwt.Token) (interface{}, error) {
		return SecretKey, nil
	})
	claims := parsed.Claims.(jwt.MapClaims)

	if _, exists := claims["org_id"]; exists {
		t.Error("MFA step token should not contain an org_id claim")
	}
}
