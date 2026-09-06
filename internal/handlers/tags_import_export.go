package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// TagImportRequest represents the import request
type TagImportRequest struct {
	GatewayID int    `json:"gateway_id" binding:"required"`
	Content   string `json:"content" binding:"required"`
	Historize bool   `json:"historize"` // Optional, defaults to false

	// HistorizeDeadband is optional and defaults to the column default, 0.
	//
	// It used to be hardcoded to 0.1 here, which is not what anybody chose: a
	// tag created through the UI gets 0, an identical tag arriving through an
	// import got change filtering nobody asked for. On a temperature that lives
	// inside a narrow band, 0.1 silently drops the readings an operator imported
	// the tag to see. A pointer so that "not sent" and "sent as zero" stay
	// distinguishable.
	HistorizeDeadband *float64 `json:"historize_deadband"`
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
	// Example: MyBool : BOOL AT 40001.0
	// Example: SiemensVal : INT AT %MW100
	pattern := regexp.MustCompile(`^(\S+)\s*:\s*(\w+)\s+AT\s+(.+)$`)
	matches := pattern.FindStringSubmatch(line)

	if matches == nil {
		return nil, fmt.Errorf("invalid format: %s", line)
	}

	alias := matches[1]
	dataType := strings.ToUpper(matches[2])
	address := matches[3]

	// The list must match the CHECK constraint on tags.data_type, and it did
	// not: UINT, UDINT and WORD parsed here and were then rejected by the
	// database on INSERT. The line was accepted, the row was not, and the
	// operator got a Postgres constraint message instead of a sentence telling
	// them which types this accepts.
	//
	// They are rejected here rather than mapped onto INT or DINT. A silent
	// widening of a WORD into a signed INT changes what the value MEANS —
	// 40000 in a WORD is 40000, in an INT16 it is -25536 — and an import is
	// exactly the moment nobody would notice.
	validTypes := map[string]bool{
		"BOOL": true, "INT": true, "DINT": true, "REAL": true, "STRING": true,
	}
	if !validTypes[dataType] {
		return nil, fmt.Errorf(
			"unsupported data type %q (supported: BOOL, INT, DINT, REAL, STRING)", dataType)
	}

	return &ParsedTag{
		Alias:    alias,
		DataType: dataType,
		Address:  address,
	}, nil
}

// ImportTags handles POST /api/tags/import
//
// The import is ALL OR NOTHING, and that is a deliberate change from what it
// used to be.
//
// It used to walk the lines writing as it went, collecting failures into an
// error list, and then — whatever had happened — send the reload command to the
// gateway. A thousand-tag import that failed at line five hundred left four
// hundred and ninety-nine tags written, five hundred and one missing, and a
// driver restarted onto that half-configured gateway. It then polled addresses
// that no longer matched the tag list, which does not look like a failed import:
// it looks like a plant reading wrong.
//
// Now every line is parsed before anything is written. If a single line does not
// parse, nothing is written and the errors come back with Created and Updated at
// zero, so the operator fixes the file and imports again. What does get written
// goes in one transaction, and the reload only follows a commit.
func (h *TagsHandler) ImportTags(c *gin.Context) {
	var req TagImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// The org comes from the JWT by way of OrganizationContext, not from the
	// X-Organization-ID header this handler used to read on its own. The
	// middleware already refuses a header that disagrees with the token, so
	// reading the header was not a hole — but it made this endpoint the only
	// one that fails with 400 when the header is absent, even though the token
	// says exactly which organization the caller is in.
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Organization context required"})
		return
	}

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

	// ── Parse everything first ────────────────────────────────────────────
	type importLine struct {
		num    int
		parsed *ParsedTag
	}
	var toApply []importLine
	result := TagImportResult{}

	for lineNum, line := range strings.Split(req.Content, "\n") {
		parsed, parseErr := parseTagLine(line)
		if parseErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Line %d: %s", lineNum+1, parseErr.Error()))
			continue
		}
		if parsed == nil {
			continue // blank line or comment
		}
		toApply = append(toApply, importLine{num: lineNum + 1, parsed: parsed})
	}

	if len(result.Errors) > 0 {
		// Nothing written. Returning 200 with the error list is what the web UI
		// already renders, and the zero counts say plainly that the file has to
		// be fixed rather than half of it having landed.
		c.JSON(http.StatusOK, result)
		return
	}
	if len(toApply) == 0 {
		c.JSON(http.StatusOK, result)
		return
	}

	deadband := 0.0
	if req.HistorizeDeadband != nil {
		deadband = *req.HistorizeDeadband
	}

	// ── Apply in one transaction ──────────────────────────────────────────
	ctx := c.Request.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[API] tag import: cannot open transaction: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Import failed"})
		return
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	for _, item := range toApply {
		var existingID int
		lookupErr := tx.QueryRowContext(ctx, `
			SELECT id FROM tags WHERE gateway_id = $1 AND code = $2
		`, req.GatewayID, item.parsed.Address).Scan(&existingID)

		switch lookupErr {
		case sql.ErrNoRows:
			if _, execErr := tx.ExecContext(ctx, `
				INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, req.GatewayID, item.parsed.Address, item.parsed.Alias, item.parsed.DataType,
				req.Historize, deadband); execErr != nil {
				c.JSON(http.StatusConflict, gin.H{
					"error":  fmt.Sprintf("Line %d: failed to create: %s", item.num, execErr.Error()),
					"detail": "nothing was imported; the whole file is applied or none of it is",
				})
				return
			}
			result.Created++
		case nil:
			if _, execErr := tx.ExecContext(ctx, `
				UPDATE tags SET alias = $1, data_type = $2, historize = $3 WHERE id = $4
			`, item.parsed.Alias, item.parsed.DataType, req.Historize, existingID); execErr != nil {
				c.JSON(http.StatusConflict, gin.H{
					"error":  fmt.Sprintf("Line %d: failed to update: %s", item.num, execErr.Error()),
					"detail": "nothing was imported; the whole file is applied or none of it is",
				})
				return
			}
			result.Updated++
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  fmt.Sprintf("Line %d: DB error: %s", item.num, lookupErr.Error()),
				"detail": "nothing was imported; the whole file is applied or none of it is",
			})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[API] tag import: commit failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Import failed"})
		return
	}

	// Only now. A reload sent over a rolled-back import restarts the driver onto
	// the tag list it already had, which is harmless but pointless; a reload sent
	// over a HALF-applied import was the actual damage.
	if (result.Created > 0 || result.Updated > 0) && h.mqttClient != nil {
		topic := fmt.Sprintf("sys/command/reload/%d", req.GatewayID)
		if pubErr := h.mqttClient.Publish(topic, "reload"); pubErr != nil {
			// Same as Update: the import succeeded, but the driver will not pick
			// the new tags up until it reconnects, so say so.
			log.Printf("[API] tag import applied but reload command to gateway %d failed: %v", req.GatewayID, pubErr)
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
