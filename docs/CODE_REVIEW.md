# Code Review — OpenEdge Industrial Middleware

**Data:** 2026-03-08
**Branch:** `claude/code-review-documentation-0PrTG`
**Revisore:** Claude Code (Anthropic)
**Scope:** Revisione completa del codebase Go (sicurezza, qualità, architettura)

---

## Indice

1. [Panoramica Architetturale](#1-panoramica-architetturale)
2. [Problemi di Sicurezza — Critici](#2-problemi-di-sicurezza--critici)
3. [Problemi di Sicurezza — Medi](#3-problemi-di-sicurezza--medi)
4. [Qualità del Codice](#4-qualità-del-codice)
5. [Problemi di Design e Architettura](#5-problemi-di-design-e-architettura)
6. [Pratiche Positive](#6-pratiche-positive)
7. [Riepilogo e Priorità](#7-riepilogo-e-priorità)

---

## 1. Panoramica Architetturale

OpenEdge è un middleware Go per l'Industrial IoT che funge da gateway SCADA/IIoT. Il sistema espone una REST API (Gin) che permette di configurare gateway industriali, raccogliere dati da tag OPC-UA, Modbus TCP, Siemens S7 e MQTT, e pubblicarli su broker MQTT (incluso Sparkplug B).

**Stack tecnologico:**
- **Framework HTTP:** Gin
- **Database:** PostgreSQL + TimescaleDB (per serie temporali)
- **Cache/Realtime:** Redis
- **Messaggistica:** MQTT (Eclipse Paho), Sparkplug B
- **Autenticazione:** JWT (HS256) + bcrypt
- **Protocolli industriali:** Modbus TCP, Siemens S7, OPC-UA

**Gerarchia dei dati (multi-tenant):**
```
Organization → Site → Area → Gateway → Tag
```

---

## 2. Problemi di Sicurezza — Critici

### 2.1 Chiave JWT hardcoded con fallback insicuro

**File:** `internal/auth/auth.go:15`

```go
var SecretKey = []byte("industrial-edge-secret-key-change-me-in-production")
```

**Problema:** La chiave segreta JWT è hardcoded come valore di default. Se la variabile d'ambiente non viene configurata, il sistema parte comunque con una chiave nota a chiunque legga il codice sorgente. Qualsiasi attaccante potrebbe generare token JWT validi.

**Raccomandazione:** Caricare la chiave esclusivamente da variabile d'ambiente, fallire all'avvio se non è impostata o è troppo corta:

```go
func LoadSecretKey() []byte {
    key := os.Getenv("JWT_SECRET_KEY")
    if len(key) < 32 {
        log.Fatal("JWT_SECRET_KEY must be set and at least 32 characters")
    }
    return []byte(key)
}
```

---

### 2.2 Nessuna verifica che il token JWT appartenga all'organizzazione richiesta

**File:** `internal/middleware/organization.go`, `internal/middleware/auth.go`

**Problema:** Il middleware `OrganizationContext` accetta l'header `X-Organization-ID` e lo mette nel contesto, ma non verifica mai che l'utente autenticato (dal JWT) appartenga effettivamente a quell'organizzazione. Un utente con un JWT valido per l'org 1 può passare `X-Organization-ID: 2` e accedere ai dati dell'org 2 (almeno a livello di contesto — i controlli per-handler sono l'unica protezione).

Sebbene i singoli handler verifichino la proprietà delle risorse, questa verifica è per gateway/tag/area, ma non viene mai controllato che `user.org_id == requested_org_id` a livello di middleware.

**Raccomandazione:** Aggiungere nel middleware `OrganizationContext` una query che verifichi che l'utente (estratto dal JWT via `RequireAuth`) appartenga all'organizzazione richiesta, oppure aggiungere il campo `org_id` al JWT e verificarlo direttamente.

---

### 2.3 Encryption fallback silenzioso su plaintext

**File:** `internal/crypto/cipher.go:21-22`

```go
if len(key) != 32 {
    return plaintext, nil  // fallback silenzioso
}
```

**Problema:** Se `ENCRYPTION_KEY` non è configurata o ha lunghezza errata, la funzione `Encrypt` restituisce il testo in chiaro senza alcun avviso. Il chiamante non sa che i dati non sono stati cifrati. Credenziali di connessione (gateway password, chiavi MQTT) finiscono in database in chiaro.

**Raccomandazione:** In assenza di chiave valida, restituire un errore esplicito anziché il plaintext. In alternativa, loggare un warning critico almeno all'avvio dell'applicazione.

---

### 2.4 Panic potenziale nel middleware `RequireRole`

**File:** `internal/middleware/auth.go:63`

```go
userRole := models.UserRole(mapClaims["role"].(string))
```

**Problema:** Type assertion non sicura. Se il claim `role` è assente dal token JWT o non è una stringa, il programma va in **panic** (runtime panic per type assertion fallita). Questo può essere sfruttato da un attaccante che costruisce un token con claim malformati.

**Raccomandazione:**

```go
roleVal, ok := mapClaims["role"].(string)
if !ok {
    c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Forbidden: missing role claim"})
    return
}
userRole := models.UserRole(roleVal)
```

---

### 2.5 Zip Slip e assenza di limite sulla decompressione

**File:** `internal/handlers/backup.go:151-184`

**Problema 1 (Zip Slip):** Durante il restore, i file vengono estratti con:
```go
fpath := filepath.Join(tempDir, f.Name)
```
Tuttavia non c'è un controllo esplicito che `fpath` rimanga dentro `tempDir`. Un archivio malevolo con entry come `../../etc/passwd` potrebbe scrivere fuori dalla directory temporanea. `filepath.Join` normalizza i path, ma non protegge contro i path `../` che rimangono dentro il volume montato.

**Problema 2 (Zip Bomb):** `io.Copy(outFile, rc)` senza limite di dimensione. Un archivio zip malevolo con dati fortemente compressi (zip bomb) può esaurire la memoria del server.

**Raccomandazione:**
```go
// Zip Slip check
if !strings.HasPrefix(filepath.Clean(fpath)+string(os.PathSeparator), filepath.Clean(tempDir)+string(os.PathSeparator)) {
    return fmt.Errorf("zip slip detected: %s", f.Name)
}

// Size limit
const maxExtractSize = 600 * 1024 * 1024 // 600 MB
_, err = io.CopyN(outFile, rc, maxExtractSize)
```

---

## 3. Problemi di Sicurezza — Medi

### 3.1 Nessun rate limiting sull'endpoint di login

**File:** `internal/handlers/auth.go` (endpoint POST `/api/auth/login`)

Nessun rate limiting né meccanismo anti-brute-force sull'endpoint di autenticazione. Un attaccante può tentare migliaia di password al secondo.

**Raccomandazione:** Implementare rate limiting (es. con `golang.org/x/time/rate` o middleware Gin) sull'endpoint di login. Considerare un lockout temporaneo dopo N tentativi falliti.

---

### 3.2 Ownership check assente in `GetCurrentValue` e `Write`

**File:** `internal/handlers/tags.go:597` (`GetCurrentValue`), `internal/handlers/tags.go:670` (`Write`)

`GetCurrentValue` verifica solo che il tag esista nel DB, non che appartenga all'organizzazione dell'utente. `Write` idem. Un utente autenticato può leggere il valore attuale o inviare comandi di scrittura a qualsiasi tag, indipendentemente dall'organizzazione.

**Raccomandazione:** Aggiungere lo stesso ownership check presente in `Get` anche in `GetCurrentValue` e `Write`.

---

### 3.3 SSL disabilitato per PostgreSQL

**File:** `internal/db/db.go:15`

```go
"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
```

SSL è hardcoded come disabilitato. In un deployment di produzione, le credenziali e i dati vengono trasmessi in chiaro tra l'applicazione e il database.

**Raccomandazione:** Rendere `sslmode` configurabile via variabile d'ambiente, con default `require` in produzione.

---

### 3.4 Errori di audit log inghiottiti silenziosamente

**File:** `internal/auth/auth.go:112`

```go
go func() {
    if _, err := s.db.Exec(query, ...); err != nil {
        // Log error but don't crash
    }
}()
```

Gli errori di scrittura all'audit log vengono ignorati completamente (nemmeno loggati). Questo significa che tentativi di login falliti potrebbero non essere registrati senza che nessuno se ne accorga.

**Raccomandazione:** Loggare almeno l'errore: `log.Printf("[AUDIT] Failed to write audit log: %v", err)`.

---

## 4. Qualità del Codice

### 4.1 Messaggio di errore di validazione `data_type` incompleto

**File:** `internal/handlers/tags.go:122` e `499`

```go
c.JSON(http.StatusBadRequest, gin.H{"error": "data_type must be 'INT', 'REAL', 'BOOL', or 'DINT'"})
```

La funzione `validateDataType` (riga 60) accetta anche `STRING`, ma il messaggio di errore non lo menziona. L'utente dell'API non sa che `STRING` è un valore valido.

---

### 4.2 Doppio round-trip nel `Get` del tag per ownership check

**File:** `internal/handlers/tags.go:288-335`

Il metodo `Get` prima esegue una query per ottenere il tag, poi una seconda query separata per verificare l'ownership. Questo crea due round-trip al DB e un potenziale TOCTOU window. La stessa query può fare entrambe le cose:

```sql
SELECT t.id, t.gateway_id, t.code, t.alias, t.data_type, t.historize,
       t.historize_deadband, t.sort_order, t.created_at, s.org_id
FROM tags t
JOIN gateways g ON t.gateway_id = g.id
JOIN areas a ON g.area_id = a.id
JOIN sites s ON a.site_id = s.id
WHERE t.id = $1
```

Lo stesso pattern si ripete in `gateways.go` nel metodo `Get`.

---

### 4.3 Inconsistenza nel formato del topic MQTT per l'health cleanup

**File:** `internal/handlers/gateways.go:554`

```go
h.mqttClient.PublishWithQoS(fmt.Sprintf("sys/health/%s", id), "", 1, true)
```

Viene usato `%s` con `id` (una stringa da `c.Param("id")`), producendo ad esempio `sys/health/42`. Tuttavia negli altri punti del codice i topic health usano il gateway ID intero: `fmt.Sprintf("sys/health/%d", gateway.ID)`. Se il driver pubblica su `sys/health/42` e qui si pulisce `sys/health/42` coincide, ma l'inconsistenza rende il codice fragile.

---

### 4.4 Commento step mancante nella Delete del gateway

**File:** `internal/handlers/gateways.go:531-548`

```go
// 1. Delete Tags
_, err = tx.Exec("DELETE FROM tags WHERE gateway_id = $1", id)
// 3. Delete Gateway
_, err = tx.Exec("DELETE FROM gateways WHERE id = $1", id)
```

Lo "Step 2" è mancante nel commento (probabilmente era per la history data o per gli allarmi, poi rimosso). I commenti numerati senza continuità creano confusione.

---

### 4.5 Errore ignorato in `strconv.Atoi` nel `Delete` del tag

**File:** `internal/handlers/tags.go:359`

```go
id, _ := strconv.Atoi(idStr)
```

L'errore di conversione viene ignorato. Se `idStr` non è un intero valido, `id` sarà `0`, e la query cercherà un tag con `id=0` (che non esiste), restituendo 404. Questo è un comportamento accettabile ma inconsistente con altri handler che restituiscono 400.

---

### 4.6 DB operations con mutex lock nell'alarm manager

**File:** `internal/alarms/manager.go:200-211`

```go
func (m *Manager) tickDelays() {
    m.mu.Lock()
    defer m.mu.Unlock() // Lock tenuto per tutta la durata

    // ...
    m.db.QueryRow("SELECT alias FROM tags WHERE id = $1", pt.tagID).Scan(&alias)
    // ...
}
```

Le query al database vengono eseguite mentre il mutex `m.mu` è locked. Questo blocca tutte le valutazioni di allarme (chiamate da `EvaluateTag`) per tutta la durata delle query DB. Con alta frequenza di polling o DB lento, può causare starvation dei goroutine.

**Raccomandazione:** Raccogliere gli `alias` da DB prima di acquisire il lock, o rilasciare il lock intorno alle operazioni DB e ri-acquisirlo.

---

### 4.7 Duplicazione di codice nel backup schedulato

**File:** `internal/handlers/backup.go:613-645` vs `backup.go:59-76`

Il blocco di codice per l'invocazione di `pg_dump` è quasi identico in `ExportBackup` e `RunScheduledBackup`. Una refactoring verso una funzione `runPgDump(tempDir string) (string, error)` eliminerebbe la duplicazione.

---

## 5. Problemi di Design e Architettura

### 5.1 Gestione dello schema frammentata in tre posti

Lo schema del database viene creato/gestito in:

1. **`migrations/*.sql`** — file SQL con numerazione progressiva
2. **`internal/db/db.go:runAutoMigrations`** — crea `audit_logs` inline in Go
3. **`internal/handlers/backup.go:EnsureTimescaleDBStructures`** — crea `tag_data`, `system_events`, `backup_settings`, `alarm_definitions`, `alarm_events` in Go

Questa frammentazione rende difficile capire lo schema completo, ricreare il DB da zero, e garantire che le migrazioni siano idempotenti e ordinate. La funzione `EnsureTimescaleDBStructures` viene chiamata anche durante il restore, mescolando logica di restore con logica di schema.

**Raccomandazione:** Centralizzare tutta la gestione dello schema nei file `migrations/`, usare un migration runner standard (es. `golang-migrate/migrate`). Le funzioni Go di creazione tabelle dovrebbero essere sostituite da file `.sql` corrispondenti.

---

### 5.2 Nessuna validazione della lunghezza degli input stringa

Campi come `name`, `code`, `alias` non hanno validazione della lunghezza prima di arrivare al database. La lunghezza massima è definita solo dai vincoli del DB (`VARCHAR(255)`, etc.), ma errori del DB vengono esposti all'utente come "Failed to create tag" senza dettagli chiari.

**Raccomandazione:** Aggiungere `binding:"max=255"` o validatori custom nei struct di request.

---

### 5.3 `go.mod` ha un commento dentro il blocco `require`

**File:** `go.mod:21-23`

```
require (
    // ...
// Sparkplug B support: For full Protobuf support, add:
// github.com/eclipse/sparkplugb v0.5.0
// Note: Current implementation uses JSON encoding for Sparkplug B payloads
)
```

I commenti dentro il blocco `require` sono sintassi Go modules valida, ma inusuale. Meglio spostarli fuori dal blocco o in un file `README` separato.

---

### 5.4 Nessun contesto di cancellazione nelle query DB negli handler HTTP

La maggior parte delle query DB usa `h.db.Query(...)` o `h.db.QueryRow(...)` senza passare il `context.Context` della richiesta HTTP. Se il client abbandona la connessione, le query DB continuano a girare inutilmente.

**Raccomandazione:** Usare `h.db.QueryContext(c.Request.Context(), ...)` e `h.db.QueryRowContext(c.Request.Context(), ...)` negli handler.

---

## 6. Pratiche Positive

Le seguenti pratiche meritano riconoscimento come esempi di buona implementazione:

| Pratica | File/Posizione |
|---------|---------------|
| **Bcrypt** per l'hashing delle password | `internal/auth/auth.go:59` |
| **Query parametrizzate** ovunque — nessun rischio SQL injection | Tutti gli handler |
| **Connection pool** configurato correttamente (MaxOpen, MaxIdle, TTL) | `internal/db/db.go:30-36` |
| **Audit logging** asincrono per non bloccare le request | `internal/auth/auth.go:111` |
| **Path traversal protection** nel backup download/delete | `internal/handlers/backup.go:536-538` |
| **Credential masking** nei log del backup | `internal/handlers/backup.go:714` |
| **Pulizia dei retained MQTT** alla cancellazione di gateway/tag | `internal/handlers/gateways.go:439-521` |
| **Sparkplug B DDEATH** inviato correttamente alla cancellazione | `internal/handlers/gateways.go:492-521` |
| **Deadband** implementato nella logica di clearing degli allarmi | `internal/alarms/manager.go:392-405` |
| **Isolamento multi-tenant** applicato consistentemente nella maggior parte dei CRUD | Tutti gli handler |
| **Interfacce** per `MQTTClient` e `RedisClient` — favorisce il testing | `internal/handlers/tags.go:19-22` |
| **AES-256-GCM** con nonce random per la cifratura delle credenziali | `internal/crypto/cipher.go` |
| **JWT con scadenza 24h** — non token a vita infinita | `internal/auth/auth.go:89` |

---

## 7. Riepilogo e Priorità

### Priorità Alta (da risolvere prima del deploy in produzione)

| # | Problema | File | Impatto |
|---|----------|------|---------|
| P1 | Chiave JWT hardcoded | `auth/auth.go:15` | Compromissione completa dell'autenticazione |
| P2 | Nessuna verifica org nel JWT | `middleware/auth.go`, `middleware/organization.go` | Accesso cross-tenant |
| P3 | Encryption fallback silenzioso | `crypto/cipher.go:21` | Credenziali in chiaro nel DB |
| P4 | Panic in `RequireRole` | `middleware/auth.go:63` | Crash del server su token malformati |
| P5 | Zip Slip + Zip Bomb nel restore | `handlers/backup.go:151-184` | RCE / DoS |
| P6 | Nessun ownership check in `GetCurrentValue` / `Write` | `handlers/tags.go:597,670` | Accesso non autorizzato ai tag |

### Priorità Media (da risolvere prima del v1.1)

| # | Problema | File |
|---|----------|------|
| M1 | Nessun rate limiting sul login | `handlers/auth.go` |
| M2 | SSL disabilitato per PostgreSQL | `db/db.go:15` |
| M3 | Errori audit log inghiottiti | `auth/auth.go:112` |
| M4 | DB queries con mutex lock negli allarmi | `alarms/manager.go:200` |
| M5 | Nessun `context.Context` nelle query DB | Tutti gli handler |

### Priorità Bassa (miglioramenti futuri)

| # | Problema | File |
|---|----------|------|
| L1 | Schema DB frammentato in 3 posti | `db/db.go`, `backup.go`, `migrations/` |
| L2 | Messaggio errore `data_type` incompleto | `handlers/tags.go:122` |
| L3 | Duplicazione codice `pg_dump` | `handlers/backup.go` |
| L4 | Commento step mancante (Step 2) nel Delete gateway | `handlers/gateways.go:531` |
| L5 | `go.mod` commento dentro blocco `require` | `go.mod:21` |

---

*Documento generato durante la code review del branch `claude/code-review-documentation-0PrTG`.*
