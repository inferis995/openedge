// Sparkplug B Payload type definitions
// Based on Eclipse Sparkplug B specification
// https://github.com/eclipse/tahu/blob/master/sparkplug_b/sparkplug_b.proto
//
// These types are used for reference and documentation purposes.
// Actual encoding/decoding is handled by protobuf.go using protowire.

package sparkplug

// ProtoPayload represents the top-level Sparkplug B payload (for reference)
// Actual encoding uses the Payload type in types.go
type ProtoPayload struct {
	Timestamp uint64         // milliseconds since epoch
	Seq       uint32         // sequence number (rolls over at 256)
	Uuid      string         // unique identifier
	Body      []byte         // optional binary data
	Metrics   []*ProtoMetric // list of metrics
}

// ProtoMetric represents a single metric in a Sparkplug B payload
type ProtoMetric struct {
	Name         string  // metric name/alias
	Alias        uint64  // numeric alias
	Timestamp    uint64  // milliseconds since epoch
	DataType     uint32  // data type (see DataType constants)
	IsHistorical bool    // is this a historical value
	IsNull       bool    // is the value null
	Quality      uint32  // quality value

	// Value fields - only one should be set based on DataType
	IntValue     int32
	LongValue    int64
	FloatValue   float32
	DoubleValue  float64
	BooleanValue bool
	StringValue  string
	BytesValue   []byte
}

// ProtoMetricMetaData contains optional metric metadata
type ProtoMetricMetaData struct {
	IsMultiPart bool
	ContentType string
	Size        uint64
	Seq         uint32
	FileName    string
	FileType    string
	Md5         string
	Description string
}

// ProtoPropertySet contains a set of properties
type ProtoPropertySet struct {
	Keys   []string
	Values []*ProtoPropertyValue
}

// ProtoPropertyValue represents a property value
type ProtoPropertyValue struct {
	Type         uint32
	IsNull       bool
	IntValue     int32
	LongValue    int64
	FloatValue   float32
	DoubleValue  float64
	BooleanValue bool
	StringValue  string
}

// ProtoDataSet represents a dataset value
type ProtoDataSet struct {
	NumOfColumns uint32
	Columns      []string
	Types        []uint32
	Rows         []*ProtoDataSetRow
}

// ProtoDataSetRow represents a row in a dataset
type ProtoDataSetRow struct {
	Elements []*ProtoDataSetValue
}

// ProtoDataSetValue represents a value in a dataset row
type ProtoDataSetValue struct {
	IntValue     int32
	LongValue    int64
	FloatValue   float32
	DoubleValue  float64
	BooleanValue bool
	StringValue  string
	BytesValue   []byte
}

// ProtoTemplate represents a template value
type ProtoTemplate struct {
	Version      string
	Metrics      []*ProtoMetric
	Parameters   []*ProtoParameter
	TemplateRef  string
	IsDefinition bool
}

// ProtoParameter represents a template parameter
type ProtoParameter struct {
	Name  string
	Type  uint32
	Value interface{}
}
