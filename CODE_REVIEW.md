# Code Review Completo - Progetto Industrial Edge Middleware

## 1. Panoramica del Progetto

Il progetto è un sistema middleware industriale per la raccolta dati IoT/IIoT con architettura basata su:

- **Backend**: Go (driver Redis, S7, engine-historian)
- **Frontend**: React/TypeScript (web-ui)
- **Database**: PostgreSQL con TimescaleDB
- **Cache/Message Broker**: Redis e Mosquitto MQTT
- **Protocolli**: Legacy MQTT, Sparkplug B

---

## 2. Criticità Elevate (High)

### 2.1 Credenziali Hardcoded in docker-compose.yml

**File**: `docker-compose.yml:8-10,85-86`

```yaml
POSTGRES_PASSWORD: industrial_pass
DB_PASSWORD: industrial_pass
```

**Problema**: Le credenziali sono hardcoded nel file docker-compose.yml che è tracciato da git. Questo è un rischio di sicurezza grave.

**Raccomandazione**: Usare variabili d'ambiente da file `.env` o Docker secrets.

---

### 2.2 Timeout HTTP hardcoded

**File**: `services/web-ui/src/api/client.ts:18`

```typescript
timeout: 10000,
```

**Problema**: Timeout fisso di 10 secondi senza considerazione del tipo di operazione. Le query di storico possono richiedere più tempo.

**Raccomandazione**: Implementare timeout differenziati per endpoint.

---

### 2.3 Gestione Errori Assente nei Driver Go

**File**: `services/driver-s7/main.go:708`

```go
result := d.s7Client.ReadTag(tag.Code, dataType)
```

**Problema**: Il codice non verifica se `d.s7Client` è nil prima di chiamare `ReadTag`, causando potenziali panic.

**Raccomandazione**: Aggiungere controllo nil prima di ogni accesso.

---

### 2.4 Connessione Redis Non Verificata All'Avvio

**File**: `services/driver-redis/main.go:122-135`

**Problema**: Il driver si avvia anche se Redis non è raggiungibile, senza retry logic robusto.

**Raccomandazione**: Implementare retry con backoff esponenziale all'avvio.

---

## 3. Criticità Medie (Medium)

### 3.1 Inconsistenza nel Formato Topic MQTT

**File**: `services/driver-s7/main.go:618` vs `services/driver-redis/main.go:618`

**Problema**: I driver usano formati topic leggermente diversi. Il driver S7 usa slugify sul gateway name, ma in modo inconsistente.

**Raccomandazione**: Normalizzare la generazione dei topic in tutti i driver.

---

### 3.2 Mapping Tipi di Dati Incompleto

**File**: `services/driver-redis/main.go:514-533`

```go
case "INT", "DINT", "UINT":
    // parsing logic
case "REAL", "FLOAT":
    // parsing logic
```

**Problema**: Gestisce solo alcuni tipi di dati. Tipi come LREAL, LINT, UDINT non sono gestiti esplicitamente.

**Raccomandazione**: Aggiungere supporto per tutti i tipi S7 standard.

---

### 3.3 Ignorare Errori JSON Unmarshal

**File**: `services/driver-s7/main.go:621,772`

```go
payload, _ := json.Marshal(...)
```

**Problema**: Gli errori di marshalling vengono ignorati con `_`. Questo può mascherare problemi.

**Raccomandazione**: Loggare almeno gli errori di marshalling.

---

### 3.4 Lack of Connection Pooling

**File**: `services/engine-historian/main.go:123`

```go
database, err := sql.Open("postgres", dbConnStr)
```

**Problema**: Non vengono configurati limiti di connessione. In caso di alto carico, il database potrebbe essere sovraccarico.

**Raccomandazione**: Configurare `SetMaxOpenConns`, `SetMaxIdleConns`.

---

### 3.5 Cache Redis Senza Invalidation

**File**: `services/engine-historian/main.go:563,603`

```go
s.redisClient.Set(cacheKey, string(tagInfoJSON), 60*time.Second)
```

**Problema**: La cache ha TTL fisso di 60 secondi ma non c'è invalidazione quando i dati nel DB cambiano.

**Raccomandazione**: Implementare invalidation pattern o usare cache più breve.

---

## 4. Criticità Basse (Low)

### 4.1 Log Inconsistenti

**File**: Diversi file Go

- `driver-s7/main.go`: usa `log.Println` semplice
- `driver-redis/main.go`: usa format `[DRIVER-REDIS]`
- `engine-historian/main.go`: usa format `[HISTORIAN]`

**Problema**: Formato log non standardizzato tra i servizi.

**Raccomandazione**: Usare un logger strutturato (es. zerolog, zap).

---

### 4.2 Commenti di Documentazione Assenti

**File**: Diversi file TypeScript

Le funzioni principali mancano di JSDoc/TSDoc comments.

**Raccomandazione**: Aggiungere documentazione alle API pubbliche e hook.

---

### 4.3 State Management Duplicato

**File**: `useNavigationStore.ts`, `useTrendStore.ts`, `useMqttStore.ts`

**Problema**: Ogni store ha la sua logica di stato. Non c'è una separazione chiara delle responsabilità.

**Raccomandazione**: Consolidare store correlati o usare pattern più strutturato.

---

### 4.4 Error Handling in useEffect

**File**: `useSparkplugListener.ts:127-133`

```typescript
} catch {}
```

**Problema**: Errori silenziati con catch vuoto. Impossibile diagnosticare problemi.

**Raccomandazione**: Almeno loggare gli errori in ambiente development.

---

## 5. Opportunità di Miglioramento

### 5.1 Performance

1. **Batch Insert PostgreSQL**: L'historian fa insert singoli. Usare bulk insert per migliorare throughput.
2. **MGET Redis**: Il driver Redis già usa MGET (bene), ma può essere esteso agli altri driver.
3. **React.memo**: Alcuni componenti React potrebbero beneficiare di memoizzazione.

### 5.2 Robustezza

1. **Circuit Breaker**: Implementare per le connessioni a database e MQTT.
2. **Graceful Shutdown**: Alcuni servizi non chiudono correttamente le connessioni.
3. **Retry Logic**: Standardizzare i retry con backoff.

### 5.3 Sicurezza

1. **Rate Limiting**: Implementare sul backend API.
2. **Input Validation**: Validazione più rigorosa degli input MQTT.
3. **SQL Injection**: Verificare che le query parametrizzate siano usate ovunque.

### 5.4 Manutenibilità

1. **Test**: Assenza di test unitari visibili nel progetto.
2. **Code Generation**: Considerare generazione di tipi TypeScript da Go.
3. **CI/CD**: Pipeline non visibile nel repo.

---

## 6. Riepilogo per Severità

| Severità | Conteggio | Priorità |
|----------|-----------|----------|
| Alta     | 4         | Immediata |
| Media    | 5         | Breve termine |
| Bassa    | 4         | Opportunistico |

---

## 7. Raccomandazioni Finali

1. **Immediato**: Spostare credenziali in env file
2. **Breve termine**: Aggiungere validazione input e gestione errori
3. **Medio termine**: Aggiungere test, migliorare logging, implementare circuit breaker
4. **Lungo termine**: Refactoring per microservizi, documentazione completa

---

*Documento generato in data: 2026-03-05*
