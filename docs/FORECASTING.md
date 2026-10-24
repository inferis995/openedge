# Previsioni sui tag — piano e architettura

Stato: **fase 1 su master**: motore statistico sempre attivo, Timestral opzionale.

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

### Fase 1 — fondamenta (fatta)
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
- [x] Modello predefinito: Timestral-28M (cambiabile con `TIMESTRAL_MODEL`, vedi "Installare il modello")
- [x] Pagina **Previsioni** (Analisi → Previsioni): riepilogo per stato, elenco
      filtrabile, dettaglio con storico e previsione, impostazioni per
      organizzazione, sito, area e gateway con ereditarietà
- [x] Modulo `forecast` tra le pagine visibili: spento per un'organizzazione,
      non si calcola nulla per lei e l'interfaccia lo nasconde

### Fase 2 — previsioni operative (fatta)

Su ogni tag con una previsione **affidabile** (mai su una poco affidabile):

- [x] **Allarme previsto.** La previsione è confrontata con le regole
      d'allarme già configurate sul tag (`alarm_definitions`: soglie alta,
      altissima, bassa, bassissima; stati attivo/non attivo per i digitali).
      Nessuna configurazione in più. Probabilità letta dalla fascia:

      | likelihood | quando | |
      |---|---|---|
      | `almost_certain` | anche il bordo ottimista (P10 per una soglia alta) la supera | ~90% |
      | `likely` | la mediana la supera | più probabile che no |
      | `possible` | solo il bordo pessimista (P90) la supera | ~10–50% |

      Una regola già superata adesso non è una previsione (ci pensa il sistema
      d'allarmi). Quando una regola diventa `likely` o `almost_certain` parte
      **una** notifica sui canali degli allarmi (email, Telegram, Slack…),
      come avviso: soggetto `previsione/<tag>`, nessuna riga in `alarm_events`.
- [x] **Vita residua** per i segnali `degradation`: se la soglia è oltre
      l'orizzonte, la tendenza della mediana viene seguita fino a lei (al
      massimo un anno): `likelihood: trend`, `extrapolated: true`. È
      un'estrapolazione, e l'interfaccia la presenta come tale ("se
      l'andamento continua").
- [x] **Anomalie.** A ogni giro i valori arrivati sono confrontati con la
      previsione precedente: anomalo un valore oltre mezza fascia fuori da
      P10–P90 (circa fuori P1–P99). Più robusto dello z-score di
      `/api/aiops/anomalies`, perché tiene conto di cicli e tendenze.
- [x] Dove si vedono: pagina Previsioni (riquadri *A rischio soglia* e *Fuori
      previsione*, colonna *Rischio*, dettaglio), scheda Previsione del tag,
      API (`risks`, `anomaly`, `summary.at_risk`, `summary.anomalous`), CLI
      (`openedge forecast list --status at_risk`), MCP
      (`get_forecast_overview` con `status: at_risk`).

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

## Installare il modello Timestral

### Serve davvero?

No. Le previsioni funzionano **senza installare niente**: core-api ha il motore
statistico in Go, attivo di default su ogni installazione (on-prem, VPS,
Coolify). Timestral è un secondo motore, opzionale. Quando c'è, per ogni tag
core-api prova entrambi sullo storico già noto e tiene quello che ha fatto
meglio. Se il servizio si ferma, si torna da soli al motore statistico.

### Cosa succede, e quando si scarica cosa

| Momento | Cosa viene scaricato | Da dove | Quanto |
|---|---|---|---|
| Build dell'immagine (`make forecast-up`, o Coolify) | Python, PyTorch per CPU, codice `timestral` | PyPI, download.pytorch.org, GitHub | ~1–1,5 GB di immagine |
| Al posto della build, con GHCR | l'immagine già pronta | ghcr.io/inferis995/openedge/forecaster | idem |
| **Primo avvio** del container | i pesi del modello scelto | Hugging Face (`mistralai/Timestral-*`) | da 21 MB a 837 MB |
| Avvii successivi | niente: i pesi restano nel volume `forecaster-models` | — | — |

Finché i pesi non sono caricati, `/health` del servizio risponde 503 e core-api
usa solo il motore statistico.

### Requisiti

Server x86_64 o ARM64 con Docker, **nessuna GPU**. Valori indicativi per il
container (pesi + PyTorch + margine), da confermare con `docker stats` sul
proprio carico:

| Modello (`TIMESTRAL_MODEL`) | Pesi | RAM per il container | Quando |
|---|---|---|---|
| `mistralai/Timestral-5M` | 21 MB | 1 GB | macchine piccole, edge |
| `mistralai/Timestral-28M` (**default**) | 111 MB | 2 GB | scelta normale |
| `mistralai/Timestral-123M` | 490 MB | 3 GB | più precisione |
| `mistralai/Timestral-209M` | 837 MB | 4 GB | il più preciso |

- CPU: 2 core (`FORECASTER_CPUS`, `FORECASTER_THREADS`). Il calcolo è a lotti
  ogni minuto per i tag in scadenza, non a ogni apertura di pagina.
- Disco: immagine ~1,5 GB + pesi.
- Rete: in uscita verso Hugging Face al primo avvio, oppure installazione
  offline (sotto).
- La durata reale di un lotto si legge nei log: `N series in X s`.

### On-prem (`docker-compose.yml`)

```bash
make forecast-up          # aggiunge FORECASTER_URL al .env, avvia il servizio, riavvia core-api
make forecast-status      # {"status": "ok", "model": "mistralai/Timestral-28M"} quando è pronto
make forecast-logs        # "model … loaded in …s", poi "N series in X s"
make forecast-down        # spegne il modello, resta il motore statistico
```

Modello diverso: `TIMESTRAL_MODEL=mistralai/Timestral-123M` nel `.env`, poi
`make forecast-up` (e alza `FORECASTER_MEM_LIMIT`, vedi la tabella).

### VPS (Traefik)

Come on-prem, con `STACK=vps`:

```bash
make forecast-up STACK=vps
make forecast-status STACK=vps
```

Con le immagini già pronte invece della build (consigliato su VPS piccoli):

```bash
docker compose -f docker-compose.yml -f docker-compose.vps.yml -f docker-compose.ghcr.yml \
  --profile forecast pull forecaster
make forecast-up STACK=vps
```

### Coolify

1. Nella risorsa OpenEdge, attiva il profilo compose **`forecast`** (come per
   `edge`).
2. Variabili d'ambiente: `FORECASTER_URL=http://forecaster:8000` e, se vuoi,
   `TIMESTRAL_MODEL`, `FORECASTER_MEM_LIMIT`, `FORECASTER_CPUS`.
3. Redeploy. Il servizio non ha dominio né porte: parla solo con core-api.
4. Controllo: log del container `forecaster` → `model … loaded`.

### Server senza internet

Su una macchina con internet:

```bash
pip install huggingface_hub
huggingface-cli download mistralai/Timestral-28M --local-dir Timestral-28M
# copia la cartella sul server (config.json + model.safetensors)
```

Sul server:

```bash
make forecast-import-model DIR=./Timestral-28M
# nel .env:
TIMESTRAL_MODEL=/models/Timestral-28M
HF_HUB_OFFLINE=1
make forecast-up
```

L'immagine va costruita o scaricata (GHCR) dove c'è rete e poi caricata con
`docker save` / `docker load`.

### Come verificare che lo usi davvero

- Pagina **Analisi → Previsioni**: in alto *Motori: statistico · Timestral*.
- Dettaglio di un tag: *Motore: timestral · mistralai/Timestral-28M* sui tag
  dove ha fatto meglio del motore statistico (sugli altri resta *statistical*:
  è voluto).
- Prometheus: `openedge_forecast_results_total{engine="timestral"}` cresce;
  `openedge_forecast_engine_errors_total{engine="timestral"}` resta fermo.
- CLI: `openedge forecast list` stampa `Engines: statistical, timestral`.

### Se qualcosa non va

| Sintomo | Causa probabile | Cosa fare |
|---|---|---|
| `forecast-status`: "Not ready" per minuti | download dei pesi in corso o bloccato | `make forecast-logs`; rete verso huggingface.co, oppure installazione offline |
| log: `could not load model … OSError` | pesi non trovati con `HF_HUB_OFFLINE=1` | controlla `TIMESTRAL_MODEL` e il contenuto del volume |
| container riavviato, `OOMKilled` | RAM insufficiente per il modello | alza `FORECASTER_MEM_LIMIT` o scegli un modello più piccolo |
| motori: solo *statistico* | `FORECASTER_URL` assente o core-api non riavviato | `make forecast-up` riavvia core-api |
| errori `engine_errors_total{engine="timestral"}` | servizio giù o lento | i tag continuano con il motore statistico; vedi i log |

### Spegnere

- Solo il modello: `make forecast-down` (resta il motore statistico).
- Tutte le previsioni: `FORECAST_ENABLED=false` su core-api.
- Per una sola organizzazione: Organizzazioni → Impostazioni → Pagine visibili →
  togli *Previsioni*.

### Licenza

Il codice di inferenza (`timestral`) è Apache-2.0. I pesi sono sotto "Modified
MIT" (file `LICENSE` in ogni repository su Hugging Face). Leggerla prima di
installare presso clienti grandi: se i pesi vengono distribuiti con
l'installazione, i termini valgono per loro.
