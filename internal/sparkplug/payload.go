package sparkplug

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// PayloadEncoder handles encoding Sparkplug B payloads
// Note: For full Protobuf support, use the official Sparkplug B library
// This implementation provides a simplified JSON-based encoding that's compatible
// with Sparkplug B structure but not the binary Protobuf format.
// For production use, integrate: github.com/eclipse/sparkplugb

// The sequence counter that used to live here — a package-level `seqNum` shared
// by every SparkplugClient built in one process — is gone. Sequence numbers are
// per edge node by definition, so a second gateway in the same driver process
// stole numbers from the first and both streams looked full of gaps to a host.
// The counter now belongs to SparkplugClient (see client.go: nextSeq), and the
// seq to stamp is passed in by the caller that owns it.

// CreatePayload creates a Sparkplug B payload from tag data.
// seq is the caller's (per-edge-node) sequence number for this message.
func CreatePayload(tags []TagData, seq uint64) *Payload {
	metrics := make([]Metric, 0, len(tags))

	for _, tag := range tags {
		metric := Metric{
			// TagData.DeviceID holds the tag ALIAS: it is the metric name, not
			// the Sparkplug device — the device is the gateway (see client.go).
			Name:      tag.DeviceID,
			DataType:  mapDataType(tag.DataType),
			Timestamp: tag.Timestamp,
			Quality:   ConvertLegacyQualityToSparkplug(tag.Quality),
			Value:     tag.Value,
		}
		metrics = append(metrics, metric)
	}

	return &Payload{
		Timestamp: time.Now().UnixMilli(),
		Seq:       seq,
		Metrics:   metrics,
	}
}

// CreateDDATAPayload creates a DDATA payload for regular data updates
func CreateDDATAPayload(deviceID string, tags []TagData, seq uint64) *Payload {
	return CreatePayload(tags, seq)
}

// CreateDBIRTHPayload creates a DBIRTH payload for device birth messages
func CreateDBIRTHPayload(deviceID string, tags []TagData, seq uint64) *Payload {
	return CreatePayload(tags, seq)
}

// CreateSingleMetricPayload creates a payload with a single metric
func CreateSingleMetricPayload(deviceID string, tag TagData, seq uint64) *Payload {
	return &Payload{
		Timestamp: time.Now().UnixMilli(),
		Seq:       seq,
		Metrics: []Metric{
			{
				Name:      tag.DeviceID,
				DataType:  mapDataType(tag.DataType),
				Timestamp: tag.Timestamp,
				Quality:   ConvertLegacyQualityToSparkplug(tag.Quality),
				Value:     tag.Value,
			},
		},
	}
}

// mapDataType maps Ralph data types to Sparkplug B data types
func mapDataType(ralphType string) MetricDataType {
	if dt, ok := DataTypeMapping[ralphType]; ok {
		return dt
	}
	return DataTypeUnknown
}

// ConvertValue converts a value to the appropriate type based on Sparkplug B data type
func ConvertValue(value interface{}, dataType MetricDataType) (interface{}, error) {
	switch dataType {
	case DataTypeBoolean:
		return toBool(value), nil
	case DataTypeInt8, DataTypeInt16, DataTypeInt32, DataTypeInt64:
		return toInt64(value), nil
	case DataTypeUInt8, DataTypeUInt16, DataTypeUInt32, DataTypeUInt64:
		return toUInt64(value), nil
	case DataTypeFloat:
		return toFloat32(value), nil
	case DataTypeDouble:
		return toFloat64(value), nil
	case DataTypeString:
		return fmt.Sprintf("%v", value), nil
	default:
		return value, nil
	}
}

// Conversion helpers

func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return val != 0
	case float32, float64:
		return val != 0
	case string:
		return val == "true" || val == "1" || val == "on"
	default:
		return false
	}
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int8:
		return int64(val)
	case int16:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case uint:
		return int64(val)
	case uint8:
		return int64(val)
	case uint16:
		return int64(val)
	case uint32:
		return int64(val)
	case uint64:
		return int64(val)
	case float32:
		return int64(val)
	case float64:
		return int64(val)
	default:
		return 0
	}
}

func toUInt64(v interface{}) uint64 {
	switch val := v.(type) {
	case int:
		return uint64(val)
	case int8:
		return uint64(val)
	case int16:
		return uint64(val)
	case int32:
		return uint64(val)
	case int64:
		return uint64(val)
	case uint:
		return uint64(val)
	case uint8:
		return uint64(val)
	case uint16:
		return uint64(val)
	case uint32:
		return uint64(val)
	case uint64:
		return val
	case float32:
		return uint64(val)
	case float64:
		return uint64(val)
	default:
		return 0
	}
}

func toFloat32(v interface{}) float32 {
	switch val := v.(type) {
	case float32:
		return val
	case float64:
		return float32(val)
	case int:
		return float32(val)
	case int8:
		return float32(val)
	case int16:
		return float32(val)
	case int32:
		return float32(val)
	case int64:
		return float32(val)
	case uint:
		return float32(val)
	case uint8:
		return float32(val)
	case uint16:
		return float32(val)
	case uint32:
		return float32(val)
	case uint64:
		return float32(val)
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	default:
		return 0
	}
}

// DecodePayload decodes a Sparkplug B Protobuf payload
// Note: This is a simplified implementation. For full Protobuf support,
// integrate the official Sparkplug B library.
func DecodePayload(data []byte) (*Payload, error) {
	// For now, this returns an error indicating Protobuf is needed
	// The full implementation would use github.com/eclipse/sparkplugb
	return nil, fmt.Errorf("Protobuf decoding requires github.com/eclipse/sparkplugb library")
}

// DecodePayloadJSON decodes a JSON-formatted Sparkplug B payload
// This is useful for testing and for systems that don't require binary Protobuf
func DecodePayloadJSON(data []byte) (*Payload, error) {
	var payload Payload
	// Simple JSON structure for testing
	// Production should use Protobuf
	if err := decodeJSON(data, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// decodeJSON is a helper for JSON decoding
func decodeJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// LogPayload logs payload information for debugging
func LogPayload(payload *Payload, topic string) {
	log.Printf("[SPARKPLUG] Topic: %s, Seq: %d, Metrics: %d, Timestamp: %d",
		topic, payload.Seq, len(payload.Metrics), payload.Timestamp)
	for _, m := range payload.Metrics {
		log.Printf("[SPARKPLUG]   Metric: %s, Type: %d, Value: %v, Quality: %d",
			m.Name, m.DataType, m.Value, m.Quality)
	}
}
