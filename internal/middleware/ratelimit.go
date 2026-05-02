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

const maxVisitors = 10000

var (
	visitors   = make(map[string]*visitor, maxVisitors)
	visitorsMu sync.Mutex

	globalVisitors   = make(map[string]*visitor, maxVisitors)
	globalVisitorsMu sync.Mutex
)

func init() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			cleanupVisitors(&visitorsMu, visitors)
			cleanupVisitors(&globalVisitorsMu, globalVisitors)
		}
	}()
}

func cleanupVisitors(mu *sync.Mutex, m map[string]*visitor) {
	mu.Lock()
	defer mu.Unlock()
	for ip, v := range m {
		if time.Since(v.lastSeen) > 15*time.Minute {
			delete(m, ip)
		}
	}
}

// evictOldest removes one arbitrary entry from the map to make room.
// Must be called with the mutex held.
func evictOldest(m map[string]*visitor) {
	var oldestKey string
	var oldestTime time.Time
	for k, v := range m {
		if oldestKey == "" || v.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.lastSeen
		}
	}
	if oldestKey != "" {
		delete(m, oldestKey)
	}
}

func getLimiter(mu *sync.Mutex, m map[string]*visitor, ip string, r rate.Limit, burst int) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()
	v, exists := m[ip]
	if !exists {
		if len(m) >= maxVisitors {
			evictOldest(m)
		}
		v = &visitor{limiter: rate.NewLimiter(r, burst)}
		m[ip] = v
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// LoginRateLimit limits login attempts to 10 per minute (burst: 5) per IP.
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 10 req/min = one every 6 seconds, burst 5
		if !getLimiter(&visitorsMu, visitors, c.ClientIP(), rate.Every(6*time.Second), 5).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many login attempts. Please wait before trying again.",
			})
			return
		}
		c.Next()
	}
}

// GlobalRateLimit limits all API requests to 300 per minute (burst: 50) per IP.
// Applied to the entire /api/ group to prevent scraping and abuse.
func GlobalRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 300 req/min = one every 200ms, burst 50
		if !getLimiter(&globalVisitorsMu, globalVisitors, c.ClientIP(), rate.Every(200*time.Millisecond), 50).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please slow down.",
			})
			return
		}
		c.Next()
	}
}
