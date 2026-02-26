package sparkplug

// Sparkplug B Protobuf encoding implementation
// Based on official Eclipse Tahu specification
// https://github.com/eclipse/tahu/blob/master/sparkplug_b/sparkplug_b.proto
//
// Field numbers MUST match the official specification for interoperability!

import (
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// ============================================================================
// Payload Field Numbers (from official sparkplug_b.proto)
// ============================================================================
const (
	// Payload message fields
	payloadTimestampField protowire.Number = 1 // uint64 - Timestamp at message sending time
	payloadMetricsField   protowire.Number = 2 // repeated Metric - The metrics
	payloadSeqField       protowire.Number = 3 // uint32 - Sequence number
	payloadUuidField      protowire.Number = 4 // string - UUID to track message type
	payloadBodyField      protowire.Number = 5 // bytes - Optional body
)

// ============================================================================
// Metric Field Numbers (from official sparkplug_b.proto)
// ============================================================================
const (
	metricNameField         protowire.Number = 1  // string - Metric name
	metricAliasField        protowire.Number = 2  // uint64 - Metric alias
	metricTimestampField    protowire.Number = 3  // uint64 - Timestamp
	metricDataTypeField     protowire.Number = 4  // uint32 - DataType enum
	metricIsHistoricalField protowire.Number = 5  // bool - Is historical data
	metricIsTransientField  protowire.Number = 6  // bool - Is transient (don't store)
	metricIsNullField       protowire.Number = 7  // bool - Is null value
	metricMetaDataField     protowire.Number = 8  // MetaData message
	metricPropertiesField   protowire.Number = 9  // PropertySet message

	// Value fields (oneof)
	metricIntValueField     protowire.Number = 10 // int32
	metricLongValueField    protowire.Number = 11 // int64
	metricFloatValueField   protowire.Number = 12 // float32
	metricDoubleValueField  protowire.Number = 13 // float64
	metricBooleanValueField protowire.Number = 14 // bool
	metricStringValueField  protowire.Number = 15 // string
	metricBytesValueField   protowire.Number = 16 // bytes
	metricDataSetField      protowire.Number = 17 // DataSet
	metricTemplateField     protowire.Number = 18 // Template
)

// ============================================================================
// PropertySet Field Numbers (for Quality)
// ============================================================================
const (
	propertySetKeysField   protowire.Number = 1 // repeated string
	propertySetValuesField protowire.Number = 2 // repeated PropertyValue
)

// PropertyValue fields
const (
	propertyValueTypeField     protowire.Number = 1 // uint32
	propertyValueIsNullField   protowire.Number = 2 // bool
	propertyValueIntField      protowire.Number = 3 // uint32
	propertyValueLongField     protowire.Number = 4 // uint64
	propertyValueFloatField    protowire.Number = 5 // float32
	propertyValueDoubleField   protowire.Number = 6 // float64
	propertyValueBooleanField  protowire.Number = 7 // bool
	propertyValueStringField   protowire.Number = 8 // string
)

// EncodePayload encodes a Sparkplug B payload to Protobuf binary format
// Following the official Eclipse Tahu specification
func EncodePayload(payload *Payload) ([]byte, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload is nil")
	}

	var data []byte

	// Encode timestamp (field 1, varint for uint64)
	if payload.Timestamp > 0 {
		data = protowire.AppendTag(data, payloadTimestampField, protowire.VarintType)
		data = protowire.AppendVarint(data, uint64(payload.Timestamp))
	}

	// Encode metrics (field 2, repeated message)
	for _, metric := range payload.Metrics {
		metricData, err := encodeMetric(&metric)
		if err != nil {
			return nil, fmt.Errorf("failed to encode metric %s: %w", metric.Name, err)
		}
		data = protowire.AppendTag(data, payloadMetricsField, protowire.BytesType)
		data = protowire.AppendBytes(data, metricData)
	}

	// Encode sequence number (field 3, varint)
	data = protowire.AppendTag(data, payloadSeqField, protowire.VarintType)
	data = protowire.AppendVarint(data, uint64(payload.Seq))

	// Encode UUID if present (field 4, string)
	if payload.UUID != "" {
		data = protowire.AppendTag(data, payloadUuidField, protowire.BytesType)
		data = protowire.AppendString(data, payload.UUID)
	}

	// Body (field 5) is optional, skip if empty

	return data, nil
}

// encodeMetric encodes a single metric to Protobuf format
func encodeMetric(metric *Metric) ([]byte, error) {
	if metric == nil {
		return nil, fmt.Errorf("metric is nil")
	}

	var data []byte

	// Encode name (field 1, string)
	if metric.Name != "" {
		data = protowire.AppendTag(data, metricNameField, protowire.BytesType)
		data = protowire.AppendString(data, metric.Name)
	}

	// Encode alias (field 2, uint64) - optional, skip if 0
	// Note: alias is used for bandwidth optimization after DBIRTH

	// Encode timestamp (field 3, uint64)
	if metric.Timestamp > 0 {
		data = protowire.AppendTag(data, metricTimestampField, protowire.VarintType)
		data = protowire.AppendVarint(data, uint64(metric.Timestamp))
	}

	// Encode data type (field 4, uint32) - REQUIRED
	data = protowire.AppendTag(data, metricDataTypeField, protowire.VarintType)
	data = protowire.AppendVarint(data, uint64(metric.DataType))

	// Encode is_historical (field 5, bool)
	if metric.IsHistorical {
		data = protowire.AppendTag(data, metricIsHistoricalField, protowire.VarintType)
		data = protowire.AppendVarint(data, 1)
	}

	// Encode is_transient (field 6, bool) - skip, not used
	// Encode is_null (field 7, bool)
	if metric.IsNull {
		data = protowire.AppendTag(data, metricIsNullField, protowire.VarintType)
		data = protowire.AppendVarint(data, 1)
	}

	// Encode properties (field 9) - used for Quality in Sparkplug B
	if metric.Quality > 0 {
		propertiesData := encodeQualityProperty(metric.Quality)
		data = protowire.AppendTag(data, metricPropertiesField, protowire.BytesType)
		data = protowire.AppendBytes(data, propertiesData)
	}

	// Encode value based on data type (fields 10-16)
	if !metric.IsNull && metric.Value != nil {
		data = encodeMetricValue(data, metric.DataType, metric.Value)
	}

	return data, nil
}

// encodeQualityProperty creates a PropertySet with the quality value
// In Sparkplug B, quality is stored as a property with key "quality"
func encodeQualityProperty(quality int32) []byte {
	var data []byte

	// Keys (field 1)
	data = protowire.AppendTag(data, propertySetKeysField, protowire.BytesType)
	data = protowire.AppendString(data, "quality")

	// PropertyValue for quality (field 2)
	propValue := encodePropertyValue(uint32(DataTypeInt32), int32(quality))
	data = protowire.AppendTag(data, propertySetValuesField, protowire.BytesType)
	data = protowire.AppendBytes(data, propValue)

	return data
}

// encodePropertyValue encodes a PropertyValue message
func encodePropertyValue(dataType uint32, value interface{}) []byte {
	var data []byte

	// Type (field 1)
	data = protowire.AppendTag(data, propertyValueTypeField, protowire.VarintType)
	data = protowire.AppendVarint(data, uint64(dataType))

	// Value based on type
	switch v := value.(type) {
	case int32:
		data = protowire.AppendTag(data, propertyValueIntField, protowire.VarintType)
		data = protowire.AppendVarint(data, uint64(v))
	case int64:
		data = protowire.AppendTag(data, propertyValueLongField, protowire.VarintType)
		data = protowire.AppendVarint(data, uint64(v))
	case uint32:
		data = protowire.AppendTag(data, propertyValueIntField, protowire.VarintType)
		data = protowire.AppendVarint(data, uint64(v))
	case uint64:
		data = protowire.AppendTag(data, propertyValueLongField, protowire.VarintType)
		data = protowire.AppendVarint(data, v)
	case float32:
		data = protowire.AppendTag(data, propertyValueFloatField, protowire.Fixed32Type)
		data = protowire.AppendFixed32(data, math.Float32bits(v))
	case float64:
		data = protowire.AppendTag(data, propertyValueDoubleField, protowire.Fixed64Type)
		data = protowire.AppendFixed64(data, math.Float64bits(v))
	case bool:
		data = protowire.AppendTag(data, propertyValueBooleanField, protowire.VarintType)
		if v {
			data = protowire.AppendVarint(data, 1)
		} else {
			data = protowire.AppendVarint(data, 0)
		}
	case string:
		data = protowire.AppendTag(data, propertyValueStringField, protowire.BytesType)
		data = protowire.AppendString(data, v)
	}

	return data
}

// encodeMetricValue encodes the metric value based on its data type
func encodeMetricValue(data []byte, dataType MetricDataType, value interface{}) []byte {
	switch dataType {
	case DataTypeBoolean:
		if v, ok := toBoolValue(value); ok {
			data = protowire.AppendTag(data, metricBooleanValueField, protowire.VarintType)
			if v {
				data = protowire.AppendVarint(data, 1)
			} else {
				data = protowire.AppendVarint(data, 0)
			}
		}

	case DataTypeInt8, DataTypeInt16, DataTypeInt32:
		if v, ok := toInt32Value(value); ok {
			data = protowire.AppendTag(data, metricIntValueField, protowire.VarintType)
			data = protowire.AppendVarint(data, uint64(int32(v)))
		}

	case DataTypeInt64:
		if v, ok := toInt64Value(value); ok {
			data = protowire.AppendTag(data, metricLongValueField, protowire.VarintType)
			data = protowire.AppendVarint(data, uint64(v))
		}

	case DataTypeUInt8, DataTypeUInt16, DataTypeUInt32:
		if v, ok := toUInt32Value(value); ok {
			data = protowire.AppendTag(data, metricIntValueField, protowire.VarintType)
			data = protowire.AppendVarint(data, uint64(v))
		}

	case DataTypeUInt64:
		if v, ok := toUInt64Value(value); ok {
			data = protowire.AppendTag(data, metricLongValueField, protowire.VarintType)
			data = protowire.AppendVarint(data, v)
		}

	case DataTypeFloat:
		if v, ok := toFloat32Value(value); ok {
			data = protowire.AppendTag(data, metricFloatValueField, protowire.Fixed32Type)
			data = protowire.AppendFixed32(data, math.Float32bits(v))
		}

	case DataTypeDouble:
		if v, ok := toFloat64Value(value); ok {
			data = protowire.AppendTag(data, metricDoubleValueField, protowire.Fixed64Type)
			data = protowire.AppendFixed64(data, math.Float64bits(v))
		}

	case DataTypeString, DataTypeText:
		if v, ok := toStringValue(value); ok {
			data = protowire.AppendTag(data, metricStringValueField, protowire.BytesType)
			data = protowire.AppendString(data, v)
		}

	case DataTypeBytes:
		if v, ok := toBytesValue(value); ok {
			data = protowire.AppendTag(data, metricBytesValueField, protowire.BytesType)
			data = protowire.AppendBytes(data, v)
		}

	default:
		// For unknown types, try to encode as string
		if v := fmt.Sprintf("%v", value); v != "" {
			data = protowire.AppendTag(data, metricStringValueField, protowire.BytesType)
			data = protowire.AppendString(data, v)
		}
	}

	return data
}

// DecodePayloadProtobuf decodes Protobuf binary data to a Sparkplug B payload
func DecodePayloadProtobuf(data []byte) (*Payload, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	payload := &Payload{
		Metrics: []Metric{},
	}

	var fieldNum protowire.Number
	var wireType protowire.Type
	var offset int

	for offset < len(data) {
		tag, n := protowire.ConsumeVarint(data[offset:])
		if n < 0 {
			return nil, fmt.Errorf("failed to read field tag at offset %d", offset)
		}
		offset += n

		fieldNum, wireType = protowire.DecodeTag(tag)
		if fieldNum == 0 {
			return nil, fmt.Errorf("invalid field number at offset %d", offset)
		}

		switch fieldNum {
		case payloadTimestampField:
			if wireType != protowire.VarintType {
				return nil, fmt.Errorf("unexpected wire type for timestamp: %v", wireType)
			}
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read timestamp")
			}
			offset += n
			payload.Timestamp = int64(v)

		case payloadMetricsField:
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("unexpected wire type for metrics: %v", wireType)
			}
			metricData, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read metric")
			}
			offset += n

			metric, err := decodeMetric(metricData)
			if err != nil {
				return nil, fmt.Errorf("failed to decode metric: %w", err)
			}
			payload.Metrics = append(payload.Metrics, *metric)

		case payloadSeqField:
			if wireType != protowire.VarintType {
				return nil, fmt.Errorf("unexpected wire type for seq: %v", wireType)
			}
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read seq")
			}
			offset += n
			payload.Seq = v

		case payloadUuidField:
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("unexpected wire type for uuid: %v", wireType)
			}
			v, n := protowire.ConsumeString(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read uuid")
			}
			offset += n
			payload.UUID = v

		case payloadBodyField:
			if wireType != protowire.BytesType {
				return nil, fmt.Errorf("unexpected wire type for body: %v", wireType)
			}
			_, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read body")
			}
			offset += n

		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to skip unknown field %d", fieldNum)
			}
			offset += n
		}
	}

	return payload, nil
}

// decodeMetric decodes a single metric from Protobuf format
func decodeMetric(data []byte) (*Metric, error) {
	metric := &Metric{}

	var fieldNum protowire.Number
	var wireType protowire.Type
	var offset int

	for offset < len(data) {
		tag, n := protowire.ConsumeVarint(data[offset:])
		if n < 0 {
			return nil, fmt.Errorf("failed to read field tag at offset %d", offset)
		}
		offset += n

		fieldNum, wireType = protowire.DecodeTag(tag)
		if fieldNum == 0 {
			return nil, fmt.Errorf("invalid field number at offset %d", offset)
		}

		switch fieldNum {
		case metricNameField:
			v, n := protowire.ConsumeString(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read name")
			}
			offset += n
			metric.Name = v

		case metricAliasField:
			_, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read alias")
			}
			offset += n
			// alias stored but not used in our Metric struct

		case metricTimestampField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read timestamp")
			}
			offset += n
			metric.Timestamp = int64(v)

		case metricDataTypeField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read data type")
			}
			offset += n
			metric.DataType = MetricDataType(v)

		case metricIsHistoricalField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read is_historical")
			}
			offset += n
			metric.IsHistorical = v != 0

		case metricIsTransientField:
			_, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read is_transient")
			}
			offset += n
			// is_transient not stored

		case metricIsNullField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read is_null")
			}
			offset += n
			metric.IsNull = v != 0

		case metricMetaDataField:
			// Skip metadata for now
			_, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read metadata")
			}
			offset += n

		case metricPropertiesField:
			propsData, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read properties")
			}
			offset += n
			// Decode properties to extract quality
			quality := decodeQualityFromProperties(propsData)
			if quality > 0 {
				metric.Quality = quality
			}

		case metricIntValueField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read int_value")
			}
			offset += n
			// Decode as signed int32 (zigzag not used in proto2)
			metric.Value = int32(v)

		case metricLongValueField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read long_value")
			}
			offset += n
			metric.Value = int64(v)

		case metricFloatValueField:
			v, n := protowire.ConsumeFixed32(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read float_value")
			}
			offset += n
			metric.Value = math.Float32frombits(v)

		case metricDoubleValueField:
			v, n := protowire.ConsumeFixed64(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read double_value")
			}
			offset += n
			metric.Value = math.Float64frombits(v)

		case metricBooleanValueField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read boolean_value")
			}
			offset += n
			metric.Value = v != 0

		case metricStringValueField:
			v, n := protowire.ConsumeString(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read string_value")
			}
			offset += n
			metric.Value = v

		case metricBytesValueField:
			v, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to read bytes_value")
			}
			offset += n
			metric.Value = v

		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data[offset:])
			if n < 0 {
				return nil, fmt.Errorf("failed to skip unknown field %d", fieldNum)
			}
			offset += n
		}
	}

	return metric, nil
}

// decodeQualityFromProperties extracts quality from PropertySet
func decodeQualityFromProperties(data []byte) int32 {
	var keys []string
	var values []int32
	var fieldNum protowire.Number
	var wireType protowire.Type
	var offset int

	for offset < len(data) {
		tag, n := protowire.ConsumeVarint(data[offset:])
		if n < 0 {
			return 0
		}
		offset += n

		fieldNum, wireType = protowire.DecodeTag(tag)

		switch fieldNum {
		case propertySetKeysField:
			v, n := protowire.ConsumeString(data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
			keys = append(keys, v)

		case propertySetValuesField:
			propData, n := protowire.ConsumeBytes(data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
			val := decodePropertyValue(propData)
			values = append(values, val)

		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
		}
	}

	// Find "quality" key and return its value
	for i, key := range keys {
		if key == "quality" && i < len(values) {
			return values[i]
		}
	}

	return 0
}

// decodePropertyValue decodes a PropertyValue message
func decodePropertyValue(data []byte) int32 {
	var result int32
	var propType uint32
	var fieldNum protowire.Number
	var wireType protowire.Type
	var offset int

	for offset < len(data) {
		tag, n := protowire.ConsumeVarint(data[offset:])
		if n < 0 {
			return 0
		}
		offset += n

		fieldNum, wireType = protowire.DecodeTag(tag)

		switch fieldNum {
		case propertyValueTypeField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
			propType = uint32(v)

		case propertyValueIntField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
			result = int32(v)

		case propertyValueLongField:
			v, n := protowire.ConsumeVarint(data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
			result = int32(v)

		default:
			n := protowire.ConsumeFieldValue(fieldNum, wireType, data[offset:])
			if n < 0 {
				return 0
			}
			offset += n
		}
	}

	_ = propType // type info available if needed
	return result
}

// CreateProtoPayload creates a Protobuf-encoded Sparkplug B payload from tag data
func CreateProtoPayload(tags []TagData) ([]byte, error) {
	payload := CreatePayload(tags)
	return EncodePayload(payload)
}

// CreateProtoDDATAPayload creates a Protobuf-encoded DDATA payload
func CreateProtoDDATAPayload(deviceID string, tags []TagData) ([]byte, error) {
	payload := CreateDDATAPayload(deviceID, tags)
	return EncodePayload(payload)
}

// CreateProtoDBIRTHPayload creates a Protobuf-encoded DBIRTH payload
func CreateProtoDBIRTHPayload(deviceID string, tags []TagData) ([]byte, error) {
	payload := CreateDBIRTHPayload(deviceID, tags)
	return EncodePayload(payload)
}

// CreateProtoNDEATHPayload creates a Protobuf-encoded NDEATH payload
func CreateProtoNDEATHPayload() ([]byte, error) {
	payload := &Payload{
		Timestamp: time.Now().UnixMilli(),
		Seq:       0,
		Metrics:   []Metric{},
	}
	return EncodePayload(payload)
}

// CreateProtoDDEATHPayload creates a Protobuf-encoded DDEATH payload
func CreateProtoDDEATHPayload(deviceID string) ([]byte, error) {
	payload := &Payload{
		Timestamp: time.Now().UnixMilli(),
		Seq:       0,
		Metrics:   []Metric{},
	}
	return EncodePayload(payload)
}

// ============================================================================
// Value conversion helpers
// ============================================================================

func toBoolValue(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return val != 0, true
	case float32, float64:
		return val != 0, true
	case string:
		return val == "true" || val == "1" || val == "on", true
	default:
		return false, false
	}
}

func toInt32Value(v interface{}) (int32, bool) {
	switch val := v.(type) {
	case int:
		return int32(val), true
	case int8:
		return int32(val), true
	case int16:
		return int32(val), true
	case int32:
		return val, true
	case int64:
		return int32(val), true
	case uint:
		return int32(val), true
	case uint8:
		return int32(val), true
	case uint16:
		return int32(val), true
	case uint32:
		return int32(val), true
	case uint64:
		return int32(val), true
	case float32:
		return int32(val), true
	case float64:
		return int32(val), true
	default:
		return 0, false
	}
}

func toInt64Value(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int8:
		return int64(val), true
	case int16:
		return int64(val), true
	case int32:
		return int64(val), true
	case int64:
		return val, true
	case uint:
		return int64(val), true
	case uint8:
		return int64(val), true
	case uint16:
		return int64(val), true
	case uint32:
		return int64(val), true
	case uint64:
		return int64(val), true
	case float32:
		return int64(val), true
	case float64:
		return int64(val), true
	default:
		return 0, false
	}
}

func toUInt32Value(v interface{}) (uint32, bool) {
	switch val := v.(type) {
	case int:
		return uint32(val), true
	case int8:
		return uint32(val), true
	case int16:
		return uint32(val), true
	case int32:
		return uint32(val), true
	case int64:
		return uint32(val), true
	case uint:
		return uint32(val), true
	case uint8:
		return uint32(val), true
	case uint16:
		return uint32(val), true
	case uint32:
		return val, true
	case uint64:
		return uint32(val), true
	case float32:
		return uint32(val), true
	case float64:
		return uint32(val), true
	default:
		return 0, false
	}
}

func toUInt64Value(v interface{}) (uint64, bool) {
	switch val := v.(type) {
	case int:
		return uint64(val), true
	case int8:
		return uint64(val), true
	case int16:
		return uint64(val), true
	case int32:
		return uint64(val), true
	case int64:
		return uint64(val), true
	case uint:
		return uint64(val), true
	case uint8:
		return uint64(val), true
	case uint16:
		return uint64(val), true
	case uint32:
		return uint64(val), true
	case uint64:
		return val, true
	case float32:
		return uint64(val), true
	case float64:
		return uint64(val), true
	default:
		return 0, false
	}
}

func toFloat32Value(v interface{}) (float32, bool) {
	switch val := v.(type) {
	case float32:
		return val, true
	case float64:
		return float32(val), true
	case int:
		return float32(val), true
	case int8:
		return float32(val), true
	case int16:
		return float32(val), true
	case int32:
		return float32(val), true
	case int64:
		return float32(val), true
	case uint:
		return float32(val), true
	case uint8:
		return float32(val), true
	case uint16:
		return float32(val), true
	case uint32:
		return float32(val), true
	case uint64:
		return float32(val), true
	default:
		return 0, false
	}
}

func toFloat64Value(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}

func toStringValue(v interface{}) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func toBytesValue(v interface{}) ([]byte, bool) {
	switch val := v.(type) {
	case []byte:
		return val, true
	case string:
		return []byte(val), true
	default:
		return nil, false
	}
}
