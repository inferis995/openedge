# OpenEdge API — Skill per Agenti Paperclip
## NET CARING S.R.L. | IndustrialAI-Ops Platform

Questo documento descrive come gli agenti Paperclip devono interagire con
l'istanza OpenEdge del cliente. Leggi tutto prima di fare qualsiasi chiamata API.

---

## 1. Autenticazione

Tutti gli endpoint (eccetto `/health` e `/ready`) richiedono un JWT Bearer token
e l'header `X-Organization-ID`.

### Login
```
POST http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/api/auth/login
Content-Type: application/json

{"username": "{OPENEDGE_USERNAME}", "password": "{OPENEDGE_PASSWORD}"}
```

Risposta:
```json
{"token": "eyJ...", "user": {"id": 1, "role": "admin"}}
```

Il token scade dopo **24 ore**. Rinnova prima di ogni sessione di lavoro.

### Header obbligatori per ogni chiamata
```
Authorization: Bearer {TOKEN}
X-Organization-ID: {OPENEDGE_ORG_ID}
```

### Script shell per ottenere TOKEN
```bash
TOKEN=$(curl -s -X POST http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"{OPENEDGE_USERNAME}","password":"{OPENEDGE_PASSWORD}"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

---

## 2. Health Check (pubblico, senza auth)

```
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/health
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/ready
```

- `/health` → `{"status":"ok"}` se il server risponde
- `/ready` → `{"status":"ready","db":"ok","redis":"ok"}` se DB e Redis sono up

**Usa `/ready` per verificare che OpenEdge sia completamente operativo prima di fare query.**

---

## 3. Endpoint AI-Ops (scopo primario degli agenti)

Questi endpoint sono ottimizzati per gli agenti: una sola chiamata restituisce
tutto il necessario, senza N query separate.

---

### 3.1 GET /api/aiops/summary — Snapshot organizzazione

**Quando usarlo:** all'inizio di ogni ciclo di analisi, per avere il quadro completo.

```
GET /api/aiops/summary?hours=24
```

Parametri:
- `hours` (default: 24, min: 1, max: 720) — finestra temporale in ore

Risposta:
```json
{
  "org_id": 1,
  "org_name": "SORICAL",
  "period_hours": 24,
  "generated_at": "2026-03-28T07:00:00Z",
  "active_alarms_count": 3,
  "critical_alarms_count": 1,
  "total_gateways_count": 12,
  "tags": [
    {
      "tag_id": 5,
      "alias": "Portata_Ingresso",
      "data_type": "REAL",
      "gateway_name": "PLC-Serbatoio1",
      "site_name": "Sito-Crotone",
      "avg_value": 42.5,
      "min_value": 38.1,
      "max_value": 47.2,
      "sample_count": 2880,
      "has_alarm": true,
      "alarm_count": 1
    }
  ]
}
```

Note:
- `avg_value`, `min_value`, `max_value` possono essere `null` se il tag non ha dati storici nel periodo (gateway offline)
- `sample_count` = numero di campioni nel periodo (0 = nessun dato = problema di connettività)
- `has_alarm` = true se il tag ha avuto almeno un allarme nel periodo

---

### 3.2 GET /api/aiops/anomalies — Rilevamento anomalie Z-score

**Quando usarlo:** per analisi statistica approfondita su tag specifici sospetti.

```
GET /api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30
```

Parametri:
- `tag_id` (required) — ID del tag da analizzare
- `window_hours` (default: 168 = 1 settimana, max: 720) — finestra da analizzare
- `baseline_days` (default: 30, min: 7, max: 365) — giorni di baseline per calcolare mean/stddev

Risposta normale:
```json
{
  "tag_id": 5,
  "tag_alias": "Portata_Ingresso",
  "baseline_mean": 42.8,
  "baseline_std_dev": 1.4,
  "anomaly_count": 3,
  "anomalies": [
    {
      "bucket": "2026-03-27T14:00:00Z",
      "value": 51.2,
      "z_score": 5.98,
      "direction": "high"
    }
  ]
}
```

Risposta quando non ci sono dati sufficienti per il baseline:
```json
{
  "tag_id": 5,
  "tag_alias": "Portata_Ingresso",
  "baseline_mean": 0,
  "baseline_std_dev": 0,
  "anomaly_count": 0,
  "anomalies": [],
  "note": "baseline stddev too low or no baseline data — anomaly detection not applicable"
}
```

Note:
- Soglia anomalia: `|z_score| >= 2.5` (circa 2.5 deviazioni standard dalla media)
- `direction`: "high" = valore sopra la norma, "low" = valore sotto la norma
- Un z_score alto (es. 6.0) indica un'anomalia grave; tra 2.5 e 3.5 potrebbe essere rumore
- Se `note` è presente, non ci sono dati sufficienti per l'analisi

---

### 3.3 GET /api/aiops/alarms/digest — Digest allarmi per report

**Quando usarlo:** per generare report giornalieri PDF/email.

```
GET /api/aiops/alarms/digest?hours=24
```

Parametri:
- `hours` (default: 24, min: 1, max: 720)

Risposta:
```json
{
  "period_hours": 24,
  "total_fired": 8,
  "still_active": 2,
  "cleared": 6,
  "by_severity": {
    "info": 2,
    "warning": 4,
    "critical": 2
  },
  "alarms": [
    {
      "severity": "critical",
      "tag_id": 12,
      "tag_alias": "Pressione_Rete",
      "alarm_type": "high",
      "message": "Pressione massima superata",
      "value_at_trigger": 8.7,
      "trigger_time": "2026-03-27T22:15:00Z",
      "status": "CLEARED",
      "clear_time": "2026-03-27T23:05:00Z"
    }
  ]
}
```

Note:
- `still_active` = allarmi con status ACTIVE o ACKNOWLEDGED
- `cleared` = allarmi risolti
- `clear_time` può essere null se l'allarme è ancora attivo
- Massimo 1000 allarmi per risposta

---

## 4. Endpoint Standard

### 4.1 Allarmi attivi in tempo reale

```
GET /api/alarms/active
```

Risposta: lista di allarmi con status ACTIVE o ACKNOWLEDGED.

```json
[{
  "id": 45,
  "tag_id": 12,
  "status": "ACTIVE",
  "alarm_type": "high",
  "severity": "critical",
  "message": "Pressione massima superata",
  "value_at_trigger": 8.7,
  "trigger_time": "2026-03-27T22:15:00Z"
}]
```

**Usa questo per Alarm Monitor (ogni 5 min).**

---

### 4.2 Stato gateway (connettività PLC)

```
GET /api/gateways
```

Risposta: lista gateway con stato di connessione live (da Redis).

```json
[{
  "id": 3,
  "name": "PLC-Serbatoio1",
  "driver_type": "MODBUS_TCP",
  "enabled": true,
  "connection_status": "online",
  "last_seen": 1711234567000
}]
```

Valori `connection_status`:
- `"online"` — comunicazione attiva, dati recenti
- `"offline"` — nessuna comunicazione da più di 30 secondi
- `"unknown"` — mai connesso da avvio sistema

**Usa questo per Gateway Health Monitor (ogni 15 min).**
Un gateway offline da più di 30 minuti è un problema da segnalare.

---

### 4.3 Valore corrente di un tag (real-time da Redis)

```
GET /api/tags/{tag_id}/current
```

Risposta:
```json
{
  "tag_id": 5,
  "alias": "Portata_Ingresso",
  "value": 42.5,
  "timestamp": 1711234567000,
  "quality": 0
}
```

Quality codes: **0 = Good**, **1 = Bad**

---

### 4.4 Statistiche storiche

```
GET /api/history/stats?tag_id=5&start=1711148167000&end=1711234567000
```

Parametri: `tag_id`, `start` (ms unix), `end` (ms unix)

Risposta:
```json
{
  "min_value": 38.1,
  "max_value": 47.2,
  "avg_value": 42.5,
  "std_dev": 1.4,
  "sample_count": 2880
}
```

---

### 4.5 Lista tag con gerarchia

```
GET /api/tags/with-hierarchy
```

Risposta: lista completa tag con org/site/area/gateway associato.
Utile per costruire il contesto iniziale del cliente.

---

## 5. Struttura Redis (per riferimento)

I valori real-time sono cachati in Redis. Non accedere direttamente a Redis —
usa la REST API.

| Chiave Redis | Struttura | Note |
|---|---|---|
| `realtime:{tag_id}` | `{"v":42.5,"ts":1711234567000,"q":0}` | TTL 60 giorni; assenza = mai letto |
| `gateway_health:{gw_id}` | `{"status":"online","last_seen":1711234567000}` | Aggiornato dal driver ogni scan |

---

## 6. Regole critiche per interpretare i dati

### 6.1 Gap nel trend = gateway offline, NON errore
`value = NULL` in tag_history significa che il gateway era offline in quel momento.
**Non confondere un gap con un dato errato o con 0.**
Il sistema inserisce esplicitamente marker `source='offline'` per queste finestre.

### 6.2 Timestamp in MILLISECONDI
Tutti i timestamp restituiti dalla REST API sono in millisecondi Unix (int64).
Es: `1711234567000` = `2026-03-23T15:22:47Z`

Nella risposta di `/api/aiops/*` i timestamp sono già in formato ISO 8601 (RFC3339).

### 6.3 Valori BOOL
I tag di tipo BOOL sono salvati come FLOAT:
- `1.0` = true / ON / contatto chiuso
- `0.0` = false / OFF / contatto aperto

### 6.4 Quality codes (REST API)
- `0` = Good — dato affidabile
- `1` = Bad — problema di comunicazione o sensore

**Nota:** Sparkplug B usa quality 192=Good, 0=Bad (opposto REST API). Non confondere i due contesti.

### 6.5 Source field in tag_history
| source | Significato |
|--------|-------------|
| `mqtt` | Dato normale pubblicato dal driver |
| `sparkplug_b` | Dato dal protocollo Sparkplug B |
| `offline` | Marker: gateway si è disconnesso in questo momento |
| `seed` | Valore iniettato via REST per continuità trend |

---

## 7. Pattern operativo raccomandato per gli agenti

### Alarm Monitor (ogni 5 min)
```
1. GET /api/alarms/active
2. Se severity=critical AND status=ACTIVE → apri ticket urgente + Telegram
3. Se solo WARNING → aggiungi nota a ticket giornaliero esistente
4. Se lista vuota → nessuna azione
```

### Gateway Health Monitor (ogni 15 min)
```
1. GET /api/gateways
2. Per ogni gateway: controlla connection_status
3. Se offline E last_seen > 30 minuti fa → apri ticket
4. Se torna online → chiudi ticket + notifica recovery
```

### Daily Report Agent (ogni giorno alle 07:00)
```
1. GET /api/aiops/alarms/digest?hours=24
2. GET /api/aiops/summary?hours=24
3. Genera sezione allarmi dal digest
4. Genera tabella tag critici dal summary (quelli con has_alarm=true o sample_count=0)
5. Invia PDF via email
```

### Anomaly Detector (ogni ora)
```
1. GET /api/aiops/summary?hours=1
2. Per tag con has_alarm=false E sample_count > 0:
   GET /api/aiops/anomalies?tag_id={id}&window_hours=24
3. Se anomaly_count > 0 → analisi Claude + ticket
```

---

## 8. Esempi curl completi

```bash
# Login
TOKEN=$(curl -s -X POST http://OPENEDGE_HOST:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"PASS"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

# Headers da usare per tutte le chiamate
HEADERS='-H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1"'

# Health check
curl http://OPENEDGE_HOST:8081/ready

# Summary ultime 24h
curl $HEADERS "http://OPENEDGE_HOST:8081/api/aiops/summary?hours=24"

# Anomalie su tag 5 (ultima settimana, baseline 30 giorni)
curl $HEADERS "http://OPENEDGE_HOST:8081/api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30"

# Digest allarmi ultime 24h
curl $HEADERS "http://OPENEDGE_HOST:8081/api/aiops/alarms/digest?hours=24"

# Allarmi attivi
curl $HEADERS "http://OPENEDGE_HOST:8081/api/alarms/active"

# Stato gateway
curl $HEADERS "http://OPENEDGE_HOST:8081/api/gateways"
```

---

## 9. Gestione errori

| HTTP Status | Significato | Azione |
|---|---|---|
| 200 | OK | Procedi |
| 400 | Parametro non valido | Correggi la richiesta |
| 401 | Token scaduto o mancante | Rinnova il token con POST /api/auth/login |
| 403 | Permessi insufficienti | Verifica OPENEDGE_USERNAME e ruolo |
| 404 | Tag/risorsa non trovata | Verifica tag_id e org_id |
| 500 | Errore server | Logga e riprova dopo 30 secondi |

Tutte le risposte di errore seguono questo formato:
```json
{"error": "descrizione dell'errore"}
```

---

*Documento generato automaticamente da NET CARING S.R.L. — IndustrialAI-Ops v1.0*
*Aggiornare questo file quando vengono aggiunti nuovi endpoint a OpenEdge.*
