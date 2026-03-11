# PRD: Fix OPC UA Type Mismatch & Quality Issues

**Status:** ✅ COMPLETED
**Priority:** High
**Created:** 2025-03-11
**Updated:** 2025-03-11
**Author:** Claude

---

## Executive Summary

Fixed three critical bugs in the OPC UA driver that were causing:
1. **Type mismatch errors** when writing values via MQTT
2. **Quality showing as UNKNOWN** instead of GOOD
3. **Auto-reload loop** causing driver to disconnect repeatedly

---

## 1. Problem Statement

### 1.1 Type Mismatch (Write)
When writing to OPC UA tags via MQTT broker, the write operation failed with **type mismatch** errors. The OPC UA driver did not correctly convert values to the expected OPC UA server data types.

### 1.2 Quality UNKNOWN (Read/Publish)
Tags showed **UNKNOWN** quality in the UI instead of **GOOD**, even when the OPC UA server was responding correctly.

### 1.3 Auto-Reload Loop
The driver was stuck in an infinite loop:
1. Driver publishes `online` health status
2. Driver receives its own message (subscribed to `sys/health/+`)
3. Triggers `loadConfig()` which **disconnects** the OPC UA client
4. Health loop publishes `offline` (client disconnected)
5. Historian marks tags offline with `q:1` (BAD)
6. Cycle repeats

---

## 2. Root Cause Analysis

### 2.1 Type Mismatch Root Cause
In `internal/opcua/client.go`:
- Function `WriteValue()` reads `opcuaType` from server (line 369)
- But calls `convertToVariant(value, dataType)` instead of `convertToVariantByOpcUaType(value, opcuaType)`
- The function `convertToVariantByOpcUaType()` exists but was **never used**

### 2.2 Quality UNKNOWN Root Cause
In `services/driver-opcua/main.go`:
- OPC UA client returns quality as `0` (GOOD) or `1` (BAD)
- Driver published this directly to MQTT
- But industrial standard is `192` (GOOD) or `0` (BAD)
- The conversion existed for alarm manager but **not for publish**

### 2.3 Auto-Reload Loop Root Cause
In `services/driver-opcua/main.go`:
- Driver subscribes to `sys/health/+` (all gateways)
- When it receives its own `online` message, it triggers `loadConfig()`
- `loadConfig()` disconnects and recreates the OPC UA client
- This causes a brief disconnection, triggering `offline` status
- The historian receives `offline` and marks all tags with `q:1` (BAD)

---

## 3. Implementation

### 3.1 Fix #1: Type Mismatch

**File:** `internal/opcua/client.go`

**Change in `WriteValue()` function:**
```go
// BEFORE
} else {
    v, err := convertToVariant(value, dataType)
    // ...
}

// AFTER
} else {
    var v *ua.Variant
    var err error
    conversionType := dataType

    if opcuaType != "" && opcuaType != "Unknown" && opcuaType != "Custom" {
        v, err = convertToVariantByOpcUaType(value, opcuaType)
        conversionType = opcuaType
        log.Printf("[OPC-UA WRITE] Using server type '%s' for conversion", opcuaType)
    } else {
        v, err = convertToVariant(value, dataType)
    }
    // ...
}
```

**Enhanced `convertToVariantByOpcUaType()`:**
- Fixed logic to handle `float64` (JSON numbers) correctly
- Changed order: numeric conversion first, then boolean fallback
- Added proper handling for all OPC UA types: Int16, UInt16, Int32, UInt32, Int64, UInt64, SByte, Byte, Float, Double

### 3.2 Fix #2: Quality Conversion

**File:** `services/driver-opcua/main.go`

**Change in poll loop (around line 830):**
```go
// BEFORE
if d.shouldPublish(tag.ID, value, quality) {
    d.publishTagValue(tag, value, quality)
    d.updateState(tag.ID, value, quality)
}

// AFTER
if d.shouldPublish(tag.ID, value, alarmQuality) {
    log.Printf("[OPC-UA Driver] Tag %d (%s): value=%v, quality=%s (published as %d) - PUBLISHING",
        tag.ID, tag.Alias, value, qualityStr, alarmQuality)
    d.publishTagValue(tag, value, alarmQuality)
    d.updateState(tag.ID, value, alarmQuality)
}
```

**Also fixed error case (around line 805):**
```go
// BEFORE
if d.shouldPublish(tag.ID, val, 2) {
    d.publishTagValue(tag, val, 2) // BAD quality

// AFTER
if d.shouldPublish(tag.ID, val, 0) {
    d.publishTagValue(tag, val, 0) // 0 = BAD in industrial standard
```

### 3.3 Fix #3: Auto-Reload Loop

**File:** `services/driver-opcua/main.go`

**Change in `handleHealthMessage()`:**
```go
// BEFORE
func (d *Driver) handleHealthMessage(topic string, payload []byte) {
    // ...
    if healthGatewayID != d.gatewayID {
        return
    }
    if status == "online" {
        log.Printf("[OPC-UA Driver] Gateway %d is ONLINE - auto-reloading...", d.gatewayID)
        d.loadConfig()
    }
}

// AFTER
func (d *Driver) handleHealthMessage(topic string, payload []byte) {
    // ...
    // CRITICAL: Skip processing health messages for this driver's own gateway
    if healthGatewayID == d.gatewayID {
        log.Printf("[OPC-UA Driver] Skipping own health message for gateway %d (status: %s)",
            d.gatewayID, string(payload))
        return
    }
    // Process health events for OTHER gateways only
    log.Printf("[OPC-UA Driver] Gateway %d health status: %s (not triggering auto-reload)",
        healthGatewayID, status)
}
```

---

## 4. Test Results

### 4.1 Unit Tests

**File:** `internal/opcua/client_test.go` (NEW)

```
=== RUN   TestConvertToVariantByOpcUaType
    --- PASS: bool_to_Boolean
    --- PASS: float64_1.0_to_Boolean
    --- PASS: float64_0.0_to_Boolean
    --- PASS: float64_to_Int16
    --- PASS: int_to_Int16
    --- PASS: bool_true_to_Int16
    --- PASS: bool_false_to_Int16
    --- PASS: float64_to_UInt16
    --- PASS: float64_max_UInt16
    --- PASS: float64_to_Int32
    --- PASS: float64_to_Int_(alias)
    --- PASS: float64_to_UInt32
    --- PASS: float64_to_Int64
    --- PASS: float64_to_UInt64
    --- PASS: float64_to_SByte
    --- PASS: float64_negative_to_SByte
    --- PASS: float64_to_Byte
    --- PASS: float64_to_Float
    --- PASS: float64_to_Double
    --- PASS: string_to_String
    --- PASS: int_to_String
    --- PASS: float64_to_UnknownType
--- PASS: TestConvertToVariantByOpcUaType (0.00s)

PASS
```

### 4.2 Integration Tests

**Driver Logs:**
```
[OPC-UA Driver] Skipping own health message for gateway 4 (status: online)
[MQTT] Published to topic data/.../do_valvola_1: {"tag_id":136,"org_id":1,"v":false,"ts":...,"q":192}
```

**Redis Data:**
```json
{"v":false,"ts":1773267633969,"q":192}
```

**Quality Mapping:**
| Before | After | Status |
|--------|-------|--------|
| 0 | 192 | ✅ GOOD |
| 1 | 0 | ✅ BAD |
| 2 | 0 | ✅ BAD (error case) |

### 4.3 Verification Checklist

| # | Test | Result |
|---|------|--------|
| 1 | Write to Int16 tag succeeds | ✅ PASS |
| 2 | Write to Float tag succeeds | ✅ PASS |
| 3 | Quality shows GOOD (192) in Redis | ✅ PASS |
| 4 | Driver stays connected (no loop) | ✅ PASS |
| 5 | Auto-reload only on explicit command | ✅ PASS |
| 6 | Alarm reset works correctly | ✅ PASS |
| 7 | UI shows correct quality | ✅ PASS |

---

## 5. Files Modified

| File | Change |
|------|--------|
| `internal/opcua/client.go` | Fix `WriteValue()` to use server type |
| `internal/opcua/client.go` | Enhanced `convertToVariantByOpcUaType()` |
| `internal/opcua/client_test.go` | NEW: Unit tests for type conversion |
| `services/driver-opcua/main.go` | Fix quality conversion for MQTT publish |
| `services/driver-opcua/main.go` | Fix auto-reload loop in health handler |

---

## 6. Deployment Notes

### Build Command
```bash
docker build -t industrial-driver-opcua:latest -f services/driver-opcua/Dockerfile .
```

### Deployment Steps
1. Build new image with fixes
2. Stop and remove old driver container: `docker stop driver-opcua-X && docker rm driver-opcua-X`
3. Driver-manager will recreate container with new image
4. Verify logs show: `Skipping own health message` and `q:192`

### Rollback Plan
If issues occur, revert to previous image:
```bash
docker tag industrial-driver-opcua:previous industrial-driver-opcua:latest
docker stop driver-opcua-X && docker rm driver-opcua-X
```

---

## 7. Lessons Learned

1. **Always use server-provided types** for OPC UA writes, not database-stored types
2. **Quality codes** must be converted to industrial standard (192=GOOD, 0=BAD)
3. **Self-referential MQTT messages** can cause infinite loops - always filter by source
4. **Health monitoring** should not trigger business logic reloads

---

## 8. References

- OPC UA Data Types: https://reference.opcfoundation.org/v104/Core/docs/Part3/5.1.2/
- MQTT Quality of Service: https://mosquitto.org/man/mqtt-7.html
- Sparkplug B Specification: https://sparkplug.eclipse.org/
