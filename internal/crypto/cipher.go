package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"os"
)

// keyLen is the AES-256 key size. ENCRYPTION_KEY must be exactly this many bytes.
const keyLen = 32

// ErrNoEncryptionKey is returned by Encrypt (and by ValidateKey) when ENCRYPTION_KEY
// is missing or is not exactly 32 bytes. Previously Encrypt silently returned the
// plaintext with a nil error, so callers storing MQTT/cloud credentials in
// global_settings could not tell an encrypted value from one stored raw. Failing
// loudly is the only way the caller can surface the problem.
var ErrNoEncryptionKey = errors.New("crypto: ENCRYPTION_KEY is not set or is not exactly 32 bytes (AES-256) — secrets cannot be encrypted")

// key returns the configured AES-256 key, or ok=false if it is unusable.
func key() ([]byte, bool) {
	k := os.Getenv("ENCRYPTION_KEY")
	if len(k) != keyLen {
		return nil, false
	}
	return []byte(k), true
}

// KeyConfigured reports whether a usable AES-256 ENCRYPTION_KEY is present.
func KeyConfigured() bool {
	_, ok := key()
	return ok
}

// ValidateKey returns ErrNoEncryptionKey when no usable ENCRYPTION_KEY is configured.
// Intended to be called once at startup so an operator learns about a misconfigured
// key before any credential is written, instead of discovering plaintext in the DB later.
func ValidateKey() error {
	if !KeyConfigured() {
		return ErrNoEncryptionKey
	}
	return nil
}

// Encrypt encrypts plain text using AES-GCM and the ENCRYPTION_KEY env var.
// ENCRYPTION_KEY must be a 32-character string (32 bytes = AES-256).
// It returns ErrNoEncryptionKey rather than silently passing the plaintext through.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil // Nothing to encrypt
	}

	k, ok := key()
	if !ok {
		// Fail closed: never hand back an "encrypted" value that is actually plaintext.
		log.Printf("[CRYPTO] ERROR: refusing to encrypt — %v", ErrNoEncryptionKey)
		return "", ErrNoEncryptionKey
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	// Return base64 URL encoded
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an AES-GCM base64 encoded string using the ENCRYPTION_KEY env var.
// ENCRYPTION_KEY must be a 32-character string (32 bytes = AES-256).
//
// Legacy values written before encryption was configured are stored raw, so every
// path that cannot decrypt returns the input unchanged — otherwise existing
// deployments would lose access to their settings. Unlike before, each of those
// fall-open paths is logged loudly so an operator can spot values that are sitting
// in the database in the clear.
func Decrypt(encryptedStr string) (string, error) {
	if encryptedStr == "" {
		return "", nil
	}

	k, ok := key()
	if !ok {
		// Cannot decrypt at all; assume the value is a legacy plaintext one.
		log.Printf("[CRYPTO] WARN: reading setting without decryption — %v", ErrNoEncryptionKey)
		return encryptedStr, nil
	}

	// Try to decode base64
	data, err := base64.URLEncoding.DecodeString(encryptedStr)
	if err != nil {
		// Not base64 at all → legacy plain text value.
		log.Printf("[CRYPTO] WARN: stored value is not base64 — treating as legacy plaintext (stored unencrypted)")
		return encryptedStr, nil
	}

	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		// Too short to carry a GCM nonce → legacy plain text value.
		log.Printf("[CRYPTO] WARN: stored value is shorter than the GCM nonce — treating as legacy plaintext (stored unencrypted)")
		return encryptedStr, nil
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Either the key rotated (real data loss) or this is legacy plaintext that
		// happened to base64-decode. Both are worth an operator's attention.
		log.Printf("[CRYPTO] WARN: AES-GCM open failed — treating as legacy plaintext; if ENCRYPTION_KEY was rotated this value is unrecoverable")
		return encryptedStr, nil
	}

	return string(plaintextBytes), nil
}
