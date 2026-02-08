package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var SecretKey = []byte("industrial-edge-secret-key-change-me-in-production")

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Login verifies credentials and returns a JWT token and user info
func (s *Service) Login(ctx context.Context, req models.LoginRequest) (*models.LoginResponse, error) {
	var user models.User
	var passwordHash string

	query := `SELECT id, username, password_hash, role, full_name, created_at FROM users WHERE username = $1`
	err := s.db.QueryRowContext(ctx, query, req.Username).Scan(
		&user.ID, &user.Username, &passwordHash, &user.Role, &user.FullName, &user.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, errors.New("invalid credentials")
	}
	if err != nil {
		return nil, err
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	// Generate JWT
	token, err := s.generateToken(user)
	if err != nil {
		return nil, err
	}

	// Log access
	s.logAccess(user.ID, user.Username, "LOGIN_SUCCESS", "")

	return &models.LoginResponse{
		Token: token,
		User:  user,
	}, nil
}

func (s *Service) generateToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"role":     user.Role,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(SecretKey)
}

func (s *Service) logAccess(userID int, username, eventType, ipAddress string) {
	query := `INSERT INTO access_logs (user_id, username, event_type, ip_address) VALUES ($1, $2, $3, $4)`
	// Use background context for logging to ensure it completes even if request cancels
	go func() {
		s.db.Exec(query, userID, username, eventType, ipAddress)
	}()
}
