package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// APIKeysHandler manages org API keys used by edge managers.
type APIKeysHandler struct {
	db *sql.DB
}

func NewAPIKeysHandler(db *sql.DB) *APIKeysHandler {
	return &APIKeysHandler{db: db}
}

type apiKeyRow struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	KeyPrefix   string     `json:"key_prefix"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	PlaintextKey string    `json:"key,omitempty"` // returned only on creation
}

// Create generates a new API key for the organization.
// POST /api/organizations/:id/api-keys
// Body: {"name": "edge-site-A"}.
func (h *APIKeysHandler) Create(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Format: oe_PPPPPPPP_RRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR
	//   prefix part = oe_ + 4 random bytes hex (8 chars)
	//   secret part = 16 random bytes hex (32 chars)
	pfxBytes := make([]byte, 4)
	secBytes := make([]byte, 16)
	if _, err = rand.Read(pfxBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}
	if _, err = rand.Read(secBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}

	prefix := "oe_" + hex.EncodeToString(pfxBytes)
	fullKey := prefix + "_" + hex.EncodeToString(secBytes)
	hash := keyHash(fullKey)

	var id int
	var createdAt time.Time
	err = h.db.QueryRowContext(c.Request.Context(),
		`INSERT INTO org_api_keys (org_id, name, key_prefix, key_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		orgID, req.Name, prefix, hash,
	).Scan(&id, &createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create API key"})
		return
	}

	c.JSON(http.StatusCreated, apiKeyRow{
		ID:           id,
		Name:         req.Name,
		KeyPrefix:    prefix,
		CreatedAt:    createdAt,
		PlaintextKey: fullKey, // shown once — client must save it
	})
}

// List returns all active API keys for the organization (prefix only, no secrets).
// GET /api/organizations/:id/api-keys.
func (h *APIKeysHandler) List(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(),
		`SELECT id, name, key_prefix, created_at, last_used_at
		 FROM org_api_keys
		 WHERE org_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list keys"})
		return
	}
	defer rows.Close() //nolint:errcheck

	var keys []apiKeyRow
	for rows.Next() {
		var k apiKeyRow
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.CreatedAt, &k.LastUsedAt); err != nil {
			continue
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []apiKeyRow{}
	}
	c.JSON(http.StatusOK, keys)
}

// Revoke marks an API key as revoked so it can no longer be used.
// DELETE /api/organizations/:id/api-keys/:key_id.
func (h *APIKeysHandler) Revoke(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	keyID, err := strconv.Atoi(c.Param("key_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}

	res, err := h.db.ExecContext(c.Request.Context(),
		`UPDATE org_api_keys SET revoked_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`,
		keyID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke key"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "key not found or already revoked"})
		return
	}
	c.Status(http.StatusNoContent)
}

func keyHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
