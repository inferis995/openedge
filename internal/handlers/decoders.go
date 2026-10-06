package handlers

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/decoder"
)

// DecodersHandler serves the payload decoders an MQTT gateway can use: the
// shipped presets, and a dry run of a decoder on a pasted message, so a
// script is seen to work before a gateway relies on it.
type DecodersHandler struct{}

func NewDecodersHandler() *DecodersHandler { return &DecodersHandler{} }

// Presets handles GET /api/decoders/presets.
func (h *DecodersHandler) Presets(c *gin.Context) {
	c.JSON(http.StatusOK, decoder.Presets())
}

type decoderTestRequest struct {
	Preset string `json:"preset"`
	Script string `json:"script"`
	// The message as received. payload_encoding says how it is written here:
	// "text" (default: the bytes are this text, as for a base64 frame sent
	// as text), "hex" or "base64" (binary payloads, pasted as hex/base64).
	Payload         string `json:"payload" binding:"required"`
	PayloadEncoding string `json:"payload_encoding"`
	Topic           string `json:"topic"`
}

type decodedSample struct {
	Ts     int64                  `json:"ts"`
	Time   string                 `json:"time"`
	Values map[string]interface{} `json:"values"`
}

// Test handles POST /api/decoders/test: runs a decoder on one message and
// returns what each tag could read from it. Nothing is stored or published.
func (h *DecodersHandler) Test(c *gin.Context) {
	var req decoderTestRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload is required"})
		return
	}
	cfg := map[string]interface{}{}
	if strings.TrimSpace(req.Script) != "" {
		cfg["decoder_script"] = req.Script
	} else if strings.TrimSpace(req.Preset) != "" {
		cfg["decoder_preset"] = req.Preset
	}
	dec, err := decoder.FromConfig(cfg)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error(), "samples": []decodedSample{}})
		return
	}
	if dec == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose a preset or write a script"})
		return
	}
	payload, err := decodePastedPayload(req.Payload, req.PayloadEncoding)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := dec.Decode(payload, req.Topic, time.Now())
	out := gin.H{"logs": res.Logs, "samples": toDecodedSamples(res.Samples)}
	if res.Logs == nil {
		out["logs"] = []string{}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	c.JSON(http.StatusOK, out)
}

func toDecodedSamples(in []decoder.Sample) []decodedSample {
	out := make([]decodedSample, 0, len(in))
	for _, s := range in {
		out = append(out, decodedSample{Ts: s.Time.UnixMilli(), Time: s.Time.UTC().Format(time.RFC3339), Values: s.Values})
	}
	return out
}

func decodePastedPayload(p, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "text":
		return []byte(p), nil
	case "hex":
		b, err := hex.DecodeString(strings.Join(strings.Fields(p), ""))
		if err != nil {
			return nil, fmt.Errorf("the payload is not valid hex: %w", err)
		}
		return b, nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("the payload is not valid base64: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("payload_encoding must be text, hex or base64")
}

// validateDecoderConfig refuses a gateway whose decoder would not run: a
// script that does not compile, an unknown preset, a timeout that is not a
// positive number. Saved, it would leave the gateway reading nothing with the
// reason only in the driver's log.
func validateDecoderConfig(cfg map[string]interface{}) error {
	if cfg == nil {
		return nil
	}
	if _, err := decoder.FromConfig(cfg); err != nil {
		return err
	}
	if v, ok := cfg["stale_after_minutes"]; ok && v != nil {
		f, isNum := v.(float64)
		if !isNum || f < 0 {
			return fmt.Errorf("stale_after_minutes must be a number of minutes (0 = off)")
		}
	}
	if v, ok := cfg["alarms_on_history"]; ok && v != nil {
		if _, isBool := v.(bool); !isBool {
			return fmt.Errorf("alarms_on_history must be true or false")
		}
	}
	return nil
}
