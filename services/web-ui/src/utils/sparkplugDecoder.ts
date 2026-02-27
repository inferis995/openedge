// Sparkplug B Protobuf Decoder for Frontend
// Decodes binary Protobuf messages according to Eclipse Tahu specification
// https://github.com/eclipse/tahu/blob/master/sparkplug_b/sparkplug_b.proto

// Field numbers for Payload message
const PAYLOAD_FIELDS = {
    TIMESTAMP: 1,  // fixed64/uint64
    METRICS: 2,    // repeated Metric
    SEQ: 3,        // uint32
    UUID: 4,       // string
    BODY: 5,       // bytes
} as const;

// Field numbers for Metric message
const METRIC_FIELDS = {
    NAME: 1,           // string
    ALIAS: 2,          // uint64
    TIMESTAMP: 3,      // uint64
    DATATYPE: 4,       // uint32
    IS_HISTORICAL: 5,  // bool
    IS_TRANSIENT: 6,   // bool
    IS_NULL: 7,        // bool
    METADATA: 8,       // MetaData
    PROPERTIES: 9,     // PropertySet
    INT_VALUE: 10,     // int32
    LONG_VALUE: 11,    // int64
    FLOAT_VALUE: 12,   // float
    DOUBLE_VALUE: 13,  // double
    BOOLEAN_VALUE: 14, // bool
    STRING_VALUE: 15,  // string
    BYTES_VALUE: 16,   // bytes
} as const;

// PropertySet fields
const PROPERTY_SET_FIELDS = {
    KEYS: 1,    // repeated string
    VALUES: 2,  // repeated PropertyValue
} as const;

// PropertyValue fields
const PROPERTY_VALUE_FIELDS = {
    TYPE: 1,          // uint32
    IS_NULL: 2,       // bool
    INT_VALUE: 3,     // uint32
    LONG_VALUE: 4,    // uint64
    FLOAT_VALUE: 5,   // float
    DOUBLE_VALUE: 6,  // double
    BOOLEAN_VALUE: 7, // bool
    STRING_VALUE: 8,  // string
} as const;

// DataType enum values
export const DataType = {
    Unknown: 0,
    Int8: 1,
    Int16: 2,
    Int32: 3,
    Int64: 4,
    UInt8: 5,
    UInt16: 6,
    UInt32: 7,
    UInt64: 8,
    Float: 9,
    Double: 10,
    Boolean: 11,
    String: 12,
    DateTime: 13,
    Text: 14,
} as const;

export interface DecodedMetric {
    name: string;
    alias: number;
    timestamp: number;
    dataType: number;
    isHistorical: boolean;
    isNull: boolean;
    value: number | boolean | string | null;
    quality: number;
}

export interface DecodedPayload {
    timestamp: number;
    seq: number;
    uuid: string;
    metrics: DecodedMetric[];
}

// Helper to read varint from buffer
function readVarint(buffer: Uint8Array, offset: number): { value: bigint; bytesRead: number } {
    let result = BigInt(0);
    let shift = 0;
    let bytesRead = 0;

    while (offset + bytesRead < buffer.length) {
        const byte = buffer[offset + bytesRead];
        result |= BigInt(byte & 0x7f) << BigInt(shift);
        bytesRead++;

        if ((byte & 0x80) === 0) {
            break;
        }
        shift += 7;
    }

    return { value: result, bytesRead };
}

// Helper to read fixed32 (4 bytes) as float
function readFloat(buffer: Uint8Array, offset: number): number {
    const view = new DataView(buffer.buffer, buffer.byteOffset + offset, 4);
    return view.getFloat32(0, true);
}

// Helper to read fixed64 as double
function readDouble(buffer: Uint8Array, offset: number): number {
    const view = new DataView(buffer.buffer, buffer.byteOffset + offset, 8);
    return view.getFloat64(0, true);
}

// Helper to read length-delimited field (string or bytes)
function readLengthDelimited(buffer: Uint8Array, offset: number): { value: Uint8Array; bytesRead: number } {
    const { value: length, bytesRead: lengthBytes } = readVarint(buffer, offset);
    const dataOffset = offset + lengthBytes;
    const value = buffer.slice(dataOffset, dataOffset + Number(length));
    return { value, bytesRead: lengthBytes + Number(length) };
}

// Helper to decode tag (field number + wire type)
function decodeTag(tag: bigint): { fieldNumber: number; wireType: number } {
    return {
        fieldNumber: Number(tag >> BigInt(3)),
        wireType: Number(tag & BigInt(7)),
    };
}

// Wire types
const WIRE_TYPE_VARINT = 0;
const WIRE_TYPE_FIXED64 = 1;
const WIRE_TYPE_LENGTH_DELIMITED = 2;
const WIRE_TYPE_FIXED32 = 5;

// Decode a PropertyValue message
function decodePropertyValue(buffer: Uint8Array): { type: number; value: number | boolean | string } {
    let offset = 0;
    let type = 0;
    let value: number | boolean | string = 0;

    while (offset < buffer.length) {
        const { value: tag, bytesRead: tagBytes } = readVarint(buffer, offset);
        offset += tagBytes;
        const { fieldNumber, wireType } = decodeTag(tag);

        switch (fieldNumber) {
            case PROPERTY_VALUE_FIELDS.TYPE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                type = Number(v);
                offset += bytesRead;
                break;
            }
            case PROPERTY_VALUE_FIELDS.INT_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                value = Number(v);
                offset += bytesRead;
                break;
            }
            case PROPERTY_VALUE_FIELDS.LONG_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                value = Number(v);
                offset += bytesRead;
                break;
            }
            case PROPERTY_VALUE_FIELDS.FLOAT_VALUE: {
                value = readFloat(buffer, offset);
                offset += 4;
                break;
            }
            case PROPERTY_VALUE_FIELDS.DOUBLE_VALUE: {
                value = readDouble(buffer, offset);
                offset += 8;
                break;
            }
            case PROPERTY_VALUE_FIELDS.BOOLEAN_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                value = v !== BigInt(0);
                offset += bytesRead;
                break;
            }
            case PROPERTY_VALUE_FIELDS.STRING_VALUE: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                value = new TextDecoder().decode(v);
                offset += bytesRead;
                break;
            }
            default: {
                // Skip unknown field
                offset = skipField(wireType, buffer, offset);
            }
        }
    }

    return { type, value };
}

// Decode a PropertySet message to extract quality
function decodePropertySet(buffer: Uint8Array): number {
    let offset = 0;
    const keys: string[] = [];
    const values: { type: number; value: number | boolean | string }[] = [];

    while (offset < buffer.length) {
        const { value: tag, bytesRead: tagBytes } = readVarint(buffer, offset);
        offset += tagBytes;
        const { fieldNumber, wireType } = decodeTag(tag);

        switch (fieldNumber) {
            case PROPERTY_SET_FIELDS.KEYS: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                keys.push(new TextDecoder().decode(v));
                offset += bytesRead;
                break;
            }
            case PROPERTY_SET_FIELDS.VALUES: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                values.push(decodePropertyValue(v));
                offset += bytesRead;
                break;
            }
            default: {
                offset = skipField(wireType, buffer, offset);
            }
        }
    }

    // Find "quality" key
    for (let i = 0; i < keys.length; i++) {
        if (keys[i] === 'quality' && i < values.length) {
            return Number(values[i].value) || 0;
        }
    }

    return 0;
}

// Skip a field based on wire type
function skipField(wireType: number, buffer: Uint8Array, offset: number): number {
    switch (wireType) {
        case WIRE_TYPE_VARINT: {
            const { bytesRead } = readVarint(buffer, offset);
            return offset + bytesRead;
        }
        case WIRE_TYPE_FIXED64:
            return offset + 8;
        case WIRE_TYPE_LENGTH_DELIMITED: {
            const { bytesRead } = readLengthDelimited(buffer, offset);
            return offset + bytesRead;
        }
        case WIRE_TYPE_FIXED32:
            return offset + 4;
        default:
            return offset;
    }
}

// Decode a single Metric message
function decodeMetric(buffer: Uint8Array): DecodedMetric {
    let offset = 0;
    const metric: DecodedMetric = {
        name: '',
        alias: 0,
        timestamp: 0,
        dataType: 0,
        isHistorical: false,
        isNull: false,
        value: null,
        quality: 0,
    };

    while (offset < buffer.length) {
        const { value: tag, bytesRead: tagBytes } = readVarint(buffer, offset);
        offset += tagBytes;
        const { fieldNumber, wireType } = decodeTag(tag);

        switch (fieldNumber) {
            case METRIC_FIELDS.NAME: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                metric.name = new TextDecoder().decode(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.ALIAS: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.alias = Number(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.TIMESTAMP: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.timestamp = Number(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.DATATYPE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.dataType = Number(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.IS_HISTORICAL: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.isHistorical = v !== BigInt(0);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.IS_NULL: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.isNull = v !== BigInt(0);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.PROPERTIES: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                metric.quality = decodePropertySet(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.INT_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                // Handle signed int32 (two's complement for negative values)
                let intVal = Number(v);
                if (intVal > 2147483647) {
                    intVal = intVal - 4294967296;
                }
                metric.value = intVal;
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.LONG_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.value = Number(v);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.FLOAT_VALUE: {
                metric.value = readFloat(buffer, offset);
                offset += 4;
                break;
            }
            case METRIC_FIELDS.DOUBLE_VALUE: {
                metric.value = readDouble(buffer, offset);
                offset += 8;
                break;
            }
            case METRIC_FIELDS.BOOLEAN_VALUE: {
                const { value: v, bytesRead } = readVarint(buffer, offset);
                metric.value = v !== BigInt(0);
                offset += bytesRead;
                break;
            }
            case METRIC_FIELDS.STRING_VALUE: {
                const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                metric.value = new TextDecoder().decode(v);
                offset += bytesRead;
                break;
            }
            default: {
                offset = skipField(wireType, buffer, offset);
            }
        }
    }

    return metric;
}

// Main decoder function - decodes a Sparkplug B Protobuf payload
export function decodeSparkplugB(data: Uint8Array | Buffer): DecodedPayload | null {
    try {
        const buffer = data instanceof Uint8Array ? data : new Uint8Array(data);
        let offset = 0;

        const payload: DecodedPayload = {
            timestamp: 0,
            seq: 0,
            uuid: '',
            metrics: [],
        };

        while (offset < buffer.length) {
            const { value: tag, bytesRead: tagBytes } = readVarint(buffer, offset);
            offset += tagBytes;
            const { fieldNumber, wireType } = decodeTag(tag);

            switch (fieldNumber) {
                case PAYLOAD_FIELDS.TIMESTAMP: {
                    const { value: v, bytesRead } = readVarint(buffer, offset);
                    payload.timestamp = Number(v);
                    offset += bytesRead;
                    break;
                }
                case PAYLOAD_FIELDS.METRICS: {
                    const { value: metricBuffer, bytesRead } = readLengthDelimited(buffer, offset);
                    const metric = decodeMetric(metricBuffer);
                    payload.metrics.push(metric);
                    offset += bytesRead;
                    break;
                }
                case PAYLOAD_FIELDS.SEQ: {
                    const { value: v, bytesRead } = readVarint(buffer, offset);
                    payload.seq = Number(v);
                    offset += bytesRead;
                    break;
                }
                case PAYLOAD_FIELDS.UUID: {
                    const { value: v, bytesRead } = readLengthDelimited(buffer, offset);
                    payload.uuid = new TextDecoder().decode(v);
                    offset += bytesRead;
                    break;
                }
                case PAYLOAD_FIELDS.BODY: {
                    const { bytesRead } = readLengthDelimited(buffer, offset);
                    offset += bytesRead;
                    break;
                }
                default: {
                    offset = skipField(wireType, buffer, offset);
                }
            }
        }

        return payload;
    } catch (error) {
        console.error('[SparkplugB Decoder] Error:', error);
        return null;
    }
}

// Check if data looks like Protobuf (not JSON)
export function isProtobufData(data: Uint8Array | Buffer): boolean {
    const buffer = data instanceof Uint8Array ? data : new Uint8Array(data);

    // Check if it starts with a valid Protobuf tag
    if (buffer.length < 2) return false;

    // First byte should be a tag (field number << 3 | wire type)
    const firstByte = buffer[0];
    const wireType = firstByte & 0x07;
    const fieldNumber = firstByte >> 3;

    // Valid wire types are 0, 1, 2, 5
    if (wireType > 5 || wireType === 3 || wireType === 4) return false;

    // Field number should be reasonable (1-100 for our use case)
    if (fieldNumber < 1 || fieldNumber > 100) return false;

    // Check if it's NOT valid JSON (starts with { or [)
    const firstChar = String.fromCharCode(buffer[0]);
    if (firstChar === '{' || firstChar === '[') return false;

    return true;
}

// Convert Sparkplug quality (0-255) to legacy quality (0=Good, 1=Uncertain, 2=Bad)
export function convertSparkplugQuality(quality: number): number {
    if (quality >= 192) return 0; // Good
    if (quality >= 64) return 1;  // Uncertain
    return 2; // Bad
}
