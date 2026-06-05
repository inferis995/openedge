# PRD — OpenEdge Multi-Tenant SaaS
**Versione:** 1.0  
**Data:** 2026-06-05  
**Scope:** Trasformazione da installazione single-tenant on-premises a piattaforma SaaS multi-tenant

---

## Contesto

OpenEdge è oggi un'applicazione deployata localmente: un'org, un server, un broker MQTT, tutto sullo stesso host. L'obiettivo è renderla una piattaforma SaaS dove:
- La UI e il backend risiedono su un unico dominio centrale
- Ogni cliente (org) ha il proprio Edge Manager installato in fabbrica
- I dati confluiscono su un unico broker MQTT centrale, isolati per org
- Ogni utente vede e gestisce solo la propria org
- La configurazione dei gateway avviene dalla UI centrale e si propaga all'edge automaticamente

---

## Goal 1 — MQTT Authentication & ACL per Org

**Obiettivo:** Il broker MQTT centrale non accetta più connessioni anonime dall'esterno. Ogni org ha credenziali MQTT uniche. Un org non può leggere o scrivere topic di un'altra org.

### User Stories
- Come platform admin, voglio che quando creo una nuova org vengano generate automaticamente credenziali MQTT per quell'org
- Come sistema, voglio che l'edge manager di Acme Corp non possa mai leggere i dati di Beta Srl
- Come platform admin, voglio poter revocare le credenziali di un'org senza impattare le altre

### Task tecnici

**1.1 Mosquitto — abilitare password file + ACL**
- Aggiungere a `mosquitto/config/mosquitto.conf`:
  ```
  # Listener interno Docker (rimane anonymous)
  listener 1883
  allow_anonymous true

  # Listener esterno (edge manager dei clienti)
  listener 8883
  allow_anonymous false
  password_file /mosquitto/config/passwords
  acl_file /mosquitto/config/acl
  ```
- Creare file `mosquitto/config/passwords` (inizialmente vuoto, gestito via `mosquitto_passwd`)
- Creare file `mosquitto/config/acl` con template per org

**1.2 Struttura ACL file**
```
# Org: acme-corp (ID: 5)
user acme-corp
topic readwrite data/acme-corp/#
topic readwrite spBv1.0/acme-corp-#
topic readwrite sys/write/#
topic read sys/health/#

# Org: beta-srl (ID: 9)
user beta-srl
topic readwrite data/beta-srl/#
topic readwrite spBv1.0/beta-srl-#
topic readwrite sys/write/#
topic read sys/health/#
```

**1.3 API — generazione credenziali al create org**
- In `internal/handlers/organizations.go`, funzione `Create`:
  - Generare password random (32 char, alphanumerico)
  - Eseguire `mosquitto_passwd -b /mosquitto/config/passwords {org_slug} {password}`
  - Aggiungere entry ACL per l'org
  - Inviare `SIGHUP` al processo mosquitto per reload (o `sys/command/settings-reload`)
  - Salvare le credenziali cifrate in tabella `org_mqtt_credentials(org_id, username, password_hash, created_at)`

**1.4 API — revoca credenziali**
- `DELETE /api/organizations/{id}/mqtt-credentials` (solo global admin)
- Rigenera una nuova password, aggiorna Mosquitto, aggiorna DB

**1.5 docker-compose.yml**
- Esporre porta 8883 verso esterno (oggi solo 18830 interno)
- Montare volume per `passwords` e `acl` file

### Acceptance Criteria
- [ ] Edge manager con credenziali Acme Corp riesce a connettersi su porta 8883
- [ ] Edge manager con credenziali Acme Corp riceve errore se tenta `SUBSCRIBE data/beta-srl/#`
- [ ] Connessioni interne Docker continuano a funzionare su 1883 anonymous
- [ ] Creare una nuova org via API genera automaticamente le credenziali MQTT
- [ ] `mosquitto_passwd` e `acl` file aggiornati senza restart del container (SIGHUP)

### Dipendenze
- Nessuna (goal autonomo)

---

## Goal 2 — Endpoint MQTT Esterno TLS

**Obiettivo:** I dati che viaggiano tra la fabbrica del cliente e il dominio centrale sono cifrati. La porta esterna MQTT usa TLS (8883) e WebSocket TLS (wss:// su porta 443 o 9443).

### User Stories
- Come cliente, voglio che i dati della mia fabbrica viaggino cifrati verso il cloud
- Come platform admin, voglio un certificato valido (Let's Encrypt) sul broker MQTT
- Come sviluppatore, voglio che il browser del cliente possa connettersi via WebSocket sicuro

### Task tecnici

**2.1 Certificato TLS per Mosquitto**
- Configurare Mosquitto per usare certificato Let's Encrypt già usato dal dominio
- Oppure: terminare TLS a livello nginx/reverse proxy e fare proxy verso Mosquitto interno
  ```nginx
  # Soluzione preferita: nginx stream proxy
  stream {
    server {
      listen 8883;
      proxy_pass mosquitto:1883;
      ssl_certificate /etc/letsencrypt/...;
      ssl_certificate_key /etc/letsencrypt/...;
    }
  }
  ```

**2.2 WebSocket TLS per browser**
- Aggiungere listener WSS in Mosquitto (porta 9001 con TLS) oppure via nginx
- Il client browser nel TrendPage già usa `wss://` se `window.location.protocol === 'https:'`
  - File: `services/web-ui/src/pages/TrendPage.tsx` (già gestito, nessun cambio)

**2.3 Variabili d'ambiente**
- Aggiungere a `.env.example`:
  ```
  MQTT_EXTERNAL_HOST=mqtt.yourdomain.com
  MQTT_EXTERNAL_PORT=8883
  MQTT_TLS=true
  ```

**2.4 Documentazione connessione**
- Stringa di connessione da includere nell'installer edge manager:
  ```
  mqtts://mqtt.yourdomain.com:8883
  ```

### Acceptance Criteria
- [ ] `mosquitto_pub -h mqtt.yourdomain.com -p 8883 --cafile ca.crt -u acme-corp -P password -t test/ping -m hello` funziona
- [ ] Connessione in chiaro su 8883 senza TLS viene rifiutata
- [ ] Il browser si connette via `wss://yourdomain.com/mqtt` senza errori certificato
- [ ] Connessioni interne Docker non usano TLS (nessun impatto sulle performance)

### Dipendenze
- Goal 1 (credenziali MQTT)

---

## Goal 3 — Config Pull API per Edge Manager

**Obiettivo:** L'edge manager (driver-manager) non ha più accesso diretto al database. Ottiene la lista dei gateway dalla API centrale usando una API key dell'org. Questo permette all'edge manager di essere installato in fabbrica senza accesso al DB.

### User Stories
- Come edge manager in fabbrica, voglio poter scaricare la lista dei miei gateway dalla API centrale ogni 10 secondi
- Come platform admin, voglio generare/revocare la API key di un'org senza impattare il DB
- Come cliente admin, voglio vedere la mia API key nella UI per configurare l'edge manager

### Task tecnici

**3.1 Tabella org_api_keys**
```sql
CREATE TABLE org_api_keys (
    id SERIAL PRIMARY KEY,
    org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key_hash VARCHAR(64) NOT NULL UNIQUE,  -- SHA-256 della key
    key_prefix VARCHAR(8) NOT NULL,         -- Es. "oe_5f2a" per identificazione
    name VARCHAR(100),
    created_at TIMESTAMP DEFAULT NOW(),
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP
);
```

**3.2 Nuovo endpoint: GET /api/edge/config**
- Autenticazione: header `X-API-Key: oe_5f2a_<random>`
- Risolve org_id dalla API key
- Restituisce:
```json
{
  "org_id": 5,
  "org_name": "acme-corp",
  "mqtt": {
    "host": "mqtt.yourdomain.com",
    "port": 8883,
    "tls": true,
    "username": "acme-corp",
    "password": "<plain, mostrata solo alla generazione>"
  },
  "gateways": [
    {
      "id": 42,
      "name": "PLC Linea 1",
      "driver_type": "S7",
      "connection_config": { "host": "192.168.1.10", "rack": 0, "slot": 1 },
      "scan_rate_ms": 1000,
      "enabled": true,
      "tags": [
        { "id": 101, "code": "DB1.DBD0", "alias": "temperatura", "data_type": "REAL", "historize": true }
      ]
    }
  ]
}
```
- File: `internal/handlers/edge_config.go` (nuovo)

**3.3 Middleware API Key**
- `internal/middleware/api_key.go` — valida `X-API-Key`, imposta org_id in context
- Simile al middleware JWT esistente in `internal/middleware/auth.go`

**3.4 Modifica driver-manager**
- In `services/driver-manager/main.go`, funzione `syncGateways()`:
  - Se `EDGE_API_KEY` env var è presente: usa API pull invece di DB
  - `GET {CENTRAL_API_URL}/api/edge/config` con header `X-API-Key`
  - Se non presente: usa DB locale (backward compatible, single-tenant funziona ancora)
- Aggiungere env vars: `CENTRAL_API_URL`, `EDGE_API_KEY`

**3.5 API key management nella UI**
- Nuova sezione in Settings (solo org admin): mostra API key, bottone "Rigenera"
- La password MQTT in chiaro è mostrata **solo** al momento della generazione (poi solo hash)

### Acceptance Criteria
- [ ] `curl -H "X-API-Key: oe_xxx_yyy" https://yourdomain.com/api/edge/config` restituisce gateway corretti
- [ ] Key di org A non può leggere config di org B
- [ ] Driver-manager configurato con `EDGE_API_KEY` si avvia e sincronizza gateway ogni 10s
- [ ] Driver-manager senza `EDGE_API_KEY` continua a funzionare come prima (single-tenant)
- [ ] Revocare una API key entro 10 secondi ferma la sincronizzazione dell'edge manager

### Dipendenze
- Goal 1 (credenziali MQTT incluse nella config response)

---

## Goal 4 — Edge Manager Packaging

**Obiettivo:** Il cliente scarica un pacchetto pre-configurato dalla UI, lo installa in fabbrica e l'edge manager si avvia senza configurazione manuale.

### User Stories
- Come cliente, voglio scaricare un installer con un click dalla UI senza dover configurare nulla manualmente
- Come cliente IT, voglio installare l'edge manager come servizio Windows o Linux
- Come edge manager, voglio auto-aggiornarmi quando esce una nuova versione

### Task tecnici

**4.1 Struttura pacchetto edge manager**
```
openedge-edge-{org_slug}-{version}.zip
├── docker-compose.yml          # Solo driver-manager + drivers
├── .env                        # Pre-compilato con API key, MQTT creds, endpoint
├── install.sh                  # Linux: systemd service
├── install.ps1                 # Windows: Windows Service via NSSM
├── README.txt
└── update.sh                   # Pull nuove immagini Docker
```

**4.2 File .env pre-compilato (generato dal server)**
```env
# Generato automaticamente per: Acme Corp
# Data: 2026-06-05

CENTRAL_API_URL=https://yourdomain.com
EDGE_API_KEY=oe_5f2a_xxxxxxxxxxxxxxxxxxxx

MQTT_HOST=mqtt.yourdomain.com
MQTT_PORT=8883
MQTT_TLS=true
MQTT_USERNAME=acme-corp
MQTT_PASSWORD=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

ORG_ID=5
ORG_SLUG=acme-corp

# Docker image registry
DRIVER_REGISTRY=registry.yourdomain.com
DRIVER_VERSION=1.2.0
```

**4.3 docker-compose.yml (solo edge)**
```yaml
services:
  driver-manager:
    image: ${DRIVER_REGISTRY}/industrial-driver-manager:${DRIVER_VERSION}
    env_file: .env
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    restart: unless-stopped
```

**4.4 API endpoint — genera e scarica installer**
- `GET /api/organizations/{id}/edge-installer` (solo org admin + global admin)
- Genera il pacchetto ZIP on-the-fly con i file sopra pre-compilati
- Logga il download nell'audit log
- File: `internal/handlers/edge_installer.go` (nuovo)

**4.5 Script install.sh (Linux/systemd)**
- Riusa e adatta `systemd/install.sh` esistente
- Target: solo driver-manager (non il full stack)
- Configura: `WorkingDirectory`, auto-start, restart policy

**4.6 Script install.ps1 (Windows)**
- Installa Docker Desktop se non presente (con prompt)
- Crea Windows Service via NSSM
- Avvia il docker-compose

**4.7 UI — bottone "Scarica Edge Manager"**
- In pagina org settings: card "Edge Manager" con stato connessione + bottone download
- Mostra ultimo ping dell'edge manager (da Redis `edge_ping:{org_id}`)

### Acceptance Criteria
- [ ] Click su "Scarica Edge Manager" nella UI genera e scarica il ZIP in <3 secondi
- [ ] Su Linux: `bash install.sh` avvia il servizio systemd e appare `online` nella UI entro 30 secondi
- [ ] Su Windows: `install.ps1` avvia il servizio Windows
- [ ] Dopo reboot del PC cliente, il servizio riparte automaticamente
- [ ] Il ZIP contiene credenziali valide pre-compilate (nessuna config manuale richiesta)

### Dipendenze
- Goal 3 (API key e config pull)

---

## Goal 5 — Customer Self-Service

**Obiettivo:** Il cliente admin può gestire autonomamente la propria infrastruttura dalla UI: creare/modificare gateway, aggiungere tag, gestire utenti della propria org, senza l'intervento del platform admin.

### User Stories
- Come cliente admin, voglio aggiungere un nuovo gateway PLC dalla UI e vederlo attivo in fabbrica entro 30 secondi
- Come cliente admin, voglio invitare i miei operatori via email con ruolo limitato
- Come cliente admin, voglio vedere se l'edge manager è connesso o offline
- Come operatore (utente), voglio vedere solo i dati dei siti a cui ho accesso

### Task tecnici

**5.1 Edge Manager heartbeat**
- Driver-manager pubblica ogni 30s: `sys/edge/{org_id}/heartbeat`
- Core-api salva in Redis: `edge_ping:{org_id}` = timestamp
- Nuovo campo in `/api/organizations/{id}` response: `edge_status: "online"|"offline"|"never_connected"`

**5.2 UI — Dashboard org admin**
- Nuova sezione "Infrastructure" (solo org admin):
  - Card Edge Manager: status online/offline + ultimo ping + bottone download installer
  - Lista gateway con status (online/offline, ultimo dato ricevuto)
  - Bottone "+ Aggiungi Gateway"

**5.3 UI — Invite utenti**
- Form "Invita utente" con: email, ruolo (admin/user), siti/aree assegnate
- Genera link di registrazione con token (tabella `user_invites`)
- L'utente invitato clicca il link, imposta password, viene aggiunto all'org
- File nuovo: `internal/handlers/invites.go`

**5.4 Permessi role-based affinati**
- Oggi: `admin` vs `user` — troppo binario
- Aggiungere permessi granulari in tabella `role_permissions`:
  - `can_configure_gateways`: crea/modifica gateway e tag
  - `can_write_tags`: usa i write commands
  - `can_manage_users`: invita/rimuove utenti
  - `can_view_alarms`: vede le allarmi
- Org admin ha tutti i permessi per la sua org
- User ha solo `can_view` di default

**5.5 Propagazione config in tempo reale**
- Quando org admin aggiunge un gateway dalla UI:
  - Core-api salva in DB
  - Pubblica su MQTT: `sys/config/{org_id}/reload`
  - Driver-manager in fabbrica (iscritto al topic) forza sync immediata
  - Nuovo gateway attivo entro ~5 secondi (no attesa dei 10s di polling)

### Acceptance Criteria
- [ ] Cliente admin crea gateway "PLC Linea 2", il driver parte in fabbrica entro 10 secondi
- [ ] Cliente admin non può vedere/modificare gateway di altre org
- [ ] Utente invitato riceve email con link, completa registrazione, accede alla UI con l'org corretta
- [ ] UI mostra correttamente "Edge Manager: Online" / "Offline" con timestamp ultimo ping
- [ ] Operatore con ruolo `user` non vede il bottone "Aggiungi Gateway"

### Dipendenze
- Goal 3 (config pull), Goal 4 (heartbeat topic)

---

## Goal 6 — Write Commands & Audit Trail

**Obiettivo:** Gli utenti autorizzati possono scrivere valori su tag (setpoint, comandi) dalla UI. Ogni scrittura è tracciata con chi, quando, quale valore, con quale esito.

### User Stories
- Come operatore autorizzato, voglio cliccare su un tag nella UI, inserire un valore e inviarlo al PLC
- Come manager, voglio vedere uno storico di tutte le scritture eseguite (chi, quando, cosa, da dove)
- Come sistema, voglio che una scrittura fallita (PLC non raggiungibile) sia segnalata all'utente
- Come cliente MQTT, voglio ricevere comandi di scrittura tramite lo stesso broker su cui pubblico dati

### Task tecnici

**6.1 API write command**
- `POST /api/tags/{id}/write` — già esistente in parte
- Body: `{ "value": 75.0, "comment": "setpoint linea 1" }`
- Validazione:
  - Utente ha `i3x_write: true` (JWT) o permesso `can_write_tags`
  - Tag appartiene all'org dell'utente
  - Tag ha `data_type` compatibile con il valore inviato
- Pubblica su MQTT: `sys/write/{tag_id}` con payload:
  ```json
  { "value": 75.0, "user_id": 12, "username": "mario", "ts": 1234567890, "ack_required": true }
  ```

**6.2 Tabella write_audit**
```sql
CREATE TABLE write_commands (
    id SERIAL PRIMARY KEY,
    tag_id INT NOT NULL REFERENCES tags(id),
    org_id INT NOT NULL REFERENCES organizations(id),
    user_id INT NOT NULL REFERENCES users(id),
    username VARCHAR(100),
    value_sent TEXT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',  -- pending, ack, nack, timeout
    ack_ts TIMESTAMP,
    ip_address VARCHAR(45),
    comment TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);
```

**6.3 Acknowledgement flow (opzionale ma raccomandato)**
- Driver, dopo la scrittura, pubblica: `sys/write_ack/{tag_id}` con `{ "status": "ok"|"error", "msg": "..." }`
- Core-api aggiorna `write_commands.status`
- UI riceve feedback via WebSocket/SSE entro ~2 secondi

**6.4 Routing scrittura per tipo driver**
- S7/Modbus/OPC-UA: driver riceve `sys/write/{tag_id}`, scrive direttamente al dispositivo
- MQTT driver: riceve `sys/write/{tag_id}`, pubblica su broker locale esterno al topic configurato in `connection_config.write_topic`
- Nessun driver attivo: il messaggio scade su MQTT (retain=false, TTL configurabile)

**6.5 UI — widget write**
- In ogni tag nella UI: icona matita (visibile solo se `can_write_tags`)
- Click apre popup: campo valore + campo commento + bottone "Invia"
- Dopo invio: spinner "in attesa conferma" → ✓ verde / ✗ rosso con messaggio errore
- Pagina Audit (solo admin): tabella `write_commands` filtrabile per tag, utente, data

**6.6 Sicurezza**
- Rate limiting: max 10 scritture/minuto per utente
- Valore validato contro `min`/`max` configurati nel tag (se presenti)
- Log in `audit_logs` esistente + nella nuova tabella `write_commands`

### Acceptance Criteria
- [ ] Utente con `can_write_tags` invia valore, PLC aggiorna il registro entro 2 secondi
- [ ] Utente senza `can_write_tags` non vede l'icona matita e riceve 403 se chiama l'API
- [ ] Scrittura su tag di altra org ritorna 403
- [ ] Ogni scrittura appare nell'audit trail con user, valore, timestamp, IP
- [ ] Scrittura con PLC offline mostra errore in UI entro 5 secondi (timeout ack)
- [ ] Driver MQTT forwarda la scrittura al broker locale del cliente

### Dipendenze
- Goal 1 (MQTT ACL — il driver può pubblicare su `sys/write_ack/#`)
- Goal 5 (permessi granulari `can_write_tags`)

---

## Goal 7 — Platform Admin Dashboard & Monitoring

**Obiettivo:** Il platform admin (tu) ha una vista completa su tutte le org: stato edge manager, volume dati, utenti attivi, allarmi globali. Può intervenire su qualsiasi org senza impatto sulle altre.

### User Stories
- Come platform admin, voglio vedere a colpo d'occhio quali org hanno l'edge manager online e quali offline
- Come platform admin, voglio entrare nell'org di un cliente per fare debug senza conoscere le sue credenziali
- Come platform admin, voglio monitorare il volume dati (messaggi/giorno, GB storage) per org
- Come platform admin, voglio creare nuove org e inviare al cliente le istruzioni di setup

### Task tecnici

**7.1 UI — Superadmin panel (solo global admin)**
- Nuova sezione `/admin` nella UI (route protetta da `IsGlobalAdmin()`)
- Dashboard con:
  - Tabella org: nome, utenti, gateway, edge status, dati oggi (messaggi), storage usato
  - Badge colorato: 🟢 online / 🔴 offline / ⚪ mai connesso
  - Click su org → "Impersona" (entra nella vista di quella org)

**7.2 Impersonazione org (admin feature)**
- `POST /api/admin/impersonate/{org_id}` → ritorna JWT temporaneo (1h) con `org_id` dell'org target + `impersonated_by: admin_id`
- Sessione impersonata visibile con banner giallo nella UI: "Stai visualizzando Acme Corp come admin"
- Ogni azione in impersonazione loggata in `audit_logs` con `impersonated_by`

**7.3 Metriche per org (tabella)**
```sql
CREATE TABLE org_metrics_daily (
    org_id INT NOT NULL REFERENCES organizations(id),
    date DATE NOT NULL,
    messages_received INT DEFAULT 0,
    write_commands_sent INT DEFAULT 0,
    active_gateways INT DEFAULT 0,
    storage_bytes BIGINT DEFAULT 0,
    PRIMARY KEY (org_id, date)
);
```
- Aggiornata ogni ora da una goroutine in `core-api`

**7.4 Endpoint metriche**
- `GET /api/admin/metrics?from=2026-01-01&to=2026-06-05` → metriche aggregate per tutte le org
- Usato dalla dashboard admin + potenzialmente per billing futuro

**7.5 Onboarding wizard nuova org**
- Form multi-step: nome org → crea admin utente → genera credenziali MQTT + API key → mostra istruzioni → bottone download installer
- Email automatica all'admin cliente con link installer + credenziali

**7.6 Alerting platform admin**
- Se un'org aveva l'edge manager online e poi va offline per più di X minuti → notifica in-app al platform admin
- Topic MQTT: `sys/edge/{org_id}/heartbeat` già disponibile (Goal 5)
- Salva in Redis con TTL, se scade → edge offline

### Acceptance Criteria
- [ ] Dashboard admin mostra tutte le org con edge status aggiornato in tempo reale
- [ ] Platform admin clicca "Impersona" su Acme Corp e vede la UI identica a un utente Acme Corp
- [ ] Ogni azione durante impersonazione appare nell'audit log con `impersonated_by`
- [ ] Metriche giornaliere (messaggi, storage) visibili per ogni org
- [ ] Nuovo org creato dall'admin wizard → email automatica al cliente con link installer
- [ ] Edge manager offline da >15 minuti genera notifica nella dashboard admin

### Dipendenze
- Goal 4 (heartbeat edge manager)
- Goal 5 (status edge manager in Redis)

---

## Roadmap consigliata

```
Sprint 1 (2 settimane)
└── Goal 1: MQTT Auth + ACL
└── Goal 2: TLS esterno

Sprint 2 (2 settimane)
└── Goal 3: Config Pull API

Sprint 3 (2 settimane)
└── Goal 4: Edge Manager Packaging

Sprint 4 (2 settimane)
└── Goal 5: Customer Self-Service (base)
└── Goal 6: Write Commands

Sprint 5 (2 settimane)
└── Goal 5: Self-Service (invite utenti)
└── Goal 7: Admin Dashboard
```

## Architettura finale

```
FABBRICA CLIENTE
┌─────────────────────────────────┐
│  Edge Manager (installato)      │
│  driver-manager                 │  ← scarica config da API ogni 10s
│  ├─ driver-s7     → PLC         │
│  ├─ driver-modbus → Modbus      │
│  └─ driver-mqtt   → broker loc. │
└──────────────┬──────────────────┘
               │ MQTT TLS 8883 + credenziali per-org
               │ heartbeat, data, write_ack
               ▼
TUO DOMINIO
┌─────────────────────────────────────────────────┐
│  nginx (TLS termination)                        │
│  Mosquitto 1883 (interno) + 8883 TLS (esterno)  │
│  core-api 8081  ── PostgreSQL + TimescaleDB     │
│  engine-historian                               │
│  web-ui (https://yourdomain.com)                │
│  Redis (edge status, realtime cache)            │
└─────────────────────────────────────────────────┘
               ▲
  mario@acme.com  → JWT {org_id:5}   → vede solo Acme
  admin@piattaforma.com → JWT {global_admin} → vede tutto
```

## Note di sicurezza

- Credenziali MQTT mai esposte in chiaro dopo la generazione (solo hash in DB)
- API key con prefix visibile (`oe_5f2a_...`) per identificazione rapida
- TLS obbligatorio su tutte le connessioni esterne
- Write commands richiedono doppia validazione (JWT + permesso granulare)
- Ogni azione sensibile loggata in `audit_logs` (già esistente)
- Impersonazione admin tracciata separatamente
- Rate limiting su write commands e login (già presente su login)
