# Sparkplug B - TODO

## Stato Attuale

**NON è vero Sparkplug B** - usa JSON invece di Protobuf.

- ✅ Struttura topic corretta: `spBv1.0/{group}/{msgType}/{node}/{device}`
- ✅ Qualità corretta: 192 = GOOD, 64 = UNCERTAIN, 0 = BAD
- ✅ Gateway deletion manda DDEATH
- ❌ **MANCA: Codifica Protobuf binaria**

## Cosa Fare Domani

### 1. Installare protoc
```bash
choco install protoc
```

### 2. Scaricare sparkplug_b.proto
```bash
# Da Eclipse Tahu
curl -o internal/sparkplug/sparkplug_b.proto https://raw.githubusercontent.com/eclipse/tahu/master/sparkplug_b/sparkplug_b.proto
```

### 3. Generare codice Go
```bash
cd internal/sparkplug
protoc --go_out=. --go_opt=paths=source_relative sparkplug_b.proto
```

### 4. Aggiornare client.go
Sostituire `UseProtobuf = false` con `UseProtobuf = true`

### 5. Rimuovere protobuf.go placeholder
Eliminare il file placeholder e usare il codice generato

## File Modificati Oggi

| File | Modifica |
|------|----------|
| `internal/sparkplug/types.go` | Aggiunti JSON tag |
| `internal/sparkplug/client.go` | UseProtobuf flag |
| `internal/sparkplug/protobuf.go` | Placeholder (da rimuovere) |
| `internal/sparkplug/payload.go` | Conversione qualità |
| `internal/handlers/gateways.go` | DDEATH on delete |

## Riferimenti

- https://github.com/eclipse/tahu - Eclipse Tahu (Sparkplug B reference)
- https://sparkplug.eclipse.org/ - Specifica ufficiale
- https://protoc-installation-guide.readthedocs.io/ - Guida protoc
