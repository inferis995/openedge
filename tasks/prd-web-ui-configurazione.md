# PRD: Web UI di Configurazione - Industrial Edge Middleware

## Introduction

Creare una Web UI completa con React + TypeScript per configurare e gestire tutto il sistema Industrial Edge Middleware. L'applicazione permetterà la gestione gerarchica di Organizzazioni, Sites, Areas, Gateways (S7/Modbus), Tag e Allarmi, con funzionalità avanzate di autenticazione, notifiche real-time e gestione della configurazione.

## Goals

- Fornire un'interfaccia web moderna e reattiva per la configurazione del sistema
- Gestire l'intera gerarchia: Organizzazioni → Sites → Areas → Gateways → Tags
- Monitorare e gestire gli allarmi in tempo reale
- Implementare autenticazione e autorizzazione per utenti
- Abilitare export/import della configurazione completa
- Garantire un'esperienza utente fluida con feedback immediato

## User Stories

### US-001: Configurazione progetto React + Vite

**Description:** Come sviluppatore, voglio inizializzare il progetto React con Vite, TypeScript e tutte le dipendenze necessarie per poter iniziare lo sviluppo.

**Acceptance Criteria:**

- [ ] Creare directory `services/web-ui/` con struttura completa
- [ ] Inizializzare progetto Vite con React 18 + TypeScript
- [ ] Configurare Tailwind CSS con shadcn/ui components
- [ ] Installare tutte le dipendenze: React Query, Zustand, Axios, React Router v7, React Hook Form, Zod, TanStack Table, Lucide React
- [ ] Configurare vite.config.ts con proxy per API backend
- [ ] Creare Dockerfile multi-stage per build di produzione
- [ ] Configurare nginx.conf per servire l'applicazione
- [ ] `npm run dev` avvia l'app su localhost:3000
- [ ] Typecheck passa senza errori

### US-002: Configurazione API Client e TypeScript Types

**Description:** Come sviluppatore, voglio configurare il client Axios e definire tutti i TypeScript types per le entità del sistema per garantire type-safety.

**Acceptance Criteria:**

- [ ] Creare `api/client.ts` con configurazione Axios
- [ ] Configurare interceptor per request/response
- [ ] Definire TypeScript interfaces per tutte le entità (Organization, Site, Area, Gateway, Tag, Alarm, HealthStats)
- [ ] Definire DTOs per creazione e aggiornamento (CreateXxxDto, UpdateXxxDto)
- [ ] Creare moduli API per ogni risorsa (organizations.ts, sites.ts, areas.ts, gateways.ts, tags.ts, alarms.ts, health.ts)
- [ ] Typecheck passa senza errori

### US-003: Componenti UI Base (shadcn/ui)

**Description:** Come sviluppatore, voglio implementare tutti i componenti UI base basati su shadcn/ui per costruire l'interfaccia utente.

**Acceptance Criteria:**

- [ ] Creare `lib/utils.ts` con funzione `cn()` per class merging
- [ ] Implementare Button con varianti (default, destructive, outline, secondary, ghost, link)
- [ ] Implementare Input con supporto per tutti gli HTML input attributes
- [ ] Implementare Label per form fields
- [ ] Implementare Select basato su Radix UI
- [ ] Implementare Dialog per modali
- [ ] Implementare Badge con varianti (default, secondary, destructive, outline, success, warning)
- [ ] Implementare Card (Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter)
- [ ] Implementare Table con tutti i componenti (Table, TableHeader, TableBody, TableRow, TableHead, TableCell)
- [ ] Implementare Toast e Toaster per notifiche
- [ ] Implementare Separator per separazioni visive
- [ ] Implementare Breadcrumb per navigazione gerarchica
- [ ] Implementare Switch per toggle booleani
- [ ] Typecheck passa senza errori

### US-004: Layout Principale con Sidebar e Header

**Description:** Come utente, voglio un layout con sidebar di navigazione e header informativo per navigare facilmente tra le diverse sezioni dell'applicazione.

**Acceptance Criteria:**

- [ ] Creare `Sidebar.tsx` con items di navigazione: Dashboard, Organizations, Sites, Areas, Gateways, Tags, Alarms
- [ ] Implementare active state con evidenziazione della pagina corrente
- [ ] Creare `Header.tsx` che mostra il contesto di navigazione (Org/Site/Area selezionati)
- [ ] Creare `MainLayout.tsx` che combina Sidebar, Header e contenuto pagina
- [ ] Il layout è responsive e funziona su diverse dimensioni schermo
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-005: State Management con Zustand

**Description:** Come sviluppatore, voglio implementare uno store Zustand per gestire lo stato di navigazione (Organizzazione/Site/Area selezionati) condiviso tra le pagine.

**Acceptance Criteria:**

- [ ] Creare `stores/useNavigationStore.ts` con stato per selectedOrg, selectedSite, selectedArea
- [ ] Implementare azioni: setSelectedOrg, setSelectedSite, setSelectedArea, clearSelection
- [ ] La selezione di una Organizzazione resetta Site e Area
- [ ] La selezione di un Site resetta Area
- [ ] Typecheck passa senza errori

### US-006: Custom Hooks per Data Fetching

**Description:** Come sviluppatore, voglio creare custom hooks React Query per data fetching, mutations e cache invalidation per tutte le risorse API.

**Acceptance Criteria:**

- [ ] Creare `hooks/useOrganizations.ts` con query, create, delete mutations
- [ ] Creare `hooks/useSites.ts` con filtering by org_id e mutations
- [ ] Creare `hooks/useAreas.ts` con filtering by site_id e mutations
- [ ] Creare `hooks/useGateways.ts` con filtering by area_id, create, update, delete mutations
- [ ] Creare `hooks/useTags.ts` con filtering by gateway_id e mutations
- [ ] Creare `hooks/useAlarms.ts` con filtering, acknowledge mutation e polling ogni 5 secondi
- [ ] Creare `hooks/useHealth.ts` con polling ogni 10 secondi
- [ ] Tutte le mutations invalidano la cache appropriata
- [ ] Typecheck passa senza errori

### US-007: Dashboard Page

**Description:** Come utente, voglio una dashboard che mostri le statistiche del sistema e un riepilogo delle organizzazioni recenti per avere una panoramica immediata dello stato del sistema.

**Acceptance Criteria:**

- [ ] Creare `pages/DashboardPage.tsx`
- [ ] Mostrare 6 card statistiche: Organizations, Sites, Gateways, Online Gateways, Tags, Active Alarms
- [ ] Ogni card mostra icona colorata appropriata e valore
- [ ] Mostrare lista delle prime 5 organizzazioni con ID badge
- [ ] Gestire stato loading durante fetch dati
- [ ] I dati si aggiornano automaticamente tramite polling
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-008: Organizations Management Page

**Description:** Come utente, voglio gestire le organizzazioni (creare, visualizzare, eliminare) per definire la struttura gerarchica base del sistema.

**Acceptance Criteria:**

- [ ] Creare `pages/OrganizationsPage.tsx`
- [ ] Mostrare tabella con colonne: ID, Name, Created At, Actions
- [ ] Implementare dialog per creare nuova organizzazione con campo name
- [ ] Implementare delete con conferma (confirm dialog)
- [ ] Mostrare stato vuoto quando non ci sono organizzazioni
- [ ] Mostrare stato loading durante fetch
- [ ] Refresh automatico dopo create/delete
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-009: Sites Management Page

**Description:** Come utente, voglio gestire i siti associati alle organizzazioni per organizzare geograficamente o funzionalmente le risorse.

**Acceptance Criteria:**

- [ ] Creare `pages/SitesPage.tsx`
- [ ] Mostrare tabella con colonne: ID, Organization, Name, Created At, Actions
- [ ] Implementare dialog con select per Organization e campo Name
- [ ] Implementare delete con conferma
- [ ] Mostrare stato vuoto e loading
- [ ] Refresh automatico dopo operazioni
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-010: Areas Management Page

**Description:** Come utente, voglio gestire le aree all'interno dei siti per creare una suddivisione logica delle risorse.

**Acceptance Criteria:**

- [ ] Creare `pages/AreasPage.tsx`
- [ ] Mostrare tabella con colonne: ID, Site, Name, Created At, Actions
- [ ] Implementare dialog con select per Site e campo Name
- [ ] Implementare delete con conferma
- [ ] Mostrare stato vuoto e loading
- [ ] Refresh automatico dopo operazioni
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-011: Gateways Management Page

**Description:** Come utente, voglio gestire i gateway PLC (S7/Modbus) con configurazioni di connessione complete per abilitare la comunicazione con i dispositivi industriali.

**Acceptance Criteria:**

- [ ] Creare `pages/GatewaysPage.tsx`
- [ ] Mostrare tabella con: ID, Area, Name, Driver, IP, Scan Rate, Status (Enabled/Disabled), Actions
- [ ] Implementare dialog con:
  - Select per Area
  - Campo Name
  - Select per Driver Type (S7/MODBUS_TCP)
  - Campo IP Address
  - Campi Rack/Slot per S7
  - Campi Slave ID/Port per Modbus TCP
  - Campo Scan Rate in ms
  - Toggle per Enabled
- [ ] Implementare toggle on/off per abilitare/disabilitare gateway
- [ ] Implementare delete con conferma
- [ ] Badge per Driver Type e Status
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-012: Tags Management Page

**Description:** Come utente, voglio gestire i tag PLC con configurazione di data type, storizzazione e allarmi per definire i punti di misura e controllo.

**Acceptance Criteria:**

- [ ] Creare `pages/TagsPage.tsx`
- [ ] Mostrare tabella con: Code (PLC Address), Alias, Gateway, Type, Historize (Yes/No + deadband), Alarm (Priority/None), Actions
- [ ] Implementare dialog con:
  - Select per Gateway
  - Campo Code (indirizzo PLC)
  - Campo Alias
  - Select per Data Type (INT, REAL, BOOL, DINT)
  - Toggle Historize con campo Deadband condizionale
  - Toggle Alarm con campi Threshold, Operator, Priority condizionali
- [ ] Implementare delete con conferma
- [ ] Badge per Data Type e Historize/Alarm status
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-013: Alarms Management Page

**Description:** Come utente, voglio visualizzare e gestire gli allarmi con filtri per stato e funzione di acknowledge per gestire le situazioni di allarme.

**Acceptance Criteria:**

- [ ] Creare `pages/AlarmsPage.tsx`
- [ ] Mostrare tabella con: ID, State, Message, Tag, Triggered At, Acknowledged At, Actions
- [ ] Implementare filtro per State (All, Active, RTN, Acknowledged, Clear)
- [ ] Badge colorati per stati: Active (red), RTN (yellow), Acknowledged (blue), Clear (gray)
- [ ] Pulsante "Acknowledge" per allarmi in stato Active/RTN
- [ ] Auto-refresh ogni 5 secondi
- [ ] Icone Clock e Check per timestamp
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-014: Configurazione CORS nel Backend

**Description:** Come sviluppatore, voglio configurare CORS nel core-api per permettere alla Web UI di comunicare con il backend.

**Acceptance Criteria:**

- [ ] Aggiornare `services/core-api/main.go` con import `github.com/gin-contrib/cors`
- [ ] Configurare middleware CORS con:
  - AllowOrigins: http://localhost:3000, http://127.0.0.1:3000
  - AllowMethods: GET, POST, PUT, DELETE, OPTIONS
  - AllowHeaders: Origin, Content-Type, Authorization
  - AllowCredentials: true
  - MaxAge: 12 * time.Hour
- [ ] Aggiornare `go.mod` con `github.com/gin-contrib/cors`
- [ ] Backend accetta richieste da localhost:3000

### US-015: Autenticazione e Autorizzazione

**Description:** Come utente, voglio effettuare login e logout con controllo degli accessi basato sui ruoli per proteggere l'accesso alla configurazione. Il sistema deve supportare refresh token automatico per evitare login frequenti.

**Acceptance Criteria:**

- [ ] Creare pagina Login con form username/password
- [ ] Implementare JWT token storage in localStorage (access token + refresh token)
- [ ] Implementare Axios interceptor per aggiungere Authorization header
- [ ] Implementare refresh token automatico in background quando access token scade
- [ ] Creare hook `useAuth` per stato autenticazione e ruolo utente
- [ ] Implementare ProtectedRoute component per guardare le pagine
- [ ] Definire ruoli: Admin (tutti i permessi), Viewer/Operator (sola lettura)
- [ ] Implementare controlli autorizzazione per azioni basati sul ruolo
- [ ] Nascondere pulsanti Create/Edit/Delete per ruoli Viewer/Operator
- [ ] Implementare logout con pulizia token
- [ ] Reindirizzare al login se non autenticato o token non valido
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-016: WebSocket per Real-time Alarm Notifications

**Description:** Come utente, voglio ricevere notifiche in tempo reale quando vengono attivati nuovi allarmi senza dover刷新 la pagina.

**Acceptance Criteria:**

- [ ] Implementare WebSocket connection in `hooks/useAlarmsWebSocket.ts`
- [ ] Stabilire connessione WebSocket al backend endpoint `/ws/alarms`
- [ ] Ricevere eventi: ALARM_TRIGGERED, ALARM_ACKNOWLEDGED, ALARM_CLEARED
- [ ] Aggiornare automaticamente la lista allarmi quando arrivano eventi
- [ ] Mostrare toast notification per nuovi allarmi attivi
- [ ] Gestire riconnessione automatica in caso di disconnessione
- [ ] Implementare heartbeat per keep-alive
- [ ] Chiudere connessione all'unmount del componente
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-017: Export Configurazione

**Description:** Come utente, voglio esportare l'intera configurazione del sistema in formato JSON con versione e timestamp per backup e scopi di migrazione.

**Acceptance Criteria:**

- [ ] Creare `api/config.ts` con funzione `exportConfiguration()`
- [ ] Backend endpoint `/api/config/export` restituisce JSON con:
  - Campo `version: "1.0.0"`
  - Campo `exported_at` con timestamp ISO 8601
  - Organizzazioni complete
  - Sites con relativi areas
  - Areas con relativi gateways
  - Gateways con relativi tags
  - Configurazioni complete di connessione e allarmi
- [ ] Creare pulsante "Export Configuration" in Dashboard
- [ ] Triggerare download del file JSON con nome `industrial-edge-config-YYYYMMDD-HHMMSS.json`
- [ ] Gestire stati loading ed errori
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-018: Import Configurazione

**Description:** Come utente, voglio importare una configurazione esistente da file JSON per ripristinare o migrare la configurazione del sistema, con scelta tra strategia merge o replace.

**Acceptance Criteria:**

- [ ] Creare funzione `importConfiguration(jsonFile, strategy)` in `api/config.ts`
- [ ] Backend endpoint `/api/config/import` accetta JSON multipart con parametro strategy
- [ ] Validare struttura JSON e versione prima dell'import
- [ ] Creare UI con file input per selezionare file JSON
- [ ] Mostrare preview della configurazione da importare (conteggi: org, sites, areas, gateways, tags)
- [ ] Mostrare versione del file e data di export
- [ ] Offrire scelta tra strategia MERGE o REPLACE
- [ ] MERGE aggiorna solo le entità nel file, mantiene quelle esistenti
- [ ] REPLACE sovrascrive completamente la configurazione
- [ ] Conferma obbligatoria prima dell'import
- [ ] Mostrare progress bar durante import
- [ ] Mostrare toast di successo/errore con dettagli
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

### US-019: Integrazione Docker Compose

**Description:** Come operatore, voglio avviare l'intero sistema (backend + frontend) con un solo comando docker-compose per semplificare il deployment.

**Acceptance Criteria:**

- [ ] Aggiornare `docker-compose.yml` con servizio web-ui
- [ ] Configurare environment variable VITE_API_URL
- [ ] Configurare dipendenza da core-api
- [ ] Configurare network industrial-network
- [ ] Expose port 3000 per web-ui
- [ ] `docker-compose up` avvia backend e frontend
- [ ] Frontend accessibile su http://localhost:3000
- [ ] Frontend comunica correttamente con backend

### US-020: Integrazione Toaster in App

**Description:** Come utente, voglio vedere notifiche toast per operazioni riuscite/fallite per avere feedback immediato sulle azioni.

**Acceptance Criteria:**

- [ ] Aggiungere `Toaster` component in `App.tsx`
- [ ] Integrare `useToast` hook in tutte le mutations
- [ ] Mostrare toast su successo create/update/delete
- [ ] Mostrare toast su errore con messaggio specifico
- [ ] Toast auto-dismiss dopo timeout
- [ ] Typecheck passa senza errori
- [ ] Verify in browser using dev-browser skill

## Functional Requirements

### Setup e Configurazione
- FR-1: Il progetto deve essere configurato con Vite, React 18, TypeScript
- FR-2: Devono essere installate tutte le dipendenze specificate (React Query, Zustand, Axios, React Router v7, shadcn/ui components)
- FR-3: Devono essere configurati Tailwind CSS e file di configurazione (vite.config.ts, tsconfig.json, tailwind.config.js)

### API e Types
- FR-4: Il client Axios deve essere configurato con base URL da environment variable
- FR-5: Devono essere definiti TypeScript interfaces per tutte le entità: Organization, Site, Area, Gateway, Tag, Alarm, HealthStats
- FR-6: Devono essere definiti DTOs per creazione (CreateXxxDto) e aggiornamento (UpdateXxxDto)
- FR-7: Devono essere creati moduli API per ogni risorsa con funzioni CRUD

### Componenti UI
- FR-8: Devono essere implementati tutti i componenti shadcn/ui specificati
- FR-9: Ogni componente deve supportare varianti e sizes dove appropriato
- FR-10: I componenti devono essere accessibili e keyboard-friendly

### Layout e Navigazione
- FR-11: La Sidebar deve mostrare tutte le voci di navigazione con active state
- FR-12: L'Header deve mostrare il contesto di navigazione corrente (Org/Site/Area)
- FR-13: Il MainLayout deve combinare Sidebar, Header e area contenuto

### State Management
- FR-14: Lo store Zustand deve gestire selectedOrg, selectedSite, selectedArea
- FR-15: La selezione deve propagare correttamente (selezione Org resetta Site e Area)
- FR-16: Lo store deve essere utilizzato dalle pagine per filtrare i dati

### Data Fetching
- FR-17: Ogni risorsa deve avere un custom hook React Query
- FR-18: Le hooks devono supportare filtering by parent entity (es. sites by org_id)
- FR-19: Le mutations devono invalidare la cache appropriata
- FR-20: Gli allarmi devono auto-refresh ogni 5 secondi
- FR-21: Le health stats devono auto-refresh ogni 10 secondi

### Pagine - Dashboard
- FR-22: La Dashboard deve mostrare 6 card statistiche
- FR-23: Le statistiche devono essere aggiornate in real-time
- FR-24: Devono essere mostrate le prime 5 organizzazioni

### Pagine - CRUD Operations
- FR-25: Ogni pagina (Organizations, Sites, Areas, Gateways, Tags) deve mostrare una tabella con i dati
- FR-26: Ogni pagina deve avere un pulsante per creare nuovi elementi
- FR-27: Ogni pagina deve avere azioni di delete con conferma
- FR-28: Le tabelle devono gestire stati vuoto e loading
- FR-29: Gateways page deve supportare toggle on/off
- FR-30: Tags page deve supportare campi condizionali per historize e alarm
- FR-31: Alarms page deve supportare filtro per stato

### Autenticazione
- FR-32: Il login deve accettare username e password
- FR-33: Il JWT token deve essere salvato in localStorage
- FR-34: Le richieste API devono includere Authorization header
- FR-35: Le pagine protette devono redirect al login se non autenticati
- FR-36: Devono essere definiti ruoli: Admin, Operator, Viewer con permessi appropriati

### Real-time Notifications
- FR-37: Deve essere stabilita una connessione WebSocket per gli allarmi
- FR-38: Gli eventi WebSocket devono aggiornare la lista allarmi
- FR-39: Nuovi allarmi attivi devono mostrare toast notification
- FR-40: La connessione deve ristabilirsi automaticamente se si interrompe

### Export/Import
- FR-41: L'export deve generare un file JSON con tutta la configurazione
- FR-42: Il file exportato deve avere nome con timestamp
- FR-43: Il file exportato deve includere campi `version` e `exported_at`
- FR-44: L'import deve accettare un file JSON e inviarlo al backend
- FR-45: L'import deve mostrare preview e richiedere conferma
- FR-46: L'import deve offrire scelta tra strategia MERGE o REPLACE
- FR-47: MERGE aggiorna solo le entità nel file, mantiene quelle esistenti
- FR-48: REPLACE sovrascrive completamente la configurazione esistente

### Docker Integration
- FR-49: Il docker-compose.yml deve includere il servizio web-ui
- FR-50: Il servizio web-ui deve dipendere da core-api
- FR-51: Il frontend deve essere accessibile su porta 3000

## Non-Goals (Out of Scope)

- Monitoraggio real-time dei valori dei tag (solo configurazione)
- Grafici e trend storici dei dati
- Gestione utenti e permessi da UI (solo autenticazione base)
- Configurazione avanzata del broker MQTT da UI
- Gestione dei template di configurazione
- Versioning della configurazione
- Multi-lingua support (solo inglese/italiano)
- Mobile responsive design (solo desktop/tablet)
- Dark mode (solo light theme iniziale)

## Design Considerations

### UI/UX Requirements
- Utilizzare shadcn/ui per consistenza visiva
- Seguire design pattern: tables per liste, dialogs per forms
- Colori semantici: success (verde), warning (giallo), destructive (rosso)
- Loading states per tutte le operazioni asincrone
- Error handling con messaggi chiari
- Toast notifications per feedback immediato

### Component Reuse
- Riutilizzare Table component per tutte le liste
- Riutilizzare Dialog component per tutti i form
- Riutilizzare Badge per stati e tipi
- Riutilizzare Select per dropdowns

### Navigation
- Breadcrumb per mostrare percorso gerarchico (es: Organizations > Org1 > Site1 > Area1)
- Context retention quando si naviga tra pagine
- Quick navigation dal Sidebar

## Technical Considerations

### Known Constraints
- Backend (core-api) già completo con tutti gli endpoint
- CORS deve essere configurato nel backend
- WebSocket endpoint deve essere implementato nel backend

### Dependencies
- Node.js 20+ per build
- Docker per containerizzazione
- Connessione a Redis per caching (lato backend)
- Connessione a InfluxDB per storizzazione dati

### Integration Points
- Core API su porta 8080
- WebSocket endpoint: `/ws/alarms`
- Export endpoint: `/api/config/export`
- Import endpoint: `/api/config/import`

### Performance Requirements
- Primo render della dashboard < 1 secondo
- Transizioni tra pagine < 500ms
- WebSocket handshake < 1 secondo
- Export di configurazione < 5 secondi per 1000 tag

### Security Requirements
- JWT token scade dopo 24 ore
- **Refresh token automatico in background** - implementare rotazione dei token
- HTTPS in produzione
- Input sanitization su tutti i form
- XSS prevention via React automatic escaping

### Role-Based Access Control
Definizione ruoli e permessi:

| Ruolo     | Create | Read | Update | Delete | Descrizione                              |
|-----------|--------|------|--------|--------|------------------------------------------|
| **Admin** | ✓      | ✓    | ✓      | ✓      | Accesso completo a tutte le operazioni    |
| **Viewer**| ✗      | ✓    | ✗      | ✗      | Solo visualizzazione (read-only)          |
| **Operator**| ✗   | ✓    | ✗      | ✗      | Solo visualizzazione (stesso di Viewer)   |

**Nota:** Inizialmente Operator e Viewer hanno gli stessi permessi (sola lettura). In futuro, Operator potrebbe avere permessi aggiuntivi limitati.

## Success Metrics

- Tempo per creare una nuova organizzazione completa con sites, areas, gateways e tags < 5 minuti
- Numero di click per creare un tag con allarme < 10
- Tempo di rilevamento nuovo allarme via WebSocket < 2 secondi
- Export/import di configurazione con 500 elementi < 30 secondi
- Zero errori TypeScript in produzione
- 95%+ uptime della Web UI

## Open Questions

**Tutte le domande aperte sono state risolte. Di seguito le decisioni prese:**

### Q1: Come gestiremo i refresh token per JWT?
**Decisione:** Implementare refresh token automatico in background. Il sistema userà access token (short-lived, 24 ore) e refresh token (long-lived). Quando l'access token scade, il sistema usa automaticamente il refresh token per ottenerne uno nuovo senza interrompere l'utente.

**Impatto:**
- US-015 aggiornata con requisito refresh token
- AXios interceptor gestirà il refresh automatico
- Backend deve implementare endpoint `/api/auth/refresh`

### Q2: Il backend WebSocket endpoint è già implementato?
**Decisione:** Da verificare - lo stato corrente dell'endpoint `/ws/alarms` è sconosciuto.

**Impatto:**
- US-016 prosegue assumendo che l'endpoint sarà disponibile
- Se non implementato, va creato nel backend:
  - Endpoint WebSocket `/ws/alarms`
  - Eventi: `ALARM_TRIGGERED`, `ALARM_ACKNOWLEDGED`, `ALARM_CLEARED`
  - Broadcast a tutti i client connessi

### Q3: Qual è la strategia per merge/replace durante import?
**Decisione:** L'utente sceglie tra MERGE o REPLACE all'import.

**Strategie:**
- **MERGE:** Aggiorna solo le entità presenti nel file, mantiene quelle esistenti non nel file
- **REPLACE:** Sovrascrive completamente la configurazione (cancella tutto e ricrea dal file)

**Impatto:**
- US-018 aggiornata con scelta strategia
- Backend `/api/config/import` accetta parametro `strategy`
- UI mostra radio button per selezionare strategia prima dell'import

### Q4: Servono permessi granulari per ruolo Operator?
**Decisione:** No - ruoli semplici per MVP:

| Ruolo     | Create | Read | Update | Delete |
|-----------|--------|------|--------|--------|
| Admin     | ✓      | ✓    | ✓      | ✓      |
| Viewer    | ✗      | ✓    | ✗      | ✗      |
| Operator  | ✗      | ✓    | ✗      | ✗      |

**Nota:** Operator e Viewer hanno gli stessi permessi (sola lettura). In futuro Operator potrebbe avere permessi aggiuntivi.

**Impatto:**
- US-015 aggiornata con definizione ruoli
- Pulsanti Create/Edit/Delete nascosti per ruoli non-Admin
- RBAC table aggiunta in Technical Considerations

### Q5: Come gestiremo la versione della configurazione per export/import?
**Decisione:** Il file JSON include campi `version` e `exported_at`.

**Schema export:**
```json
{
  "version": "1.0.0",
  "exported_at": "2026-01-24T12:00:00Z",
  "organizations": [...],
  "sites": [...],
  "areas": [...],
  "gateways": [...],
  "tags": [...]
}
```

**Impatto:**
- US-017 e US-018 aggiornate
- Version permette future migrazioni del formato
- `exported_at` utile per tracciabilità

## Implementation Phases

### Phase 1: Foundation (US-001 to US-006)
Setup progetto, componenti base, API client, state management

### Phase 2: Core CRUD Pages (US-007 to US-013)
Dashboard e pagine di gestione per tutte le entità

### Phase 3: Backend Integration (US-014 to US-015)
CORS e autenticazione

### Phase 4: Advanced Features (US-016 to US-018)
WebSocket, Export/Import

### Phase 5: Production Ready (US-019 to US-020)
Docker Compose, final polish e testing
