package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

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
	var failedCount int
	var lockedUntil sql.NullTime

	query := `SELECT id, username, password_hash, role, full_name, org_id, i3x_write, created_at,
	           COALESCE(failed_login_count,0), locked_until
	           FROM users WHERE username = $1`
	err := s.db.QueryRowContext(ctx, query, req.Username).Scan(
		&user.ID, &user.Username, &passwordHash, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt,
		&failedCount, &lockedUntil,
	)

	if err == sql.ErrNoRows {
		s.logAudit(nil, req.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason": "user_not_found",
		}, false)
		return nil, errors.New("invalid credentials")
	}
	if err != nil {
		return nil, err
	}

	// Account lockout check
	if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason": "account_locked",
		}, false)
		return nil, errors.New("account temporaneamente bloccato, riprova tra qualche minuto")
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		newCount := failedCount + 1
		var lockSQL string
		if newCount >= 5 {
			lockSQL = `UPDATE users SET failed_login_count=$1, locked_until=NOW()+INTERVAL '15 minutes' WHERE id=$2`
		} else {
			lockSQL = `UPDATE users SET failed_login_count=$1 WHERE id=$2`
		}
		go func() { _, _ = s.db.Exec(lockSQL, newCount, user.ID) }()
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason": "invalid_password", "attempt": newCount,
		}, false)
		return nil, errors.New("invalid credentials")
	}

	// Reset lockout on success
	go func() {
		_, _ = s.db.Exec(
			`UPDATE users SET failed_login_count=0, locked_until=NULL, last_login_at=NOW(), last_login_ip=$1 WHERE id=$2`,
			ipAddress, user.ID,
		)
	}()

	// Generate JWT
	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
		"role": user.Role,
	}, true)

	return &models.LoginResponse{
		Token: token,
		User:  user,
	}, nil
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
