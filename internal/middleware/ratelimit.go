package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var (
	visitors   = make(map[string]*visitor)
	visitorsMu sync.Mutex
)

func init() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			visitorsMu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 15*time.Minute {
					delete(visitors, ip)
				}
			}
			visitorsMu.Unlock()
		}
	}()
}

func getVisitorLimiter(ip string) *rate.Limiter {
	visitorsMu.Lock()
	defer visitorsMu.Unlock()
	v, exists := visitors[ip]
	if !exists {
		// 10 attempts per minute, burst of 5
		v = &visitor{limiter: rate.NewLimiter(rate.Every(6*time.Second), 5)}
		visitors[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// LoginRateLimit limits login attempts to 10 per minute (burst: 5) per IP address.
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !getVisitorLimiter(c.ClientIP()).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many login attempts. Please wait before trying again.",
			})
			return
		}
		c.Next()
	}
}
