package i3x

import "time"

// Quality represents OPC-UA style quality codes used in i3X Access API
type Quality int

const (
	QualityBad       Quality = 0
	QualityUncertain Quality = 64
	QualityGood      Quality = 192
)

// DataType represents i3X standard property data types
type DataType string

const (
	DataTypeBoolean DataType = "Boolean"
	DataTypeInt32   DataType = "Int32"
	DataTypeFloat   DataType = "Float"
	DataTypeString  DataType = "String"
)

// EquipmentType classifies assets in the i3X equipment hierarchy
type EquipmentType string

const (
	EquipmentTypeAssembly  EquipmentType = "Assembly"  // logical grouping (org, site, area)
	EquipmentTypeEquipment EquipmentType = "Equipment" // physical device / gateway
)

// AlarmSeverity maps internal severity levels to i3X standard labels
type AlarmSeverity string

const (
	AlarmSeverityInfo     AlarmSeverity = "Info"
	AlarmSeverityWarning  AlarmSeverity = "Warning"
	AlarmSeverityCritical AlarmSeverity = "Critical"
)

// AlarmStatus maps internal alarm states to i3X standard labels
type AlarmStatus string

const (
	AlarmStatusActive       AlarmStatus = "Active"
	AlarmStatusAcknowledged AlarmStatus = "Acknowledged"
	AlarmStatusCleared      AlarmStatus = "Cleared"
)

// Equipment represents a physical or logical industrial asset in i3X format.
// Gateways are mapped as EquipmentTypeEquipment; org / site / area nodes as
// EquipmentTypeAssembly.
type Equipment struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        EquipmentType          `json:"type"`
	Description string                 `json:"description,omitempty"`
	ParentID    string                 `json:"parentId,omitempty"`
	Path        string                 `json:"path,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

// PropertyValue holds the live value of a property at a specific instant
type PropertyValue struct {
	Value     interface{} `json:"value"`
	Quality   Quality     `json:"quality"`
	Timestamp time.Time   `json:"timestamp"`
}

// Property represents a tag (data point) belonging to a piece of equipment
type Property struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	EquipmentID string         `json:"equipmentId"`
	DataType    DataType       `json:"dataType"`
	Historize   bool           `json:"historize"`
	Current     *PropertyValue `json:"current,omitempty"`
}

// Alarm represents an alarm event in i3X format
type Alarm struct {
	ID            string        `json:"id"`
	PropertyID    string        `json:"propertyId"`
	PropertyName  string        `json:"propertyName"`
	EquipmentID   string        `json:"equipmentId"`
	EquipmentName string        `json:"equipmentName"`
	Severity      AlarmSeverity `json:"severity"`
	Status        AlarmStatus   `json:"status"`
	AlarmType     string        `json:"alarmType"`
	Message       string        `json:"message"`
	Value         interface{}   `json:"value,omitempty"`
	TriggerTime   time.Time     `json:"triggerTime"`
	ClearTime     *time.Time    `json:"clearTime,omitempty"`
	AckTime       *time.Time    `json:"ackTime,omitempty"`
	AckUser       *string       `json:"ackUser,omitempty"`
}

// WritePropertyRequest is the request body for writing a property value
type WritePropertyRequest struct {
	Value interface{} `json:"value" binding:"required"`
}

// ListResponse is a generic paginated response wrapper
type ListResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// PropertyHistoryPoint is a single raw sample from the tag historian.
type PropertyHistoryPoint struct {
	Value     *float64  `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

// MapDataType converts internal data type strings to i3X DataType
func MapDataType(internal string) DataType {
	switch internal {
	case "BOOL":
		return DataTypeBoolean
	case "INT", "DINT":
		return DataTypeInt32
	case "REAL":
		return DataTypeFloat
	default:
		return DataTypeString
	}
}

// MapQuality converts an internal quality integer and value presence to an OPC-UA quality code.
// Internal scale (after the driver unification): 0 = GOOD, 1 = UNCERTAIN, 2 = BAD.
// A missing value is always Bad regardless of the stored quality.
func MapQuality(q int, hasValue bool) Quality {
	if !hasValue {
		return QualityBad
	}
	switch q {
	case 0:
		return QualityGood
	case 1:
		return QualityUncertain
	default:
		// 2 (BAD) or any unexpected value — treat as Bad rather than silently
		// upgrading to Good (the previous behaviour misreported failed reads).
		return QualityBad
	}
}

// MapSeverity converts internal severity strings to i3X AlarmSeverity
func MapSeverity(s string) AlarmSeverity {
	switch s {
	case "warning":
		return AlarmSeverityWarning
	case "critical":
		return AlarmSeverityCritical
	default:
		return AlarmSeverityInfo
	}
}

// MapStatus converts internal alarm status strings to i3X AlarmStatus
func MapStatus(s string) AlarmStatus {
	switch s {
	case "ACKNOWLEDGED":
		return AlarmStatusAcknowledged
	case "CLEARED":
		return AlarmStatusCleared
	default:
		return AlarmStatusActive
	}
}
