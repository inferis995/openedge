// Package audit provides a single Insert helper used across handlers to
// record operator-significant actions (tag writes, recipe loads,
// destructive admin operations). The schema is owned by the migration
// in internal/db; this package never DDLs.
//
// Why a dedicated package: the auth service already owns the
// login/logout half, but it's wrapped in a Service struct with its own
// dependencies. Handlers for tag writes / recipes / etc. don't need
// any of that — they just need a one-line "log this action with these
// fields" call that doesn't block the HTTP path.
package audit

import (
	"database/sql"
	"encoding/json"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// Entry is the wire shape an audit caller fills in. ipAddress and
// userAgent are pulled from the gin.Context automatically by Log() to
// keep call sites short; the caller only carries the domain fields.
type Entry struct {
	Action  string                 // free-text verb, e.g. "tag.write", "recipe.load"
	Success bool
	Details map[string]interface{} // serialised to jsonb in audit_logs.details
}

// Log inserts an audit_logs row, taking the actor + request metadata
// from the gin.Context. Non-blocking — runs in a goroutine; failures
// are logged centrally but never propagate to the caller (a failing
// audit insert must not undo a successful tag write).
func Log(c *gin.Context, db *sql.DB, e Entry) {
	if db == nil {
		return
	}
	uid, uname := actor(c)
	ip := c.ClientIP()
	if ip == "" {
		ip = "0.0.0.0"
	}
	ua := c.Request.UserAgent()
	var details []byte
	if len(e.Details) > 0 {
		details, _ = json.Marshal(e.Details)
	}

	go func() {
		var userIDArg interface{}
		if uid > 0 {
			userIDArg = uid
		}
		_, err := db.Exec(`
			INSERT INTO audit_logs (user_id, username, action, ip_address, user_agent, details, success)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			userIDArg, uname, e.Action, ip, ua, details, e.Success,
		)
		if err != nil {
			log.Printf("[AUDIT] insert failed (action=%s user=%s): %v", e.Action, uname, err)
		}
	}()
}

// actor extracts (user_id, username) from JWT claims stashed in the
// context. Returns zero values when no auth is present (e.g. a service
// account writing) — the audit row then records "system".
func actor(c *gin.Context) (int, string) {
	v, ok := c.Get(middleware.UserKey)
	if !ok {
		return 0, ""
	}
	claims, ok := v.(jwt.MapClaims)
	if !ok {
		return 0, ""
	}
	id := 0
	switch n := claims["user_id"].(type) {
	case float64:
		id = int(n)
	case int:
		id = n
	case int64:
		id = int(n)
	}
	name, _ := claims["username"].(string)
	return id, name
}
