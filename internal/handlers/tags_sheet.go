package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
	"github.com/ralph/industrial-edge-middleware/internal/tagsheet"
)

// maxSheetBytes bounds an uploaded tag sheet. Twenty thousand rows of fifteen
// columns is well under this; a larger file is a wrong file.
const maxSheetBytes = 10 << 20

// gatewayInScope checks the caller may act on the gateway and returns its
// driver type. It writes the error response itself.
//
// The organization comes from the JWT by way of OrganizationContext. A global
// admin has none of their own and may act on any gateway. The tag export used
// to read X-Organization-ID by hand instead, and so refused a global admin
// who did not send one with a 400 — the same mistake the text import had.
func (h *TagsHandler) gatewayInScope(c *gin.Context, gatewayID int) (string, bool) {
	var gatewayOrgID int
	var driverType string
	err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT s.org_id, g.driver_type FROM gateways g
		JOIN areas a ON g.area_id = a.id
		JOIN sites s ON a.site_id = s.id
		WHERE g.id = $1`, gatewayID).Scan(&gatewayOrgID, &driverType)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Gateway not found"})
		return "", false
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the gateway"})
		return "", false
	}
	orgID, hasOrg := middleware.GetOrganizationID(c)
	switch {
	case !hasOrg && !middleware.IsGlobalAdmin(c):
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization context required"})
		return "", false
	case hasOrg && orgID != gatewayOrgID:
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return "", false
	}
	return driverType, true
}

func sheetLang(c *gin.Context) string {
	if l := strings.ToLower(c.Query("lang")); l == "it" || l == "en" {
		return l
	}
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Accept-Language")), "it") {
		return "it"
	}
	return "en"
}

func sendSheet(c *gin.Context, format, base, lang string, tags []tagsheet.Tag) {
	var data []byte
	var err error
	var contentType string
	switch format {
	case "xlsx":
		data, err = tagsheet.WriteXLSX(lang, tags)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case "csv":
		data, err = tagsheet.WriteCSV(lang, tags)
		contentType = "text/csv; charset=utf-8"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be csv or xlsx"})
		return
	}
	if err != nil {
		log.Printf("[API] writing tag sheet: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not write the file"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, base, format))
	c.Data(http.StatusOK, contentType, data)
}

// fileSafe keeps a name usable in a download file name.
func fileSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// TagImportTemplate handles GET /api/tags/import/template?gateway_id=&format=xlsx|csv&lang=it|en
//
// An empty sheet with the right headers and four example rows whose addresses
// are in the format of this gateway's driver.
func (h *TagsHandler) TagImportTemplate(c *gin.Context) {
	gatewayID, err := strconv.Atoi(c.Query("gateway_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id required"})
		return
	}
	driverType, ok := h.gatewayInScope(c, gatewayID)
	if !ok {
		return
	}
	lang := sheetLang(c)
	format := c.DefaultQuery("format", "xlsx")
	base := "tag-template"
	if lang == "it" {
		base = "modello-tag"
	}
	sendSheet(c, format, base+"-"+fileSafe(strings.ToLower(driverType)), lang, tagsheet.Examples(lang, driverType))
}

// existingTag is what the import compares a row with.
type existingTag struct {
	id        int
	udt       bool
	sheetTag  tagsheet.Tag
	hasJSONPt bool
}

func (h *TagsHandler) loadSheetTags(ctx context.Context, gatewayID int) ([]string, map[string]existingTag, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, code, alias, data_type, COALESCE(historize, false), COALESCE(historize_deadband, 0),
		       eu_unit, eu_decimals, scaling_enabled, scaling_raw_min, scaling_raw_max,
		       scaling_eu_min, scaling_eu_max, scaling_clamp, invert, json_path,
		       udt_instance_id IS NOT NULL
		FROM tags WHERE gateway_id = $1
		ORDER BY sort_order, id`, gatewayID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var order []string
	byAddress := map[string]existingTag{}
	for rows.Next() {
		var e existingTag
		var jp sql.NullString
		t := &e.sheetTag
		if err := rows.Scan(&e.id, &t.Address, &t.Alias, &t.DataType, &t.Historize, &t.Deadband,
			&t.Unit, &t.Decimals, &t.Scaling, &t.RawMin, &t.RawMax, &t.EuMin, &t.EuMax,
			&t.Clamp, &t.Invert, &jp, &e.udt); err != nil {
			return nil, nil, err
		}
		t.JSONPath, e.hasJSONPt = jp.String, jp.Valid
		order = append(order, t.Address)
		byAddress[t.Address] = e
	}
	return order, byAddress, rows.Err()
}

// ExportTagSheet writes a gateway's tags as CSV or Excel.
func (h *TagsHandler) exportTagSheet(c *gin.Context, gatewayID int, format string) {
	order, byAddress, err := h.loadSheetTags(c.Request.Context(), gatewayID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the tags"})
		return
	}
	tags := make([]tagsheet.Tag, 0, len(order))
	for _, a := range order {
		tags = append(tags, byAddress[a].sheetTag)
	}
	var name string
	_ = h.db.QueryRowContext(c.Request.Context(), `SELECT name FROM gateways WHERE id = $1`, gatewayID).Scan(&name)
	sendSheet(c, format, "tag-"+fileSafe(name), sheetLang(c), tags)
}

// SheetRowResult is one row of an import preview.
type SheetRowResult struct {
	Line     int                `json:"line"`
	Alias    string             `json:"alias"`
	Address  string             `json:"address"`
	DataType string             `json:"data_type"`
	Action   string             `json:"action"` // create | update | unchanged | invalid
	Changes  []string           `json:"changes,omitempty"`
	Problems []tagsheet.Problem `json:"problems,omitempty"`
}

// SheetImportResult is what an import of a sheet found, and did.
type SheetImportResult struct {
	Rows      []SheetRowResult   `json:"rows"`
	Problems  []tagsheet.Problem `json:"problems,omitempty"` // about the sheet, not a row
	Columns   []string           `json:"columns"`
	Created   int                `json:"created"`
	Updated   int                `json:"updated"`
	Unchanged int                `json:"unchanged"`
	Invalid   int                `json:"invalid"`
	Applied   bool               `json:"applied"`
}

// ImportTagSheet handles POST /api/tags/import/file (multipart: gateway_id,
// file, apply).
//
// Without apply it only answers what WOULD happen, row by row: created,
// updated (and which fields), unchanged, or wrong and why. The web UI shows
// that before anything is written. With apply=true it writes, under the same
// all-or-nothing rule as the text import: a single invalid row and nothing is
// written; otherwise one transaction, and the driver is told to reload only
// after it commits.
func (h *TagsHandler) ImportTagSheet(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSheetBytes+(1<<20))

	gatewayID, err := strconv.Atoi(c.PostForm("gateway_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "gateway_id required"})
		return
	}
	if _, ok := h.gatewayInScope(c, gatewayID); !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	if fh.Size > maxSheetBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file too large (max 10 MB)"})
		return
	}
	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read the file"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSheetBytes+1))
	_ = f.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read the file"})
		return
	}
	apply := c.PostForm("apply") == "true"

	sheet := tagsheet.Parse(data)
	ctx := c.Request.Context()
	_, existing, err := h.loadSheetTags(ctx, gatewayID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read the existing tags"})
		return
	}

	res := planSheet(sheet, existing)
	if !apply || res.Invalid > 0 || sheet.Blocking() || res.Created+res.Updated == 0 {
		c.JSON(http.StatusOK, res)
		return
	}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Import failed"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	for i := range sheet.Rows {
		row := &sheet.Rows[i]
		var execErr error
		switch res.Rows[i].Action {
		case "create":
			execErr = insertSheetRow(ctx, tx, gatewayID, row)
		case "update":
			execErr = updateSheetRow(ctx, tx, existing[row.Address].id, row, sheet.Present)
		default:
			continue
		}
		if execErr != nil {
			log.Printf("[API] tag sheet import, line %d: %v", row.Line, execErr)
			c.JSON(http.StatusConflict, gin.H{
				"error":  fmt.Sprintf("Line %d: %v", row.Line, execErr),
				"detail": "nothing was imported; the whole file is applied or none of it is",
			})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Import failed"})
		return
	}
	res.Applied = true

	if h.mqttClient != nil {
		if pubErr := h.mqttClient.Publish(fmt.Sprintf("sys/command/reload/%d", gatewayID), "reload"); pubErr != nil {
			log.Printf("[API] tag sheet applied but reload command to gateway %d failed: %v", gatewayID, pubErr)
		}
	}
	c.JSON(http.StatusOK, res)
}

// planSheet decides, row by row, what an import would do.
func planSheet(sheet *tagsheet.Sheet, existing map[string]existingTag) SheetImportResult {
	res := SheetImportResult{Rows: []SheetRowResult{}}
	for _, p := range sheet.Problems {
		if p.Line == 0 {
			res.Problems = append(res.Problems, p)
		}
	}
	for _, f := range tagsheet.Columns {
		if sheet.Present[f] {
			res.Columns = append(res.Columns, string(f))
		}
	}
	for i := range sheet.Rows {
		row := &sheet.Rows[i]
		r := SheetRowResult{Line: row.Line, Alias: row.Alias, Address: row.Address, DataType: row.DataType,
			Problems: sheet.RowProblems(row.Line)}
		e, found := existing[row.Address]
		switch {
		case len(r.Problems) > 0:
			r.Action = "invalid"
		case found && e.udt:
			// A tag that belongs to a UDT instance is generated from the type;
			// editing it here would be undone, or would break the instance.
			r.Action = "invalid"
			r.Problems = append(r.Problems, tagsheet.Problem{Line: row.Line, Field: tagsheet.FAddress, Code: "udt_managed"})
		case !found:
			r.Action = "create"
		default:
			r.Changes = changes(&e.sheetTag, row)
			if len(r.Changes) == 0 {
				r.Action = "unchanged"
			} else {
				r.Action = "update"
			}
		}
		switch r.Action {
		case "invalid":
			res.Invalid++
		case "create":
			res.Created++
		case "update":
			res.Updated++
		case "unchanged":
			res.Unchanged++
		}
		res.Rows = append(res.Rows, r)
	}
	return res
}

// changes lists the fields a row would change on an existing tag. Absent
// cells change nothing.
func changes(old *tagsheet.Tag, row *tagsheet.Row) []string {
	var out []string
	add := func(f tagsheet.Field, differs bool) {
		if differs {
			out = append(out, string(f))
		}
	}
	add(tagsheet.FAlias, row.Alias != old.Alias)
	add(tagsheet.FDataType, row.DataType != old.DataType)
	add(tagsheet.FHistorize, row.Historize != nil && *row.Historize != old.Historize)
	add(tagsheet.FDeadband, row.Deadband != nil && *row.Deadband != old.Deadband)
	add(tagsheet.FUnit, row.Unit != nil && *row.Unit != old.Unit)
	add(tagsheet.FDecimals, row.Decimals != nil && *row.Decimals != old.Decimals)
	add(tagsheet.FScaling, row.Scaling != nil && *row.Scaling != old.Scaling)
	add(tagsheet.FRawMin, row.RawMin != nil && *row.RawMin != old.RawMin)
	add(tagsheet.FRawMax, row.RawMax != nil && *row.RawMax != old.RawMax)
	add(tagsheet.FEuMin, row.EuMin != nil && *row.EuMin != old.EuMin)
	add(tagsheet.FEuMax, row.EuMax != nil && *row.EuMax != old.EuMax)
	add(tagsheet.FClamp, row.Clamp != nil && *row.Clamp != old.Clamp)
	add(tagsheet.FInvert, row.Invert != nil && *row.Invert != old.Invert)
	add(tagsheet.FJSONPath, row.JSONPath != nil && *row.JSONPath != old.JSONPath)
	return out
}

func orBool(p *bool, d bool) bool {
	if p != nil {
		return *p
	}
	return d
}

func orFloat(p *float64, d float64) float64 {
	if p != nil {
		return *p
	}
	return d
}

func insertSheetRow(ctx context.Context, tx *sql.Tx, gatewayID int, r *tagsheet.Row) error {
	unit := ""
	if r.Unit != nil {
		unit = *r.Unit
	}
	decimals := 2
	if r.Decimals != nil {
		decimals = *r.Decimals
	}
	var jsonPath interface{}
	if r.JSONPath != nil && *r.JSONPath != "" {
		jsonPath = *r.JSONPath
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband,
		                  eu_unit, eu_decimals, scaling_enabled, scaling_raw_min, scaling_raw_max,
		                  scaling_eu_min, scaling_eu_max, scaling_clamp, invert, json_path,
		                  sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		        COALESCE((SELECT MAX(sort_order) FROM tags WHERE gateway_id = $1), 0) + 1)`,
		gatewayID, r.Address, r.Alias, r.DataType, orBool(r.Historize, false), orFloat(r.Deadband, 0),
		unit, decimals, orBool(r.Scaling, false), orFloat(r.RawMin, 0), orFloat(r.RawMax, 100),
		orFloat(r.EuMin, 0), orFloat(r.EuMax, 100), orBool(r.Clamp, true), orBool(r.Invert, false), jsonPath)
	return err
}

// updateSheetRow writes the columns the sheet has, and only those.
func updateSheetRow(ctx context.Context, tx *sql.Tx, id int, r *tagsheet.Row, present map[tagsheet.Field]bool) error {
	sets := []string{"alias = $1", "data_type = $2"}
	args := []interface{}{r.Alias, r.DataType}
	set := func(col string, v interface{}) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if r.Historize != nil {
		set("historize", *r.Historize)
	}
	if r.Deadband != nil {
		set("historize_deadband", *r.Deadband)
	}
	if r.Unit != nil {
		set("eu_unit", *r.Unit)
	}
	if r.Decimals != nil {
		set("eu_decimals", *r.Decimals)
	}
	if r.Scaling != nil {
		set("scaling_enabled", *r.Scaling)
	}
	for _, p := range []struct {
		col string
		v   *float64
	}{{"scaling_raw_min", r.RawMin}, {"scaling_raw_max", r.RawMax}, {"scaling_eu_min", r.EuMin}, {"scaling_eu_max", r.EuMax}} {
		if p.v != nil {
			set(p.col, *p.v)
		}
	}
	if r.Clamp != nil {
		set("scaling_clamp", *r.Clamp)
	}
	if r.Invert != nil {
		set("invert", *r.Invert)
	}
	if present[tagsheet.FJSONPath] && r.JSONPath != nil {
		var v interface{}
		if *r.JSONPath != "" {
			v = *r.JSONPath
		}
		set("json_path", v)
	}
	args = append(args, id)
	// #nosec G201 -- the SET list is built from fixed column names above.
	_, err := tx.ExecContext(ctx,
		fmt.Sprintf("UPDATE tags SET %s WHERE id = $%d", strings.Join(sets, ", "), len(args)), args...)
	return err
}
