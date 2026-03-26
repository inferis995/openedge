# PRD: Code Review — Bug Fix & Stabilità

**Data:** 2026-03-26
**Versione:** 1.0
**Autore:** Code Review Analysis
**Priorità:** Alta
**Stima:** < 1 giorno

---

## 📋 Executive Summary

Questo PRD copre i problemi identificati durante una code review approfondita dell'applicazione,
successiva al `prd-security-fixes.md`. Il principio rimane invariato:

> **"Non rompere nulla che funziona"** — ogni fix è backward compatible e minimale.

---

## 🎯 Obiettivi

1. Eliminare accesso silenzioso a ID=0 in `alarms.go`
2. Aggiungere `rows.Err()` check dopo i loop di scan
3. Loggare errori silenziosi nell'audit goroutine
4. Loggare errori `ALTER TABLE` nella funzione di post-restore
5. Aggiungere validazione `db_retention_days > 3650` nell'API settings

---

## 📊 Priorità Fix

| Priorità | ID | Problema | File | Severità |
|----------|-----|----------|------|---------|
| P0 | A1 | strconv.Atoi ignorato → query su ID=0 | `alarms.go` | High |
| P0 | A2 | rows.Err() non verificato dopo loop scan | `alarms.go` | Medium |
| P1 | B1 | Errore goroutine audit log silenzioso | `auth.go` | Medium |
| P2 | C1 | ALTER TABLE errori ignorati post-restore | `backup.go` | Medium |
| P2 | D1 | db_retention_days validazione incompleta | `system.go` | Low |

---

## 🔧 Dettaglio Fix

### P0-A1: strconv.Atoi ignorato in alarms.go

**File:** `internal/handlers/alarms.go`

**Problema:**
Tre funzioni ignorano l'errore di `strconv.Atoi`. Se il parametro URL non è numerico,
`tagID` o `eventID` valgono `0`, e la query viene eseguita su `WHERE id = 0`.

```go
// BEFORE — GetTagAlarmConfig (riga 36)
tagID, _ := strconv.Atoi(c.Param("id"))

// BEFORE — SaveTagAlarmConfig (riga 216)
tagID, _ := strconv.Atoi(c.Param("id"))

// BEFORE — AcknowledgeAlarm (riga 483)
eventID, _ := strconv.Atoi(c.Param("id"))
```

**Soluzione:**
```go
// AFTER — per tutte e tre le funzioni
tagID, err := strconv.Atoi(c.Param("id"))
if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
    return
}
```

**Test di Non-Regressione:**
- [ ] `GET /api/tags/1/alarms` → 200 con lista allarmi
- [ ] `GET /api/tags/abc/alarms` → 400 con "Invalid ID"
- [ ] `PUT /api/tags/1/alarms` → 200
- [ ] `POST /api/alarms/1/ack` → 200
- [ ] `POST /api/alarms/abc/ack` → 400

---

### P0-A2: rows.Err() non verificato + scan error silenzioso

**File:** `internal/handlers/alarms.go`

**Problema:**
Il pattern `if err == nil { append(...) }` ignora silenziosamente gli errori di scan.
Inoltre, `rows.Err()` non viene mai verificato dopo il loop, perdendo eventuali errori
di rete/db intercorsi durante l'iterazione.

```go
// BEFORE
for rows.Next() {
    var a models.AlarmDefinition
    err := rows.Scan(...)
    if err == nil {         // errore ignorato!
        alarms = append(alarms, a)
    }
}
// rows.Err() non verificato
```

**Soluzione:**
```go
// AFTER
for rows.Next() {
    var a models.AlarmDefinition
    if err := rows.Scan(...); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan alarm"})
        return
    }
    alarms = append(alarms, a)
}
if err := rows.Err(); err != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": "Query iteration error"})
    return
}
```

Applicare a: `GetTagAlarmConfig`, `GetActiveAlarms`, `GetAlarmHistory`.

**Test di Non-Regressione:**
- [ ] Lista allarmi attivi funziona
- [ ] Storia allarmi funziona
- [ ] Configurazione allarmi tag funziona

---

### P1-B1: Errore goroutine audit log silenzioso

**File:** `internal/auth/auth.go`

**Problema:**
Gli errori di scrittura nel DB per l'audit log vengono silenziati completamente.
Se la tabella `audit_logs` non esiste o il DB è giù, non si ha traccia del problema.

```go
// BEFORE (riga 117)
go func() {
    if _, err := s.db.Exec(query, ...); err != nil {
        // Log error but don't crash  ← commento ma nessun log!
    }
}()
```

**Soluzione:**
```go
// AFTER
go func() {
    if _, err := s.db.Exec(query, userID, username, action, ipAddress, userAgent, detailsJSON, success); err != nil {
        log.Printf("[AUDIT] Failed to write audit log for user %s action %s: %v", username, action, err)
    }
}()
```

**Test di Non-Regressione:**
- [ ] Login funziona normalmente
- [ ] Logout funziona normalmente
- [ ] Il log mostra entries di audit in caso di errore DB

---

### P2-C1: ALTER TABLE errori ignorati in ensureCriticalConstraints

**File:** `internal/handlers/backup.go`

**Problema:**
`ensureCriticalConstraints()` esegue ALTER TABLE senza mai verificare il risultato.
Se un vincolo FK non viene creato, l'integrità referenziale è silenziosamente assente.

```go
// BEFORE (righe 387-396)
h.db.Exec(`ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey`)
h.db.Exec(`ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey FOREIGN KEY ...`)
// errori completamente ignorati
```

**Soluzione:**
```go
// AFTER
if _, err := h.db.Exec(`ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey`); err != nil {
    log.Printf("[BACKUP] Warning: could not drop constraint tags_gateway_id_fkey: %v", err)
}
if _, err := h.db.Exec(`ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey FOREIGN KEY (gateway_id) REFERENCES gateways(id) ON DELETE CASCADE`); err != nil {
    log.Printf("[BACKUP] Warning: could not add constraint tags_gateway_id_fkey: %v", err)
}
// stesso pattern per gli altri constraint
```

**Test di Non-Regressione:**
- [ ] Restore da backup valido funziona
- [ ] I log mostrano warning solo in caso di errore constraint

---

### P2-D1: db_retention_days validazione incompleta

**File:** `internal/handlers/system.go`

**Problema:**
L'API `PUT /api/system/settings` valida che `db_retention_days >= 0` ma non il limite
superiore. Un valore come `99999` verrebbe accettato e poi corretto silenziosamente
da `InitializeRetentionPolicy`. Meglio rifiutarlo subito in modo esplicito.

```go
// BEFORE (riga 368)
if *req.DBRetentionDays < 0 {
    c.JSON(http.StatusBadRequest, gin.H{"error": "db_retention_days cannot be negative"})
    return
}
```

**Soluzione:**
```go
// AFTER
if *req.DBRetentionDays < 0 || *req.DBRetentionDays > 3650 {
    c.JSON(http.StatusBadRequest, gin.H{"error": "db_retention_days must be between 0 and 3650"})
    return
}
```

**Test di Non-Regressione:**
- [ ] Valore 30 → 200
- [ ] Valore 0 (disabilita) → 200
- [ ] Valore -1 → 400
- [ ] Valore 3651 → 400
- [ ] Valore 3650 → 200

---

## 🧪 Smoke Test Rapido

```bash
# Dopo ogni fix:
go build ./internal/...

# API smoke test:
curl -s http://localhost:8081/api/health
curl -s -X GET http://localhost:8081/api/tags/abc/alarms  # deve dare 400
curl -s -X POST http://localhost:8081/api/alarms/abc/ack  # deve dare 400
```

---

## 📋 Ordine di Implementazione

1. **A1** — strconv in alarms.go (3 funzioni)
2. **A2** — rows.Err() in alarms.go (3 loop)
3. **B1** — audit log goroutine error
4. **C1** — ensureCriticalConstraints logging
5. **D1** — retention days upper bound

---

## 🔄 Rollback

```bash
git revert HEAD  # rollback dell'ultimo commit se problemi
```

---

## ✅ Checklist Finale Pre-Merge

- [ ] Login/logout funziona
- [ ] Lista tag funziona
- [ ] Allarmi attivi funzionano
- [ ] Configurazione allarmi tag funziona
- [ ] Acknowledge allarme funziona
- [ ] Backup/restore funziona
- [ ] PUT /api/system/settings funziona
- [ ] Docker containers healthy
- [ ] Nessun errore nei log al startup

---

**Fine Documento**
