package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReorderTagsRequest defines the payload for reordering tags
type ReorderTagsRequest struct {
	TagIDs []int `json:"tag_ids" binding:"required"`
}

// ReorderTags updates the sort_order of tags based on the provided list order
// PUT /api/tags/reorder
func (h *TagsHandler) ReorderTags(c *gin.Context) {
	var req ReorderTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
		return
	}
	defer tx.Rollback()

	// Update sort_order for each tag based on its index in the list
	stmt, err := tx.Prepare("UPDATE tags SET sort_order = $1 WHERE id = $2")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare statement"})
		return
	}
	defer stmt.Close()

	for i, id := range req.TagIDs {
		// Use index as sort_order (float)
		order := float64(i)
		if _, err := stmt.Exec(order, id); err != nil {
			log.Printf("Failed to update sort_order for tag %d: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tag order"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Notify clients via MQTT/Redis if needed (optional for ordering)
	// h.publishConfigUpdate()

	c.JSON(http.StatusOK, gin.H{"message": "Tags reordered successfully"})
}
