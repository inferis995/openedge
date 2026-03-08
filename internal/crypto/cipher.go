package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"os"
)

// Encrypt encrypts plain text using AES-GCM and the ENCRYPTION_KEY env var
// ENCRYPTION_KEY must be a 32-character string (32 bytes = AES-256)
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil // Nothing to encrypt
	}

	key := os.Getenv("ENCRYPTION_KEY")
	// If key is not set or not valid length for AES-256 (32 bytes), fallback to plaintext
	if len(key) != 32 {
		return plaintext, nil
	}

	block, err := aes.NewCipher([]byte(key))
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

// Decrypt decrypts an AES-GCM base64 encoded string using ENCRYPTION_KEY env var
// ENCRYPTION_KEY must be a 32-character string (32 bytes = AES-256)
func Decrypt(encryptedStr string) (string, error) {
	if encryptedStr == "" {
		return "", nil
	}

	key := os.Getenv("ENCRYPTION_KEY")
	if len(key) != 32 {
		return encryptedStr, nil // fallback if no valid key is configured
	}

	// Try to decode base64
	data, err := base64.URLEncoding.DecodeString(encryptedStr)
	if err != nil {
		// Possibly not encrypted (e.g. legacy plain text), ignore error and return raw
		return encryptedStr, nil
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		// Too short to be encrypted string, return original
		return encryptedStr, nil
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Decryption failed (maybe wrong key, or it's just plain text that happened to base64 decode)
		return encryptedStr, nil
	}

	return string(plaintextBytes), nil
}
