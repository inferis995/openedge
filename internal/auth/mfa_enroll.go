package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/models"
)

// Enrollment at login, for an organization that requires MFA.
//
// The password stage hands such a user a short token that proves the password
// and nothing else: it can read a new TOTP secret and confirm it, and the
// confirmation completes the login. It is not a session — no API accepts it
// but the two enrollment endpoints — and it expires in ten minutes.

const mfaSetupTTL = 10 * time.Minute

func generateMFASetupToken(userID int, username string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":   userID,
		"username":  username,
		"mfa_setup": true,
		"exp":       time.Now().Add(mfaSetupTTL).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(SecretKey)
}

// ParseMFASetupToken returns the user an enrollment token was issued to.
func ParseMFASetupToken(tok string) (int, string, error) {
	t, err := jwt.Parse(tok, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return SecretKey, nil
	})
	if err != nil || !t.Valid {
		return 0, "", errors.New("invalid or expired setup token")
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok || claims["mfa_setup"] != true {
		return 0, "", errors.New("not an MFA setup token")
	}
	id, ok := claims["user_id"].(float64)
	if !ok || id <= 0 {
		return 0, "", errors.New("invalid setup token")
	}
	username, _ := claims["username"].(string)
	return int(id), username, nil
}

// CompleteMFAEnrollment confirms the first code of a secret stored by the
// enrollment start, turns MFA on, and completes the login: a session token,
// and the recovery codes, shown once.
func (s *Service) CompleteMFAEnrollment(ctx context.Context, setupToken, code, ipAddress, userAgent string) (*models.LoginResponse, error) {
	userID, _, err := ParseMFASetupToken(setupToken)
	if err != nil {
		return nil, err
	}

	var user models.User
	var secret sql.NullString
	var enabled bool
	var failedLoginCount int
	var lockedUntil sql.NullTime
	if qErr := s.db.QueryRowContext(ctx,
		`SELECT id, username, role, full_name, org_id, i3x_write, created_at, totp_secret, totp_enabled,
		        failed_login_count, locked_until
		 FROM users WHERE id = $1`, userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt,
		&secret, &enabled, &failedLoginCount, &lockedUntil); qErr != nil {
		return nil, errors.New("user not found")
	}
	if enabled {
		return nil, errors.New("MFA is already enabled: sign in again")
	}
	if !secret.Valid || secret.String == "" {
		return nil, errors.New("start the enrollment first")
	}
	// The same lockout budget as the password and MFA stages.
	if lockErr := s.checkLockout(ctx, user, lockedUntil, ipAddress, userAgent, "mfa_enroll"); lockErr != nil {
		return nil, lockErr
	}
	ok, counter := validateTOTPWithCounter(secret.String, code)
	if !ok {
		newCount := s.registerFailedLogin(ctx, user, failedLoginCount, ipAddress)
		s.logAudit(&user.ID, user.Username, "mfa_enroll", ipAddress, userAgent, map[string]interface{}{
			"reason": "invalid_code", "failed_login_count": newCount,
		}, false)
		return nil, errors.New("invalid code")
	}

	if _, uErr := s.db.ExecContext(ctx,
		`UPDATE users SET totp_enabled = true, last_totp_counter = $1, last_login_at = NOW(), last_login_ip = $2 WHERE id = $3`,
		counter, ipAddress, user.ID); uErr != nil {
		return nil, uErr
	}
	codes, err := s.GenerateRecoveryCodes(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	s.clearLockoutState(ctx, user.ID)
	s.fillPermissions(ctx, &user)
	token, err := s.generateToken(ctx, &user)
	if err != nil {
		return nil, err
	}
	s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
		"role": user.Role, "mfa": true, "mfa_enrolled": true,
	}, true)
	return &models.LoginResponse{Token: token, User: user, RecoveryCodes: codes}, nil
}
