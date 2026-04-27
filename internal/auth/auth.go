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

	query := `SELECT id, username, password_hash, role, full_name, org_id, i3x_write, created_at FROM users WHERE username = $1`
	err := s.db.QueryRowContext(ctx, query, req.Username).Scan(
		&user.ID, &user.Username, &passwordHash, &user.Role, &user.FullName, &user.OrgID, &user.I3xWrite, &user.CreatedAt,
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

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		// Log failed login attempt (wrong password)
		s.logAudit(&user.ID, user.Username, "login", ipAddress, userAgent, map[string]interface{}{
			"reason": "invalid_password",
		}, false)
		return nil, errors.New("invalid credentials")
	}

	// Generate JWT
	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	// Log successful login
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
