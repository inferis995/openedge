---
name: openedge
description: OpenEdge Industrial IoT Middleware — monitor allarmi, leggi dati real-time, storico, anomalie via REST + i3X (on-prem self-hosted)
version: 3.0.0
tags: [industrial, iot, alarms, historian, timeseries, scada, i3x, cesmii, monitoring, on-prem]
---

# OpenEdge — Skill di monitor

Questo documento descrive come un agente AI **osserva** un'istanza OpenEdge:
legge allarmi, valori real-time, storico, salute dei gateway, anomalie.

OpenEdge in master è un'installazione **on-prem single-tenant**: un solo
server (di solito in fabbrica), un'unica organizzazione, gli utenti del
cliente accedono solo a quella. Niente multi-tenant, niente cloud SaaS.

> Per **installare / risolvere problemi / configurare** il sistema in
> produzione, usa la skill `openedge-ops.md`. Questa è solo per "leggere
> cosa sta succedendo".

Leggi tutto prima di fare qualsiasi chiamata API.

---

## Variabili d'ambiente attese

```bash
OPENEDGE_HOST=localhost            # o l'IP/host del PC industriale
OPENEDGE_PORT=8081
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=admin123         # cambiala al primo login
OPENEDGE_ORG_ID=1                  # in on-prem single-tenant è sempre 1
```

L'`OPENEDGE_ORG_ID` in master è **costante** (1) perché c'è una sola
organizzazione. Lo passi comunque come header `X-Organization-ID: 1` su
ogni chiamata — il backend usa l'org del token JWT se l'header manca,
ma includerlo esplicito è più chiaro.

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

### Script shell per ottenere TOKEN

```bash
TOKEN=$(curl -s -X POST http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"{OPENEDGE_USERNAME}","password":"{OPENEDGE_PASSWORD}"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
```

### Header obbligatori per ogni chiamata

```
Authorization: Bearer {TOKEN}
X-Organization-ID: {OPENEDGE_ORG_ID}
```

---

## 2. Health Check (pubblico, senza auth)

```
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/health
GET http://{OPENEDGE_HOST}:{OPENEDGE_PORT}/ready
```

- `/health` → `{"status":"ok"}` — server risponde
- `/ready` → `{"status":"ready","db":"ok","redis":"ok"}` — tutto up

**Usa `/ready` per verificare che OpenEdge sia operativo prima di fare query.**

---

## 3. i3X Access API — Protocollo CESMII Standard

Il prefisso è `/api/i3x/v1/`. Tutti gli endpoint richiedono auth + org.

Questa è l'API **vendor-neutral** compatibile con la specifica CESMII i3X v1.
Usa per integrazioni con sistemi esterni, SCADA, o agenti che devono leggere/scrivere
dati in formato standard senza conoscere la struttura interna di OpenEdge.

### 3.1 ID Format

Tutti gli ID nell'i3X API sono stringhe con prefisso:

| Tipo | Formato | Esempio |
|------|---------|---------|
| Organizzazione | `org-{n}` | `org-1` |
| Sito | `site-{n}` | `site-3` |
| Area | `area-{n}` | `area-7` |
| Gateway / Equipment | `gw-{n}` | `gw-2` |
| Tag / Property | `tag-{n}` | `tag-42` |

### 3.2 Quality Codes

Questi codici valgono per **tutti i protocolli** (Modbus, S7, OPC UA, MQTT, Redis).
Lo standard i3X usa la codifica numerica OPC-UA indipendentemente dal driver sottostante.

| Valore | Significato |
|--------|-------------|
| `192` | Good — dato affidabile |
| `64` | Uncertain |
| `0` | Bad — problema comunicazione o dato assente |

> ⚠️ L'API standard REST usa `0=Good, 1=Uncertain, 2=Bad`. L'i3X usa `192=Good, 64=Uncertain, 0=Bad`. Non confonderli.

### 3.3 Data Types

| i3X | Tipi interni corrispondenti |
|-----|-----------------------------|
| `Boolean` | `BOOL` |
| `Int32` | `INT`, `DINT` |
| `Float` | `REAL` |
| `String` | `STRING`, `WORD`, altri |

---

### GET /api/i3x/v1/equipment — Lista equipment

Restituisce la gerarchia completa: org → sito → area → gateway.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment
```

Risposta:
```json
{
  "items": [
    {
      "id": "org-1",
      "name": "MyOrganization",
      "type": "Assembly",
      "parentId": null,
      "path": "MyOrganization"
    },
    {
      "id": "site-3",
      "name": "Sito-Crotone",
      "type": "Assembly",
      "parentId": "org-1",
      "path": "MyOrganization/Sito-Crotone"
    },
    {
      "id": "gw-2",
      "name": "PLC-Serbatoio1",
      "type": "Equipment",
      "parentId": "area-7",
      "path": "MyOrganization/Sito-Crotone/Zona-A/PLC-Serbatoio1",
      "attributes": {
        "driver_type": "MODBUS_TCP",
        "connection_status": "online",
        "enabled": true
      }
    }
  ],
  "total": 5
}
```

Tipi: `Assembly` = nodo logico (org/site/area), `Equipment` = dispositivo fisico (gateway).

---

### GET /api/i3x/v1/equipment/:id — Dettaglio equipment

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2
```

---

### GET /api/i3x/v1/equipment/:id/properties — Tag di un equipment

Lista tutti i tag (properties) di un gateway, con valore corrente da Redis.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2/properties
```

Risposta:
```json
{
  "items": [
    {
      "id": "tag-42",
      "name": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "dataType": "Float",
      "historize": true,
      "current": {
        "value": 42.5,
        "quality": 192,
        "timestamp": "2026-04-27T10:30:00Z"
      }
    },
    {
      "id": "tag-43",
      "name": "Pompa_On",
      "equipmentId": "gw-2",
      "dataType": "Boolean",
      "historize": true,
      "current": {
        "value": true,
        "quality": 192,
        "timestamp": "2026-04-27T10:30:01Z"
      }
    }
  ],
  "total": 2
}
```

Se `current` è assente → il tag non ha mai ricevuto un valore (gateway mai connesso).

---

### GET /api/i3x/v1/equipment/:id/properties/:propId — Singola property

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2/properties/tag-42
```

---

### GET /api/i3x/v1/properties — Tutti i tag dell'organizzazione

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/properties
```

---

### GET /api/i3x/v1/properties/:id — Singola property con valore corrente

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/properties/tag-42
```

---

### PUT /api/i3x/v1/properties/:id/value — Scrivi valore su un tag

Richiede permesso `i3x_write` nel JWT oppure ruolo `admin`.

```bash
curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 1" \
  -H "Content-Type: application/json" \
  -d '{"value": 1}' \
  http://localhost:8081/api/i3x/v1/properties/tag-43/value
```

Risposta: `{"message": "Write command sent"}`

Il comando è inviato al driver via MQTT (`cmd/write/{gateway_id}`). La risposta
arriva in modo asincrono: il valore aggiornato sarà visibile nel campo `current`
alla prossima lettura dopo che il driver ha eseguito la scrittura sul PLC.

Errori possibili:
- `403 FORBIDDEN` — utente senza `i3x_write` o accesso a org diversa
- `503 MQTT_UNAVAILABLE` — broker MQTT non connesso

---

### GET /api/i3x/v1/alarms — Allarmi attivi in formato i3X

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/alarms
```

Risposta:
```json
{
  "items": [
    {
      "id": "alarm-45",
      "propertyId": "tag-42",
      "propertyName": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "equipmentName": "PLC-Serbatoio1",
      "severity": "Critical",
      "status": "Active",
      "alarmType": "high",
      "message": "Portata massima superata",
      "value": 98.7,
      "triggerTime": "2026-04-27T08:15:00Z",
      "clearTime": null
    }
  ],
  "total": 1
}
```

Status values: `Active`, `Acknowledged`, `Cleared`
Severity values: `Info`, `Warning`, `Critical`

---

### GET /api/i3x/v1/alarms/history — Storico allarmi

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/alarms/history
```

Risposta analoga a `/alarms` ma include anche allarmi con status `Cleared`.

---

## 4. AI-Ops API — Endpoint ottimizzati per agenti

Questi endpoint restituiscono tutto in una sola chiamata, senza N query separate.

---

### GET /api/aiops/summary?hours=24 — Snapshot organizzazione

**Quando usarlo:** all'inizio di ogni ciclo di analisi.

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/summary?hours=24"
```

Risposta:
```json
{
  "org_id": 1,
  "org_name": "MyOrganization",
  "period_hours": 24,
  "generated_at": "2026-04-27T07:00:00Z",
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
- `avg/min/max_value` null → gateway offline nel periodo, nessun dato
- `sample_count = 0` → problema connettività
- `has_alarm = true` → almeno un allarme nel periodo

---

### GET /api/aiops/anomalies — Rilevamento anomalie Z-score

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30"
```

Parametri:
- `tag_id` (required)
- `window_hours` (default: 168 = 1 settimana, max: 720)
- `baseline_days` (default: 30, min: 7, max: 365)

Risposta:
```json
{
  "tag_id": 5,
  "tag_alias": "Portata_Ingresso",
  "baseline_mean": 42.8,
  "baseline_std_dev": 1.4,
  "anomaly_count": 3,
  "anomalies": [
    {
      "bucket": "2026-04-26T14:00:00Z",
      "value": 51.2,
      "z_score": 5.98,
      "direction": "high"
    }
  ]
}
```

Soglia: `|z_score| >= 2.5`. Direction: `high` = sopra la norma, `low` = sotto.

---

### GET /api/aiops/alarms/digest?hours=24 — Digest allarmi per report

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  "http://localhost:8081/api/aiops/alarms/digest?hours=24"
```

Risposta:
```json
{
  "period_hours": 24,
  "total_fired": 8,
  "still_active": 2,
  "cleared": 6,
  "by_severity": {"info": 2, "warning": 4, "critical": 2},
  "alarms": [
    {
      "severity": "critical",
      "tag_id": 12,
      "tag_alias": "Pressione_Rete",
      "alarm_type": "high",
      "message": "Pressione massima superata",
      "value_at_trigger": 8.7,
      "trigger_time": "2026-04-26T22:15:00Z",
      "status": "CLEARED",
      "clear_time": "2026-04-26T23:05:00Z"
    }
  ]
}
```

---

## 5. Endpoint Standard

### Allarmi attivi

```
GET /api/alarms/active
```

### Stato gateway (connettività PLC)

```
GET /api/gateways
```

`connection_status`: `"online"` | `"offline"` | `"unknown"`

### Valore corrente tag (da Redis)

```
GET /api/tags/{tag_id}/current
```

Risposta: `{"tag_id":5,"alias":"Portata_Ingresso","value":42.5,"timestamp":1711234567000,"quality":0}`

Quality REST: **0 = Good**, **1 = Uncertain**, **2 = Bad** (opposto i3X!)

### Statistiche storiche

```
GET /api/history/stats?tag_id=5&start=1711148167000&end=1711234567000
```

### Tag con gerarchia completa

```
GET /api/tags/with-hierarchy
```

---

## 6. Regole critiche per interpretare i dati

### Quality codes — NON confondere i due contesti

| API | Good | Uncertain | Bad |
|-----|------|-----------|-----|
| REST standard (`q`) | `0` | `1` | `2` |
| i3X Access API | `192` | `64` | `0` |

> La scala interna `q` dei driver è **0=Good, 1=Uncertain, 2=Bad** (tutti i driver
> emettono `0` per i valori buoni e `2` in caso di guasto/comunicazione persa).

### Timestamp

- REST standard: millisecondi Unix (`1711234567000`)
- i3X API e AI-Ops: ISO 8601 (`2026-04-27T10:30:00Z`)

### Valori BOOL

Salvati come float: `1.0` = ON, `0.0` = OFF.
In i3X la property `Boolean` restituisce già `true`/`false`.

### Gap nel trend = gateway offline, NON errore

`value = NULL` in tag_history = gateway offline in quel momento.
Il sistema inserisce marker `source='offline'` per queste finestre.

### Source field in tag_history

| source | Significato |
|--------|-------------|
| `mqtt` | Dato normale dal driver |
| `sparkplug_b` | Dato dal protocollo Sparkplug B |
| `offline` | Marker: gateway disconnesso |
| `seed` | Valore iniettato via REST per continuità trend |

---

## 7. Quando usare i3X vs API standard

| Scenario | API da usare |
|----------|-------------|
| Integrazione con sistema esterno (SCADA, MES, cloud) | **i3X** |
| Lettura valore corrente di un tag specifico | i3X `GET /properties/{id}` o REST `GET /tags/{id}/current` |
| Scrittura valore su PLC | **i3X** `PUT /properties/{id}/value` |
| Lista tag con gerarchia | **i3X** `GET /equipment/{id}/properties` |
| Analisi anomalie / report | **AI-Ops** |
| Alarm monitor real-time | AI-Ops o i3X `/alarms` |
| Dashboard interna OpenEdge | REST standard |

---

## 8. Pattern operativo per agenti

### Alarm Monitor (ogni 5 min)

```
1. GET /api/i3x/v1/alarms
2. Se severity=Critical AND status=Active → notifica urgente
3. Se solo Warning → aggiungi nota a ticket giornaliero
4. Lista vuota → nessuna azione
```

### Gateway Health Monitor (ogni 15 min)

```
1. GET /api/gateways
2. Per ogni gateway: controlla connection_status
3. Se offline E last_seen > 30 min → apri ticket
4. Se torna online → chiudi ticket + notifica recovery
```

### Daily Report (ogni giorno alle 07:00)

```
1. GET /api/aiops/alarms/digest?hours=24
2. GET /api/aiops/summary?hours=24
3. Genera sezione allarmi dal digest
4. Tabella tag critici (has_alarm=true o sample_count=0)
5. Invia report
```

### Anomaly Detector (ogni ora)

```
1. GET /api/aiops/summary?hours=1
2. Per tag con has_alarm=false E sample_count > 0:
   GET /api/aiops/anomalies?tag_id={id}&window_hours=24
3. Se anomaly_count > 0 → analisi + ticket
```

### Lettura valore e scrittura via i3X

```
# Leggi valore corrente
GET /api/i3x/v1/properties/tag-42

# Scrivi valore (richiede i3x_write o admin)
PUT /api/i3x/v1/properties/tag-43/value
{"value": 1}
```

---

## 9. Rate Limiting

Il sistema applica due livelli di rate limit per IP:

| Limite | Endpoint | Risposta |
|--------|----------|----------|
| 10 req/min (burst 5) | `POST /api/auth/login` | `429 Too Many Requests` |
| 300 req/min (burst 50) | Tutti gli altri `/api/*` | `429 Too Many Requests` |

**Gli agenti devono gestire il 429.** Se ricevi 429, aspetta almeno 1 secondo e riprova con backoff esponenziale. Non ciclare su centinaia di tag senza pausa.

Pattern consigliato per agenti che leggono molti tag:
```
# Usa gli endpoint aggregati (una sola chiamata) invece di N chiamate singole
GET /api/i3x/v1/equipment/{id}/properties   ← tutti i tag del gateway in una volta
GET /api/aiops/summary                       ← snapshot org completo
```

---

## 10. Gestione errori

| HTTP | Significato | Azione |
|------|-------------|--------|
| 200 | OK | Procedi |
| 400 | Parametro non valido | Correggi la richiesta |
| 401 | Token scaduto o mancante | Rinnova con POST /api/auth/login |
| 403 | Permessi insufficienti | Verifica ruolo utente o claim i3x_write |
| 404 | Risorsa non trovata | Verifica ID e org_id |
| 429 | Rate limit superato | Attendi ed esegui retry con backoff esponenziale (2s, 4s, 8s) |
| 503 | MQTT non connesso | Solo per PUT write — broker offline |
| 500 | Errore server | Logga e riprova dopo 30 secondi |

Tutte le risposte di errore:
```json
{"error": "descrizione"}
```

Errori i3X (formato esteso):
```json
{"code": "FORBIDDEN", "message": "i3X write permission required"}
```

> Le risposte includono ora header di sicurezza standard (`X-Frame-Options`, `X-Content-Type-Options`, ecc.).
> I dettagli degli errori interni (path DB, stack trace) non vengono mai esposti nelle response — sono loggati solo server-side.

---

## 11. Domande tipiche dell'operatore — risposte rapide

Queste sono le domande che un operatore o un on-call fanno mentre stanno
guardando OpenEdge. Per ognuna l'agente fa **una sola chiamata**,
estrae il dato, risponde.

### "Ci sono allarmi attivi adesso?"

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/i3x/v1/alarms \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('items', [])
crit = [a for a in items if a['severity'] == 'Critical']
warn = [a for a in items if a['severity'] == 'Warning']
print(f'{len(items)} allarmi attivi: {len(crit)} critical, {len(warn)} warning')
for a in crit:
    print(f'  🔴 {a[\"propertyName\"]} ({a[\"equipmentName\"]}) — {a[\"message\"]} — dalle {a[\"triggerTime\"]}')"
```

### "Quando è scattato l'ultimo critical?"

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/i3x/v1/alarms/history?limit=200" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
crits = [a for a in d.get('items', []) if a['severity'] == 'Critical']
if not crits:
    print('Nessun critical negli ultimi record')
else:
    a = crits[0]  # endpoint ritorna più recenti per primi
    print(f'Ultimo critical: {a[\"propertyName\"]} alle {a[\"triggerTime\"]} — {a[\"message\"]}')"
```

### "Quanti allarmi nelle ultime 24h e di che tipo?"

Usa il digest AI-Ops — una chiamata, risposta pronta da leggere:

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/aiops/alarms/digest?hours=24" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f'{d[\"total_fired\"]} allarmi nelle {d[\"period_hours\"]}h — {d[\"still_active\"]} ancora attivi, {d[\"cleared\"]} risolti')
for sev, n in d['by_severity'].items():
    if n: print(f'  {sev}: {n}')"
```

### "Tutti i gateway sono online?"

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/gateways \
  | python3 -c "
import sys, json
gws = json.load(sys.stdin)
off = [g for g in gws if g.get('connection_status') == 'offline']
unk = [g for g in gws if g.get('connection_status') == 'unknown']
on  = [g for g in gws if g.get('connection_status') == 'online']
print(f'{len(on)}/{len(gws)} gateway online')
for g in off: print(f'  ❌ {g[\"name\"]} offline')
for g in unk: print(f'  ⚠ {g[\"name\"]} unknown (driver sta partendo)')"
```

### "Il sistema sta funzionando? Postgres e Redis ok?"

```bash
curl -s http://$OPENEDGE_HOST:$OPENEDGE_PORT/ready
# Atteso: {"status":"ready","db":"ok","redis":"ok"}
```

Se uno dei due non è `ok`, è un problema di infrastruttura — passa a
`openedge-ops.md` per il fix.

### "Qual è il valore corrente di [tag X]?"

```bash
# Se conosci il tag_id (es. 42):
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/tags/42/current
# {"tag_id":42,"alias":"Portata_Ingresso","value":42.5,"timestamp":...,"quality":0}
```

Se ricevi `quality: 2` (Bad), il gateway è offline o il driver ha
perso il PLC — passa a `openedge-ops.md`.

### "Mostrami gli ultimi 60 min di [tag X]"

```bash
NOW=$(date +%s)000
PAST=$(( NOW - 60*60*1000 ))
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/history/stats?tag_id=42&start=$PAST&end=$NOW"
# Ritorna avg/min/max/std-dev/sample_count del periodo
```

### "Che turno è adesso? Chi è di servizio?"

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/shifts/current \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
if not d.get('shift'):
    print('Nessun turno in corso (orario fuori range / giorno non coperto)'); sys.exit(0)
s = d['shift']
h, m = divmod(d['time_left_min'], 60)
ops = ', '.join(o['username'] for o in d['operators']) or '(nessun operatore designato)'
print(f'Turno: {s[\"name\"]} ({s[\"start_time\"]}-{s[\"end_time\"]})')
print(f'Restano: {h}h {m}m')
print(f'Operatori: {ops}')"
```

### "Siamo in manutenzione adesso? Le notifiche sono silenziate?"

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/maintenance?active=true" \
  | python3 -c "
import sys, json
ws = json.load(sys.stdin)
if not ws:
    print('Nessuna manutenzione attiva — notifiche regolari'); sys.exit(0)
for w in ws:
    print(f'⚠ Manutenzione: {w[\"title\"]} fino a {w[\"end_at\"]}')
    if w.get('reason'): print(f'  Motivazione: {w[\"reason\"]}')
print()
print('Email/Telegram NON escono finché c''è una finestra attiva.')"
```

### "Quanti allarmi sono scattati durante il turno corrente?"

Il backend lo calcola già lato dashboard:

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
sh = d.get('shift')
if not sh:
    print('Nessun turno in corso'); sys.exit(0)
print(f'Turno \"{sh[\"name\"]}\": {sh[\"alarms_this_shift\"]} allarmi scattati')"
```

### "C'è qualcosa di anomalo rispetto al normale?"

Per il tag su cui hai il sospetto, usa l'anomaly detector AI-Ops
(Z-score con baseline a 30 giorni):

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/aiops/anomalies?tag_id=42&window_hours=168&baseline_days=30"
```

Se `anomaly_count > 0`, riporta i bucket con `|z_score| >= 2.5`.

### "Quali KPI di produzione abbiamo configurato? Valori correnti?"

I custom KPI (pezzi/turno, kWh, OEE component, ecc.) appaiono nello
stesso `kpi[]` del dashboard overview con `key` prefissato da `custom_`.
Una sola call ritorna sistema + custom KPI insieme:

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
customs = [k for k in d['kpi'] if k['key'].startswith('custom_')]
if not customs:
    print('Nessun KPI di produzione configurato. Definiscilo con POST /api/custom-kpis.'); sys.exit(0)
print(f'KPI di produzione configurati ({len(customs)}):')
for k in customs:
    arrow = '↑' if k['trend']=='up' else '↓' if k['trend']=='down' else '→'
    delta = f' {arrow} {abs(k[\"delta_pct\"]):.0f}%' if k['trend']!='flat' else ''
    target = ''
    if k.get('target') is not None:
        sign = '≤' if k['good_when']=='down' else '≥'
        ok = '✓' if k.get('target_met') else '✗'
        target = f' (target {sign} {k[\"target\"]} {ok})'
    print(f'  {k[\"label\"]}: {k[\"value\"]}{k[\"unit\"]}{delta}{target}')"
```

---

## 11b. Dashboard overview — KPI in una sola call

Per rispondere a "come va il sistema in generale?" senza fare N query
separate, usa **`GET /api/dashboard/overview`**. Una chiamata → tutto
quello che la pagina Cruscotto mostra:

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview
```

Response shape:
```json
{
  "generated_at": "2026-06-04T07:00:00Z",
  "system":     {"ready": true, "db_ok": true, "api_uptime_sec": 86400},
  "alarms":     {
    "active_by_level": {"critical": 1, "high": 3, "medium": 8, "low": 0},
    "active_total": 12, "last_24h_fired": 45,
    "trend_7d": [{"bucket": "...", "count": 5}, ...],
    "recent_top5": [...]
  },
  "gateways":   {"online": 12, "offline": 0, "unknown": 0},
  "operations": {
    "notif_email_enabled": true, "notif_telegram_enabled": false,
    "notif_min_severity": "high",
    "recipe_loads_24h": 4, "writes_24h": 28, "logins_24h": 6
  },
  "activity":   [/* 12 eventi recenti fusi (alarm/recipe/write/login) */],
  "kpi": [
    {"key":"alarms_per_day", "label":"Allarmi al giorno",
     "value":6.4, "unit":"/g", "trend":"down", "delta_pct":-18,
     "good_when":"down"},
    ...
  ]
}
```

### Risposta sintetica con un solo Python inline

```bash
curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
a = d['alarms']['active_by_level']
ops = d['operations']
print(f'Sistema: {\"OK\" if d[\"system\"][\"ready\"] else \"DOWN\"} (uptime {d[\"system\"][\"api_uptime_sec\"]//3600}h)')
print(f'Allarmi attivi: {a[\"critical\"]} critical, {a[\"high\"]} high, {a[\"medium\"]} medium')
print(f'Ultime 24h: {ops[\"recipe_loads_24h\"]} ricette, {ops[\"writes_24h\"]} write PLC, {ops[\"logins_24h\"]} login')
print()
print('KPI principali:')
for k in d['kpi']:
    arrow = '↑' if k['trend']=='up' else '↓' if k['trend']=='down' else '→'
    good = ((k['trend']=='up' and k['good_when']=='up') or (k['trend']=='down' and k['good_when']=='down'))
    sign = '✓' if good or k['trend']=='flat' else '⚠'
    print(f'  {sign} {k[\"label\"]}: {k[\"value\"]}{k[\"unit\"]} {arrow} {abs(k[\"delta_pct\"]):.0f}%')"
```

Output esempio:
```
Sistema: OK (uptime 24h)
Allarmi attivi: 1 critical, 3 high, 8 medium
Ultime 24h: 4 ricette, 28 write PLC, 6 login

KPI principali:
  ✓ Allarmi al giorno: 6.4/g ↓ 18%
  ⚠ Critical attivi: 1 → 0%
  ✓ Write PLC (24h): 28 ↑ 22%
  ✓ Ricette caricate (24h): 4 ↑ 33%
  → Login (24h): 6 → 0%
  ✓ Tag in errore (1h): 0 → 0%
```

### KPI disponibili in `kpi[]`

| key | Cosa misura | `good_when` |
|---|---|---|
| `alarms_per_day` | Media giornaliera ultimi 7gg | down (meno è meglio) |
| `open_critical` | Critical attivi adesso (snapshot) | down |
| `writes_24h` | Comandi write PLC nelle 24h | up (più attività = più uso) |
| `recipe_loads_24h` | Ricette caricate nelle 24h | up |
| `logins_24h` | Login operatori nelle 24h | flat (informativo) |
| `bad_quality_1h` | Tag con quality > 0 nell'ultima ora | down (meno è meglio) |

Trend: `up` se delta% > +2, `down` se < -2, `flat` altrimenti (sotto il
rumore). `delta_pct` confronta col periodo immediatamente precedente
della stessa durata.


Sul host che ospita OpenEdge (o su un secondo host con accesso di rete),
imposta cron job che chiamano gli endpoint sopra e notificano via
email / Telegram / webhook quando trovano qualcosa.

### Setup base

```bash
# 1. Crea un file con le credenziali (modo 600, mai in git)
cat > /etc/openedge-monitor.env <<EOF
OPENEDGE_HOST=localhost
OPENEDGE_PORT=8081
OPENEDGE_USERNAME=admin
OPENEDGE_PASSWORD=<la-tua-password>
OPENEDGE_ORG_ID=1
EOF
chmod 600 /etc/openedge-monitor.env

# 2. Script helper che ottiene un token fresco a ogni esecuzione
cat > /usr/local/bin/openedge-token.sh <<'EOF'
#!/bin/bash
set -e
source /etc/openedge-monitor.env
curl -s -X POST "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$OPENEDGE_USERNAME\",\"password\":\"$OPENEDGE_PASSWORD\"}" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])"
EOF
chmod +x /usr/local/bin/openedge-token.sh
```

### Cron #1 — alert su allarmi critical attivi (ogni 5 min)

```bash
# /usr/local/bin/openedge-check-critical.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

CRIT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/i3x/v1/alarms" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
crits = [a for a in d.get('items', []) if a['severity'] == 'Critical']
if not crits: sys.exit(0)
for a in crits:
    print(f'[CRITICAL] {a[\"propertyName\"]} ({a[\"equipmentName\"]}) — {a[\"message\"]} — {a[\"triggerTime\"]}')")

if [ -n "$CRIT" ]; then
    # Sostituisci con il tuo canale di notifica
    echo "$CRIT" | mail -s "[OpenEdge] Critical alarms attivi" oncall@azienda.it
    # oppure: webhook Slack/Telegram:
    # curl -s -X POST -d "$CRIT" "https://hooks.slack.com/services/XXX/YYY/ZZZ"
fi
```

Crontab:
```cron
*/5 * * * * /usr/local/bin/openedge-check-critical.sh >> /var/log/openedge-monitor.log 2>&1
```

### Cron #2 — alert su gateway offline (ogni 15 min)

```bash
# /usr/local/bin/openedge-check-gateways.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

OFF=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/gateways" \
  | python3 -c "
import sys, json
gws = json.load(sys.stdin)
off = [g for g in gws if g.get('connection_status') == 'offline']
if not off: sys.exit(0)
for g in off:
    print(f'[OFFLINE] gateway \"{g[\"name\"]}\" (id={g[\"id\"]}, type={g[\"driver_type\"]})')")

if [ -n "$OFF" ]; then
    echo "$OFF" | mail -s "[OpenEdge] Gateway offline" oncall@azienda.it
fi
```

Crontab:
```cron
*/15 * * * * /usr/local/bin/openedge-check-gateways.sh >> /var/log/openedge-monitor.log 2>&1
```

### Cron #3 — daily report (ogni giorno alle 07:00)

```bash
# /usr/local/bin/openedge-daily-report.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

REPORT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/aiops/alarms/digest?hours=24" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f'OpenEdge — sommario 24h')
print(f'Allarmi totali: {d[\"total_fired\"]} ({d[\"still_active\"]} attivi, {d[\"cleared\"]} risolti)')
for sev, n in d['by_severity'].items():
    if n: print(f'  {sev}: {n}')
print()
print('Top 5 ultimi allarmi:')
for a in d['alarms'][:5]:
    print(f'  [{a[\"severity\"]}] {a[\"tag_alias\"]} alle {a[\"trigger_time\"]} — {a[\"message\"]}')")

echo "$REPORT" | mail -s "[OpenEdge] Daily report $(date +%F)" team@azienda.it
```

Crontab:
```cron
0 7 * * * /usr/local/bin/openedge-daily-report.sh >> /var/log/openedge-monitor.log 2>&1
```

### Cron #4 — health probe (ogni minuto)

```bash
# /usr/local/bin/openedge-health.sh
#!/bin/bash
source /etc/openedge-monitor.env
if ! curl -fsS -m 3 "http://$OPENEDGE_HOST:$OPENEDGE_PORT/ready" >/dev/null; then
    echo "[$(date)] /ready NOT responding" | tee -a /var/log/openedge-health.log
    # Page on-call
    curl -s -X POST -d 'OpenEdge /ready down' "https://events.pagerduty.com/integration/XXX/enqueue"
fi
```

Crontab:
```cron
* * * * * /usr/local/bin/openedge-health.sh
```

### Cron #5 — KPI digest mattutino (ogni giorno alle 06:30)

Usa l'endpoint unificato `/api/dashboard/overview` per inviare un sommario
KPI giornaliero a chi gestisce l'impianto. Una sola call, output denso.

```bash
# /usr/local/bin/openedge-kpi-digest.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

REPORT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
a = d['alarms']
o = d['operations']
sys_ok = d['system']['ready']
print('OpenEdge — sommario giornaliero')
print(f'Sistema: {\"OK\" if sys_ok else \"DOWN\"}  Uptime: {d[\"system\"][\"api_uptime_sec\"]//3600}h')
print(f'Allarmi attivi: {a[\"active_total\"]} ({a[\"active_by_level\"][\"critical\"]} critical, {a[\"active_by_level\"][\"high\"]} high)')
print(f'Allarmi nelle 24h: {a[\"last_24h_fired\"]} totali')
print()
print('Attività operatori (24h):')
print(f'  Ricette caricate: {o[\"recipe_loads_24h\"]}')
print(f'  Write PLC:        {o[\"writes_24h\"]}')
print(f'  Login:            {o[\"logins_24h\"]}')
print()
print('KPI con variazione rispetto al periodo precedente:')
for k in d['kpi']:
    arrow = '↑' if k['trend']=='up' else '↓' if k['trend']=='down' else '→'
    if k['trend'] == 'flat': continue
    print(f'  {k[\"label\"]}: {k[\"value\"]}{k[\"unit\"]} {arrow} {abs(k[\"delta_pct\"]):.0f}%')
")

echo "$REPORT" | mail -s "[OpenEdge] KPI digest $(date +%F)" team@azienda.it
# o Telegram bot:
# curl -s -X POST "https://api.telegram.org/bot<TOKEN>/sendMessage" \
#   --data-urlencode "chat_id=<CHAT>" \
#   --data-urlencode "text=$REPORT"
```

Crontab:
```cron
30 6 * * * /usr/local/bin/openedge-kpi-digest.sh >> /var/log/openedge-monitor.log 2>&1
```

### Cron #6 — soglia KPI custom (controllo per-business-rule)

Quando l'utente dice *"avvisami se gli allarmi al giorno superano N"* o
*"avvisami se i tag in errore superano 5"* — leggi il KPI specifico dalla
response e confrontalo con la soglia.

```bash
# /usr/local/bin/openedge-kpi-threshold.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)
THRESHOLD=10   # personalizza per il tuo impianto

ALARMS=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
for k in d['kpi']:
    if k['key'] == 'alarms_per_day':
        print(int(k['value']))
        break")

if [ "$ALARMS" -gt "$THRESHOLD" ]; then
    echo "[ALERT] Allarmi giornalieri = $ALARMS (soglia $THRESHOLD)" \
      | mail -s "[OpenEdge] Soglia KPI superata" oncall@azienda.it
fi
```

Crontab (ogni 4 ore):
```cron
0 */4 * * * /usr/local/bin/openedge-kpi-threshold.sh >> /var/log/openedge-monitor.log 2>&1
```

### Cron #7 — report fine turno automatico

Quando finisce un turno (es. alle 14:00 per il Mattina, 22:00 per il
Pomeriggio, 06:00 per il Notte), invia un riassunto agli operatori
designati: nome turno, durata, allarmi scattati, ricette caricate.

Approccio semplice: cron multipli, uno per ora di fine turno. Niente
schedulazione dinamica — basta riflettere nel crontab gli orari dei
tuoi turni reali.

```bash
# /usr/local/bin/openedge-shift-end-report.sh
#!/bin/bash
set -e
source /etc/openedge-monitor.env
TOKEN=$(/usr/local/bin/openedge-token.sh)

# Il turno che è FINITO un attimo fa (NOW() - 1min cade ancora dentro)
# verrà ritornato come "current" se chiamiamo qualche secondo prima del
# fine. Se chiamiamo dopo il fine, /current può essere null — quindi
# usiamo /api/dashboard/overview che cattura `shift` quando il cron
# parte ESATTAMENTE all'orario fine (sotto un minuto è ok).
REPORT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/dashboard/overview" \
  | python3 -c "
import sys, json
d = json.load(sys.stdin)
sh = d.get('shift')
if not sh:
    sys.exit(0)
ops = d['operations']
print(f'OpenEdge — fine turno {sh[\"name\"]}')
print(f'Allarmi scattati nel turno: {sh[\"alarms_this_shift\"]}')
print(f'Operatori in servizio: {\", \".join(sh.get(\"operators\", [])) or \"-\"}')
print()
print('Attività durante il turno (ultime 24h, approssimazione):')
print(f'  Ricette caricate: {ops[\"recipe_loads_24h\"]}')
print(f'  Write PLC: {ops[\"writes_24h\"]}')")

[ -z "$REPORT" ] && exit 0
echo "$REPORT" | mail -s "[OpenEdge] Fine turno" capoturno@azienda.it
```

Crontab — un'entry per ogni ora di fine dei tuoi turni:

```cron
# Mattina finisce alle 14:00
59 13 * * 1-5 /usr/local/bin/openedge-shift-end-report.sh >> /var/log/openedge-monitor.log 2>&1
# Pomeriggio finisce alle 22:00
59 21 * * 1-5 /usr/local/bin/openedge-shift-end-report.sh >> /var/log/openedge-monitor.log 2>&1
# Notte finisce alle 06:00
59 5 * * 2-6 /usr/local/bin/openedge-shift-end-report.sh >> /var/log/openedge-monitor.log 2>&1
```

(Notte di venerdì→sabato finisce sabato mattina, quindi il cron del
turno notte è sabato come "day-of-week" — adatta ai tuoi turni.)

### Anti-spam — dedupe ed escalation

Tutti gli script qui sopra **rinotificano** ogni esecuzione finché la
condizione persiste. Per evitare spam:

- **Idempotenza con marker file**: salva l'id dell'ultimo alarm
  notificato e notifica di nuovo solo se cambia.
- **Escalation a stadi**: prima warning email, dopo 30 min escala a SMS.
- **Soppressione orari**: aggiungi al cron `&& [ $(date +%H) -ge 7 -a $(date +%H) -le 22 ]` per non svegliare nessuno alle 03:00 (a meno che non sia critical).

Esempio dedupe per i critical:

```bash
STATE_FILE=/var/lib/openedge-monitor/last-critical-ids
mkdir -p $(dirname "$STATE_FILE")
TOKEN=$(/usr/local/bin/openedge-token.sh)
CURRENT=$(curl -s -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $OPENEDGE_ORG_ID" \
  "http://$OPENEDGE_HOST:$OPENEDGE_PORT/api/i3x/v1/alarms" \
  | python3 -c "
import sys, json
print(','.join(sorted(a['id'] for a in json.load(sys.stdin).get('items', []) if a['severity']=='Critical')))")
PREV=$(cat "$STATE_FILE" 2>/dev/null || echo "")
echo "$CURRENT" > "$STATE_FILE"
[ "$CURRENT" = "$PREV" ] && exit 0   # niente cambiato → niente notifica
# ... resto invio notifica ...
```

---

## 13. Flusso operativo riassunto per l'agente monitor

```
Domanda dell'utente: "qualcosa non va?"
   │
   ▼
1. GET /ready                                   → sistema up?
2. GET /api/i3x/v1/alarms                       → critical attivi?
3. GET /api/gateways                            → gateway offline?
4. GET /api/aiops/summary?hours=1               → tag senza sample / sotto media?
   │
   ▼
Sintesi:
  ✅ tutto ok → "Nessun problema rilevato negli ultimi 60 min"
  ⚠ warning → elenca i gateway/tag con problemi, suggerisci a cosa guardare
  🔴 critical → riporta titolo + timestamp, richiama l'umano (non risolvere da solo)
```

L'agente monitor **non scrive** sui PLC e **non riavvia** servizi. Quei
gesti sono riservati alla skill `openedge-ops.md`.

---

*Aggiornare questo file quando vengono aggiunti nuovi endpoint a OpenEdge.*
