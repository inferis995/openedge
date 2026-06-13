package middleware

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const APIKeyContextKey = "api_key_org_id"

// RequireAPIKey authenticates requests via an X-API-Key header.
// On success it stores the org_id in the gin context under APIKeyContextKey.
// Key format: oe_PPPPPPPP_RRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR
// where PPPPPPPP is the 8-char hex prefix (stored in DB) and R... is the random part.
func RequireAPIKey(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		if key == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing X-API-Key header"})
			return
		}

		parts := strings.SplitN(key, "_", 3)
		if len(parts) != 3 || parts[0] != "oe" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key format"})
			return
		}
		prefix := "oe_" + parts[1]
		hash := apiKeyHash(key)

		var orgID int
		err := db.QueryRowContext(c.Request.Context(),
			`SELECT org_id FROM org_api_keys
			 WHERE key_prefix = $1 AND key_hash = $2 AND revoked_at IS NULL`,
			prefix, hash,
		).Scan(&orgID)

		if err == sql.ErrNoRows {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or revoked API key"})
			return
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		go func() {
			_, _ = db.Exec(
				`UPDATE org_api_keys SET last_used_at = $1
				 WHERE key_prefix = $2 AND key_hash = $3`,
				time.Now(), prefix, hash,
			)
		}()

		c.Set(APIKeyContextKey, orgID)
		c.Next()
	}
}

func apiKeyHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
