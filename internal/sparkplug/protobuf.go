package sparkplug

// Sparkplug B Protobuf definitions
// Based on Eclipse Sparkplug B specification
// https://github.com/eclipse/sparkplug.b
//
// NOTE: This is a placeholder for true Protobuf support.
// To enable true Protobuf encoding:
// 1. Install protoc: https://grpc.io/docs/protoc-installation/
// 2. Get the Sparkplug B proto file from Eclipse Tahu:
//    https://github.com/eclipse/tahu/tree/master/sparkplug_b
// 3. Generate Go code: protoc --go_out=. sparkplug_b.proto
// 4. Replace this file with the generated code
// 5. Set UseProtobuf = true in client.go

import (
	"fmt"
)

// EncodePayload encodes a Sparkplug B payload to Protobuf binary format
// NOTE: This is a placeholder. When Protobuf is enabled, this will return an error.
func EncodePayload(payload *Payload) ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented - install protoc and generate protobuf code, or use JSON encoding (UseProtobuf = false)")
}

// DecodePayloadProtobuf decodes Protobuf binary data to a Sparkplug B payload
// NOTE: This is a placeholder. When Protobuf is enabled, this will return an error.
func DecodePayloadProtobuf(data []byte) (*Payload, error) {
	return nil, fmt.Errorf("Protobuf decoding not implemented - install protoc and generate protobuf code, or use JSON encoding (UseProtobuf = false)")
}

// CreateProtoPayload creates a Protobuf-encoded Sparkplug B payload
// NOTE: This is a placeholder.
func CreateProtoPayload(tags []TagData) ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented")
}

// CreateProtoDDATAPayload creates a Protobuf-encoded DDATA payload
// NOTE: This is a placeholder.
func CreateProtoDDATAPayload(deviceID string, tags []TagData) ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented")
}

// CreateProtoDBIRTHPayload creates a Protobuf-encoded DBIRTH payload
// NOTE: This is a placeholder.
func CreateProtoDBIRTHPayload(deviceID string, tags []TagData) ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented")
}

// CreateProtoNDEATHPayload creates a Protobuf-encoded NDEATH payload
// NOTE: This is a placeholder.
func CreateProtoNDEATHPayload() ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented")
}

// CreateProtoDDEATHPayload creates a Protobuf-encoded DDEATH payload
// NOTE: This is a placeholder.
func CreateProtoDDEATHPayload(deviceID string) ([]byte, error) {
	return nil, fmt.Errorf("Protobuf encoding not implemented")
}
