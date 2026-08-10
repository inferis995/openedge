package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/telemetry"
)

// Metrics records request count and latency for every route.
//
// Without this, telemetry.HTTPRequestsTotal and HTTPRequestDuration were
// declared, exported at /metrics, and never written by anything but their own
// unit tests — so the APIHighLatency alert, which reads
// http_request_duration_seconds_bucket, could not fire under any circumstances.
// An alert that cannot fire is worse than a missing one: the dashboard is green
// because nothing is measured, not because nothing is wrong.
//
// The label is c.FullPath() — the ROUTE TEMPLATE ("/api/tags/:id"), not the
// request path ("/api/tags/8412"). Using the raw path would mint a new
// Prometheus series per tag, per user, per organisation, which is the standard
// way to take down a monitoring stack with its own instrumentation. Requests
// that match no route have an empty FullPath and collapse to "<unmatched>",
// so a scan for random URLs cannot do the same thing.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		method := c.Request.Method

		telemetry.HTTPRequestDuration.
			WithLabelValues(method, route).
			Observe(time.Since(start).Seconds())

		telemetry.HTTPRequestsTotal.
			WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).
			Inc()
	}
}
