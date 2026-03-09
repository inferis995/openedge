# Alarm Architecture - OpenEdge Industrial Middleware

## Overview

The alarm system provides real-time monitoring and alerting for industrial tags across all PLC drivers (OPC UA, Modbus, S7).

## Architecture

### Components

1. **Alarm Manager** (`internal/alarms/manager.go`)
   - Core alarm evaluation engine
   - Manages active alarm tracks with delay support
   - Handles database persistence
   - Triggers callbacks for MQTT publishing

2. **Driver Integration** (driver-opcua, driver-modbus, driver-s7)
   - Evaluates alarms during tag polling
   - Publishes alarm state via MQTT
   - Supports Legacy, Sparkplug B, and Dual publishing modes

3. **Database** (`alarm_events` table)
   - Persists alarm history
   - Tracks ACTIVE, ACKNOWLEDGED, CLEARED states
   - Prevents duplicate ACTIVE events

## Data Flow

```
┌─────────────┐
│   PLC       │ Tag Value
└──────┬──────┘
       │
       ▼
┌───────────────────────────────────────┐
│  Driver (OPC UA/Modbus/S7)           │
│  - Read tag value                     │
│  - Convert quality (0→192)           │
│  - Call alarmManager.EvaluateTag()   │
└──────────────┬────────────────────────┘
               │
               ▼
┌───────────────────────────────────────┐
│  Alarm Manager                       │
│  - Check alarm conditions            │
│  - Start/stop tracking               │
│  - Handle delays (ticker)            │
│  - Fire alarm when triggered         │
└──────┬──────────────────┬─────────────┘
       │                  │
       ▼                  ▼
┌─────────────┐    ┌──────────────────┐
│  Database   │    │  MQTT Publish    │
│  INSERT/    │    │  (via callback)  │
│  UPDATE     │    │  - publishDual() │
└─────────────┘    │  - Legacy topic  │
                   │  - Sparkplug B   │
                   └────────┬─────────┘
                            │
                            ▼
                   ┌─────────────────────┐
                   │  UI / Subscribers  │
                   │  - Receives alarm  │
                   │  - Displays state  │
                   └─────────────────────┘
```

## Key Implementation Details

### 1. Single MQTT Publish (No Duplicates)

**✅ CORRECT:**
```go
// In fireAlarmEvent():
// - Save to database
// - Trigger callback (NO direct MQTT publish)

// In Driver OnAlarmEvent callback:
// - publishDual() publishes ONCE to:
//   • Legacy: data/.../..._Alarm
//   • Sparkplug B: spBv1.0/...
```

**❌ WRONG (causes duplicates):**
```go
// Publishing in BOTH places = 2 messages
```

### 2. Alarm Evaluation Flow

```go
EvaluateTag(value, quality):
  if quality != 192: return  // Skip bad quality

  floatVal = toFloat(value)  // Convert bool/int/float

  for each definition:
    isViolating = isConditionViolated(def, floatVal)
    track = activeTracks[defID]

    if isViolating:
      if !track:
        // Start tracking
        track = createTrack()
        if delay == 0:
          fireAlarmEvent("ACTIVE")  // Immediate
          track.Triggered = true
      // else: already tracking, wait for ticker
    else:
      if track && isCleared(def, floatVal):
        if track.Triggered:
          fireAlarmEvent("CLEARED")
        delete(track)
```

### 3. Delay Handling (Ticker)

```go
tickDelays():  // Runs every 1 second
  for each track:
    if !track.Triggered:
      if elapsed >= track.Definition.DelaySeconds:
        fireAlarmEvent("ACTIVE")  // ✅ FIXED: Was skipping due to bug
        track.Triggered = true
```

**CRITICAL FIX:** The check at line 207-210 was causing alarms to never fire:
```go
// ❌ WRONG (was causing bug):
if track.Triggered {
    continue  // Skip because we just set it!
}

// ✅ CORRECT (after fix):
// Remove the check - we already verified it needs triggering
```

### 4. MQTT Publishing Modes

All drivers support 3 publish modes via `settingsManager.Get().PublishMode`:

| Mode | Legacy MQTT | Sparkplug B | Description |
|------|-------------|-------------|-------------|
| `PublishModeLegacyOnly` | ✅ | ❌ | Only legacy JSON format |
| `PublishModeSparkplugOnly` | ❌ | ✅ | Only Sparkplug B Protobuf |
| `PublishModeDual` (default) | ✅ | ✅ | Both formats simultaneously |

### 5. Alarm States

| State | Description | Database Record |
|-------|-------------|-----------------|
| `ACTIVE` | Alarm condition met | `status='ACTIVE'`, `clear_time=NULL` |
| `ACKNOWLEDGED` | User acknowledged | `status='ACKNOWLEDGED'` |
| `CLEARED` | Condition no longer met | `status='CLEARED'`, `clear_time=NOW` |

### 6. Quality Conversion

OPC UA quality is converted during evaluation:
- OPC UA: `0 = GOOD`, `1+ = BAD`
- Industrial Edge: `192 = GOOD`, `0 = BAD`

```go
alarmQuality := 192
if quality != 0 {
    alarmQuality = 0  // BAD
}
```

## Configuration

### Alarm Definition (Database)

```sql
CREATE TABLE alarm_definitions (
    id SERIAL PRIMARY KEY,
    tag_id INT REFERENCES tags(id),
    alarm_type VARCHAR,  -- bool_true, bool_false, high, low, etc.
    threshold DECIMAL,
    deadband DECIMAL DEFAULT 0,
    delay_seconds INT DEFAULT 0,
    severity VARCHAR,  -- info, warning, error
    message TEXT,
    enabled BOOLEAN DEFAULT true
);
```

### Settings (Database)

```sql
CREATE TABLE settings (
    key VARCHAR PRIMARY KEY,
    value JSONB
);

-- Example:
-- key='alarm_publish_mode', value='"dual"'
```

## Driver Integration Checklist

Each driver MUST:

- ✅ Initialize: `alarms.NewManager(database, mqttClient, gatewayID)`
- ✅ Set callback: `alarmManager.OnAlarmEvent = func(...) { publishDual(...) }`
- ✅ Start ticker: `go alarmManager.StartTicker(context.Background())`
- ✅ Load definitions: `alarmManager.LoadDefinitions()`
- ✅ Evaluate: `alarmManager.EvaluateTag(tagID, alias, value, quality)`
- ✅ Use `publishDual()` for MQTT (not direct publish)

## Testing Checklist

- [ ] Alarm triggers when condition is met (after delay)
- [ ] Alarm clears when condition is no longer met
- [ ] Only ONE MQTT message per alarm state change
- [ ] Database record created for ACTIVE
- [ ] Database record updated to CLEARED
- [ ] Works in all 3 publish modes (Legacy, Sparkplug, Dual)
- [ ] No duplicate alarms in UI
- [ ] Acknowledgment flow works
- [ ] Quality conversion correct (GOOD=192, BAD=0)

## Troubleshooting

### Alarm not firing

1. Check logs for `[ALARM-MANAGER-DEBUG]` output
2. Verify alarm definition exists and is enabled
3. Check quality is 192 (GOOD)
4. Verify delay has elapsed
5. Check for duplicate prevention (existing ACTIVE in DB)

### Duplicate alarms in UI

1. Check if `fireAlarmEvent()` publishes directly to MQTT
2. Ensure only callback publishes MQTT
3. Verify UI subscribes to only ONE topic (not both)

### Alarm not clearing

1. Verify `isCleared()` logic for alarm type
2. Check deadband configuration
3. Verify `isConditionViolated()` returns false
4. Check track is removed from `activeTracks`

## Files

- `internal/alarms/manager.go` - Core alarm logic
- `services/driver-opcua/main.go` - OPC UA integration
- `services/driver-modbus/main.go` - Modbus integration
- `services/driver-s7/main.go` - S7 integration
- `internal/sparkplug/client.go` - DualPublisher implementation

## Version History

- **2026-03-09**: Fixed `tickDelays()` bug causing alarms not to fire after delay
- **2026-03-09**: Removed duplicate MQTT publish (single publish via callback)
- **2026-03-09**: Added comprehensive debug logging
