package auth

import "fmt"

// MinPasswordLength is the shortest password this system will store.
//
// It was six. Six characters is a few seconds of offline guessing against a
// bcrypt hash on a rented GPU, and the endpoint that accepted them —
// POST /api/auth/accept-invite — is public and creates a real account inside a
// customer's tenant. The same six applied to POST /api/users, to changing your
// own password, and to the reset-by-email flow: every path in the product that
// sets a password.
//
// Twelve is what scripts/preflight.sh already demands of the initial global
// admin, so this makes one rule out of two, and it is the length NIST SP
// 800-63B pairs with the rest of its advice — no composition rules, no forced
// rotation, length is the thing that matters.
//
// This changes nothing for anyone already signed in: every caller below is a
// path that SETS a password, never one that checks an existing one.
const MinPasswordLength = 12

// ValidatePassword reports whether a password may be stored.
func ValidatePassword(pw string) error {
	// Runes, not bytes: an accented or non-Latin password is not shorter than
	// it looks, and counting bytes would let len("ü"*6) pass as twelve.
	if n := len([]rune(pw)); n < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters (got %d)", MinPasswordLength, n)
	}
	return nil
}
