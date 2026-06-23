package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testKey32 = "12345678901234567890123456789012" // exactly 32 chars

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv("ENCRYPTION_KEY", key)
}

// ---------------------------------------------------------------------------
// Encrypt
// ---------------------------------------------------------------------------

func TestEncrypt_EmptyString(t *testing.T) {
	// Empty input → empty output, no error, regardless of key
	setKey(t, testKey32)
	got, err := Encrypt("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestEncrypt_NoKey_ReturnsPlaintext(t *testing.T) {
	// No ENCRYPTION_KEY set → passthrough
	setKey(t, "")
	plain := "hello world"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != plain {
		t.Errorf("expected plaintext passthrough, got %q", got)
	}
}

func TestEncrypt_ShortKey_ReturnsPlaintext(t *testing.T) {
	setKey(t, "tooshort") // not 32 chars
	plain := "some secret"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != plain {
		t.Errorf("expected plaintext passthrough with short key, got %q", got)
	}
}

func TestEncrypt_LongKey_ReturnsPlaintext(t *testing.T) {
	setKey(t, strings.Repeat("a", 64)) // too long
	plain := "some secret"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != plain {
		t.Errorf("expected plaintext passthrough with long key, got %q", got)
	}
}

func TestEncrypt_ValidKey_ReturnsBase64(t *testing.T) {
	setKey(t, testKey32)
	plain := "my secret value"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == plain {
		t.Error("encrypted output should differ from plaintext")
	}
	// Must be valid base64-URL encoding
	if _, err := base64.URLEncoding.DecodeString(got); err != nil {
		t.Errorf("output is not valid base64-URL: %v", err)
	}
}

func TestEncrypt_ValidKey_DifferentCiphertextsEachCall(t *testing.T) {
	// AES-GCM uses a random nonce, so two encryptions of the same plaintext differ
	setKey(t, testKey32)
	plain := "same plaintext"
	ct1, err1 := Encrypt(plain)
	ct2, err2 := Encrypt(plain)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if ct1 == ct2 {
		t.Error("two encryptions of the same string should differ (random nonce)")
	}
}

// ---------------------------------------------------------------------------
// Decrypt
// ---------------------------------------------------------------------------

func TestDecrypt_EmptyString(t *testing.T) {
	setKey(t, testKey32)
	got, err := Decrypt("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestDecrypt_NoKey_ReturnsInput(t *testing.T) {
	setKey(t, "")
	ct := "anything"
	got, err := Decrypt(ct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ct {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestDecrypt_ShortKey_ReturnsInput(t *testing.T) {
	setKey(t, "tooshort")
	ct := "ciphertext"
	got, err := Decrypt(ct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ct {
		t.Errorf("expected passthrough with short key, got %q", got)
	}
}

func TestDecrypt_NotBase64_ReturnsOriginal(t *testing.T) {
	// A string that is not valid base64-URL decodes to an error — should return original
	setKey(t, testKey32)
	raw := "not-valid-base64!!!"
	got, err := Decrypt(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != raw {
		t.Errorf("expected original string back, got %q", got)
	}
}

func TestDecrypt_TooShortAfterBase64_ReturnsOriginal(t *testing.T) {
	// Valid base64 that decodes to fewer bytes than the GCM nonce size (12 bytes)
	setKey(t, testKey32)
	// Encode 5 bytes → less than nonce size
	short := base64.URLEncoding.EncodeToString([]byte("hello"))
	got, err := Decrypt(short)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the original encoded string (too short)
	if got != short {
		t.Errorf("expected original short ciphertext back, got %q", got)
	}
}

func TestDecrypt_WrongKey_ReturnsOriginal(t *testing.T) {
	// Encrypt with key A, then try to decrypt with key B → should return original without error
	setKey(t, testKey32)
	ct, err := Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}

	// Switch to a different valid key
	setKey(t, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAaaaa")
	got, decErr := Decrypt(ct)
	if decErr != nil {
		t.Fatalf("unexpected error on wrong key: %v", decErr)
	}
	// Should return the ciphertext unchanged (decryption failed gracefully)
	if got != ct {
		t.Errorf("expected ciphertext back on wrong key, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Round-trip
// ---------------------------------------------------------------------------

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	setKey(t, testKey32)
	cases := []string{
		"hello world",
		"super secret token",
		"unicode: 日本語 αβγ",
		strings.Repeat("a", 1000),
		"special chars: !@#$%^&*()",
		"with\nnewlines\nand\ttabs",
	}
	for _, plain := range cases {
		ct, err := Encrypt(plain)
		if err != nil {
			t.Errorf("Encrypt(%q) error: %v", plain, err)
			continue
		}
		got, err := Decrypt(ct)
		if err != nil {
			t.Errorf("Decrypt error for %q: %v", plain, err)
			continue
		}
		if got != plain {
			t.Errorf("round-trip mismatch: got %q, want %q", got, plain)
		}
	}
}

func TestEncryptDecrypt_EmptyRoundTrip(t *testing.T) {
	setKey(t, testKey32)
	ct, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	got, err := Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if got != "" {
		t.Errorf("empty round-trip: got %q, want \"\"", got)
	}
}

func TestEncryptDecrypt_PlaintextPassthroughRoundTrip(t *testing.T) {
	// When no key is set, both Encrypt and Decrypt are identity → round-trip works
	setKey(t, "")
	plain := "passthrough value"
	ct, _ := Encrypt(plain)   // returns plain unchanged
	got, _ := Decrypt(ct)     // receives plain, not valid base64 → returns it unchanged
	if got != plain {
		t.Errorf("passthrough round-trip: got %q, want %q", got, plain)
	}
}

// ---------------------------------------------------------------------------
// Key boundary: exactly 32 chars is the only valid length
// ---------------------------------------------------------------------------

func TestEncrypt_ExactlyOneByteTooShort(t *testing.T) {
	setKey(t, strings.Repeat("x", 31))
	plain := "test"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != plain {
		t.Errorf("31-char key should passthrough, got %q", got)
	}
}

func TestEncrypt_ExactlyOneByteTooLong(t *testing.T) {
	setKey(t, strings.Repeat("x", 33))
	plain := "test"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != plain {
		t.Errorf("33-char key should passthrough, got %q", got)
	}
}

func TestEncrypt_Exactly32Chars_Encrypts(t *testing.T) {
	setKey(t, strings.Repeat("x", 32))
	plain := "test"
	got, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == plain {
		t.Error("32-char key should encrypt, not passthrough")
	}
}
