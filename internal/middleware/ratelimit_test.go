package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The limiter had no tests at all. What exercised it was the acceptance suite
// tripping over it — which is not coverage, it is a flake: the suite fires
// requests as fast as the machine allows, exhausts the burst in the first
// seconds, and some later test fails with a 429 that has nothing to do with
// what it was checking.
//
// Making the limit configurable would have removed even that, so here is the
// coverage it was standing in for.

func request(t *testing.T, h gin.HandlerFunc, ip string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(h)
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code
}

// reset clears the per-IP state between tests, which is otherwise shared
// through a package-level map and makes the order of tests matter.
func reset() {
	globalVisitorsMu.Lock()
	globalVisitors = make(map[string]*visitor, maxVisitors)
	globalVisitorsMu.Unlock()
}

func TestTheBurstIsAllowedAndThenRequestsAreRefused(t *testing.T) {
	reset()
	t.Setenv("API_RATE_LIMIT_PER_MINUTE", "60") // one a second: the burst is what matters here
	t.Setenv("API_RATE_LIMIT_BURST", "5")

	h := GlobalRateLimit()

	for i := 1; i <= 5; i++ {
		if code := request(t, h, "10.0.0.1"); code != http.StatusOK {
			t.Fatalf("request %d of the burst returned %d, want 200", i, code)
		}
	}
	if code := request(t, h, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("the request past the burst returned %d, want 429", code)
	}
}

// One noisy address must not lock everybody else out — on a platform where
// every tenant's plant calls the same API, that would turn one misbehaving box
// into an outage for all of them.
func TestOneAddressBeingRefusedDoesNotAffectAnother(t *testing.T) {
	reset()
	t.Setenv("API_RATE_LIMIT_PER_MINUTE", "60")
	t.Setenv("API_RATE_LIMIT_BURST", "2")

	h := GlobalRateLimit()

	for i := 0; i < 2; i++ {
		_ = request(t, h, "10.0.0.1")
	}
	if code := request(t, h, "10.0.0.1"); code != http.StatusTooManyRequests {
		t.Fatalf("the noisy address was not limited: %d", code)
	}

	if code := request(t, h, "10.0.0.2"); code != http.StatusOK {
		t.Fatalf("a different address was refused with %d because another one was noisy", code)
	}
}

// The environment is read once, when the middleware is built. A value that is
// absent, zero or nonsense must leave the shipped defaults in place rather than
// producing a limiter that refuses everything.
func TestNonsenseInTheEnvironmentLeavesTheDefaults(t *testing.T) {
	for _, bad := range []string{"", "0", "-1", "molto", "300.5"} {
		t.Run("value "+bad, func(t *testing.T) {
			t.Setenv("API_RATE_LIMIT_PER_MINUTE", bad)
			t.Setenv("API_RATE_LIMIT_BURST", bad)

			limit, burst := globalRate()
			if burst != defaultGlobalBurst {
				t.Errorf("burst = %d, want the default %d", burst, defaultGlobalBurst)
			}
			if float64(limit) != float64(defaultGlobalRatePerMinute)/60.0 {
				t.Errorf("limit = %v, want the default", limit)
			}
		})
	}
}

func TestTheEnvironmentRaisesTheLimit(t *testing.T) {
	t.Setenv("API_RATE_LIMIT_PER_MINUTE", "6000")
	t.Setenv("API_RATE_LIMIT_BURST", "500")

	limit, burst := globalRate()
	if burst != 500 {
		t.Errorf("burst = %d, want 500", burst)
	}
	if float64(limit) != 100 {
		t.Errorf("limit = %v requests a second, want 100", limit)
	}
}

// The shipped defaults are a decision, not an accident: a box polls once a
// minute, so fifty of them behind one address use a sixth of the budget.
func TestTheShippedDefaultsLeaveRoomForAPlant(t *testing.T) {
	t.Setenv("API_RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("API_RATE_LIMIT_BURST", "")

	limit, _ := globalRate()
	perMinute := float64(limit) * 60

	if perMinute < 50 {
		t.Errorf("the default allows %.0f requests a minute; fifty boxes polling once a "+
			"minute would not fit", perMinute)
	}
}
