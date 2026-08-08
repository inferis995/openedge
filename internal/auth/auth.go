package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

const (
	// maxFailedLogins / lockoutDuration implement the documented policy:
	// 5 failures → 30-minute lock. Both the password stage and the MFA stage
	// spend from this same budget.
	maxFailedLogins = 5
	lockoutDuration = 30 * time.Minute

	// TOTP parameters. totpSkew is deliberately kept at 1 step (±30 s, i.e. a 90 s
	// acceptance window) — RFC 6238 §5.2 names one step as the largest skew that
	// should be tolerated. Combined with replay tracking (users.last_totp_counter)
	// each generated code is usable exactly once.
	totpPeriod = 30
	totpSkew   = 1
)

var totpOpts = totp.ValidateOpts{
	Period:    totpPeriod,
	Skew:      0, // we walk the window ourselves so we learn WHICH step matched
	Digits:    otp.DigitsSix,
	Algorithm: otp.AlgorithmSHA1,
}

// validateTOTPWithCounter validates code against secret and, on success, returns the
// time-step counter that matched. The caller needs the counter to reject replays:
// totp.Validate alone accepts the same code for the entire skew window.
func validateTOTPWithCounter(secret, code string) (bool, int64) {
	if secret == "" || code == "" {
		return false, 0
	}
	current := time.Now().Unix() / totpPeriod
	for delta := int64(-totpSkew); delta <= totpSkew; delta++ {
		counter := current + delta
		ok, err := totp.ValidateCustom(code, secret, time.Unix(counter*totpPeriod, 0).UTC(), totpOpts)
		if err == nil && ok {
			return true, counter
		}
	}
	return false, 0
}

func validateTOTP(secret, code string) bool {
	ok, _ := validateTOTPWithCounter(secret, code)
	return ok
}

// GenerateRecoveryCodes creates 8 one-time recovery codes, stores hashes in DB,
// and returns the plaintext codes to show to the user once.
func (s *Service) GenerateRecoveryCodes(ctx context.Context, userID int) ([]string, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=$1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 8)
	for i := range codes {
		b := make([]byte, 6)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		plain := fmt.Sprintf("%X-%X-%X", b[0:2], b[2:4], b[4:6])
		// DefaultCost, not MinCost: a recovery code carries only ~48 bits of entropy,
		// so cost 4 made an offline crack of a leaked mfa_recovery_codes table cheap.
		// Existing stored codes keep working — bcrypt encodes its cost in the hash,
		// so CompareHashAndPassword still verifies them at their original cost.
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO mfa_recovery_codes (user_id, code_hash) VALUES ($1, $2)`, userID, string(hash)); err != nil {
			return nil, err
		}
		codes[i] = plain
	}
	return codes, nil
}

// SecretKey is loaded from JWT_SECRET env var at startup; the process exits if it is not set.
var SecretKey []byte

func init() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("[AUTH] JWT_SECRET environment variable is required. " +
			"Generate one with: openssl rand -hex 32")
	}
	if len(secret) < 32 {
		log.Fatal("[AUTH] JWT_SECRET is too short — minimum 32 characters required. " +
			"Generate a secure one with: openssl rand -hex 32")
	}
	SecretKey = []byte(secret)
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// LoginRequestWithMeta extends LoginRequest with metadata for audit logging
type LoginRequestWithMeta struct {
	models.LoginRequest
	IPAddress string
	UserAgent string
}

// Login verifies credentials and returns a JWT token and user info
func (s *Service) Login(ctx context.Context, req models.LoginRequest) (*models.LoginResponse, error) {
	return s.LoginWithMeta(ctx, req, "", "")
}

// registerFailedLogin increments failed_login_count and, at maxFailedLogins, locks
// the account for lockoutDuration. It is shared by the password stage and the MFA
// stage so that MFA guesses are counted against the same budget — previously the
// counters were reset the instant the password matched, which meant an attacker
// holding a valid password could brute force the 6-digit TOTP code forever.
// Returns the new failure count.
func (s *Service) registerFailedLogin(ctx context.Context, user models.User, currentCount int, ipAddress string) int {
	newCount := currentCount + 1
	if newCount >= maxFailedLogins {
		lockUntil := time.Now().Add(lockoutDuration)
		_, _ = s.db.ExecContext(ctx,
			`UPDATE users SET failed_login_count=$1, locked_until=$2 WHERE id=$3`,
			newCount, lockUntil, user.ID)
		// Log security event
		go func() {
			_, _ = s.db.ExecContext(context.Background(), `INSERT INTO security_events (org_id, event_type, severity, actor, resource, detail)
				VALUES ($1, 'account_locked', 'high', $2, $3, '{}')`,
				user.OrgID, user.Username, ipAddress)
		}()
	} else {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE users SET failed_login_count=$1 WHERE id=$2`,
			newCount, user.ID)
	}
	return newCount
}

// clearLockoutState resets the lockout counters. Called only once a login is
// COMPLETE — i.e. after the MFA stage when MFA is enabled, never merely because
// the password was right.
func (s *Service) clearLockoutState(ctx context.Context, userID int) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET failed_login_count=0, locked_until=NULL WHERE id=$1`, userID)
}

// checkLockout reports the lockout error for a user, or nil if not locked.
func (s *Service) checkLockout(ctx context.Context, user models.User, lockedUntil sql.NullTime, ipAddress, userAgent, action string) error {
	if !lockedUntil.Valid || !lockedUntil.Time.After(time.Now()) {
		return nil
	}
	s.logAudit(&user.ID, user.Username, action, ipAddress, userAgent, map[string]interface{}{
		"reason":       "account_locked",
		"locked_until": lockedUntil.Time.Format(time.RFC3339),
	}, false)
	// Log security event
	go func() {
		_, _ = s.db.ExecContext(context.Background(), `INSERT INTO security_events (org_id, event_type, severity, actor, resource, detail)
			VALUES ($1, 'account_locked_attempt', 'high', $2, $3, '{}')`,
			user.OrgID, user.Username, ipAddress)
	}()
	return errors.New("account bloccato fino a " + lockedUntil.Time.Format("15:04:05 02/01/2006"))
}

// LoginWithMeta verifies credentials and returns a JWT token and user info with audit logging
func (s *Service) LoginWithMeta(ctx context.Context, req models.LoginRequest, ipAddress, userAgent string) (*models.LoginResponse, error) {
	var user models.User
	var passwordHash string
	var failedLoginCount int
	var lockedUntil sql.NullTime
	var totpEnabled bool
	var totpSecret sql.NullString

	query := `SELECT id, username, password_hash, role, full_name, org_id, i3x_write, created_at,
	          failed_login_count, locked_until, totp_enabled, totp_secret FROM users WHERE username = $1`
	err := s.db.QueryRowContext(ctx, query, req.Username).Scan(
		&user.ID, &user.Username, &passwordHash, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt,
		&failedLoginCount, &lockedUntil, &totpEnabled, &totpSecret,
	)

	if err == sql.ErrNoRows {
		// Log failed login attempt (user not found)
		s.logAudit(nil, req.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason": "user_not_found",
		}, false)
		return nil, errors.New("invalid credentials")
	}
	if err != nil {
		return nil, err
	}

	// Check account lockout
	if lockErr := s.checkLockout(ctx, user, lockedUntil, ipAddress, userAgent, "login"); lockErr != nil {
		return nil, lockErr
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		newCount := s.registerFailedLogin(ctx, user, failedLoginCount, ipAddress)
		// Log failed login attempt (wrong password)
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason":             "invalid_password",
			"failed_login_count": newCount,
		}, false)
		return nil, errors.New("invalid credentials")
	}

	// NOTE: the lockout counters are deliberately NOT reset here. The password is
	// only the first factor; clearing the budget now would make the 5-failure lock
	// unreachable for the MFA stage. They are cleared once the login is COMPLETE —
	// below for the no-MFA path, and in CompleteMFALogin otherwise.

	// Block login if org requires MFA and user hasn't set it up
	if !totpEnabled {
		var orgMFARequired bool
		if user.OrgID != nil {
			if err := s.db.QueryRowContext(ctx,
				`SELECT mfa_required FROM organizations WHERE id=$1`, *user.OrgID,
			).Scan(&orgMFARequired); err != nil && err != sql.ErrNoRows {
				// DB error: fail closed — deny login rather than silently bypass MFA policy
				return nil, fmt.Errorf("failed to verify org MFA policy: %w", err)
			}
		}
		if orgMFARequired {
			return &models.LoginResponse{MFASetupRequired: true}, nil
		}
	}

	// If MFA is enabled, return a short-lived challenge token instead of the full JWT
	if totpEnabled && totpSecret.Valid && totpSecret.String != "" {
		mfaToken, err := s.generateMFAToken(user.ID)
		if err != nil {
			return nil, err
		}
		return &models.LoginResponse{MFARequired: true, MFAToken: mfaToken}, nil
	}

	// No MFA — login is complete, so the lockout budget may be cleared now.
	s.clearLockoutState(ctx, user.ID)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at=NOW(), last_login_ip=$1 WHERE id=$2`, ipAddress, user.ID)

	token, err := s.generateToken(ctx, user)
	if err != nil {
		return nil, err
	}

	s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
		"role": user.Role,
	}, true)

	return &models.LoginResponse{Token: token, User: user}, nil
}

// generateMFAToken creates a 5-minute JWT used only as a step-up challenge token.
func (s *Service) generateMFAToken(userID int) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"mfa_step": true,
		"exp":      time.Now().Add(5 * time.Minute).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(SecretKey)
}

// CompleteMFALogin verifies a TOTP code from a mfa_step token and returns the full JWT.
func (s *Service) CompleteMFALogin(ctx context.Context, mfaToken, code, ipAddress, userAgent string) (*models.LoginResponse, error) {
	token, err := jwt.Parse(mfaToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return SecretKey, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired MFA token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["mfa_step"] != true {
		return nil, errors.New("not an MFA step token")
	}
	userIDFloat, ok := claims["user_id"].(float64)
	if !ok || userIDFloat == 0 {
		return nil, errors.New("invalid MFA token: missing user_id")
	}
	userID := int(userIDFloat)

	var user models.User
	var totpSecret string
	var failedLoginCount int
	var lockedUntil sql.NullTime
	var lastTOTPCounter sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		`SELECT id, username, role, full_name, org_id, i3x_write, created_at, totp_secret,
		        failed_login_count, locked_until, last_totp_counter
		 FROM users WHERE id = $1 AND totp_enabled = true`, userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt, &totpSecret,
		&failedLoginCount, &lockedUntil, &lastTOTPCounter)
	if err != nil {
		return nil, errors.New("user not found or MFA not enabled")
	}

	// The MFA stage enforces the same lockout as the password stage: an attacker who
	// already knows the password must not get unlimited attempts at the 6-digit code.
	if lockErr := s.checkLockout(ctx, user, lockedUntil, ipAddress, userAgent, "mfa_verify"); lockErr != nil {
		return nil, lockErr
	}

	totpOK, matchedCounter := validateTOTPWithCounter(totpSecret, code)
	// Replay guard: a TOTP code stays arithmetically valid for the whole skew window,
	// so without this a code observed once (shoulder-surfed, phished, logged by a
	// proxy) could be submitted again. Counters must strictly increase.
	if totpOK && lastTOTPCounter.Valid && matchedCounter <= lastTOTPCounter.Int64 {
		newCount := s.registerFailedLogin(ctx, user, failedLoginCount, ipAddress)
		s.logAudit(&user.ID, user.Username, "mfa_verify", ipAddress, userAgent, map[string]interface{}{
			"reason":             "replayed_code",
			"failed_login_count": newCount,
		}, false)
		return nil, errors.New("codice MFA non valido")
	}

	usedRecoveryCode := false
	if !totpOK {
		// Try recovery codes
		rows, qErr := s.db.QueryContext(ctx,
			`SELECT id, code_hash FROM mfa_recovery_codes WHERE user_id=$1 AND used_at IS NULL`, userID)
		usedID := 0
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var rcID int
				var rcHash string
				if err := rows.Scan(&rcID, &rcHash); err != nil {
					continue
				}
				if bcrypt.CompareHashAndPassword([]byte(rcHash), []byte(code)) == nil {
					usedID = rcID
					break
				}
			}
		}
		if usedID == 0 {
			newCount := s.registerFailedLogin(ctx, user, failedLoginCount, ipAddress)
			s.logAudit(&user.ID, user.Username, "mfa_verify", ipAddress, userAgent, map[string]interface{}{
				"reason":             "invalid_code",
				"failed_login_count": newCount,
			}, false)
			return nil, errors.New("codice MFA non valido")
		}
		usedRecoveryCode = true
		_, _ = s.db.ExecContext(ctx, `UPDATE mfa_recovery_codes SET used_at=NOW() WHERE id=$1`, usedID)
		s.logAudit(&user.ID, user.Username, "mfa_verify_recovery", ipAddress, userAgent, map[string]interface{}{
			"recovery_code_id": usedID,
		}, true)
	}

	// Login is complete — now, and only now, clear the lockout budget.
	s.clearLockoutState(ctx, user.ID)
	if !usedRecoveryCode {
		// Burn the accepted time step so the same code cannot be presented again.
		_, _ = s.db.ExecContext(ctx,
			`UPDATE users SET last_totp_counter=$1 WHERE id=$2`, matchedCounter, user.ID)
	}
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at=NOW(), last_login_ip=$1 WHERE id=$2`, ipAddress, user.ID)

	fullToken, err := s.generateToken(ctx, user)
	if err != nil {
		return nil, err
	}

	s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
		"role": user.Role, "mfa": true,
	}, true)

	return &models.LoginResponse{Token: fullToken, User: user}, nil
}

// tokenVersion reads users.token_version — the JWT invalidation epoch, bumped on
// password reset. Falls back to 0 (never blocks a login) if the column cannot be read.
func (s *Service) tokenVersion(ctx context.Context, userID int) int {
	if s.db == nil {
		return 0
	}
	var v int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(token_version, 0) FROM users WHERE id=$1`, userID).Scan(&v); err != nil {
		log.Printf("[AUTH] Failed to read token_version for user %d: %v", userID, err)
		return 0
	}
	return v
}

func (s *Service) generateToken(ctx context.Context, user models.User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":   user.ID,
		"username":  user.Username,
		"role":      user.Role,
		"i3x_write": user.I3xWrite,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
		// JWT invalidation epoch. ResetPassword bumps users.token_version so that
		// sessions minted with the old password can be repudiated.
		//
		// TODO(security): nothing VERIFIES this claim yet. middleware.RequireAuth must
		// compare it against users.token_version and reject on mismatch — that file is
		// outside the scope of this change, so today the claim is informational only
		// and pre-reset JWTs stay valid until their 24h expiry.
		"token_version": s.tokenVersion(ctx, user.ID),
	}

	// Include org_id if it's not nil (NULL for global admin)
	if user.OrgID != nil {
		claims["org_id"] = *user.OrgID
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(SecretKey)
}

// GenerateTokenForUser creates a JWT for an SSO-provisioned user.
// orgID = 0 is treated as global admin (org_id = NULL in claims).
func (s *Service) GenerateTokenForUser(userID int, username, role string, orgID int) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"role":     role,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}
	if orgID > 0 {
		claims["org_id"] = orgID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(SecretKey)
}

// logAudit writes an audit log entry to the database
func (s *Service) logAudit(userID *int, username, action, ipAddress, userAgent string, details map[string]interface{}, success bool) {
	if ipAddress == "" {
		ipAddress = "0.0.0.0"
	}

	var detailsJSON []byte
	if details != nil {
		detailsJSON, _ = json.Marshal(details)
	}

	query := `INSERT INTO audit_logs (user_id, username, action, ip_address, user_agent, details, success)
	          VALUES ($1, $2, $3, $4, $5, $6, $7)`

	// Use background goroutine for logging to not block the request
	go func() {
		if _, err := s.db.Exec(query, userID, username, action, ipAddress, userAgent, detailsJSON, success); err != nil {
			log.Printf("[AUDIT] Failed to write audit log for user %s action %s: %v", username, action, err)
		}
	}()
}

// Logout logs the logout event
func (s *Service) Logout(userID int, username, ipAddress, userAgent string) {
	s.logAudit(&userID, username, "logout", ipAddress, userAgent, nil, true)
}
