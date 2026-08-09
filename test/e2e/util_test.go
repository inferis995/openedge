//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
)

// uniqueSuffix returns a short random string so a test run never collides with
// leftovers from a previous one (organisation and user names are unique).
func uniqueSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b)
}
