package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/ralph/industrial-edge-middleware/internal/notifications"
	"golang.org/x/crypto/bcrypt"
)

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

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (user_id, token) VALUES ($1, $2)`, userID, token)
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

	var tokenID, userID int
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id FROM password_reset_tokens
		 WHERE token = $1 AND used_at IS NULL AND expires_at > NOW()`,
		token)
	if err := row.Scan(&tokenID, &userID); err == sql.ErrNoRows {
		return errors.New("token is invalid or has expired")
	} else if err != nil {
		return errors.New("failed to validate token")
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

	if _, err = tx.ExecContext(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), userID); err != nil {
		return errors.New("failed to update password")
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE password_reset_tokens SET used_at = NOW() WHERE id = $1`, tokenID); err != nil {
		return errors.New("failed to mark token as used")
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
