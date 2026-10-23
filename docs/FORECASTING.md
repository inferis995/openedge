# Previsioni sui tag — piano e architettura

Stato: **fase 1 in sviluppo** (branch `claude/ciao-Cvmm2`, non ancora su master).

## Obiettivo

Prevedere l'andamento di qualsiasi tag — sensori di processo, macchine, contatori,
stati, batterie — senza codice per cliente. Il comportamento lo decide il **tipo di
segnale** del tag, dedotto dai dati e correggibile per organizzazione, sito, area,
gateway o singolo tag.

Principi:

1. **Generico.** Nessun riferimento a un settore o a un dispositivo nel codice. I
   pacchetti di settore (rete idrica, compressori…) sono dati: profili predefiniti.
2. **Mai una previsione inaffidabile mostrata come buona.** Ogni previsione è
   confrontata sui dati passati con una previsione ingenua; se non la batte è
   marcata *poco affidabile* e l'interfaccia lo dice.
3. **Il modello è sostituibile.** core-api parla con un'interfaccia; Timestral è il
   primo motore, una previsione statistica in Go è sempre disponibile.
4. **I dati non escono dal server.** Il modello gira in locale, su CPU.
5. **Opzionale.** Senza il servizio `forecaster` l'app funziona come prima, con le
   previsioni statistiche.

## Architettura

```
            ogni N minuti
core-api ── forecast.Runner ──► legge tag_history (aggregati Timescale)
               │                 classifica il segnale, ricampiona
               │
               ├─► Forecaster "statistical" (Go, sempre presente)
               └─► Forecaster "timestral"  (HTTP → servizio forecaster, opzionale)
               │
               ▼
          backtest (stesso motore sui dati già noti) → affidabilità
               │
               ▼
          tag_forecasts (una riga per tag, ultima previsione)
               │
     GET /api/tags/:id/forecast ──► Trend: fascia di previsione dopo "adesso"
     GET /api/forecast/overview ──► pagina Previsioni
```

### Tipi di segnale (`kind`)

| kind | riconosciuto da | cosa si prevede |
|---|---|---|
| `process` | default per numerici | valore, fascia P10–P90 |
| `cyclic` | autocorrelazione forte a 24 h o 7 g | valore con stagionalità |
| `degradation` | tendenza netta rispetto al rumore, poca stagionalità | valore e tendenza (fase 2: vita residua) |
| `counter` | non decresce quasi mai (reset ammessi) | il ritmo (differenze), poi il totale |
| `state` | BOOL o solo 0/1 | quota di tempo a 1 per intervallo |

### Profilo di previsione ed ereditarietà

`forecast_profiles(org_id, scope, scope_id, settings jsonb)` con `scope` in
`org | site | area | gateway | tag`. Le impostazioni effettive di un tag sono la
fusione dal livello più generale al più specifico; ogni campo assente eredita.

```json
{ "enabled": true, "kind": "auto", "horizon_hours": 24, "context_days": 28,
  "state_tag_id": null }
```

Default (nessun profilo): attivo su tag numerici storicizzati, `kind` automatico,
orizzonte 24 h.

### Frequenza (risoluzione)

Scelta dai dati: il passo è il più piccolo tra 1 min, 5 min, 15 min, 1 h, 1 g che
non sia più fine dell'intervallo tipico tra le misure. Un Modbus a 1 s lavora a
1–15 min, un data logger che manda ogni 8 h a 1 h o 1 g. Il contesto è limitato
a 2048 passi, l'orizzonte a 512.

### Affidabilità (backtest)

L'ultimo tratto lungo quanto l'orizzonte viene nascosto, previsto con lo stesso
motore e confrontato con i valori veri:

- `skill = 1 − MAE(modello) / MAE(previsione ingenua)` — > 0 batte l'ingenua;
- `coverage` — quota dei valori veri dentro la fascia P10–P90 (ideale 0,8).

`status`: `ok` (skill > 0), `unreliable` (skill ≤ 0), `learning` (storico
insufficiente), `error`.

## Fasi

### Fase 1 — fondamenta (in corso)
- [x] Piano
- [x] Migrazioni: `forecast_profiles`, `tag_forecasts`
- [x] `internal/forecast`: serie, ricampionamento, classificazione, previsione
      statistica, backtest, fusione profili, interfaccia `Forecaster`, client HTTP
- [x] Runner in core-api (pianificazione, lotti, timeout, metriche Prometheus)
- [x] API: `GET /api/tags/:id/forecast`, `POST /api/tags/:id/forecast/refresh`,
      `GET|PUT /api/forecast/profiles`
- [x] Servizio `services/forecaster` (Python, Timestral su CPU), profilo compose
      `forecast`, `FORECASTER_URL` in core-api
- [x] Trend: fascia di previsione e indicatore di affidabilità
- [x] Tag: scheda *Previsione* (attiva, tipo, orizzonte, stato attuale)
- [x] Test e documentazione d'esercizio
- [ ] Prova sui dati veri (Sorical) e scelta della dimensione del modello
- [x] Pagina **Previsioni** (Analisi → Previsioni): riepilogo per stato, elenco
      filtrabile, dettaglio con storico e previsione, impostazioni per
      organizzazione, sito, area e gateway con ereditarietà
- [x] Modulo `forecast` tra le pagine visibili: spento per un'organizzazione,
      non si calcola nulla per lei e l'interfaccia lo nasconde

### Fase 2 — previsioni operative
- Allarme previsto: probabilità di superare le soglie già configurate (allarmi e
  `tag_bands`) entro l'orizzonte, come avviso ISA-18.2, non come allarme.
- Vita residua per `degradation`: tempo stimato al limite.
- Anomalia: valore reale fuori dalla fascia prevista in precedenza (sostituisce
  lo z-score di `/api/aiops/anomalies`).

### Fase 3 — macchine
- Asset: gruppo di tag della stessa macchina, previsti insieme (multivariato).
- Condizionamento sullo stato di marcia (`state_tag_id`, profili OEE): il modello
  vede solo i periodi in marcia.
- Turni e finestre di manutenzione come covariate note in futuro.
- Indice di salute dell'asset.

### Fase 4 — estensioni
- Pacchetti di settore (profili JSON).
- Previsioni su contatori e KPI/OEE (fine turno, fine mese).
- Strumento `forecast` per il server MCP della CLI.

## Test

```bash
# Go, con un Postgres usa-e-getta (le tabelle vengono ricreate):
OPENEDGE_TEST_DSN='postgres://user@127.0.0.1:5432/scratch?sslmode=disable' \
  go test -p 1 ./internal/forecast/ ./internal/handlers/ -run 'Forecast|Postgres'
# Servizio Python, con un modello finto (solo numpy):
cd services/forecaster && python -m unittest -v test_app.py
```

## Esercizio

```bash
# Attivare il motore Timestral (opzionale)
docker compose --profile forecast up -d forecaster
# core-api: FORECASTER_URL=http://forecaster:8000
```

Il modello predefinito è `mistralai/Timestral-28M` (111 MB, CPU). Si cambia con
`TIMESTRAL_MODEL`. I pesi si scaricano una volta da Hugging Face e restano nel
volume `forecaster-cache`; per un server senza internet si copiano nel volume e si
imposta `HF_HUB_OFFLINE=1`.

**Licenza.** Il codice di inferenza (`timestral`) è Apache-2.0; i pesi sono sotto
"Modified MIT" (file `LICENSE` in ogni repository su Hugging Face). Leggerla prima
di installare presso clienti grandi: se i pesi vengono distribuiti con
l'installazione, i termini valgono per loro.
