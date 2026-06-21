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
	"github.com/pquerna/otp/totp"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func validateTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
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
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
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
	if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason":       "account_locked",
			"locked_until": lockedUntil.Time.Format(time.RFC3339),
		}, false)
		// Log security event
		go func() {
			_, _ = s.db.ExecContext(context.Background(), `INSERT INTO security_events (org_id, event_type, severity, actor, resource, detail)
				VALUES ($1, 'account_locked_attempt', 'high', $2, $3, '{}')`,
				user.OrgID, user.Username, ipAddress)
		}()
		return nil, errors.New("account bloccato fino a " + lockedUntil.Time.Format("15:04:05 02/01/2006"))
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		// Increment failed login count
		newCount := failedLoginCount + 1
		if newCount >= 5 {
			// Lock the account for 30 minutes
			lockUntil := time.Now().Add(30 * time.Minute)
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
		// Log failed login attempt (wrong password)
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason":             "invalid_password",
			"failed_login_count": newCount,
		}, false)
		return nil, errors.New("invalid credentials")
	}

	// Successful password auth — reset lockout state
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET failed_login_count=0, locked_until=NULL WHERE id=$1`, user.ID)

	// Block login if org requires MFA and user hasn't set it up
	if !totpEnabled {
		var orgMFARequired bool
		if user.OrgID != nil {
			_ = s.db.QueryRowContext(ctx,
				`SELECT mfa_required FROM organizations WHERE id=$1`, *user.OrgID,
			).Scan(&orgMFARequired)
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

	// No MFA — complete login
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at=NOW(), last_login_ip=$1 WHERE id=$2`, ipAddress, user.ID)

	token, err := s.generateToken(user)
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
	userID := int(claims["user_id"].(float64))

	var user models.User
	var totpSecret string
	err = s.db.QueryRowContext(ctx,
		`SELECT id, username, role, full_name, org_id, i3x_write, created_at, totp_secret
		 FROM users WHERE id = $1 AND totp_enabled = true`, userID,
	).Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt, &totpSecret)
	if err != nil {
		return nil, errors.New("user not found or MFA not enabled")
	}

	if !validateTOTP(totpSecret, code) {
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
			s.logAudit(&user.ID, user.Username, "mfa_verify", ipAddress, userAgent, map[string]interface{}{
				"reason": "invalid_code",
			}, false)
			return nil, errors.New("codice MFA non valido")
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE mfa_recovery_codes SET used_at=NOW() WHERE id=$1`, usedID)
		s.logAudit(&user.ID, user.Username, "mfa_verify_recovery", ipAddress, userAgent, map[string]interface{}{
			"recovery_code_id": usedID,
		}, true)
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET last_login_at=NOW(), last_login_ip=$1 WHERE id=$2`, ipAddress, user.ID)

	fullToken, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
		"role": user.Role, "mfa": true,
	}, true)

	return &models.LoginResponse{Token: fullToken, User: user}, nil
}

func (s *Service) generateToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":   user.ID,
		"username":  user.Username,
		"role":      user.Role,
		"i3x_write": user.I3xWrite,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
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
