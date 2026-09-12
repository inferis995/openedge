package middleware

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
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
			slog.Warn("login rate limit exceeded", "ip", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many login attempts. Please wait before trying again.",
			})
			return
		}
		c.Next()
	}
}

// Defaults for the global limit: 300 requests a minute per IP, with a burst of
// 50. Enough for a plant's boxes — each polls once a minute, so fifty of them
// behind one address still use a sixth of the budget — and low enough to make
// scraping the API tedious.
const (
	defaultGlobalRatePerMinute = 300
	defaultGlobalBurst         = 50
)

// globalRate reads the limit from the environment, falling back to the defaults.
//
// Configurable because of what it does to a test suite: an acceptance run fires
// requests as fast as the machine allows, exhausts the burst in the first
// seconds, and from then on any test that makes several calls in a row fails
// with a 429 that has nothing to do with what it was testing. The limiter is
// covered by its own tests now, rather than by whichever acceptance test
// happened to trip over it.
func globalRate() (rate.Limit, int) {
	perMinute := defaultGlobalRatePerMinute
	if v, err := strconv.Atoi(os.Getenv("API_RATE_LIMIT_PER_MINUTE")); err == nil && v > 0 {
		perMinute = v
	}
	burst := defaultGlobalBurst
	if v, err := strconv.Atoi(os.Getenv("API_RATE_LIMIT_BURST")); err == nil && v > 0 {
		burst = v
	}
	return rate.Limit(float64(perMinute) / 60.0), burst
}

// GlobalRateLimit limits all API requests per IP.
// Applied to the entire /api/ group to prevent scraping and abuse.
func GlobalRateLimit() gin.HandlerFunc {
	limit, burst := globalRate()
	return func(c *gin.Context) {
		if !getLimiter(&globalVisitorsMu, globalVisitors, c.ClientIP(), limit, burst).Allow() {
			slog.Warn("global rate limit exceeded", "ip", c.ClientIP(), "path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please slow down.",
			})
			return
		}
		c.Next()
	}
}
