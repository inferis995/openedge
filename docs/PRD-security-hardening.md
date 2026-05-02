# PRD — Security Hardening for Production
**Branch**: `claude/security-hardening`  
**Status**: ✅ ALL TASKS COMPLETE  
**Approach**: One task at a time — test after each fix before proceeding

---

## Regole di esecuzione

- Lavorare **solo** su `claude/security-hardening`, mai su master
- Dopo ogni task: `go build ./...` + verifica manuale endpoint se possibile
- Ogni task ha un commit dedicato con messaggio descrittivo
- Se un fix rompe qualcosa → revert del singolo commit, non dell'intero branch
- Al termine di tutti i task → PR verso master

---

## Task List

---

### TASK 1 — SQL Injection: history aggregation query
**Priorità**: CRITICO  
**File**: `internal/handlers/history.go` ~riga 442  
**Stato**: `[x]` DONE

**Problema**:
```go
query := fmt.Sprintf(`SELECT ... %s(value) as val ...`, sqlAgg)
```
`sqlAgg` viene da parametro HTTP `agg` e, nonostante ci sia uno switch per mapparlo,
viene comunque usato in `fmt.Sprintf` direttamente. Un attaccante può forgiare
una stringa che sfugge allo switch e finisce nella query grezza.

**Fix**:
Dopo lo switch che mappa `agg` → `sqlAgg`, aggiungere un controllo whitelist esplicito
prima dell'`fmt.Sprintf`:
```go
validAgg := map[string]bool{"AVG": true, "MIN": true, "MAX": true, "COUNT": true}
if !validAgg[sqlAgg] {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid aggregation function"})
    return
}
```

**Test**: `go build ./...` + curl con `?agg=max;DROP TABLE tags--` deve ritornare 400.

---

### TASK 2 — WebSocket: CheckOrigin accetta tutti i domain
**Priorità**: CRITICO  
**File**: `internal/handlers/websocket.go` ~riga 29  
**Stato**: `[x]` DONE

**Problema**:
```go
CheckOrigin: func(r *http.Request) bool { return true }
```
Qualsiasi sito esterno può aprire una WebSocket verso l'API e ricevere
dati real-time (valori tag, allarmi).

**Fix**:
Validare l'`Origin` header contro una lista di origini consentite configurabile via env:
```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    allowed := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
    for _, a := range allowed {
        if strings.TrimSpace(a) == origin {
            return true
        }
    }
    return false
},
```
Aggiungere `ALLOWED_ORIGINS=http://localhost:3000` in `.env.example`.

**Test**: WebSocket da origin non consentita deve ricevere `403 Forbidden`.

---

### TASK 3 — CORS: origini hardcoded con IP interni
**Priorità**: IMPORTANTE (da fare subito perché IP privato nel codice)  
**File**: `services/core-api/main.go` ~riga 204  
**Stato**: `[x]` DONE

**Problema**:
```go
AllowOrigins: []string{
    "http://localhost:3000",
    "http://127.0.0.1:3000",
    "http://100.97.150.10:9090",   // ← IP interno hardcoded
    "http://100.97.150.10:8081",   // ← IP interno hardcoded
}
```

**Fix**:
Leggere da env var `ALLOWED_ORIGINS` (stessa usata in TASK 2):
```go
allowedOrigins := []string{"http://localhost:3000"}
if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
    allowedOrigins = strings.Split(origins, ",")
}
r.Use(cors.New(cors.Config{
    AllowOrigins: allowedOrigins,
    ...
}))
```

**Test**: `go build ./...` + verificare che CORS funzioni con `ALLOWED_ORIGINS=http://localhost:3000`.

---

### TASK 4 — Security Headers HTTP mancanti
**Priorità**: IMPORTANTE  
**File**: `services/core-api/main.go` (aggiungere middleware)  
**Stato**: `[x]` DONE

**Problema**: Nessun header di sicurezza nelle response HTTP.

**Fix**:
Aggiungere un middleware Gin prima di tutte le route:
```go
r.Use(func(c *gin.Context) {
    c.Header("X-Content-Type-Options", "nosniff")
    c.Header("X-Frame-Options", "DENY")
    c.Header("X-XSS-Protection", "1; mode=block")
    c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
    c.Next()
})
```
Non aggiungere HSTS finché TLS non è configurato (altrimenti blocca HTTP locale).

**Test**: `curl -I http://localhost:8081/health` deve mostrare i nuovi header.

---

### TASK 5 — Error disclosure: errori interni esposti nelle response
**Priorità**: CRITICO  
**File**: `internal/handlers/backup.go`, `internal/handlers/history.go`, altri handler  
**Stato**: `[x]` DONE

**Problema**:
```go
c.JSON(500, gin.H{"error": fmt.Sprintf("Database backup failed: %v", err)})
// err contiene: "pq: connect: /var/run/postgresql/.s.PGSQL.5432 no such file"
```
Percorsi interni, connection string, schema DB esposti al client.

**Fix**:
Creare un helper che logga l'errore reale internamente e restituisce solo un messaggio generico:
```go
func internalError(c *gin.Context, msg string, err error) {
    log.Printf("[ERROR] %s: %v", msg, err)
    c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
}
```
Sostituire tutti i `fmt.Sprintf(..., err)` nelle response con questo helper.

**File da toccare**:
- `internal/handlers/backup.go` (righe 74, 237, 280, 289)
- `internal/handlers/history.go` (righe 289, 442)
- Grep completo: `grep -rn "Sprintf.*err" internal/handlers/`

**Test**: `go build ./...` + verificare che le response 500 non contengano path o messaggi DB.

---

### TASK 6 — Memory leak: rate limiter visitor map illimitata
**Priorità**: CRITICO  
**File**: `internal/middleware/ratelimit.go`  
**Stato**: `[x]` DONE

**Problema**:
La mappa `visitors` cresce senza limite. Un attaccante con IP spoofing può
riempire la RAM del server prima che il cleanup (ogni 5 min) intervenga.

**Fix**:
Aggiungere un limite massimo di entry nella mappa e scartare le più vecchie:
```go
const maxVisitors = 10000

func getVisitor(ip string) *rate.Limiter {
    mu.Lock()
    defer mu.Unlock()
    if len(visitors) >= maxVisitors {
        // rimuovi il più vecchio
        for k := range visitors {
            delete(visitors, k)
            break
        }
    }
    // ... resto della logica
}
```

**Test**: `go build ./...` — test unitario che verifica che la mappa non superi il limite.

---

### TASK 7 — Swagger esposto in produzione
**Priorità**: IMPORTANTE  
**File**: `services/core-api/main.go` ~riga 434  
**Stato**: `[x]` DONE

**Problema**:
`/swagger/*any` è sempre disponibile, espone tutta la struttura API.

**Fix**:
Aggiungere condizione su env var:
```go
if os.Getenv("SWAGGER_ENABLED") == "true" {
    r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
    log.Println("[WARNING] Swagger UI enabled — disable in production")
}
```
Aggiungere `SWAGGER_ENABLED=false` in `.env.example`.

**Test**: Senza `SWAGGER_ENABLED=true`, `GET /swagger/index.html` deve ritornare 404.

---

### TASK 8 — Rate limiting globale (non solo login)
**Priorità**: IMPORTANTE  
**File**: `services/core-api/main.go`, `internal/middleware/ratelimit.go`  
**Stato**: `[x]` DONE

**Problema**:
Solo `/api/auth/login` ha rate limiting. Tutti gli altri endpoint sono illimitati.
Un attaccante può fare scraping massiccio di dati storici o spam di delete.

**Fix**:
Aggiungere un rate limiter globale più permissivo (es. 300 req/min per IP) applicato
a tutto il gruppo `/api/`:
```go
// GlobalRateLimit — 300 req/min, burst 50
func GlobalRateLimit() gin.HandlerFunc { ... }

// In main.go
api := r.Group("/api")
api.Use(middleware.GlobalRateLimit())
```

**Test**: `go build ./...` + verifica che il login rate limit esistente non sia rotto.

---

### TASK 9 — Query DB senza context timeout
**Priorità**: IMPORTANTE  
**File**: Molteplici handler  
**Stato**: `[x]` DONE

**Problema**:
`h.db.QueryRow(...)` senza context timeout. Se il DB è lento o bloccato,
le goroutine HTTP rimangono appese indefinitamente esaurendo il connection pool.

**Fix**:
Nei handler critici (tags, history, gateways) aggiungere context con timeout:
```go
ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
defer cancel()
row := h.db.QueryRowContext(ctx, query, args...)
```

**Scope**: Almeno i handler più chiamati:
- `internal/handlers/tags.go`
- `internal/handlers/history.go`
- `internal/handlers/gateways.go`

**Test**: `go build ./...`.

---

### TASK 10 — docker-compose: password fallback e JWT_SECRET obbligatorio
**Priorità**: CRITICO  
**File**: `docker-compose.yml`, `.env.example`  
**Stato**: `[x]` DONE

**Problema**:
```yaml
POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-industrial_pass}  # fallback insicuro
JWT_SECRET: ${JWT_SECRET}  # se non definito → stringa vuota → firma JWT invalida
```

**Fix**:
- Rimuovere il fallback `industrial_pass` — se manca la var, il container non parte (errore esplicito è meglio di default insicuro)
- Aggiungere validazione in `docker-compose.yml` con `required` syntax oppure documentare chiaramente in `.env.example` che entrambe le var sono **obbligatorie**
- Aggiungere in `internal/auth/auth.go` il check che `JWT_SECRET` abbia lunghezza minima (almeno 32 char)

**Test**: Avviare con `.env` senza `JWT_SECRET` → core-api deve rifiutarsi di partire con errore chiaro.

---

### TASK 11 — Logging strutturato
**Priorità**: MINORE (ma importante per observability in prod)  
**File**: Tutto il codebase  
**Stato**: `[x]` DONE

**Problema**:
Tutti i log usano `log.Printf` — non machine-parseable, difficile da aggregare
con strumenti come Loki, Datadog, CloudWatch.

**Fix**:
Introdurre `log/slog` (stdlib Go 1.21+, già disponibile) come logger strutturato:
```go
slog.Info("gateway connected", "gateway_id", gwID, "driver", driverType)
slog.Error("DB query failed", "error", err, "query", "tags.list")
```
Fare un passaggio sui log critici (auth, alarms, gateway events) senza toccare tutti i `log.Printf` minori.

**Test**: `go build ./...` + verificare output JSON-parseable.

---

## Riepilogo priorità di esecuzione

| Ordine | Task | Priorità | Rischio rottura |
|--------|------|----------|-----------------|
| 1 | SQL Injection history | CRITICO | Basso |
| 2 | WebSocket CheckOrigin | CRITICO | Basso |
| 3 | CORS hardcoded | CRITICO | Basso |
| 4 | Security headers | IMPORTANTE | Minimo |
| 5 | Error disclosure | CRITICO | Basso |
| 6 | Rate limiter memory leak | CRITICO | Basso |
| 7 | Swagger in prod | IMPORTANTE | Minimo |
| 8 | Rate limiting globale | IMPORTANTE | Medio |
| 9 | Query timeout | IMPORTANTE | Medio |
| 10 | docker-compose passwords | CRITICO | Medio |
| 11 | Logging strutturato | MINORE | Basso |

---

## Criteri di completamento

Un task è **DONE** quando:
1. Il codice compila: `go build ./...`
2. Il caso di attacco/errore specifico è verificato manualmente o con curl
3. Il commit è sul branch `claude/security-hardening`
4. Il task è marcato `[x]` in questo PRD

Il branch viene mergiato su master solo quando **tutti i task CRITICO e IMPORTANTE** sono `[x]`.
