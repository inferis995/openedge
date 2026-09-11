package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/internal/inventory"
	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// InventoryHandler serves the list of what is installed.
type InventoryHandler struct {
	db *sql.DB
}

func NewInventoryHandler(db *sql.DB) *InventoryHandler {
	return &InventoryHandler{db: db}
}

// scope returns the organization the caller may see, where zero means all of
// them.
//
// It is taken from the JWT and never from a parameter or a header: an inventory
// is the one document that lists every address in a plant, and a tenant reading
// somebody else's would be the worst possible thing to get wrong here.
func (h *InventoryHandler) scope(c *gin.Context) int {
	if middleware.IsGlobalAdmin(c) {
		return 0
	}
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		// No organization in the token and not a global admin: nothing to show.
		// -1 matches no row rather than every row, which is what 0 would do.
		return -1
	}
	return orgID
}

// GetInventory handles GET /api/inventory.
func (h *InventoryHandler) GetInventory(c *gin.Context) {
	orgID := h.scope(c)
	if orgID < 0 {
		c.JSON(http.StatusOK, gin.H{
			"devices": []inventory.Device{},
			"summary": inventory.Summarize(nil),
		})
		return
	}

	devices, err := inventory.Collect(c.Request.Context(), h.db, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the inventory"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"devices":      devices,
		"summary":      inventory.Summarize(devices),
		"generated_at": time.Now().UTC(),
	})
}

// ExportInventoryCSV handles GET /api/inventory.csv.
func (h *InventoryHandler) ExportInventoryCSV(c *gin.Context) {
	orgID := h.scope(c)

	var devices []inventory.Device
	if orgID >= 0 {
		var err error
		devices, err = inventory.Collect(c.Request.Context(), h.db, orgID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the inventory"})
			return
		}
	}

	filename := fmt.Sprintf("inventario-dispositivi-%s.csv", time.Now().UTC().Format("2006-01-02"))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)

	if err := inventory.WriteCSV(c.Writer, devices); err != nil {
		// The headers are already on the wire, so there is no status left to
		// change: log it and let the truncated file speak for itself rather
		// than appending an error message into the middle of a CSV.
		_ = c.Error(err)
	}
}
