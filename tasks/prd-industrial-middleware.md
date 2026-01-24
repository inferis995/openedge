# PRD: Industrial Edge Middleware - Multi-Tenant Data Acquisition System

## 1. Introduction/Overview

Sistema middleware containerizzato (Docker) per l'acquisizione, normalizzazione e gestione di dati industriali da PLC multipli. Il sistema astrae l'hardware di campo, normalizza i dati in formato JSON standard, gestisce una gerarchia organizzativa multi-tenant, esegue logiche di allarmistica e storicizzazione a bordo (Edge), ed è configurabile interamente via REST API con Single Source of Truth.

Il sistema è progettato per integratori di sistemi che devono configurare e gestire installazioni industriali per clienti multipli, con requisiti di elevata scalabilità, ottimizzazione delle risorse e minima manutenzione.

## 2. Goals

- Astrarre completamente l'hardware di campo (PLC S7, Modbus TCP) mediante un'interfaccia di configurazione unificata
- Normalizzare tutti i dati in formato JSON standardizzato con timestamp UTC e quality indicator
- Supportare una gerarchia organizzativa complessa multi-tenant (Organization → Site → Area → Gateway → Tag)
- Fornire una REST API come Single Source of Truth per tutta la configurazione di sistema
- Eseguire logiche di allarmistica in tempo reale con state machine completa
- Storizzare dati time-series con ottimizzazioni deadband e batch write
- Garantire elevata scalabilità con minimo consumo di risorse (Go + Docker)
- Supportare configurazione dinamica senza riavvio dei servizi
- Ottimizzare il traffico di rete con Report by Exception (RBE) e polling intelligente

## 3. User Stories

### US-001: Create multi-tenant organizational structure

**Description:** Come integratore di sistemi, voglio definire una gerarchia organizzativa (Organizzazione → Site → Area) così da poter separare logicamente diversi clienti/impianti.

**Acceptance Criteria:**

- [ ] API POST /api/organizations crea una nuova organizzazione
- [ ] API POST /api/sites crea un site associato a un'organizzazione
- [ ] API POST /api/areas crea un'area associata a un site
- [ ] Il database enforcea l'integrità referenziale (Foreign Keys)
- [ ] Non è possibile eliminare un'organizzazione se ha sites associati
- [ ] Typecheck passes (Go code compiles without errors)

### US-002: Configure S7 PLC gateway

**Description:** Come integratore di sistemi, voglio configurare un gateway PLC Siemens S7 (IP, rack, slot) così da acquisire dati dall'automazione di campo.

**Acceptance Criteria:**

- [ ] API POST /api/gateways crea un nuovo gateway con driver_type="S7"
- [ ] Il campo connection_config contiene JSONB con {ip, rack, slot}
- [ ] Il campo scan_rate_ms definisce la frequenza di polling (default 1000ms)
- [ ] Il campo enabled permette di disabilitare il gateway senza eliminarlo
- [ ] Typecheck passes
- [ ] Verifica tramite API call che il gateway venga salvato correttamente

### US-003: Configure Modbus TCP gateway

**Description:** Come integratore di sistemi, voglio configurare un gateway Modbus TCP (IP, slave ID) così da acquisire dati da dispositivi compatibili.

**Acceptance Criteria:**

- [ ] API POST /api/gateways crea un nuovo gateway con driver_type="MODBUS_TCP"
- [ ] Il campo connection_config contiene JSONB con {ip, slave_id, port}
- [ ] Il campo scan_rate_ms definisce la frequenza di polling
- [ ] Typecheck passes
- [ ] Verifica tramite API call che il gateway venga salvato correttamente

### US-004: Define data tags with metadata

**Description:** Come integratore di sistemi, voglio definire i tag (punti dati) da acquisire con alias semantico, tipo dato e configurazioni di storico/allarme così da normalizzare i dati industriali.

**Acceptance Criteria:**

- [ ] API POST /api/tags crea un nuovo tag associato a un gateway
- [ ] Il campo code identifica l'indirizzo nel PLC (es. "DB1.DBW0" per S7)
- [ ] Il campo alias fornisce un nome semantico (es. "Temp_Forno")
- [ ] Il campo data_type supporta: "INT", "REAL", "BOOL", "DINT"
- [ ] I campi historize, alarm_enabled, alarm_threshold sono configurabili
- [ ] Typecheck passes

### US-005: Driver S7 reads and publishes data

**Description:** Come sistema, il driver S7 deve leggere i dati dal PLC secondo la configurazione e pubblicarli su MQTT con timestamp e quality.

**Acceptance Criteria:**

- [ ] Il driver si connette al PLC S7 usando IP, rack, slot dalla configurazione
- [ ] Il driver esegue polling ottimizzato (massimo byte in una sola PDU)
- [ ] I dati vengono pubblicati su topic: data/{org}/{site}/{area}/{gateway}/{alias}
- [ ] Il payload JSON contiene: {v: value, ts: timestamp_ms, q: quality}
- [ ] Il timestamp è in UTC millisecondi
- [ ] La qualità è 1 (Good) o 0 (Bad)
- [ ] Report by Exception: pubblica solo se il valore è cambiato
- [ ] Typecheck passes

### US-006: Driver Modbus reads and publishes data

**Description:** Come sistema, il driver Modbus TCP deve leggere i dati dal dispositivo secondo la configurazione e pubblicarli su MQTT.

**Acceptance Criteria:**

- [ ] Il driver si connette al dispositivo Modbus usando IP, slave_id, port
- [ ] Il driver supporta registri Holding Input (tipi INT, REAL)
- [ ] I dati vengono pubblicati su topic: data/{org}/{site}/{area}/{gateway}/{alias}
- [ ] Il payload JSON segue lo standard {v, ts, q}
- [ ] Report by Exception è abilitato
- [ ] Typecheck passes

### US-007: Dynamic configuration reload

**Description:** Come integratore di sistemi, voglio che le modifiche alla configurazione vengano applicate ai driver senza riavvio manuale così da garantire continuità di servizio.

**Acceptance Criteria:**

- [ ] Quando un tag/gateway viene modificato via API, core-api pubblica su sys/command/reload
- [ ] Il driver riceve il comando MQTT e ricarica la configurazione da PostgreSQL
- [ ] Il driver aggiorna la sua mappa interna in RAM senza interrompere il polling
- [ ] Nuovi tag vengono aggiunti al ciclo di scansione
- [ ] Tag disabilitati vengono rimossi dal ciclo di scansione
- [ ] Typecheck passes

### US-008: Real-time alarm detection

**Description:** Come sistema, voglio rilevare allarmi in tempo reale confrontando i valori con le soglie configurate così da generare notifiche immediate.

**Acceptance Criteria:**

- [ ] engine-alarm sottoscrive a data/#
- [ ] Per ogni valore ricevuto, verifica se alarm_enabled=true per il tag
- [ ] Confronta il valore con alarm_threshold usando alarm_operator (>, <, =)
- [ ] Pubblica su events/alarms con payload {tag, state, msg, ts}
- [ ] Gli stati allarme sono: ACTIVE, RTN (Return to Normal)
- [ ] Lo stato degli allarmi attivi è mantenuto in Redis
- [ ] Typecheck passes

### US-009: Alarm state machine with acknowledgment

**Description:** Come operatore, voglio poter riconoscere gli allarmi attivi così da tracciare quali sono stati presi in visione.

**Acceptance Criteria:**

- [ ] API POST /api/alarms/{id}/acknowledge per riconoscere un allarme
- [ ] Lo stato dell'allarme transisce: ACTIVE → ACKNOWLEDGED → CLEAR
- [ ] La tabella alarms in PostgreSQL traccia lo stato e i timestamp
- [ ] Gli allarmi riconosciuti sono distinguibili da quelli attivi
- [ ] Typecheck passes
- [ ] Verifica tramite API call

### US-010: Time-series data historization with deadband

**Description:** Come sistema, voglio storizzare i dati time-series applicando deadband per ridurre lo spazio disco e ottimizzare le performance.

**Acceptance Criteria:**

- [ ] engine-historian sottoscrive a data/#
- [ ] Per ogni tag con historize=true, accumula i valori in un buffer
- [ ] Applica la logica deadband: scarta il valore se |valore_prec - valore_new| < historize_deadband
- [ ] Esegue batch write su InfluxDB quando il buffer raggiunge 1000 punti o dopo 1 secondo
- [ ] I dati vengono scritti nel bucket specificato con organization come tag
- [ ] Typecheck passes

### US-011: Query historical data

**Description:** Come integratore di sistemi, voglio interrogare i dati storici per generare report e analisi trend.

**Acceptance Criteria:**

- [ ] API GET /api/history?tag_id={id}&start={iso}&end={iso} restituisce i dati storici
- [ ] I risultati includono timestamp, valore e qualità
- [ ] Supporta aggregazioni (mean, min, max, avg) con specifica intervallo
- [ ] Typecheck passes
- [ ] Verifica tramite API call

### US-012: Multi-tenant data isolation

**Description:** Come integratore di sistemi, voglio che i dati di ogni organizzazione siano isolati così da garantire separazione logica tra clienti.

**Acceptance Criteria:**

- [ ] I topic MQTT includono il nome organizzazione: data/{org}/...
- [ ] Le query API filtrano automaticamente per organization dell'utente autenticato
- [ ] Non è possibile accedere ai dati di un'altra organizzazione
- [ ] InfluxDB usa organization come tag per separare i dati
- [ ] Typecheck passes

### US-013: Gateway health monitoring

**Description:** Come operatore, voglio monitorare lo stato di salute dei gateway (connessi/disconnessi) così da identificare rapidamente problemi di comunicazione.

**Acceptance Criteria:**

- [ ] Ogni gateway pubblica uno stato LWT (Last Will Testament) su sys/health/{gateway_id}
- [ ] Lo stato è "online" o "offline"
- [ ] API GET /api/gateways restituisce il campo connection_status
- [ ] L'ultimo timestamp di comunicazione viene tracciato
- [ ] Typecheck passes
- [ ] Verifica tramite API call

### US-014: Driver lifecycle management

**Description:** Come sistema, driver-manager deve avviare/fermare i container driver in base alla configurazione del database.

**Acceptance Criteria:**

- [ ] driver-manager polla PostgreSQL per identificare gateway con enabled=true
- [ ] Per ogni gateway, avvia il container driver appropriato (driver-s7 o driver-modbus)
- [ ] Se un gateway viene disabilitato, il container driver viene fermato
- [ ] Il log delle operazioni viene scritto su stdout e tracciato in Docker
- [ ] Typecheck passes

### US-015: Redis cache for real-time values

**Description:** Come sistema, voglio mantenere gli ultimi valori in Redis per accesso rapido senza interrogare InfluxDB.

**Acceptance Criteria:**

- [ ] engine-historian aggiorna la chiave Redis realtime:{tag_id} ad ogni nuovo valore
- [ ] Il valore TTL è configurabile (default 60 giorni)
- [ ] API GET /api/tags/{id}/current restituisce il valore da Redis
- [ ] Typecheck passes
- [ ] Verifica tramite API call

## 4. Functional Requirements

### Organizational Structure
- FR-1: Il sistema deve supportare una gerarchia a 4 livelli: Organization → Site → Area → Gateway
- FR-2: Ogni Gateway appartiene a esattamente un'Area
- FR-3: Ogni Tag appartiene a esattamente un Gateway
- FR-4: L'integrità referenziale deve essere enforceata dal database

### Gateway Configuration
- FR-5: Il sistema deve supportare driver S7 e Modbus TCP
- FR-6: La configurazione di connessione deve essere memorizzata come JSONB
- FR-7: Lo scan rate deve essere configurabile per gateway (default 1000ms)
- FR-8: I gateway devono poter essere disabilitati senza rimozione

### Data Acquisition
- FR-9: I driver devono eseguire polling ottimizzato (massimi byte per PDU)
- FR-10: Il timestamp deve essere in UTC millisecondi
- FR-11: Il payload JSON deve seguire il formato {v: value, ts: timestamp_ms, q: quality}
- FR-12: I topic MQTT devono seguire il pattern: data/{org}/{site}/{area}/{gateway}/{alias}
- FR-13: Report by Exception deve essere abilitato di default

### Alarm Management
- FR-14: Il sistema deve supportare soglie con operatori: >, <, =
- FR-15: Gli stati allarme devono essere: ACTIVE, RTN, ACKNOWLEDGED, CLEAR
- FR-16: Gli allarmi attivi devono essere tracciati in Redis
- FR-17: Gli eventi allarme devono essere pubblicati su events/alarms
- FR-18: La priorità allarme deve essere configurabile (1-5)

### Historization
- FR-19: La storizzazione deve essere basata su flag historize per tag
- FR-20: Il deadband deve essere applicato prima della scrittura
- FR-21: Il batch write deve accumulare 1000 punti o 1 secondo
- FR-22: InfluxDB deve usare organization come tag per multi-tenancy

### Configuration Management
- FR-23: Tutta la configurazione deve avvenire via REST API
- FR-24: Le modifiche alla configurazione devono innescare reload dei driver via MQTT
- FR-25: I driver devono supportare hot-reload senza riavvio

### API Endpoints
- FR-26: POST /api/organizations - Crea organizzazione
- FR-27: GET /api/organizations - Lista organizzazioni
- FR-28: POST /api/sites - Crea site
- FR-29: GET /api/sites?org_id={id} - Lista sites per organizzazione
- FR-30: POST /api/areas - Crea area
- FR-31: GET /api/areas?site_id={id} - Lista aree per site
- FR-32: POST /api/gateways - Crea gateway
- FR-33: GET /api/gateways?area_id={id} - Lista gateway per area
- FR-34: PUT /api/gateways/{id} - Aggiorna gateway
- FR-35: POST /api/tags - Crea tag
- FR-36: GET /api/tags?gateway_id={id} - Lista tag per gateway
- FR-37: PUT /api/tags/{id} - Aggiorna tag
- FR-38: GET /api/history - Query dati storici
- FR-39: GET /api/tags/{id}/current - Valore corrente da Redis
- FR-40: POST /api/alarms/{id}/acknowledge - Riconosci allarme

## 5. Non-Goals (Out of Scope)

### Version 1.0
- Non sono inclusi driver OPC UA, Ethernet/IP, PROFIBUS (solo S7 e Modbus TCP)
- Non è inclusa una UI web/dashboard (solo REST API)
- Non è inclusa autenticazione/autorizzazione avanzata (JWT/OAuth in versione successiva)
- Non sono inclusi workflow di approvazione configurazione
- Non è inclusa una notifica allarmi via email/SMS
- Non sono inclusi analytics avanzati o dashboarding
- Non è incluso un sistema di backup/restore automatico
- Non è incluso un sistema di deployment distribuito (Kubernetes)
- Non sono inclusi script di migrazione dati da sistemi legacy
- Non è inclusa gestione versioning della configurazione (git-like)

### Caratteristiche Esplicitamente Escluse
- Allarmi basati su trend o predittivi
- Calcolo di KPI derivati a bordo
- Gestione di file di ricetta o batch
- Integrazione con MES/ERP
- Logging su syslog centralizzato (in versione successiva)

## 6. Design Considerations

### UI/UX
- Non è prevista UI web per MVP Version 1.0
- L'interfaccia primaria è la REST API
- Documentazione OpenAPI/Swagger deve essere disponibile
- CLI tool opzionale per operazioni comuni

### Convenzioni
- Tutti i timestamp sono UTC in millisecondi (Unix epoch)
- I nomi delle organizzazioni devono essere slug-friendly per topic MQTT
- Gli alias tag devono essere univoci per gateway

### Componenti Riutilizzabili
- Libreria comune per payload JSON {v, ts, q}
- Libreria comune per connessione MQTT client
- Struttura comune per configurazione driver

## 7. Technical Considerations

### Stack Tecnologico Definitivo

**OS Base**
- Debian 12 "Bookworm" Slim o Alpine Linux per container

**Container Runtime**
- Docker Engine + Docker Compose

**Linguaggio Core**
- Go (Golang) per tutti i servizi (drivers, engine, API)
- Motivazione: Goroutines per concorrenza, binari piccoli, RAM efficiente

**Message Broker**
- Eclipse Mosquitto MQTT v5
- Topic namespace: data/, events/, sys/

**Database Relazionale**
- PostgreSQL 16
- Schema: organizations, sites, areas, gateways, tags, alarms

**Cache Layer**
- Redis Stack
- Stato allarmi volatili
- Ultimi valori real-time

**Time-Series Database**
- InfluxDB v2
- Storico dati storici
- Organization as tag

### Architettura Microservizi

**1. core-api** (Port 8080)
- REST API con framework (es. Gin o Echo)
- Validazione richiesta e scrittura PostgreSQL
- Generazione config driver e comando MQTT reload

**2. driver-manager** (Orchestrator)
- Polla PostgreSQL per gateway enabled
- Avvia/ferma container driver
- Lifecycle management

**3. driver-s7 / driver-modbus** (Workers)
- Lettura asincrona da PLC
- Pubblicazione MQTT con RBE
- Configurazione dinamica via sottoscrizione MQTT

**4. engine-alarm** (Watchdog)
- Sottoscrizione data/#
- Confronto soglie in memoria
- State machine allarmi
- Pubblicazione events/alarms

**5. engine-historian** (Archivist)
- Sottoscrizione data/#
- Buffer batch (1000 punti o 1 sec)
- Deadband filtering
- Batch write InfluxDB

### Ottimizzazioni Edge

**Polling Intelligente**
- Driver S7: calcola ottimamente letture multiple in una PDU
- Unpacking byte in memoria invece di chiamate singole

**Report by Exception (RBE)**
- Driver pubblica su MQTT solo se valore cambiato
- Riduzione traffico MQTT del 90%

**Configurazione Dinamica**
- Reload senza riavvio
- Mappa interna RAM aggiornata hot-swap

### Database Schema (PostgreSQL)

```sql
-- Gerarchia
CREATE TABLE organizations (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE sites (
    id SERIAL PRIMARY KEY,
    org_id INT REFERENCES organizations(id),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE areas (
    id SERIAL PRIMARY KEY,
    site_id INT REFERENCES sites(id),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE gateways (
    id SERIAL PRIMARY KEY,
    area_id INT REFERENCES areas(id),
    name TEXT NOT NULL,
    driver_type VARCHAR(20) NOT NULL CHECK (driver_type IN ('S7', 'MODBUS_TCP')),
    connection_config JSONB NOT NULL,
    scan_rate_ms INT DEFAULT 1000,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE tags (
    id SERIAL PRIMARY KEY,
    gateway_id INT REFERENCES gateways(id),
    code VARCHAR(50) NOT NULL,
    alias VARCHAR(100) NOT NULL,
    data_type VARCHAR(20) NOT NULL CHECK (data_type IN ('INT', 'REAL', 'BOOL', 'DINT')),
    historize BOOLEAN DEFAULT FALSE,
    historize_deadband FLOAT DEFAULT 0.0,
    alarm_enabled BOOLEAN DEFAULT FALSE,
    alarm_threshold FLOAT,
    alarm_operator VARCHAR(2) CHECK (alarm_operator IN ('>', '<', '=)),
    alarm_priority INT DEFAULT 1 CHECK (alarm_priority BETWEEN 1 AND 5),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE alarms (
    id SERIAL PRIMARY KEY,
    tag_id INT REFERENCES tags(id),
    state VARCHAR(20) NOT NULL CHECK (state IN ('ACTIVE', 'RTN', 'ACKNOWLEDGED', 'CLEAR')),
    message TEXT,
    triggered_at TIMESTAMPTZ NOT NULL,
    acknowledged_at TIMESTAMPTZ,
    cleared_at TIMESTAMPTZ
);
```

### Docker Compose Structure

```yaml
version: '3.8'
services:
  postgres:
    image: postgres:16
  redis:
    image: redis/redis-stack
  mosquitto:
    image: eclipse-mosquitto:2
  influxdb:
    image: influxdb:2
  core-api:
    build: ./services/core-api
  driver-manager:
    build: ./services/driver-manager
  driver-s7:
    build: ./services/driver-s7
  driver-modbus:
    build: ./services/driver-modbus
  engine-alarm:
    build: ./services/engine-alarm
  engine-historian:
    build: ./services/engine-historian
```

### Dipendenze Go

- `github.com/eclipse/paho.mqtt.golang` - MQTT Client
- `github.com/lib/pq` - PostgreSQL Driver
- `github.com/redis/go-redis/v9` - Redis Client
- `github.com/influxdata/influxdb-client-go/v2` - InfluxDB Client
- `github.com/gin-gonic/gin` - REST API Framework
- `github.com/robinson/gos7` - S7 Driver (or similar)
- `github.com/goburrow/modbus` - Modbus Driver

### Performance Targets

- CPU: < 30% su singolo core per driver con 100 tag @ 1000ms
- RAM: < 100MB per container driver
- Latenza: < 100ms da lettura PLC a pubblicazione MQTT
- Throughput: > 10.000 punti/secondo per engine-historian

### Vincoli

- Single-node deployment (non distribuito)
- Dipendenze esterne minime
- Nessuna GUI nativa
- Configurazione interamente via API

## 8. Success Metrics

### Funzionali
- Un integratore può configurare un nuovo gateway PLC in < 5 minuti
- Il sistema supporta almeno 50 gateway simultanei
- Il sistema supporta almeno 10.000 tag totali
- La latenza end-to-end (PLC → MQTT) è < 100ms al 95° percentile
- Il downtime per configurazione dinamica è 0 (zero restart)

### Performance
- CPU: < 50% su singolo core con 20 gateway attivi
- RAM: < 2GB totali per tutti i container
- Traffico MQTT ridotto del 90% grazie a RBE
- Storage InfluxDB ridotto del 50% grazie a deadband

### Affidabilità
- Uptime > 99.5%
- Riavvio automatico container crash
- Riconnessione automatica PLC persi
- Zero perdita dati allarmi durante crash engine-alarm (grazie a Redis)

### Sviluppo
- Binari Go compilati < 10MB per servizio
- Tempo di build < 2 minuti
- Tempo di deploy < 5 minuti

## 9. Open Questions

### Priorità Alta
- **Q1:** Come gestire le credenziali di accesso ai PLC? (password in chiaro in JSONB o sistema di cifratura?)
- **Q2:** Come gestire il failover di PostgreSQL? (Single node per MVP?)
- **Q3:** Come gestire il failover di InfluxDB? (Single node per MVP?)
- **Q4:** Come gestire la rotazione dei dati storici? (Retention policy InfluxDB?)

### Priorità Media
- **Q5:** È necessario un sistema di logging centralizzato (ELK/Loki)?
- **Q6:** Come gestire il backup del database PostgreSQL?
- **Q7:** È necessario un sistema di health check con notifiche?
- **Q8:** Come gestire il versioning della configurazione?

### Priorità Bassa
- **Q9:** È necessaria una CLI tool per operazioni comuni?
- **Q10:** È necessario supportare tls/mTLS per Mosquitto?
- **Q11:** Come gestire gli aggiornamenti dei container?
- **Q12:** È necessario un sistema di telemetry/metrics (Prometheus)?

### Decisioni Rinviate a V2
- Autenticazione/autorizzazione avanzata (JWT/OAuth)
- Supporto OPC UA
- UI web/dashboard
- Notifiche allarmi via email/SMS
- Deployment distribuito (Kubernetes)
- Analytics avanzati
- Gestione versioning configurazione
