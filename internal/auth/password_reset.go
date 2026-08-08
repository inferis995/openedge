package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/ralph/industrial-edge-middleware/internal/notifications"
	"golang.org/x/crypto/bcrypt"
)

// hashResetToken returns the hex SHA-256 digest of a reset token.
//
// Only the digest is ever persisted in password_reset_tokens.token; the plaintext
// token exists solely in the email we send. Anyone who can read the table (SQL
// injection elsewhere, a stolen pg_dump from scripts/backup.sh, a read replica, a
// DBA) therefore obtains digests, not live account-takeover tokens.
//
// A plain SHA-256 (not bcrypt) is deliberate: the token is 32 bytes of crypto/rand,
// so it has no guessable structure to brute force, and lookups must stay O(index).
//
// NOTE: reset tokens issued before this change are stored as plaintext and will no
// longer match. They become unusable and the user must request a new link — this is
// acceptable because reset tokens live for one hour.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequestPasswordReset looks up the user by email or username, inserts a one-time
// token in password_reset_tokens, and sends the link by email.
// Always returns nil so callers cannot enumerate registered emails.
func (s *Service) RequestPasswordReset(ctx context.Context, emailOrUsername string) {
	var userID int
	var username, email string
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, COALESCE(email, '') FROM users WHERE email = $1 OR username = $1 LIMIT 1`,
		emailOrUsername)
	if err := row.Scan(&userID, &username, &email); err != nil {
		// User not found — return silently to prevent enumeration.
		return
	}
	if email == "" {
		slog.Warn("password reset requested but user has no email", "username", username)
		return
	}

	token, err := generateResetToken()
	if err != nil {
		slog.Error("failed to generate reset token", "err", err)
		return
	}

	// Store the digest only — the plaintext token never touches the database.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (user_id, token) VALUES ($1, $2)`, userID, hashResetToken(token))
	if err != nil {
		slog.Error("failed to insert reset token", "err", err)
		return
	}

	host := os.Getenv("PUBLIC_HOST")
	if host == "" {
		host = "localhost:3000"
	}
	resetURL := fmt.Sprintf("https://%s/reset-password?token=%s", host, token)
	go notifications.SendPasswordResetEmail(s.db, email, username, resetURL)
}

// ResetPassword validates the token and updates the user's password.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < 6 {
		return errors.New("password must be at least 6 characters")
	}

	digest := hashResetToken(token)

	var tokenID, userID int
	var storedDigest string
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token FROM password_reset_tokens
		 WHERE token = $1 AND used_at IS NULL AND expires_at > NOW()`,
		digest)
	if err := row.Scan(&tokenID, &userID, &storedDigest); err == sql.ErrNoRows {
		return errors.New("token is invalid or has expired")
	} else if err != nil {
		return errors.New("failed to validate token")
	}
	// Belt-and-braces: the SQL equality above already selected the row, but compare
	// the digests in constant time so no timing signal is introduced here either.
	if subtle.ConstantTimeCompare([]byte(storedDigest), []byte(digest)) != 1 {
		return errors.New("token is invalid or has expired")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errors.New("failed to hash password")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("failed to start transaction")
	}
	defer tx.Rollback() //nolint:errcheck

	// A completed reset also has to undo the state an attacker (or a locked-out
	// legitimate user) left behind:
	//   - clear the lockout counters, otherwise the user resets their password and
	//     still cannot log in for 30 minutes;
	//   - bump token_version, the JWT invalidation epoch, so sessions minted with
	//     the OLD password are repudiated.
	//
	// TODO(security): token_version is only half of JWT invalidation. generateToken
	// now embeds it as a claim, but nothing rejects a JWT whose token_version is
	// stale — that check belongs in middleware.RequireAuth (compare the claim against
	// users.token_version and 401 on mismatch), which is outside this change's scope.
	// Until that lands, pre-reset JWTs remain valid until their 24h expiry.
	if _, err = tx.ExecContext(ctx,
		`UPDATE users
		 SET password_hash = $1,
		     failed_login_count = 0,
		     locked_until = NULL,
		     token_version = COALESCE(token_version, 0) + 1
		 WHERE id = $2`, string(hash), userID); err != nil {
		return errors.New("failed to update password")
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = NOW() WHERE id = $1`, tokenID); err != nil {
		return errors.New("failed to mark token as used")
	}
	// Invalidate every OTHER outstanding reset token for this user: whoever triggered
	// the extra links (including an attacker who requested one in parallel) must not
	// keep a second shot at the account.
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM password_reset_tokens WHERE user_id = $1 AND id <> $2`, userID, tokenID); err != nil {
		return errors.New("failed to invalidate outstanding reset tokens")
	}
	return tx.Commit()
}

func generateResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
