# Tag Scaling (4-20 mA → Valore Ingegneristico) — Piano Futuro

Aggiungere la possibilità di scalare i valori raw dei tag (es. segnale 4-20 mA) in valori ingegneristici (es. 0-100 m³/h) per **tutti i driver**, con un'interfaccia user-friendly nella pagina Tag.

## Panoramica

```
Raw PLC Value (0-27648) → Formula Lineare → Scaled Value (0.0 - 100.0 m³/h) → MQTT publish
```

Formula: `scaled = ((raw - rawMin) / (rawMax - rawMin)) * (engMax - engMin) + engMin`

## Modifiche necessarie

### 1. Database — 5 nuove colonne sulla tabella `tags`
```sql
ALTER TABLE tags ADD COLUMN IF NOT EXISTS scale_enabled BOOLEAN DEFAULT false;
ALTER TABLE tags ADD COLUMN IF NOT EXISTS scale_raw_min DOUBLE PRECISION DEFAULT 0;
ALTER TABLE tags ADD COLUMN IF NOT EXISTS scale_raw_max DOUBLE PRECISION DEFAULT 27648;
ALTER TABLE tags ADD COLUMN IF NOT EXISTS scale_eng_min DOUBLE PRECISION DEFAULT 0;
ALTER TABLE tags ADD COLUMN IF NOT EXISTS scale_eng_max DOUBLE PRECISION DEFAULT 100;
```

### 2. Backend Model — `internal/models/tag.go`
Aggiungere: `ScaleEnabled`, `ScaleRawMin`, `ScaleRawMax`, `ScaleEngMin`, `ScaleEngMax`

### 3. Core API — `internal/handlers/tags.go`
- Aggiornare `CreateTagRequest`, `UpdateTagRequest`
- Aggiornare `Create()`, `Update()`, `List()`, `Get()` con le nuove colonne SQL

### 4. Import/Export — `internal/handlers/tags_import_export.go`
Supporto colonne scaling nel CSV

### 5. Frontend Types — `services/web-ui/src/types/index.ts`
Aggiornare `Tag` e `CreateTagDto` interfaces

### 6. UI — `services/web-ui/src/pages/TagsPage.tsx`
Sezione "Scaling" nel form tag con switch ON/OFF, 4 input (rawMin, rawMax, engMin, engMax), e preview live.

### 7. Tutti i 5 Driver
Aggiungere `applyScaling()` in: `driver-modbus`, `driver-s7`, `driver-opcua`, `driver-mqtt`, `driver-redis`

```go
func applyScaling(tag models.Tag, rawValue interface{}) interface{} {
    if !tag.ScaleEnabled { return rawValue }
    raw, ok := toFloat64(rawValue)
    if !ok { return rawValue }
    if tag.ScaleRawMax == tag.ScaleRawMin { return rawValue }
    scaled := ((raw - tag.ScaleRawMin) / (tag.ScaleRawMax - tag.ScaleRawMin)) * 
              (tag.ScaleEngMax - tag.ScaleEngMin) + tag.ScaleEngMin
    return scaled
}
```
