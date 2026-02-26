package sparkplug

import (
	"time"
)

// SparkplugB Protocol Constants
const (
	// Topic namespace for Sparkplug B
	Namespace = "spBv1.0"

	// Message types
	MessageTypeDBIRTH  = "DBIRTH"  // Device birth (initial data on connect)
	MessageTypeNBIRTH  = "NBIRTH"  // Node birth (edge node startup)
	MessageTypeDDATA   = "DDATA"   // Device data (regular updates)
	MessageTypeNDATA   = "NDATA"   // Node data
	MessageTypeDDEATH  = "DDEATH"  // Device death (disconnect)
	MessageTypeNDEATH  = "NDEATH"  // Node death
	MessageTypeDCMD    = "DCMD"    // Device command
	MessageTypeNCMD    = "NCMD"    // Node command
)

// Quality constants (Sparkplug B uses different values than legacy)
const (
	QualityGood   = 192 // Good quality
	QualityBad    = 0   // Bad quality
	QualityUncert = 64  // Uncertain quality
)

// MetricDataType represents the Sparkplug B data type enum
type MetricDataType int32

const (
	DataTypeUnknown    MetricDataType = 0
	DataTypeInt8       MetricDataType = 1
	DataTypeInt16      MetricDataType = 2
	DataTypeInt32      MetricDataType = 3
	DataTypeInt64      MetricDataType = 4
	DataTypeUInt8      MetricDataType = 5
	DataTypeUInt16     MetricDataType = 6
	DataTypeUInt32     MetricDataType = 7
	DataTypeUInt64     MetricDataType = 8
	DataTypeFloat      MetricDataType = 9
	DataTypeDouble     MetricDataType = 10
	DataTypeBoolean    MetricDataType = 11
	DataTypeString     MetricDataType = 12
	DataTypeDateTime   MetricDataType = 13
	DataTypeText       MetricDataType = 14
	DataTypeUUID       MetricDataType = 19
	DataTypeDataSet    MetricDataType = 20
	DataTypeBytes      MetricDataType = 21
	DataTypeFile       MetricDataType = 22
	DataTypeTemplate   MetricDataType = 23
)

// Config holds Sparkplug B client configuration
type Config struct {
	// MQTT connection settings
	MQTTHost     string
	MQTTPort     int
	MQTTClientID string
	MQTTUsername string
	MQTTPassword string

	// Sparkplug B identity
	GroupID     string // Organization-Site combination (e.g., "acme-corp-factory1")
	EdgeNodeID  string // Area-Gateway combination (e.g., "production-plc01")

	// Options
	EnableLegacy bool // Also publish to legacy topics (default: true)
}

// TagData represents a tag value to be published
type TagData struct {
	TagID     int
	DeviceID  string      // Alias for the tag (used as device_id in topic)
	Value     interface{} // The actual value
	DataType  string      // Ralph data type: BOOL, INT, UINT, DINT, REAL, STRING
	Timestamp int64       // Unix timestamp in milliseconds
	Quality   int         // 0 = Good, 1 = Uncertain, 2 = Bad (legacy format)
	OrgID     int         // Organization ID
}

// Metric represents a single Sparkplug B metric
type Metric struct {
	Name         string         // Metric name (alias)
	DataType     MetricDataType // Sparkplug B data type
	Timestamp    int64          // Unix timestamp in milliseconds
	Quality      int32          // Quality code
	Value        interface{}    // The actual value
	IsHistorical bool           // Whether this is historical data
	IsNull       bool           // Whether value is null
}

// Payload represents a complete Sparkplug B payload
type Payload struct {
	Timestamp int64    // Unix timestamp in milliseconds
	Seq       uint64   // Sequence number (rolls over at 256)
	UUID      string   // Optional UUID
	Metrics   []Metric // List of metrics
}

// TopicInfo contains parsed Sparkplug B topic information
type TopicInfo struct {
	Namespace   string // spBv1.0
	GroupID     string // Organization-Site
	MessageType string // DDATA, DBIRTH, etc.
	EdgeNodeID  string // Area-Gateway
	DeviceID    string // Tag alias
}

// Client is the Sparkplug B client interface
type Client struct {
	config     Config
	seqNum     uint64
	connected  bool
	mqttClient interface{} // Will be *mqtt.Client from the mqtt package
}

// LegacyPayload represents the legacy JSON format
type LegacyPayload struct {
	TagID     int         `json:"tag_id"`
	OrgID     int         `json:"org_id"`
	Value     interface{} `json:"v"`
	Timestamp int64       `json:"ts"`
	Quality   int         `json:"q"`
}

// DataTypeMapping maps Ralph data types to Sparkplug B data types
var DataTypeMapping = map[string]MetricDataType{
	"BOOL":    DataTypeBoolean,
	"INT":     DataTypeInt16,
	"UINT":    DataTypeUInt16,
	"DINT":    DataTypeInt32,
	"REAL":    DataTypeFloat,
	"STRING":  DataTypeString,
	"FLOAT32": DataTypeFloat,
	"FLOAT64": DataTypeDouble,
}

// ConvertLegacyQualityToSparkplug converts legacy quality (0,1,2) to Sparkplug quality
func ConvertLegacyQualityToSparkplug(legacyQuality int) int32 {
	switch legacyQuality {
	case 0:
		return QualityGood
	case 1:
		return QualityUncert
	case 2:
		return QualityBad
	default:
		return QualityGood
	}
}

// ConvertSparkplugToLegacyQuality converts Sparkplug quality to legacy quality (0,1,2)
func ConvertSparkplugToLegacyQuality(sparkplugQuality int32) int {
	switch {
	case sparkplugQuality >= 192:
		return 0 // Good
	case sparkplugQuality >= 64:
		return 1 // Uncertain
	default:
		return 2 // Bad
	}
}

// GetTimestamp returns current timestamp in milliseconds
func GetTimestamp() int64 {
	return time.Now().UnixMilli()
}
