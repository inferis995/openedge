package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/models"
	"golang.org/x/crypto/bcrypt"
)

type UsersHandler struct {
	db *sql.DB
}

func NewUsersHandler(db *sql.DB) *UsersHandler {
	return &UsersHandler{db: db}
}

// CreateUserRequest represents the request body for creating a user
type CreateUserRequest struct {
	Username string          `json:"username" binding:"required"`
	Password string          `json:"password" binding:"required,min=6"`
	Role     models.UserRole `json:"role" binding:"required,oneof=admin user"`
	FullName string          `json:"full_name"`
}

// UpdateUserRequest represents the request body for updating a user
type UpdateUserRequest struct {
	Password string          `json:"password"` // Optional - only update if provided
	Role     models.UserRole `json:"role" binding:"omitempty,oneof=admin user"`
	FullName string          `json:"full_name"`
}

// List returns all users
func (h *UsersHandler) List(c *gin.Context) {
	query := `SELECT id, username, role, full_name, created_at FROM users ORDER BY id`
	rows, err := h.db.QueryContext(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan user"})
			return
		}
		users = append(users, user)
	}

	if users == nil {
		users = []models.User{}
	}

	c.JSON(http.StatusOK, users)
}

// Create adds a new user
func (h *UsersHandler) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Insert user
	query := `INSERT INTO users (username, password_hash, role, full_name) VALUES ($1, $2, $3, $4) RETURNING id, created_at`
	var user models.User
	user.Username = req.Username
	user.Role = req.Role
	user.FullName = req.FullName

	err = h.db.QueryRowContext(c.Request.Context(), query, req.Username, string(hashedPassword), req.Role, req.FullName).Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		// Check for unique constraint violation
		if err.Error() == `pq: duplicate key value violates unique constraint "users_username_key"` {
			c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, user)
}

// Update modifies an existing user
func (h *UsersHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user exists
	var existingUser models.User
	checkQuery := `SELECT id, username, role, full_name, created_at FROM users WHERE id = $1`
	err = h.db.QueryRowContext(c.Request.Context(), checkQuery, id).Scan(
		&existingUser.ID, &existingUser.Username, &existingUser.Role, &existingUser.FullName, &existingUser.CreatedAt,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	// Build update query dynamically
	if req.Password != "" {
		// Update password
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		_, err = h.db.ExecContext(c.Request.Context(), `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hashedPassword), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}
	}

	// Update role and full_name
	updateQuery := `UPDATE users SET role = COALESCE(NULLIF($1, ''), role), full_name = $2 WHERE id = $3`
	_, err = h.db.ExecContext(c.Request.Context(), updateQuery, req.Role, req.FullName, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	// Fetch updated user
	err = h.db.QueryRowContext(c.Request.Context(), checkQuery, id).Scan(
		&existingUser.ID, &existingUser.Username, &existingUser.Role, &existingUser.FullName, &existingUser.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated user"})
		return
	}

	c.JSON(http.StatusOK, existingUser)
}

// Delete removes a user
func (h *UsersHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Get current user ID from JWT claims
	userIDClaim, exists := c.Get("user_id")
	if exists {
		// Prevent self-deletion
		if currentUserID, ok := userIDClaim.(float64); ok && int(currentUserID) == id {
			c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete your own account"})
			return
		}
	}

	// Delete user
	result, err := h.db.ExecContext(c.Request.Context(), `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User deleted successfully"})
}
