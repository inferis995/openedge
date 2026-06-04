package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
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
	SiteIDs  []int           `json:"site_ids"`
	AreaIDs  []int           `json:"area_ids"`
}

// UpdateUserRequest represents the request body for updating a user
type UpdateUserRequest struct {
	Password string          `json:"password"`
	Role     models.UserRole `json:"role" binding:"omitempty,oneof=admin user"`
	FullName string          `json:"full_name"`
	OrgID    *int            `json:"org_id"`
	I3xWrite *bool           `json:"i3x_write"` // pointer so omitted ≠ false
	SiteIDs  *[]int          `json:"site_ids"`
	AreaIDs  *[]int          `json:"area_ids"`
}

type userWithScope struct {
	ID        int             `json:"id"`
	Username  string          `json:"username"`
	Role      models.UserRole `json:"role"`
	FullName  string          `json:"full_name"`
	OrgID     *int            `json:"org_id"`
	OrgName   *string         `json:"org_name,omitempty"`
	I3xWrite  bool            `json:"i3x_write"`
	SiteIDs   []int           `json:"site_ids"`
	AreaIDs   []int           `json:"area_ids"`
	CreatedAt string          `json:"created_at"`
}

// List returns all users with their site/area scope
func (h *UsersHandler) List(c *gin.Context) {
	query := `
		SELECT u.id, u.username, u.role, u.full_name, u.org_id, u.created_at, u.i3x_write,
		       o.name as org_name,
		       COALESCE(array_agg(DISTINCT us.site_id) FILTER (WHERE us.site_id IS NOT NULL), '{}') AS site_ids,
		       COALESCE(array_agg(DISTINCT ua.area_id) FILTER (WHERE ua.area_id IS NOT NULL), '{}') AS area_ids
		FROM users u
		LEFT JOIN organizations o ON u.org_id = o.id
		LEFT JOIN user_sites us ON u.id = us.user_id
		LEFT JOIN user_areas ua ON u.id = ua.user_id
		GROUP BY u.id, o.name
		ORDER BY u.id
	`
	rows, err := h.db.QueryContext(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch users"})
		return
	}
	defer rows.Close()

	var users []userWithScope
	for rows.Next() {
		var u userWithScope
		var siteIDs pq.Int64Array
		var areaIDs pq.Int64Array
		if err := rows.Scan(
			&u.ID, &u.Username, &u.Role, &u.FullName, &u.OrgID, &u.CreatedAt, &u.I3xWrite,
			&u.OrgName, &siteIDs, &areaIDs,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan user"})
			return
		}
		u.SiteIDs = int64SliceToInt(siteIDs)
		u.AreaIDs = int64SliceToInt(areaIDs)
		users = append(users, u)
	}

	if users == nil {
		users = []userWithScope{}
	}

	c.JSON(http.StatusOK, users)
}

// Create adds a new user with optional site/area scope
func (h *UsersHandler) Create(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	var userID int
	var createdAt string
	err = tx.QueryRowContext(c.Request.Context(),
		`INSERT INTO users (username, password_hash, role, full_name, org_id, i3x_write)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		req.Username, string(hashedPassword), req.Role, req.FullName, req.OrgID, req.I3xWrite,
	).Scan(&userID, &createdAt)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "users_username_key"` {
			c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	if err := insertUserSites(c, tx, userID, req.SiteIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign sites"})
		return
	}
	if err := insertUserAreas(c, tx, userID, req.AreaIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign areas"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusCreated, userWithScope{
		ID:        userID,
		Username:  req.Username,
		Role:      req.Role,
		FullName:  req.FullName,
		OrgID:     req.OrgID,
		I3xWrite:  req.I3xWrite,
		SiteIDs:   nullableIntSlice(req.SiteIDs),
		AreaIDs:   nullableIntSlice(req.AreaIDs),
		CreatedAt: createdAt,
	})
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

	var existingUser models.User
	checkQuery := `SELECT id, username, role, full_name, org_id, i3x_write, created_at FROM users WHERE id = $1`
	err = h.db.QueryRowContext(c.Request.Context(), checkQuery, id).Scan(
		&existingUser.ID, &existingUser.Username, &existingUser.Role, &existingUser.FullName,
		&existingUser.OrgID, &existingUser.I3xWrite, &existingUser.CreatedAt,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch user"})
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	if req.Password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}
		_, err = tx.ExecContext(c.Request.Context(), `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hashedPassword), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}
	}

	var existingI3xWrite bool
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT i3x_write FROM users WHERE id = $1`, id).Scan(&existingI3xWrite)
	newI3xWrite := existingI3xWrite
	if req.I3xWrite != nil {
		newI3xWrite = *req.I3xWrite
	}

	_, err = tx.ExecContext(c.Request.Context(),
		`UPDATE users SET role = COALESCE(NULLIF($1, ''), role), full_name = $2, org_id = $3, i3x_write = $4 WHERE id = $5`,
		req.Role, req.FullName, req.OrgID, newI3xWrite, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user"})
		return
	}

	if req.SiteIDs != nil {
		if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM user_sites WHERE user_id = $1`, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear site scope"})
			return
		}
		if err := insertUserSitesTx(c, tx, id, *req.SiteIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign sites"})
			return
		}
	}

	if req.AreaIDs != nil {
		if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM user_areas WHERE user_id = $1`, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear area scope"})
			return
		}
		if err := insertUserAreasTx(c, tx, id, *req.AreaIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign areas"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Fetch updated user with all scope info
	var orgName *string
	var siteIDs pq.Int64Array
	var areaIDs pq.Int64Array
	fullQuery := `
		SELECT u.id, u.username, u.role, u.full_name, u.org_id, u.i3x_write, u.created_at, o.name,
		       COALESCE(array_agg(DISTINCT us.site_id) FILTER (WHERE us.site_id IS NOT NULL), '{}'),
		       COALESCE(array_agg(DISTINCT ua.area_id) FILTER (WHERE ua.area_id IS NOT NULL), '{}')
		FROM users u
		LEFT JOIN organizations o ON u.org_id = o.id
		LEFT JOIN user_sites us ON u.id = us.user_id
		LEFT JOIN user_areas ua ON u.id = ua.user_id
		WHERE u.id = $1
		GROUP BY u.id, o.name
	`
	var resp userWithScope
	err = h.db.QueryRowContext(c.Request.Context(), fullQuery, id).Scan(
		&resp.ID, &resp.Username, &resp.Role, &resp.FullName, &resp.OrgID, &resp.I3xWrite,
		&resp.CreatedAt, &orgName, &siteIDs, &areaIDs,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated user"})
		return
	}
	resp.OrgName = orgName
	resp.SiteIDs = int64SliceToInt(siteIDs)
	resp.AreaIDs = int64SliceToInt(areaIDs)

	c.JSON(http.StatusOK, resp)
}

// Delete removes a user
func (h *UsersHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	userIDClaim, exists := c.Get("user_id")
	if exists {
		if currentUserID, ok := userIDClaim.(float64); ok && int(currentUserID) == id {
			c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete your own account"})
			return
		}
	}

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

// --- helpers ---

func insertUserSites(c *gin.Context, tx *sql.Tx, userID int, siteIDs []int) error {
	return insertUserSitesTx(c, tx, userID, siteIDs)
}

func insertUserSitesTx(c *gin.Context, tx *sql.Tx, userID int, siteIDs []int) error {
	for _, sid := range siteIDs {
		if _, err := tx.ExecContext(c.Request.Context(),
			`INSERT INTO user_sites (user_id, site_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			userID, sid,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertUserAreas(c *gin.Context, tx *sql.Tx, userID int, areaIDs []int) error {
	return insertUserAreasTx(c, tx, userID, areaIDs)
}

func insertUserAreasTx(c *gin.Context, tx *sql.Tx, userID int, areaIDs []int) error {
	for _, aid := range areaIDs {
		if _, err := tx.ExecContext(c.Request.Context(),
			`INSERT INTO user_areas (user_id, area_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			userID, aid,
		); err != nil {
			return err
		}
	}
	return nil
}

func int64SliceToInt(s pq.Int64Array) []int {
	if len(s) == 0 {
		return []int{}
	}
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

func nullableIntSlice(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}
