package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// TagImportRequest represents the import request
type TagImportRequest struct {
	GatewayID int    `json:"gateway_id" binding:"required"`
	Content   string `json:"content" binding:"required"`
	Historize bool   `json:"historize"` // Optional, defaults to false
}

// TagImportResult represents the import result
type TagImportResult struct {
	Created int      `json:"created"`
	Updated int      `json:"updated"`
	Errors  []string `json:"errors,omitempty"`
}

// ParsedTag represents a parsed tag from import text
type ParsedTag struct {
	Alias    string
	DataType string
	Address  string
}

// parseTagLine parses a single line in PLC format: "Alias : DataType AT Address;"
func parseTagLine(line string) (*ParsedTag, error) {
	// Remove comments (everything after //)
	if idx := strings.Index(line, "//"); idx != -1 {
		line = line[:idx]
	}

	// Clean up the line
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil // Empty line, skip
	}

	// Remove trailing semicolon
	line = strings.TrimSuffix(line, ";")
	line = strings.TrimSpace(line)

	// Pattern: Alias : DataType AT Address
	// Example: HMI_CFG_HBeg_1 : DINT AT 42095
	pattern := regexp.MustCompile(`^(\S+)\s*:\s*(\w+)\s+AT\s+(\d+)$`)
	matches := pattern.FindStringSubmatch(line)

	if matches == nil {
		return nil, fmt.Errorf("invalid format: %s", line)
	}

	alias := matches[1]
	dataType := strings.ToUpper(matches[2])
	address := matches[3]

	// Validate data type
	validTypes := map[string]bool{
		"BOOL": true, "INT": true, "UINT": true, "DINT": true,
		"UDINT": true, "REAL": true, "STRING": true, "WORD": true,
	}
	if !validTypes[dataType] {
		return nil, fmt.Errorf("invalid data type: %s", dataType)
	}

	return &ParsedTag{
		Alias:    alias,
		DataType: dataType,
		Address:  address,
	}, nil
}

// ImportTags handles POST /api/tags/import
func (h *TagsHandler) ImportTags(c *gin.Context) {
	var req TagImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get organization ID from header
	orgIDStr := c.GetHeader("X-Organization-ID")
	if orgIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Organization-ID header required"})
		return
	}
	orgID, _ := strconv.Atoi(orgIDStr)

	// Verify gateway belongs to organization
	var gatewayOrgID int
	err := h.db.QueryRow(`
		SELECT o.id FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`, req.GatewayID).Scan(&gatewayOrgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
		return
	}
	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	result := TagImportResult{}
	lines := strings.Split(req.Content, "\n")

	for lineNum, line := range lines {
		parsed, err := parseTagLine(line)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Line %d: %s", lineNum+1, err.Error()))
			continue
		}
		if parsed == nil {
			continue // Empty line
		}

		// Check if tag with this address already exists
		var existingID int
		err = h.db.QueryRow(`
			SELECT id FROM tags WHERE gateway_id = $1 AND code = $2
		`, req.GatewayID, parsed.Address).Scan(&existingID)

		if err == sql.ErrNoRows {
			// Create new tag
			_, err = h.db.Exec(`
				INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband)
				VALUES ($1, $2, $3, $4, $5, 0.1)
			`, req.GatewayID, parsed.Address, parsed.Alias, parsed.DataType, req.Historize)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Line %d: failed to create: %s", lineNum+1, err.Error()))
				continue
			}
			result.Created++
		} else if err == nil {
			// Update existing tag alias, data_type and historize
			_, err = h.db.Exec(`
				UPDATE tags SET alias = $1, data_type = $2, historize = $3 WHERE id = $4
			`, parsed.Alias, parsed.DataType, req.Historize, existingID)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Line %d: failed to update: %s", lineNum+1, err.Error()))
				continue
			}
			result.Updated++
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("Line %d: DB error: %s", lineNum+1, err.Error()))
		}
	}

	// Publish reload command to MQTT if changes were made
	if (result.Created > 0 || result.Updated > 0) && h.mqttClient != nil {
		topic := fmt.Sprintf("sys/command/reload/%d", req.GatewayID)
		if err := h.mqttClient.Publish(topic, "reload"); err != nil {
			// Log error but don't fail the request
			// log.Printf("[API] Failed to publish import reload: %v", err)
		}
	}

	c.JSON(http.StatusOK, result)
}

// ExportTags handles GET /api/tags/export?gateway_id=X
func (h *TagsHandler) ExportTags(c *gin.Context) {
	gatewayIDStr := c.Query("gateway_id")
	if gatewayIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id required"})
		return
	}
	gatewayID, err := strconv.Atoi(gatewayIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gateway_id"})
		return
	}

	// Get organization ID from header
	orgIDStr := c.GetHeader("X-Organization-ID")
	if orgIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Organization-ID header required"})
		return
	}
	orgID, _ := strconv.Atoi(orgIDStr)

	// Verify gateway belongs to organization
	var gatewayOrgID int
	err = h.db.QueryRow(`
		SELECT o.id FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		JOIN organizations o ON s.org_id = o.id
		WHERE g.id = $1
	`, gatewayID).Scan(&gatewayOrgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
		return
	}
	if gatewayOrgID != orgID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Query all tags for this gateway
	rows, err := h.db.Query(`
		SELECT alias, data_type, code FROM tags
		WHERE gateway_id = $1
		ORDER BY code
	`, gatewayID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var alias, dataType, code string
		if err := rows.Scan(&alias, &dataType, &code); err != nil {
			continue
		}

		// Format: Alias : DataType AT Address;
		// Pad alias to 30 chars for alignment
		paddedAlias := fmt.Sprintf("%-30s", alias)
		line := fmt.Sprintf("      %s: %s AT %s;", paddedAlias, dataType, code)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	c.JSON(http.StatusOK, gin.H{"content": content})
}
