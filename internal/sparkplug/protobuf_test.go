package sparkplug

import (
	"testing"
)

func TestEncodeDecodePayload(t *testing.T) {
	// Create a test payload
	payload := &Payload{
		Timestamp: 1234567890000,
		Seq:       42,
		UUID:      "test-uuid-123",
		Metrics: []Metric{
			{
				Name:      "temperature",
				DataType:  DataTypeFloat,
				Timestamp: 1234567890000,
				Quality:   QualityGood,
				Value:     float32(23.5),
			},
			{
				Name:      "pressure",
				DataType:  DataTypeInt32,
				Timestamp: 1234567890000,
				Quality:   QualityGood,
				Value:     int32(100),
			},
			{
				Name:      "running",
				DataType:  DataTypeBoolean,
				Timestamp: 1234567890000,
				Quality:   QualityGood,
				Value:     true,
			},
			{
				Name:      "status",
				DataType:  DataTypeString,
				Timestamp: 1234567890000,
				Quality:   QualityUncert,
				Value:     "OK",
			},
		},
	}

	// Encode to Protobuf
	encoded, err := EncodePayload(payload)
	if err != nil {
		t.Fatalf("EncodePayload failed: %v", err)
	}

	t.Logf("Encoded payload size: %d bytes", len(encoded))

	// Decode back
	decoded, err := DecodePayloadProtobuf(encoded)
	if err != nil {
		t.Fatalf("DecodePayloadProtobuf failed: %v", err)
	}

	// Verify fields
	if decoded.Timestamp != payload.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, payload.Timestamp)
	}

	if decoded.Seq != payload.Seq {
		t.Errorf("Seq mismatch: got %d, want %d", decoded.Seq, payload.Seq)
	}

	if decoded.UUID != payload.UUID {
		t.Errorf("UUID mismatch: got %s, want %s", decoded.UUID, payload.UUID)
	}

	if len(decoded.Metrics) != len(payload.Metrics) {
		t.Fatalf("Metrics count mismatch: got %d, want %d", len(decoded.Metrics), len(payload.Metrics))
	}

	// Verify each metric
	for i, m := range decoded.Metrics {
		orig := payload.Metrics[i]
		if m.Name != orig.Name {
			t.Errorf("Metric %d: Name mismatch: got %s, want %s", i, m.Name, orig.Name)
		}
		if m.DataType != orig.DataType {
			t.Errorf("Metric %d: DataType mismatch: got %d, want %d", i, m.DataType, orig.DataType)
		}
		if m.Quality != orig.Quality {
			t.Errorf("Metric %d: Quality mismatch: got %d, want %d", i, m.Quality, orig.Quality)
		}
		t.Logf("Metric %d: Name=%s, Type=%d, Value=%v, Quality=%d", i, m.Name, m.DataType, m.Value, m.Quality)
	}
}

func TestCreateProtoPayload(t *testing.T) {
	tags := []TagData{
		{
			TagID:     1,
			DeviceID:  "sensor1",
			Value:     float32(25.5),
			DataType:  "REAL",
			Timestamp: 1234567890000,
			Quality:   0, // Good
			OrgID:     1,
		},
		{
			TagID:     2,
			DeviceID:  "counter",
			Value:     int32(42),
			DataType:  "DINT",
			Timestamp: 1234567890000,
			Quality:   0, // Good
			OrgID:     1,
		},
	}

	encoded, err := CreateProtoPayload(tags, 0)
	if err != nil {
		t.Fatalf("CreateProtoPayload failed: %v", err)
	}

	t.Logf("Encoded payload size: %d bytes", len(encoded))

	// Verify we can decode it
	decoded, err := DecodePayloadProtobuf(encoded)
	if err != nil {
		t.Fatalf("DecodePayloadProtobuf failed: %v", err)
	}

	if len(decoded.Metrics) != 2 {
		t.Errorf("Expected 2 metrics, got %d", len(decoded.Metrics))
	}

	for i, m := range decoded.Metrics {
		t.Logf("Metric %d: Name=%s, Type=%d, Value=%v", i, m.Name, m.DataType, m.Value)
	}
}

func TestEncodeMetricValue(t *testing.T) {
	tests := []struct {
		name     string
		dataType MetricDataType
		value    interface{}
	}{
		{"Boolean true", DataTypeBoolean, true},
		{"Boolean false", DataTypeBoolean, false},
		{"Int32", DataTypeInt32, int32(12345)},
		{"Int64", DataTypeInt64, int64(9876543210)},
		{"Float", DataTypeFloat, float32(3.14159)},
		{"Double", DataTypeDouble, float64(3.14159265358979)},
		{"String", DataTypeString, "hello world"},
		{"UInt32", DataTypeUInt32, uint32(4294967295)},
		{"UInt64", DataTypeUInt64, uint64(18446744073709551615)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &Payload{
				Timestamp: 1234567890000,
				Seq:       1,
				Metrics: []Metric{
					{
						Name:      tt.name,
						DataType:  tt.dataType,
						Timestamp: 1234567890000,
						Quality:   QualityGood,
						Value:     tt.value,
					},
				},
			}

			encoded, err := EncodePayload(payload)
			if err != nil {
				t.Fatalf("EncodePayload failed: %v", err)
			}

			decoded, err := DecodePayloadProtobuf(encoded)
			if err != nil {
				t.Fatalf("DecodePayloadProtobuf failed: %v", err)
			}

			if len(decoded.Metrics) != 1 {
				t.Fatalf("Expected 1 metric, got %d", len(decoded.Metrics))
			}

			m := decoded.Metrics[0]
			if m.DataType != tt.dataType {
				t.Errorf("DataType mismatch: got %d, want %d", m.DataType, tt.dataType)
			}

			t.Logf("Value: %v (type: %T)", m.Value, m.Value)
		})
	}
}

func TestQualityConversion(t *testing.T) {
	// Test that quality is encoded and decoded correctly
	payload := &Payload{
		Timestamp: 1234567890000,
		Seq:       1,
		Metrics: []Metric{
			{
				Name:      "good_metric",
				DataType:  DataTypeInt32,
				Timestamp: 1234567890000,
				Quality:   QualityGood, // 192
				Value:     int32(100),
			},
			{
				Name:      "uncertain_metric",
				DataType:  DataTypeInt32,
				Timestamp: 1234567890000,
				Quality:   QualityUncert, // 64
				Value:     int32(200),
			},
			{
				Name:      "bad_metric",
				DataType:  DataTypeInt32,
				Timestamp: 1234567890000,
				Quality:   QualityBad, // 0
				Value:     int32(300),
			},
		},
	}

	encoded, err := EncodePayload(payload)
	if err != nil {
		t.Fatalf("EncodePayload failed: %v", err)
	}

	decoded, err := DecodePayloadProtobuf(encoded)
	if err != nil {
		t.Fatalf("DecodePayloadProtobuf failed: %v", err)
	}

	// Check quality values
	expected := []int32{QualityGood, QualityUncert, QualityBad}
	for i, m := range decoded.Metrics {
		if m.Quality != expected[i] {
			t.Errorf("Metric %d: Quality mismatch: got %d, want %d", i, m.Quality, expected[i])
		}
		t.Logf("Metric %d: Quality=%d", i, m.Quality)
	}
}
