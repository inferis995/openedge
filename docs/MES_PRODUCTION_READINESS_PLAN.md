# OpenEdge — Piano Production-Readiness: da SCADA a SCADA+MES

> Gap analysis e piano di implementazione basato sul webinar Mitsubishi/Qualitas
> *"SCADA e MES: ruoli distinti, valore condiviso"* (Genesis 11 + NET@PRO).
> Obiettivo: portare OpenEdge da piattaforma SCADA/IIoT professionale a
> piattaforma SCADA+MES pronta per la produzione in settori regolati.

---

## 0. Stato attuale (baseline)

**Layer SCADA — completo e competitivo:**
- Acquisizione multi-driver: OPC-UA, Modbus TCP, S7, MQTT, LoRaWAN
- Supervisione HMI / dashboard real-time (WebSocket Sparkplug)
- Allarmi: definizioni, eventi, escalation, ack, storico, notifiche
  (Email/Telegram/Slack/Teams/PagerDuty)
- Historian con trend uPlot, deadband, retention via tag_history
- Multi-tenant (org → site → area → gateway → tag), RBAC, audit_logs,
  API keys, SSO/OIDC, invites/webhooks
- i3X Access API v1 (CESMII), Fleet OTA, backup L1/L2, InfluxDB connector

**Layer MES — parziale (più avanti del previsto):**
- OEE real-time A×P×Q con profili multipli (`oee_profiles`, `oee.go`)
- Loss tree + **Pareto cause** + **MTBF/MTTR** (`oee_losses.go`)
- Turni (`shifts.go`), Ricette (`recipes.go`), Custom KPI, Report CSV
- Finestre di manutenzione (silenziamento notifiche)

**Gap MES da chiudere (cuore di questo piano):**
- ❌ Ordini di Lavoro (OdL / Work Orders)
- ❌ Tracciabilità: lotto + serial number + genealogia
- ❌ Qualità & Non Conformità (NC)
- ⚠️ Cause code legati all'ordine (parziale: loss tree ha categorie, non cause-code anagrafiche né link a OdL)
- ❌ Manutenzione su contatori/trigger automatici (predittiva)
- ❌ Compliance regolata: audit immutabile, e-signature (21 CFR Part 11)
- ❌ Integrazione ERP bidirezionale
- ❌ Energy monitoring ISO 50001 (EnPI)

---

## Convenzioni di progetto (da rispettare)

- **Tabelle**: `id SERIAL PRIMARY KEY`, `org_id INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE`,
  `created_at TIMESTAMPTZ DEFAULT NOW()`, `UNIQUE (org_id, ...)`, indici `idx_<tab>_org_*`.
- **Migrazioni**: file `migrations/YYYYMMDD_nome.sql` + registrazione inline in `internal/db/db.go`.
- **Handler**: `middleware.GetOrganizationID(c)` per lo scoping; chiamate DB con
  `QueryContext/QueryRowContext/ExecContext(context.Background(), ...)` (lint noctx);
  `defer func() { if cerr := rows.Close(); cerr != nil { log.Printf(...) } }()` (errcheck).
- **Routing API**: registrazione in `services/core-api/main.go`.
- **Frontend**: pagina in `src/pages/`, client in `src/api/`, rotta in `App.tsx`,
  voce in `components/layout/Sidebar.tsx`. Permessi via `role_permissions`.

---

## BLOCCO 1 — MES Core (priorità 1, differenziatore commerciale)

### 1.1 Ordini di Lavoro (Work Orders / OdL)

**Schema** — `migrations/2026XXXX_work_orders.sql`
```sql
CREATE TABLE IF NOT EXISTS work_orders (
    id              SERIAL PRIMARY KEY,
    org_id          INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    code            VARCHAR(100) NOT NULL,            -- numero OdL (da ERP o interno)
    product_code    VARCHAR(200),
    product_name    TEXT,
    recipe_id       INT REFERENCES recipes(id) ON DELETE SET NULL,
    area_id         INT REFERENCES areas(id) ON DELETE SET NULL,
    gateway_id      INT REFERENCES gateways(id) ON DELETE SET NULL,
    qty_planned     DOUBLE PRECISION NOT NULL DEFAULT 0,
    qty_produced    DOUBLE PRECISION NOT NULL DEFAULT 0,
    qty_scrap       DOUBLE PRECISION NOT NULL DEFAULT 0,
    uom             VARCHAR(20) DEFAULT 'pcs',
    status          VARCHAR(20) NOT NULL DEFAULT 'planned'
                    CHECK (status IN ('planned','released','running','paused','completed','closed','cancelled')),
    priority        INT DEFAULT 0,
    planned_start   TIMESTAMPTZ,
    planned_end     TIMESTAMPTZ,
    actual_start    TIMESTAMPTZ,
    actual_end      TIMESTAMPTZ,
    erp_ref         VARCHAR(200),                     -- chiave ordine ERP (sync)
    created_by      INT REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (org_id, code)
);
CREATE INDEX IF NOT EXISTS idx_wo_org_status ON work_orders(org_id, status, priority DESC);

-- Transizioni di stato per audit/traceability del ciclo dell'OdL
CREATE TABLE IF NOT EXISTS work_order_events (
    id            BIGSERIAL PRIMARY KEY,
    work_order_id INT NOT NULL REFERENCES work_orders(id) ON DELETE CASCADE,
    event_type    VARCHAR(30) NOT NULL,              -- released|started|paused|resumed|completed|scrap|note
    qty_delta     DOUBLE PRECISION,
    user_id       INT REFERENCES users(id) ON DELETE SET NULL,
    note          TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);
```

**Backend** — `internal/handlers/work_orders.go`
- `GET /api/work-orders` (filtri: status, area, gateway, date range)
- `GET /api/work-orders/:id` (con eventi + progresso %)
- `POST /api/work-orders` (crea, status=planned)
- `PUT /api/work-orders/:id` (modifica metadati)
- `POST /api/work-orders/:id/transition` (release/start/pause/complete con guardia su FSM)
- `POST /api/work-orders/:id/progress` (incrementa qty_produced/qty_scrap — chiamabile anche da automatismo tag→OdL)
- `DELETE /api/work-orders/:id`
- Permesso nuovo in `role_permissions`: `can_manage_work_orders`

**Frontend** — `src/pages/WorkOrdersPage.tsx`
- Tabella OdL con stato (badge colorato), barra avanzamento qty_produced/qty_planned, priorità
- Dialog crea/modifica con select ricetta + area + gateway
- Azioni FSM contestuali (Release → Start → Pause → Complete) come bottoni
- Pannello dettaglio con timeline eventi (riusa pattern di AlarmsPage)
- Filtri: stato, area, range date; stats bar (planned/running/completed/scrap%)

**Effort stimato:** ~2-3 gg (schema + handler + UI + test FSM).

---

### 1.2 Tracciabilità: Lotto / Serial / Genealogia

**Schema** — `migrations/2026XXXX_traceability.sql`
```sql
CREATE TABLE IF NOT EXISTS production_lots (
    id            SERIAL PRIMARY KEY,
    org_id        INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    work_order_id INT REFERENCES work_orders(id) ON DELETE SET NULL,
    lot_code      VARCHAR(200) NOT NULL,
    serial_number VARCHAR(200),                       -- NULL per produzione a lotti puri
    status        VARCHAR(20) NOT NULL DEFAULT 'good'
                  CHECK (status IN ('good','quarantine','scrap','rework','shipped')),
    produced_at   TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (org_id, lot_code, serial_number)
);
CREATE INDEX IF NOT EXISTS idx_lots_org_wo ON production_lots(org_id, work_order_id);

-- Genealogia: padre→figlio (assemblaggio) + consumo materiali/risorse
CREATE TABLE IF NOT EXISTS lot_genealogy (
    id           BIGSERIAL PRIMARY KEY,
    parent_lot_id INT NOT NULL REFERENCES production_lots(id) ON DELETE CASCADE,
    child_lot_id  INT REFERENCES production_lots(id) ON DELETE SET NULL,
    material_code VARCHAR(200),                        -- materia prima/componente (anche da ERP)
    qty           DOUBLE PRECISION,
    uom           VARCHAR(20),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Snapshot parametri di processo agganciati al lotto (dal campo SCADA)
CREATE TABLE IF NOT EXISTS lot_process_data (
    id          BIGSERIAL PRIMARY KEY,
    lot_id      INT NOT NULL REFERENCES production_lots(id) ON DELETE CASCADE,
    tag_id      INT REFERENCES tags(id) ON DELETE SET NULL,
    param_name  VARCHAR(200),
    value       DOUBLE PRECISION,
    captured_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Backend** — `internal/handlers/traceability.go`
- `GET /api/lots` (filtri: work_order, status, range, ricerca per lot_code/serial)
- `GET /api/lots/:id/genealogy` → albero completo padre/figli + materiali + process data
- `POST /api/lots` (crea lotto, link a OdL)
- `POST /api/lots/:id/genealogy` (registra consumo materiale o link componente)
- `POST /api/lots/:id/process-data` (snapshot parametri — chiamabile dall'engine al completamento pezzo)
- `POST /api/lots/:id/status` (good→quarantine/scrap/rework, scrive work_order_events)

**Frontend** — `src/pages/TraceabilityPage.tsx`
- Ricerca per lotto/serial → vista genealogia ad albero (riusa `buildTree` di I3XPage)
- "Where-used" (forward) e "where-from" (backward) in un click — il "100% tracciabilità" del deck
- Tabella parametri di processo del lotto + link al trend storico
- Badge stato lotto con azioni quarantena/scrap

**Effort stimato:** ~3 gg.

---

### 1.3 Qualità & Non Conformità (NC)

**Schema** — `migrations/2026XXXX_quality.sql`
```sql
CREATE TABLE IF NOT EXISTS quality_checks (
    id            SERIAL PRIMARY KEY,
    org_id        INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    work_order_id INT REFERENCES work_orders(id) ON DELETE SET NULL,
    lot_id        INT REFERENCES production_lots(id) ON DELETE SET NULL,
    check_name    VARCHAR(200) NOT NULL,
    measured      DOUBLE PRECISION,
    lower_limit   DOUBLE PRECISION,
    upper_limit   DOUBLE PRECISION,
    tag_id        INT REFERENCES tags(id) ON DELETE SET NULL,   -- se da SCADA automatico
    result        VARCHAR(10) NOT NULL CHECK (result IN ('pass','fail')),
    operator_id   INT REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS non_conformities (
    id            SERIAL PRIMARY KEY,
    org_id        INT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    code          VARCHAR(100) NOT NULL,
    work_order_id INT REFERENCES work_orders(id) ON DELETE SET NULL,
    lot_id        INT REFERENCES production_lots(id) ON DELETE SET NULL,
    quality_check_id INT REFERENCES quality_checks(id) ON DELETE SET NULL,
    defect_code   VARCHAR(100),                       -- anagrafica difetti
    severity      VARCHAR(20) CHECK (severity IN ('minor','major','critical')),
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open','investigating','contained','closed')),
    disposition   VARCHAR(30),                        -- use-as-is|rework|scrap|return
    opened_by     INT REFERENCES users(id) ON DELETE SET NULL,
    closed_by     INT REFERENCES users(id) ON DELETE SET NULL,
    opened_at     TIMESTAMPTZ DEFAULT NOW(),
    closed_at     TIMESTAMPTZ,
    UNIQUE (org_id, code)
);
```

**Backend** — `internal/handlers/quality.go`
- `GET/POST /api/quality/checks` (raccolta collaudi manuale o auto da tag)
- `GET/POST /api/quality/nc` + `POST /api/quality/nc/:id/transition`
- `GET /api/quality/defects/pareto` (correlazione difetto-processo, riusa pattern Pareto di oee_losses)
- Automatismo: una check `fail` su parametro critico → apre NC + mette lotto in quarantena (caso d'uso deck pag.16)

**Frontend** — `src/pages/QualityPage.tsx`
- Tab Collaudi (con limiti e pass/fail visivi) + Tab Non Conformità (workflow stato)
- Pareto difetti per causa/macchina/turno (recharts)
- Link NC ↔ lotto ↔ OdL ↔ trend parametro

**Effort stimato:** ~3 gg.

---

### 1.4 I 3 casi d'uso SCADA↔MES (l'integrazione del deck)

Logica nell'engine (core-api `handleDataUpdate` / nuovo `internal/mes/orchestrator.go`):

1. **Fermo macchina → azione OdL**: alarm critico su gateway con OdL `running`
   → auto-pausa OdL (`work_order_events` type=paused), notifica manutenzione.
2. **Qualità fuori specifica → blocco lotto**: tag oltre limite di una `quality_check`
   → NC automatica + lotto in quarantena.
3. **Cambio produzione → ricetta su PLC**: OdL transizione a `running` con `recipe_id`
   → scrive i parametri ricetta sui tag (riusa `writePropertyValue` i3X / tags write).

**Effort stimato:** ~2 gg (è collante tra moduli già esistenti).

---

## BLOCCO 2 — Compliance per settori regolati (priorità 2, gate-keeper vendite)

### 2.1 Audit trail immutabile + firma elettronica (21 CFR Part 11)
- Aggiungere a `audit_logs`: colonna `prev_hash` + `entry_hash` (hash-chain: ogni record
  include lo SHA-256 del precedente → manomissione rilevabile).
- `internal/audit/chain.go`: funzione `Append(entry)` che calcola e cataena l'hash;
  `Verify()` per validare l'intera catena (endpoint `GET /api/audit/verify`).
- E-signature: per azioni critiche (write PLC, chiusura NC, rilascio OdL) richiedere
  re-inserimento password → record firmato con `signed_by`, `signature_reason`, `signed_at`.
- UI: pagina AuditPage mostra stato catena (✓ integra / ✗ manomessa) + dialog firma.

**Effort:** ~2 gg.

### 2.2 Crittografia at-rest + retention configurabile
- Documentare/abilitare encryption-at-rest del volume Postgres (gestita a livello deploy).
- Tabella `retention_policies (org_id, data_type, keep_days)` + job di pruning su
  `tag_history`/`audit_logs`/`alarm_events` (cron nel core-api). UI in SystemPage.

**Effort:** ~1.5 gg.

### 2.3 Hardening IEC 62443 (doc + check)
- Documento di architettura: segmentazione OT/IT, DMZ, ruoli, gestione segreti.
- Checklist sicurezza in `docs/SECURITY.md`; rotazione JWT secret; rate-limit già presente.

**Effort:** ~1 gg (prevalentemente documentale).

---

## BLOCCO 3 — Integrazione ERP + Energy (priorità 3)

### 3.1 Connettore ERP bidirezionale
- `internal/connectors/erp.go`: interfaccia generica + adapter REST/CSV.
  - **IN da ERP**: ordini di lavoro (popola `work_orders.erp_ref`), anagrafiche prodotto/materiale.
  - **OUT verso ERP**: conferme produzione (qty_produced/scrap), consumi reali (da genealogia),
    certificati qualità.
- Config in `global_settings`: `erp_enabled`, `erp_base_url`, `erp_auth`, `erp_poll_interval`.
- UI: SystemPage → Integrations → ERP (come InfluxDB).

**Effort:** ~3 gg (adapter REST generico; adapter specifici SAP/Dynamics in iterazioni successive).

### 3.2 Energy monitoring ISO 50001
- Tabella `energy_meters (org_id, name, area_id, gateway_id, tag_id, uom)` + viste di
  aggregazione kWh per area/linea/macchina/turno.
- EnPI (Energy Performance Indicator) = consumo / pezzi prodotti (join con work_orders).
- UI: `src/pages/EnergyPage.tsx` con consumi per linea + EnPI + trend.

**Effort:** ~2.5 gg.

### 3.3 Manutenzione predittiva su contatori
- Estendere maintenance: tabella `maintenance_counters (org_id, gateway_id, tag_id, counter_type,
  threshold, current_value, last_reset)` (ore macchina, cicli, energia da SCADA).
- Trigger automatico: al superamento soglia → crea ordine di manutenzione + notifica.
- Calcolo MTBF/MTTR già presente (`oee_losses.go`) → collegare ai cause-code anagrafici.

**Effort:** ~2 gg.

---

## Roadmap consigliata

| Fase | Contenuto | Effort | Valore |
|------|-----------|--------|--------|
| **M1** | Blocco 1.1 + 1.2 (OdL + Tracciabilità) | ~5-6 gg | ⭐⭐⭐ Differenziatore MES base |
| **M2** | Blocco 1.3 + 1.4 (Qualità/NC + casi d'uso) | ~5 gg | ⭐⭐⭐ "100% tracciabilità" + automazione |
| **M3** | Blocco 2 (Compliance 21 CFR / IEC 62443) | ~4-5 gg | ⭐⭐ Sblocca pharma/food regolati |
| **M4** | Blocco 3 (ERP + Energy + Manut. predittiva) | ~7-8 gg | ⭐⭐ Visione completa SCADA-MES-ERP |

**Totale stimato:** ~21-24 giorni/uomo per la piena parità con la visione del deck.

## Note di verifica (per ogni fase)
```bash
JWT_SECRET=local-test-secret-key-minimum-32-chars go build ./...
JWT_SECRET=local-test-secret-key-minimum-32-chars go test ./...
cd services/web-ui && npx tsc --noEmit
```
Più test unitari sulle FSM (transizioni OdL/NC) e sull'hash-chain dell'audit.
