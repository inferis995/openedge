package handlers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/mqtt"
)

// MQTTProbeRequest is a broker the settings page wants to try.
type MQTTProbeRequest struct {
	// Target says which saved password to fall back on when Password is
	// empty: the page never receives the stored one (GetSettings masks it),
	// so "test what is saved" has to be done here.
	Target   string `json:"target" binding:"required,oneof=cloud external"`
	Host     string `json:"host" binding:"required"`
	Port     int    `json:"port" binding:"required,min=1,max=65535"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// MQTTProbeResult says whether the broker answered, and if not, why, as a
// code the web UI turns into a sentence and a raw message for the details.
type MQTTProbeResult struct {
	OK        bool   `json:"ok"`
	Scheme    string `json:"scheme"`
	Code      string `json:"code,omitempty"` // dns | refused | timeout | tls | auth | error
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

// probeTimeout bounds a test so the page answers in seconds, not in the
// client's own ten-second connect timeout plus DNS.
const probeTimeout = 6 * time.Second

// TestMQTTBroker handles POST /api/system/mqtt/test.
//
// It connects the way the platform will — the same transport rule
// (mqtt.SchemeForPort) and the same credentials — and disconnects. Before
// this, the only way to know whether a cloud broker's settings were right was
// to save them and read the engine-historian log.
func (h *SystemHandler) TestMQTTBroker(c *gin.Context) {
	var req MQTTProbeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Password == "" {
		key := "cloud_mqtt_password"
		if req.Target == "external" {
			key = "mqtt_password"
		}
		_ = h.db.QueryRowContext(c.Request.Context(),
			`SELECT value FROM global_settings WHERE key = $1`, key).Scan(&req.Password)
	}
	c.JSON(http.StatusOK, probeBroker(c.Request.Context(), req))
}

func probeBroker(ctx context.Context, req MQTTProbeRequest) MQTTProbeResult {
	res := MQTTProbeResult{Scheme: mqtt.SchemeForPort(req.Port)}
	start := time.Now()

	// Resolve and reach the port first: "no such host" and "connection
	// refused" are the two most common answers and say more than the MQTT
	// client's error for the same thing.
	dctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(req.Host, fmt.Sprint(req.Port)))
	if err != nil {
		res.Code, res.Message = classifyProbeError(err), err.Error()
		res.LatencyMS = time.Since(start).Milliseconds()
		return res
	}
	_ = conn.Close()

	// paho directly, not internal/mqtt: that client retries the first
	// connection in the background forever (ConnectRetry), which is right
	// for a service waiting for its broker and wrong here — the probe would
	// never learn why it failed (a refused password looks like a timeout)
	// and would keep dialing after the request had returned.
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("%s://%s", res.Scheme, net.JoinHostPort(req.Host, fmt.Sprint(req.Port)))).
		SetClientID(fmt.Sprintf("openedge-probe-%d", time.Now().UnixNano())).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetConnectTimeout(probeTimeout)
	if res.Scheme == "ssl" {
		// The same TLS the platform's own client uses (internal/mqtt).
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	if req.Username != "" {
		opts.SetUsername(req.Username)
	}
	if req.Password != "" {
		opts.SetPassword(req.Password)
	}
	client := paho.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(probeTimeout + time.Second) {
		err = errors.New("i/o timeout waiting for CONNACK")
	} else {
		err = tok.Error()
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Code, res.Message = classifyProbeError(err), err.Error()
		return res
	}
	client.Disconnect(100)
	res.OK = true
	return res
}

// classifyProbeError turns a dial or MQTT error into a reason the operator can
// act on.
func classifyProbeError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such host"):
		return "dns"
	case strings.Contains(msg, "refused"):
		return "refused"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "tls"), strings.Contains(msg, "x509"), strings.Contains(msg, "certificate"),
		strings.Contains(msg, "handshake"), strings.Contains(msg, "eof"):
		return "tls"
	case strings.Contains(msg, "not authorized"), strings.Contains(msg, "bad user name"),
		strings.Contains(msg, "password"), strings.Contains(msg, "identifier rejected"):
		return "auth"
	}
	return "error"
}
