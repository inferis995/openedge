package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// AuditLog represents an audit log entry
type AuditLog struct {
	ID        int             `json:"id"`
	UserID    *int            `json:"user_id"`
	Username  string          `json:"username"`
	Action    string          `json:"action"`
	IPAddress string          `json:"ip_address"`
	UserAgent string          `json:"user_agent"`
	Details   json.RawMessage `json:"details"`
	Success   bool            `json:"success"`
	CreatedAt time.Time       `json:"created_at"`
}

// AuditHandler handles audit log operations
type AuditHandler struct {
	db *sql.DB
}

// NewAuditHandler creates a new audit handler
func NewAuditHandler(db *sql.DB) *AuditHandler {
	return &AuditHandler{db: db}
}

// LogAudit writes an audit log entry to the database
func (h *AuditHandler) LogAudit(userID *int, username, action, ipAddress, userAgent string, details map[string]interface{}, success bool) error {
	var detailsJSON []byte
	var err error
	if details != nil {
		detailsJSON, err = json.Marshal(details)
		if err != nil {
			detailsJSON = nil
		}
	}

	query := `
		INSERT INTO audit_logs (user_id, username, action, ip_address, user_agent, details, success)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = h.db.Exec(query, userID, username, action, ipAddress, userAgent, detailsJSON, success)
	if err != nil {
		log.Printf("[AUDIT] Failed to write audit log: %v", err)
		return err
	}
	return nil
}

// GetAuditLogs handles GET /api/audit/logs
// @Summary Get audit logs
// @Description Get audit logs with optional filters
// @Tags audit
// @Accept json
// @Produce json
// @Param X-Organization-ID header int true "Organization ID"
// @Param start query string false "Start time (ISO 8601)"
// @Param end query string false "End time (ISO 8601)"
// @Param action query string false "Filter by action type"
// @Param user_id query int false "Filter by user ID"
// @Param success query bool false "Filter by success status"
// @Param limit query int false "Limit results (default 100, max 500)"
// @Success 200 {array} AuditLog
// @Failure 400 {object} map[string]string "Invalid request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Server error"
// @Router /api/audit/logs [get]
func (h *AuditHandler) GetAuditLogs(c *gin.Context) {
	// Check authentication
	_, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context not found"})
		return
	}

	// Parse query parameters
	startStr := c.Query("start")
	endStr := c.Query("end")
	action := c.Query("action")
	userIDStr := c.Query("user_id")
	successStr := c.Query("success")
	limitStr := c.Query("limit")

	// Default limit
	limit := 100
	if limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	// Build query with filters
	query := `
		SELECT al.id, al.user_id, al.username, al.action, al.ip_address, al.user_agent, al.details, al.success, al.created_at
		FROM audit_logs al
		WHERE 1=1
	`
	args := []interface{}{}
	argPos := 1

	// Filter by time range
	if startStr != "" {
		if startTime, err := time.Parse(time.RFC3339, startStr); err == nil {
			query += ` AND al.created_at >= $` + itoa(argPos)
			args = append(args, startTime)
			argPos++
		}
	}

	if endStr != "" {
		if endTime, err := time.Parse(time.RFC3339, endStr); err == nil {
			query += ` AND al.created_at <= $` + itoa(argPos)
			args = append(args, endTime)
			argPos++
		}
	}

	// Filter by action
	if action != "" {
		query += ` AND al.action = $` + itoa(argPos)
		args = append(args, action)
		argPos++
	}

	// Filter by user_id
	if userIDStr != "" {
		if userID, err := parseInt(userIDStr); err == nil {
			query += ` AND al.user_id = $` + itoa(argPos)
			args = append(args, userID)
			argPos++
		}
	}

	// Filter by success
	if successStr != "" {
		if successStr == "true" {
			query += ` AND al.success = true`
		} else if successStr == "false" {
			query += ` AND al.success = false`
		}
	}

	// Order and limit
	query += ` ORDER BY al.created_at DESC LIMIT $` + itoa(argPos)
	args = append(args, limit)

	// Execute query
	rows, err := h.db.Query(query, args...)
	if err != nil {
		log.Printf("[AUDIT] Failed to query audit logs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query audit logs"})
		return
	}
	defer rows.Close()

	// Parse results
	var logs []AuditLog
	for rows.Next() {
		var al AuditLog
		var userID sql.NullInt64
		var ipAddress, userAgent sql.NullString
		var details []byte

		err := rows.Scan(
			&al.ID, &userID, &al.Username, &al.Action, &ipAddress, &userAgent, &details, &al.Success, &al.CreatedAt,
		)
		if err != nil {
			log.Printf("[AUDIT] Failed to scan audit log: %v", err)
			continue
		}

		if userID.Valid {
			id := int(userID.Int64)
			al.UserID = &id
		}
		if ipAddress.Valid {
			al.IPAddress = ipAddress.String
		}
		if userAgent.Valid {
			al.UserAgent = userAgent.String
		}
		if details != nil {
			al.Details = details
		}

		logs = append(logs, al)
	}

	if logs == nil {
		logs = []AuditLog{}
	}

	c.JSON(http.StatusOK, logs)
}

// GetAuditActions handles GET /api/audit/actions
// @Summary Get available audit actions
// @Description Get list of distinct audit action types
// @Tags audit
// @Success 200 {array} string
// @Router /api/audit/actions [get]
func (h *AuditHandler) GetAuditActions(c *gin.Context) {
	query := `SELECT DISTINCT action FROM audit_logs ORDER BY action`

	rows, err := h.db.Query(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query actions"})
		return
	}
	defer rows.Close()

	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err == nil {
			actions = append(actions, action)
		}
	}

	if actions == nil {
		actions = []string{}
	}

	c.JSON(http.StatusOK, actions)
}

// Helper functions

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
