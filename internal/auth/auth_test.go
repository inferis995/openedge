package auth_test

import (
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/auth"
)

func TestMain(m *testing.M) {
	os.Setenv("JWT_SECRET", "test-jwt-secret-key-minimum-32-chars-ok!")
	os.Exit(m.Run())
}

func TestSecretKey_IsSet(t *testing.T) {
	if len(auth.SecretKey) == 0 {
		t.Fatal("SecretKey should be populated after init()")
	}
	if len(auth.SecretKey) < 32 {
		t.Errorf("SecretKey length %d, want >= 32", len(auth.SecretKey))
	}
}

func TestGenerateToken_ValidClaims(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  42,
		"username": "testuser",
		"role":     "user",
	})
	signed, err := token.SignedString(auth.SecretKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	if signed == "" {
		t.Fatal("expected non-empty signed token")
	}

	// Verify it can be parsed back
	parsed, err := jwt.Parse(signed, func(tok *jwt.Token) (interface{}, error) {
		return auth.SecretKey, nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("expected valid token")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}
	if claims["username"] != "testuser" {
		t.Errorf("username = %v, want testuser", claims["username"])
	}
}
