# PRD: Security & Code Quality Fixes

**Data:** 2026-03-12
**Versione:** 1.0
**Autore:** Code Review Analysis
**Priorità:** Alta
**Stima:** 1 giorno

---

## 📋 Executive Summary

Questo PRD definisce il piano per risolvere i problemi di sicurezza e qualità identificati durante il code review, con **massima priorità alla stabilità** delle funzionalità esistenti.

### Principio Guida
> **"Non rompere nulla che funziona"** - Ogni fix deve essere backward compatible e testato.

---

## 🎯 Obiettivi

1. Risolvere vulnerabilità di sicurezza critiche e alte
2. Migliorare gestione errori senza cambiare API
3. Aggiungere protezioni senza breaking changes
4. Mantenere 100% compatibilità funzionale

---

## 📊 Priorità Fix

| Priorità | ID | Problema | Rischio | Impatto |
|----------|-----|----------|---------|---------|
| P0 | H1 | Backup restore senza auth | Critico | Sicurezza |
| P0 | H2 | DROP SCHEMA senza verifica | Critico | Data Loss |
| P1 | C1 | SQL INTERVAL construction | Alto | Sicurezza |
| P1 | M4 | Tag write senza org check | Alto | Sicurezza |
| P2 | M1 | Path traversal backup | Medio | Sicurezza |
| P2 | M3 | Rows non chiusi | Medio | Memory Leak |
| P2 | M5 | Errori ignorati | Medio | Stabilità |
| P3 | M6-M8 | Altri problemi medi | Basso | Performance |

---

## 🔧 Dettaglio Fix

### P0-1: Backup Restore Authentication (H1)

**File:** `internal/handlers/backup.go`

**Problema:**
L'endpoint `ImportRestore` non richiede autenticazione, permettendo a chiunque di ripristinare backup.

**Soluzione:**
```go
// BEFORE (riga 112)
func (h *BackupHandler) ImportRestore(c *gin.Context) {

// AFTER
func (h *BackupHandler) ImportRestore(c *gin.Context) {
    // Richiedi autenticazione admin
    userID, exists := c.Get("user_id")
    if !exists {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
        return
    }

    // Verifica ruolo admin
    var isAdmin bool
    err := h.db.QueryRow(
        "SELECT (role = 'admin') FROM users WHERE id = $1",
        userID,
    ).Scan(&isAdmin)

    if err != nil || !isAdmin {
        c.JSON(http.StatusForbidden, gin.H{"error": "Admin access required"})
        return
    }

    // ... resto del codice invariato
```

**Test di Non-Regressione:**
- [ ] Backup restore funziona con credenziali admin
- [ ] Backup restore fallisce senza autenticazione
- [ ] Backup restore fallisce con utente non-admin
- [ ] Il formato del backup non cambia
- [ ] I dati ripristinati sono identici all'originale

**Rollback:**
Ripristinare versione originale del file.

---

### P0-2: Safe Schema Drop (H2)

**File:** `internal/handlers/backup.go`

**Problema:**
Il codice esegue `DROP SCHEMA public CASCADE` prima di verificare che il backup sia valido.

**Soluzione:**
```go
// BEFORE (riga 222)
_, err = tx.Exec(`DROP SCHEMA IF EXISTS public CASCADE`)

// AFTER
// 1. Prima verifica l'integrità del backup
tempSchema := fmt.Sprintf("temp_restore_%d", time.Now().Unix())
_, err = tx.Exec(fmt.Sprintf("CREATE SCHEMA %s", tempSchema))
if err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to create temp schema: %w", err)
}

// 2. Ripristina nello schema temporaneo
// ... restore logic into tempSchema ...

// 3. Verifica integrità dati ripristinati
var tableCount int
err = tx.QueryRow(fmt.Sprintf(
    "SELECT count(*) FROM information_schema.tables WHERE table_schema = '%s'",
    tempSchema,
)).Scan(&tableCount)

if tableCount == 0 {
    tx.Rollback()
    return fmt.Errorf("backup appears to be empty or corrupted")
}

// 4. Solo se tutto OK, scambia gli schema
_, err = tx.Exec(`DROP SCHEMA IF EXISTS public CASCADE`)
if err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to drop old schema: %w", err)
}

_, err = tx.Exec(fmt.Sprintf("ALTER SCHEMA %s RENAME TO public", tempSchema))
```

**Test di Non-Regressione:**
- [ ] Restore da backup valido funziona
- [ ] Restore da backup corrotto NON distrugge dati esistenti
- [ ] Restore da backup vuoto NON distrugge dati esistenti
- [ ] Transazione rollback correttamente su errore
- [ ] Dati esistenti preservati su fallimento

**Rollback:**
Ripristinare versione originale del file.

---

### P1-1: SQL INTERVAL Fix (C1)

**File:** `internal/handlers/history.go`

**Problema:**
Costruzione dinamica della stringa INTERVAL potrebbe essere vulnerabile.

**Soluzione:**
```go
// BEFORE (riga 75-77)
intervalLiteral := fmt.Sprintf("INTERVAL '%d days'", retentionDays)
_, err1 := h.db.Exec(`SELECT add_retention_policy('tag_history', $1, if_not_exists => true)`, intervalLiteral)

// AFTER
// Validazione rigorosa dell'input
if retentionDays < 1 || retentionDays > 3650 { // Max 10 anni
    return c.JSON(http.StatusBadRequest, gin.H{
        "error": "retention_days must be between 1 and 3650",
    })
}

// Usa parametro intero con cast sicuro
_, err1 := h.db.Exec(`
    SELECT add_retention_policy(
        'tag_history',
        make_interval(days => $1::int),
        if_not_exists => true
    )
`, retentionDays)
```

**Test di Non-Regressione:**
- [ ] Creazione retention policy funziona con valori validi
- [ ] Valori negativi vengono rifiutati
- [ ] Valori > 3650 vengono rifiutati
- [ ] Query history esistente funziona invariata
- [ ] TimescaleDB retention funziona correttamente

**Rollback:**
Ripristinare versione originale del file.

---

### P1-2: Tag Write Organization Check (M4)

**File:** `internal/handlers/tags.go`

**Problema:**
L'endpoint Write non verifica che l'utente abbia accesso all'organizzazione del tag.

**Soluzione:**
```go
// BEFORE (riga 691)
func (h *TagHandler) Write(c *gin.Context) {
    idStr := c.Param("id")

// AFTER
func (h *TagHandler) Write(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.Atoi(idStr) // Fix: non ignorare errore
    if err != nil {
        return c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tag ID"})
    }

    // Ottieni organization dell'utente
    userOrgID, _ := c.Get("org_id")
    userRole, _ := c.Get("role")

    // Se non è admin, verifica ownership
    if userRole != "admin" {
        var tagOrgID *int
        err := h.db.QueryRow(
            "SELECT o.id FROM tags t JOIN gateways g ON t.gateway_id = g.id JOIN areas a ON g.area_id = a.id JOIN sites s ON a.site_id = s.id JOIN organizations o ON s.org_id = o.id WHERE t.id = $1",
            id,
        ).Scan(&tagOrgID)

        if err != nil {
            return c.JSON(http.StatusNotFound, gin.H{"error": "Tag not found"})
        }

        if tagOrgID == nil || (userOrgID != nil && *tagOrgID != *userOrgID.(*int)) {
            return c.JSON(http.StatusForbidden, gin.H{"error": "Access denied to this tag"})
        }
    }

    // ... resto del codice invariato
```

**Test di Non-Regressione:**
- [ ] Admin può scrivere su qualsiasi tag
- [ ] Utente può scrivere solo su tag della propria org
- [ ] Scrittura su tag di altra org restituisce 403
- [ ] Formato risposta rimane identico
- [ ] MQTT publish funziona come prima

**Rollback:**
Ripristinare versione originale del file.

---

### P2-1: Path Traversal Fix (M1)

**File:** `internal/handlers/backup.go`

**Problema:**
Download backup non verifica che il file sia dentro la directory di backup.

**Soluzione:**
```go
// BEFORE (riga 536)
func (h *BackupHandler) DownloadBackup(c *gin.Context) {
    filename := c.Param("filename")
    cleanFilename := filepath.Base(filename)

// AFTER
func (h *BackupHandler) DownloadBackup(c *gin.Context) {
    filename := c.Param("filename")
    cleanFilename := filepath.Base(filename)

    // Verifica che il path risolto sia dentro BACKUP_PATH
    basePath, err := filepath.Abs(os.Getenv("BACKUP_PATH"))
    if err != nil {
        return c.JSON(http.StatusInternalServerError, gin.H{"error": "Backup path error"})
    }

    fullPath := filepath.Join(basePath, cleanFilename)
    resolvedPath, err := filepath.Abs(fullPath)
    if err != nil {
        return c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
    }

    // Verifica che il path risolto sia sotto basePath
    if !strings.HasPrefix(resolvedPath, basePath + string(filepath.Separator)) {
        log.Printf("[SECURITY] Path traversal blocked: %s resolves to %s", filename, resolvedPath)
        return c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
    }

    // ... resto del codice
```

**Test di Non-Regressione:**
- [ ] Download file valido funziona
- [ ] Path traversal `../etc/passwd` bloccato
- [ ] Path traversal assoluto `/etc/passwd` bloccato
- [ ] File fuori dalla directory bloccato
- [ ] Formato risposta invariato

**Rollback:**
Ripristinare versione originale del file.

---

### P2-2: Close Rows on Error (M3)

**File:** `internal/handlers/tags.go`

**Problema:**
Rows non vengono chiusi in caso di errore durante Scan.

**Soluzione:**
```go
// BEFORE (riga 817-819)
for rows.Next() {
    var t models.Tag
    if err := rows.Scan(...); err != nil {
        return c.JSON(http.StatusInternalServerError, ...) // rows non chiuso!
    }

// AFTER
defer rows.Close() // Sposta all'inizio del loop

for rows.Next() {
    var t models.Tag
    if err := rows.Scan(...); err != nil {
        // defer chiuderà rows automaticamente
        return c.JSON(http.StatusInternalServerError, ...)
    }
```

**Test di Non-Regressione:**
- [ ] Lista tags funziona correttamente
- [ ] Errori di scan non causano memory leak
- [ ] Connessioni DB rilasciate correttamente
- [ ] Performance invariata

**Rollback:**
Ripristinare versione originale del file.

---

### P2-3: Error Handling (M5)

**File:** `internal/handlers/gateways.go`

**Problema:**
Errori MQTT vengono ignorati silenziosamente.

**Soluzione:**
```go
// BEFORE (riga 706-710)
if h.mqttClient != nil {
    topic := fmt.Sprintf("sys/command/reload/%d", gateway.ID)
    h.mqttClient.Publish(topic, "reload") // errore ignorato
}

// AFTER
if h.mqttClient != nil {
    topic := fmt.Sprintf("sys/command/reload/%d", gateway.ID)
    if err := h.mqttClient.Publish(topic, "reload"); err != nil {
        log.Printf("[WARN] Failed to send reload command to gateway %d: %v", gateway.ID, err)
        // Non fallire la richiesta - il driver farà reload periodico comunque
    }
}
```

**Test di Non-Regressione:**
- [ ] Creazione gateway funziona
- [ ] Aggiornamento gateway funziona
- [ ] Log mostra warning se MQTT fallisce
- [ ] Operazione non fallisce per errori MQTT

**Rollback:**
Ripristinare versione originale del file.

---

## 🧪 Piano di Test

### Pre-Deployment Checklist

Per OGNI fix, eseguire:

#### 1. Unit Tests (se presenti)
```bash
go test ./internal/handlers/... -v
go test ./internal/alarms/... -v
go test ./internal/mqtt/... -v
```

#### 2. Integration Tests Manuali

| Test | Steps | Expected | Status |
|------|-------|----------|--------|
| Login | POST /api/auth/login | 200 + JWT | ☐ |
| List Tags | GET /api/tags | 200 + array | ☐ |
| Write Tag | POST /api/tags/1/write | 200 | ☐ |
| History | GET /api/history?tag_id=1 | 200 + data | ☐ |
| Backup | POST /api/backup | 200 + file | ☐ |
| Restore | POST /api/backup/restore | 200 | ☐ |
| Alarms | GET /api/alarms | 200 + array | ☐ |
| Gateways | GET /api/gateways | 200 + array | ☐ |

#### 3. Smoke Test Completo

```bash
# 1. Verifica tutti i servizi running
docker-compose ps

# 2. Verifica API health
curl http://localhost:8081/api/health

# 3. Verifica WebSocket
wscat -c ws://localhost:8081/ws/realtime

# 4. Verifica MQTT
mosquitto_sub -h localhost -p 18830 -t "data/#" -v

# 5. Verifica Redis
redis-cli -h localhost -p 6379 ping

# 6. Verifica DB
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c "SELECT 1"
```

---

## 📋 Ordine di Implementazione

### Fase 1: Security Critical (Mattina)
1. **P0-1**: Backup Restore Auth
2. **P0-2**: Safe Schema Drop
3. **Test completi dopo ogni fix**

### Fase 2: Security High (Pranzo)
4. **P1-1**: SQL INTERVAL Fix
5. **P1-2**: Tag Write Org Check
6. **Test completi dopo ogni fix**

### Fase 3: Stability (Pomeriggio)
7. **P2-1**: Path Traversal
8. **P2-2**: Close Rows
9. **P2-3**: Error Handling
10. **Test finali completi**

---

## 🔄 Strategia di Rollback

### Git Branch Strategy

```bash
# Prima di iniziare
git checkout -b fix/security-and-quality

# Dopo ogni fix, commit separato
git commit -m "fix(backup): add authentication to restore endpoint"
git commit -m "fix(backup): verify backup before dropping schema"
# etc...

# Se qualcosa non funziona
git revert HEAD  # Rollback ultimo commit

# Se tutto OK
git checkout master
git merge fix/security-and-quality
```

### Per Fix Singolo

Se un fix causa problemi:

1. **Identificare** quale fix ha causato il problema
2. **Revertare** solo quel commit
3. **Rilasciare** gli altri fix
4. **Rianalizzare** il fix problematico per un altro momento

---

## ✅ Checklist Finale

### Prima del Merge

- [ ] Tutti i test di regressione passano
- [ ] Login funziona
- [ ] CRUD tags funziona
- [ ] History funziona
- [ ] Alarms funziona
- [ ] Backup/Restore funziona (con auth)
- [ ] Gateways funziona
- [ ] MQTT publish/subcribe funziona
- [ ] WebSocket realtime funziona
- [ ] Multi-tenant isolation verificata
- [ ] Docker containers healthy
- [ ] Logs senza errori critici

### Dopo il Merge

- [ ] Push su GitHub
- [ ] Ricostruire immagini Docker
- [ ] Test su ambiente clean
- [ ] Documentare cambiamenti in CHANGELOG

---

## 📝 Note

### Cosa NON fare (per ora)

- Non cambiare firme di funzioni pubbliche
- Non cambiare formati di risposta API
- Non aggiungere nuove dipendenze
- Non modificare schema database
- Non ottimizzare query esistenti (rischiare regressione)

### Cosa fare DOPO questo PRD

1. Aggiungere unit tests automatici
2. Configurare connection pool
3. Aggiungere rate limiting
4. Implementare audit logging
5. Aggiungere metrics/observability

---

## 📞 Contatti

In caso di problemi durante l'implementazione:
- Fermarsi immediatamente
- Documentare l'errore
- Fare rollback dell'ultimo commit
- Rianalizzare il problema

---

**Fine Documento**
