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
	OrgID    *int            `json:"org_id"`
	I3xWrite bool            `json:"i3x_write"`
}

// UpdateUserRequest represents the request body for updating a user
type UpdateUserRequest struct {
	Password string          `json:"password"`
	Role     models.UserRole `json:"role" binding:"omitempty,oneof=admin user"`
	FullName string          `json:"full_name"`
	OrgID    *int            `json:"org_id"`
	I3xWrite *bool           `json:"i3x_write"` // pointer so omitted ≠ false
}

// List returns all users
func (h *UsersHandler) List(c *gin.Context) {
	query := `
		SELECT u.id, u.username, u.role, u.full_name, u.org_id, u.created_at, u.i3x_write, o.name as org_name
		FROM users u
		LEFT JOIN organizations o ON u.org_id = o.id
		ORDER BY u.id
	`
	rows, err := h.db.QueryContext(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	defer rows.Close()

	type UserWithOrgName struct {
		ID        int             `json:"id"`
		Username  string          `json:"username"`
		Role      models.UserRole `json:"role"`
		FullName  string          `json:"full_name"`
		OrgID     *int            `json:"org_id"`
		OrgName   *string         `json:"org_name,omitempty"`
		I3xWrite  bool            `json:"i3x_write"`
		CreatedAt string          `json:"created_at"`
	}

	var users []UserWithOrgName
	for rows.Next() {
		var user UserWithOrgName
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.FullName, &user.OrgID, &user.CreatedAt, &user.I3xWrite, &user.OrgName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan user"})
			return
		}
		users = append(users, user)
	}

	if users == nil {
		users = []UserWithOrgName{}
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
	query := `INSERT INTO users (username, password_hash, role, full_name, org_id, i3x_write) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`
	var user models.User
	user.Username = req.Username
	user.Role = req.Role
	user.FullName = req.FullName
	user.OrgID = req.OrgID
	user.I3xWrite = req.I3xWrite

	err = h.db.QueryRowContext(c.Request.Context(), query, req.Username, string(hashedPassword), req.Role, req.FullName, req.OrgID, req.I3xWrite).Scan(&user.ID, &user.CreatedAt)
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
	checkQuery := `SELECT id, username, role, full_name, org_id, i3x_write, created_at FROM users WHERE id = $1`
	err = h.db.QueryRowContext(c.Request.Context(), checkQuery, id).Scan(
		&existingUser.ID, &existingUser.Username, &existingUser.Role, &existingUser.FullName, &existingUser.OrgID, &existingUser.I3xWrite, &existingUser.CreatedAt,
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

	// Resolve i3x_write: keep existing value if not provided in the request
	var existingI3xWrite bool
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT i3x_write FROM users WHERE id = $1`, id).Scan(&existingI3xWrite)
	newI3xWrite := existingI3xWrite
	if req.I3xWrite != nil {
		newI3xWrite = *req.I3xWrite
	}

	// Update role, full_name, org_id, i3x_write
	updateQuery := `UPDATE users SET role = COALESCE(NULLIF($1, ''), role), full_name = $2, org_id = $3, i3x_write = $4 WHERE id = $5`
	_, err = h.db.ExecContext(c.Request.Context(), updateQuery, req.Role, req.FullName, req.OrgID, newI3xWrite, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	// Fetch updated user with org name
	var orgName *string
	fullQuery := `
		SELECT u.id, u.username, u.role, u.full_name, u.org_id, u.i3x_write, u.created_at, o.name as org_name
		FROM users u
		LEFT JOIN organizations o ON u.org_id = o.id
		WHERE u.id = $1
	`
	err = h.db.QueryRowContext(c.Request.Context(), fullQuery, id).Scan(
		&existingUser.ID, &existingUser.Username, &existingUser.Role, &existingUser.FullName,
		&existingUser.OrgID, &existingUser.I3xWrite, &existingUser.CreatedAt, &orgName,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated user"})
		return
	}

	type UserWithOrgName struct {
		ID        int             `json:"id"`
		Username  string          `json:"username"`
		Role      models.UserRole `json:"role"`
		FullName  string          `json:"full_name"`
		OrgID     *int            `json:"org_id"`
		OrgName   *string         `json:"org_name,omitempty"`
		I3xWrite  bool            `json:"i3x_write"`
		CreatedAt string          `json:"created_at"`
	}

	response := UserWithOrgName{
		ID:        existingUser.ID,
		Username:  existingUser.Username,
		Role:      existingUser.Role,
		FullName:  existingUser.FullName,
		OrgID:     existingUser.OrgID,
		OrgName:   orgName,
		I3xWrite:  existingUser.I3xWrite,
		CreatedAt: existingUser.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}

	c.JSON(http.StatusOK, response)
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
