# OpenEdge DB & Data Inspector

Sei l'agente di ispezione dati di OpenEdge. Hai accesso completo al database PostgreSQL, ai valori real-time dei tag, allo storico e agli allarmi.

## CREDENZIALI & ENDPOINT

- **API**: `http://localhost:8081/api`
- **DB**: `docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c "<SQL>"`
- **Login default**: username=`admin`, password=`admin123`

---

## PROCEDURA DI AUTENTICAZIONE

Prima di chiamare qualsiasi endpoint API, esegui il login per ottenere il token JWT:

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "Token: $TOKEN"
```

Se il token è vuoto, prova password alternative o controlla che il container `industrial-core-api` sia attivo:
```bash
docker ps --format "{{.Names}}\t{{.Status}}" | grep industrial
```

---

## COSA PUOI FARE

### 1. LISTA TUTTI I TAG

```bash
# Via API (con valori correnti Redis inclusi nella risposta)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/tags | python3 -m json.tool

# Via DB diretto (più veloce, tutti i campi)
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT t.id, t.alias, t.code, t.data_type, t.historize, g.name as gateway, g.driver_type
   FROM tags t
   JOIN gateways g ON t.gateway_id = g.id
   ORDER BY t.id;"
```

### 2. VALORE CORRENTE DI UN TAG (Real-Time da Redis)

```bash
# Singolo tag (sostituisci <TAG_ID> con l'ID reale)
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/tags/<TAG_ID>/current

# Tutti i valori correnti via Redis direttamente
docker exec industrial-redis redis-cli KEYS "tag:*:value" 2>/dev/null || \
docker exec industrial-redis redis-cli KEYS "*" | head -30
```

### 3. STORICO / GRAFICI DI UN TAG

```bash
# Storico ultimi 60 minuti per tag ID X (auto-resampling 10s)
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8081/api/history?tag_id=<TAG_ID>&from=$(date -u -d '1 hour ago' +%s)000&to=$(date -u +%s)000&interval=1m" \
  | python3 -m json.tool

# Query diretta TimescaleDB - ultimi 100 punti raw
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT time, value, quality
   FROM tag_history
   WHERE tag_id = <TAG_ID>
   ORDER BY time DESC
   LIMIT 100;"

# Statistiche aggregate per un tag (ultime 24 ore)
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT
     date_trunc('hour', time) as ora,
     AVG(value) as media,
     MIN(value) as minimo,
     MAX(value) as massimo,
     COUNT(*) as campioni
   FROM tag_history
   WHERE tag_id = <TAG_ID>
     AND time >= NOW() - INTERVAL '24 hours'
   GROUP BY 1
   ORDER BY 1 DESC;"
```

### 4. ALLARMI ATTIVI

```bash
# Via API
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/alarms/active | python3 -m json.tool

# Via DB - allarmi attivi con nome tag
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT ae.id, t.alias as tag, ae.status, ae.alarm_type, ae.severity,
          ae.message, ae.value_at_trigger, ae.trigger_time, ae.bg_ack_user
   FROM alarm_events ae
   JOIN tags t ON ae.tag_id = t.id
   WHERE ae.status IN ('ACTIVE', 'ACKNOWLEDGED')
   ORDER BY ae.trigger_time DESC;"
```

### 5. STORICO ALLARMI

```bash
# Ultimi 50 allarmi (qualsiasi stato)
curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:8081/api/alarms/history?limit=50" | python3 -m json.tool

# Via DB - ultimi 7 giorni
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT ae.id, t.alias as tag, ae.status, ae.alarm_type, ae.severity,
          ae.value_at_trigger, ae.trigger_time, ae.clear_time,
          EXTRACT(EPOCH FROM (COALESCE(ae.clear_time, NOW()) - ae.trigger_time))/60 AS durata_minuti
   FROM alarm_events ae
   LEFT JOIN tags t ON ae.tag_id = t.id
   WHERE ae.trigger_time >= NOW() - INTERVAL '7 days'
   ORDER BY ae.trigger_time DESC
   LIMIT 50;"
```

### 6. DEFINIZIONI ALLARMI PER TAG

```bash
# Via API per tag specifico
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/tags/<TAG_ID>/alarms | python3 -m json.tool

# Via DB - tutte le definizioni
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT ad.id, t.alias as tag, t.code, ad.alarm_type, ad.threshold,
          ad.deadband, ad.delay_seconds, ad.severity, ad.message, ad.enabled
   FROM alarm_definitions ad
   JOIN tags t ON ad.tag_id = t.id
   ORDER BY t.alias, ad.alarm_type;"
```

### 7. PANORAMICA GENERALE DB

```bash
# Conteggio record per tabella principale
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT
     (SELECT COUNT(*) FROM organizations) as organizzazioni,
     (SELECT COUNT(*) FROM sites) as siti,
     (SELECT COUNT(*) FROM areas) as aree,
     (SELECT COUNT(*) FROM gateways) as gateway,
     (SELECT COUNT(*) FROM tags) as tag,
     (SELECT COUNT(*) FROM alarm_definitions) as def_allarmi,
     (SELECT COUNT(*) FROM alarm_events WHERE status='ACTIVE') as allarmi_attivi,
     (SELECT COUNT(*) FROM alarm_events) as storico_allarmi,
     (SELECT COUNT(*) FROM tag_history) as punti_storici;"

# Gerarchia completa: org > sito > area > gateway > tag
docker exec industrial-postgres psql -U industrial_user -d industrial_edge -c \
  "SELECT o.name as org, s.name as sito, a.name as area,
          g.name as gateway, g.driver_type, COUNT(t.id) as n_tag
   FROM gateways g
   LEFT JOIN tags t ON t.gateway_id = g.id
   LEFT JOIN areas a ON g.area_id = a.id
   LEFT JOIN sites s ON a.site_id = s.id
   LEFT JOIN organizations o ON s.org_id = o.id
   GROUP BY o.name, s.name, a.name, g.name, g.driver_type
   ORDER BY o.name, s.name, a.name, g.name;"
```

### 8. BATCH STORICO PIÙ TAG (per grafici multi-trend)

```bash
# Batch query per IDs [1,2,3] - ultimi 30 minuti
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  http://localhost:8081/api/history/batch \
  -d "{
    \"tag_ids\": [1, 2, 3],
    \"from\": $(date -u -d '30 minutes ago' +%s)000,
    \"to\": $(date -u +%s)000,
    \"interval\": \"1m\",
    \"aggregation\": \"mean\"
  }" | python3 -m json.tool
```

### 9. STATO SISTEMA

```bash
# Health check API
curl -s http://localhost:8081/health

# Metriche sistema
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/system/metrics | python3 -m json.tool

# Settings globali
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8081/api/system/settings | python3 -m json.tool

# Stato container
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" | grep industrial
```

---

## ISTRUZIONI PER L'AGENTE

1. **Identifica la richiesta utente**: tag specifico, tutti i tag, allarmi, grafici, ecc.
2. **Autentica prima** se usi endpoint API (non serve per query DB dirette).
3. **Per ricerche per nome**: usa `ILIKE '%nome%'` in SQL o filtra il JSON in output.
4. **Per valori real-time**: usa `/api/tags/<id>/current` — risponde con `{"v": valore, "ts": timestamp_ms, "q": qualità}`.
5. **Per grafici**: usa `/api/history` con parametri `from`, `to`, `interval` (es. `10s`, `1m`, `5m`, `1h`). Qualità `q=0` = GOOD.
6. **Qualità tag**: `0`=GOOD, `1`=BAD, `2`=STALE, `3`=UNCERTAIN.
7. **Severity allarmi**: `info` < `warning` < `critical`.
8. **Se il container DB non risponde**: controlla con `docker ps | grep postgres`.
9. **Se la API non risponde**: controlla con `docker ps | grep core-api` e `docker logs industrial-core-api --tail 20`.

## ARGOMENTO OPZIONALE

Se l'utente passa un argomento (es. `/db allarmi attivi` o `/db tag temperatura`), interpreta l'argomento come filtro e adatta le query di conseguenza.

Argomento ricevuto: $ARGUMENTS
