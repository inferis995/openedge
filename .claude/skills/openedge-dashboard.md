---
name: openedge-dashboard
description: OpenEdge Dashboard Generator — crea dashboard HTML5 SCADA real-time con MQTT WebSocket
version: 1.0.0
tags: [industrial, iot, scada, dashboard, html5, mqtt, realtime, chart]
requires: [openedge.md]
---

# OpenEdge Dashboard — Skill

Genera dashboard HTML5 professionali stile SCADA/Power BI, standalone (nessun build step),
con aggiornamento **real-time via MQTT WebSocket**.

Leggi prima `openedge.md` per autenticarti e recuperare la lista di tag e gateway.

---

## Compatibilità

- **Claude Code** — `.claude/skills/openedge-dashboard.md`
- **OpenClaw** — `~/.openclaw/skills/openedge-dashboard/SKILL.md`

---

## Infrastruttura real-time

```
Browser
  │
  │  WebSocket ws://{OPENEDGE_HOST}:9001
  │
  ▼
Mosquitto MQTT broker (porta 9001 WebSocket, anonimo)
  │
  │  topic: data/{org}/{site}/{area}/{gateway}/{tag_alias}
  │
  ▼
OpenEdge driver → tag value
```

**Payload MQTT:**
```json
{"tag_id": 5, "org_id": 1, "v": 42.5, "ts": 1711234567000, "q": 0}
```
- `v` = valore (float, bool, int, string)
- `ts` = timestamp milliseconds
- `q` = qualità: **0=GOOD**, 1=BAD, 2=STALE

**Topic wildcard per gateway:**
```
data/{org_slug}/{site_slug}/{area_slug}/{gateway_slug}/#
```
Gli slug sono i nomi in lowercase con spazi→trattini (es. "PLC Pompe 1" → "plc-pompe-1").

---

## Flusso di lavoro dell'agente

### Step 1 — Recupera gerarchia da OpenEdge API

```bash
# Login (vedi openedge.md)
TOKEN=$(...)

# Lista tag con gerarchia
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/tags | python3 -m json.tool

# Lista gateway con stato
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/gateways | python3 -m json.tool
```

Ogni tag ha: `id`, `alias`, `data_type` (REAL/INT/BOOL/DINT), `gateway_id`, `code`
Ogni gateway ha: `id`, `name`, `driver_type`, `connection_status`, + `area` → `site` → `org`

### Step 2 — Costruisci il topic MQTT per ogni tag

```python
def slugify(name):
    return name.lower().replace(' ', '-').replace('_', '-')

topic = f"data/{slugify(org)}/{slugify(site)}/{slugify(area)}/{slugify(gateway)}/{slugify(tag_alias)}"
# es: data/sorical/sito-crotone/area-nord/plc-serbatoio1/portata-ingresso
```

### Step 3 — Genera il file HTML5

Crea un file `dashboard_{nome}.html` con la struttura descritta sotto.
Il file è standalone — aprilo nel browser, nessun server necessario.

---

## Librerie CDN (da includere sempre)

```html
<!-- MQTT.js WebSocket client -->
<script src="https://unpkg.com/mqtt/dist/mqtt.min.js"></script>

<!-- Chart.js per grafici trend -->
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>

<!-- Gauge.js per manometri -->
<script src="https://cdn.jsdelivr.net/npm/gaugeJS/dist/gauge.min.js"></script>
```

---

## Template HTML5 base

```html
<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>OpenEdge Dashboard — {TITOLO}</title>
  <script src="https://unpkg.com/mqtt/dist/mqtt.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/gaugeJS/dist/gauge.min.js"></script>
  <style>
    /* ── SCADA Dark Theme ── */
    :root {
      --bg:        #0d1117;
      --surface:   #161b22;
      --border:    #30363d;
      --text:      #e6edf3;
      --muted:     #8b949e;
      --good:      #3fb950;
      --warn:      #d29922;
      --critical:  #f85149;
      --blue:      #58a6ff;
      --purple:    #bc8cff;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: 'Segoe UI', system-ui, sans-serif;
      min-height: 100vh;
    }
    header {
      background: var(--surface);
      border-bottom: 1px solid var(--border);
      padding: 14px 24px;
      display: flex;
      align-items: center;
      justify-content: space-between;
    }
    header h1 { font-size: 1.1rem; font-weight: 600; letter-spacing: .5px; }
    #mqtt-status {
      font-size: .8rem;
      padding: 4px 10px;
      border-radius: 12px;
      background: #21262d;
      color: var(--muted);
    }
    #mqtt-status.connected { color: var(--good); }
    #mqtt-status.error     { color: var(--critical); }

    .grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
      gap: 16px;
      padding: 24px;
    }
    .card {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 18px;
      position: relative;
      transition: border-color .2s;
    }
    .card:hover { border-color: var(--blue); }
    .card.bad   { border-color: var(--critical); }
    .card.warn  { border-color: var(--warn); }

    .card-label {
      font-size: .72rem;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: .8px;
      margin-bottom: 8px;
    }
    .card-value {
      font-size: 2rem;
      font-weight: 700;
      font-variant-numeric: tabular-nums;
      line-height: 1;
    }
    .card-unit  { font-size: .9rem; color: var(--muted); margin-left: 4px; }
    .card-ts    { font-size: .7rem; color: var(--muted); margin-top: 8px; }
    .card-quality {
      position: absolute;
      top: 14px; right: 14px;
      width: 8px; height: 8px;
      border-radius: 50%;
      background: var(--muted);
    }
    .card-quality.good { background: var(--good); box-shadow: 0 0 6px var(--good); }
    .card-quality.bad  { background: var(--critical); }

    /* LED Bool widget */
    .led {
      width: 48px; height: 48px;
      border-radius: 50%;
      background: #21262d;
      border: 3px solid var(--border);
      margin: 8px 0;
      transition: all .3s;
    }
    .led.on  { background: var(--good);     box-shadow: 0 0 16px var(--good); border-color: var(--good); }
    .led.off { background: #21262d; border-color: var(--border); }

    /* Gauge canvas */
    .gauge-wrap { position: relative; height: 120px; }

    /* Trend card (full width) */
    .card.trend {
      grid-column: span 2;
      min-height: 200px;
    }
    @media (max-width: 600px) { .card.trend { grid-column: span 1; } }

    /* Alarm bar */
    #alarm-bar {
      background: #1a0a0a;
      border-bottom: 1px solid var(--critical);
      padding: 8px 24px;
      font-size: .82rem;
      color: var(--critical);
      display: none;
    }
    #alarm-bar.active { display: block; }
  </style>
</head>
<body>

<header>
  <h1>⚡ {TITOLO DASHBOARD}</h1>
  <span id="mqtt-status">● Connessione...</span>
</header>

<div id="alarm-bar"></div>

<div class="grid" id="grid">
  <!-- Le card vengono generate dal JS -->
</div>

<script>
// ═══════════════════════════════════════════
//  CONFIGURAZIONE — modifica questi valori
// ═══════════════════════════════════════════
const MQTT_HOST = 'localhost';
const MQTT_WS_PORT = 9001;
const ORG_ID = 1;

// Definizione tag da visualizzare
// Recupera questi dati da GET /api/tags e GET /api/gateways
const TAGS = [
  // { id, alias, type: 'real'|'bool'|'int', unit, topic, min, max, trend }
  // Esempi:
  {
    id: 1,
    alias: 'Portata Ingresso',
    type: 'real',
    unit: 'm³/h',
    topic: 'data/myorg/sito/area/gateway/portata-ingresso',
    min: 0, max: 100,
    trend: true   // mostra grafico trend
  },
  {
    id: 2,
    alias: 'Pompa 1',
    type: 'bool',
    topic: 'data/myorg/sito/area/gateway/pompa-1'
  },
  {
    id: 3,
    alias: 'Pressione Rete',
    type: 'real',
    unit: 'bar',
    topic: 'data/myorg/sito/area/gateway/pressione-rete',
    min: 0, max: 10,
    trend: true
  },
];

// ═══════════════════════════════════════════
//  STATO
// ═══════════════════════════════════════════
const state = {};         // topic → {v, ts, q}
const trendData = {};     // tag_id → {labels[], values[]}
const charts = {};        // tag_id → Chart instance
const TREND_POINTS = 60;  // ultimi N valori nel trend

// ═══════════════════════════════════════════
//  BUILD UI
// ═══════════════════════════════════════════
function buildGrid() {
  const grid = document.getElementById('grid');
  TAGS.forEach(tag => {
    const card = document.createElement('div');
    card.className = 'card';
    card.id = `card-${tag.id}`;
    if (tag.trend) card.classList.add('trend');

    if (tag.type === 'bool') {
      card.innerHTML = `
        <div class="card-quality" id="q-${tag.id}"></div>
        <div class="card-label">${tag.alias}</div>
        <div class="led off" id="led-${tag.id}"></div>
        <div class="card-value" id="val-${tag.id}" style="font-size:1rem">—</div>
        <div class="card-ts" id="ts-${tag.id}">—</div>`;
    } else if (tag.trend) {
      card.innerHTML = `
        <div class="card-quality" id="q-${tag.id}"></div>
        <div class="card-label">${tag.alias} <span class="card-unit">${tag.unit||''}</span></div>
        <div class="card-value" id="val-${tag.id}">—</div>
        <div class="card-ts" id="ts-${tag.id}">—</div>
        <canvas id="chart-${tag.id}" style="margin-top:12px;max-height:120px"></canvas>`;
      trendData[tag.id] = { labels: [], values: [] };
    } else {
      card.innerHTML = `
        <div class="card-quality" id="q-${tag.id}"></div>
        <div class="card-label">${tag.alias}</div>
        <div class="card-value" id="val-${tag.id}">—<span class="card-unit">${tag.unit||''}</span></div>
        <div class="card-ts" id="ts-${tag.id}">—</div>`;
    }
    grid.appendChild(card);

    // Inizializza chart se trend
    if (tag.trend) {
      const ctx = document.getElementById(`chart-${tag.id}`);
      charts[tag.id] = new Chart(ctx, {
        type: 'line',
        data: {
          labels: [],
          datasets: [{
            data: [],
            borderColor: '#58a6ff',
            backgroundColor: 'rgba(88,166,255,0.08)',
            borderWidth: 2,
            pointRadius: 0,
            fill: true,
            tension: 0.3
          }]
        },
        options: {
          responsive: true,
          animation: false,
          plugins: { legend: { display: false } },
          scales: {
            x: { display: false },
            y: {
              min: tag.min ?? undefined,
              max: tag.max ?? undefined,
              grid: { color: '#21262d' },
              ticks: { color: '#8b949e', font: { size: 10 } }
            }
          }
        }
      });
    }
  });
}

// ═══════════════════════════════════════════
//  UPDATE UI
// ═══════════════════════════════════════════
function updateCard(tag, payload) {
  const { v, ts, q } = payload;
  const card  = document.getElementById(`card-${tag.id}`);
  const valEl = document.getElementById(`val-${tag.id}`);
  const tsEl  = document.getElementById(`ts-${tag.id}`);
  const qEl   = document.getElementById(`q-${tag.id}`);
  if (!card) return;

  // Quality indicator
  qEl.className = 'card-quality ' + (q === 0 ? 'good' : 'bad');
  card.classList.toggle('bad', q !== 0);

  // Timestamp
  tsEl.textContent = new Date(ts).toLocaleTimeString('it-IT');

  if (tag.type === 'bool') {
    const on = v === true || v === 1 || v === 1.0;
    document.getElementById(`led-${tag.id}`).className = 'led ' + (on ? 'on' : 'off');
    valEl.textContent = on ? 'ON' : 'OFF';
    valEl.style.color = on ? 'var(--good)' : 'var(--muted)';
  } else {
    const num = typeof v === 'number' ? v.toFixed(tag.decimals ?? 2) : v;
    valEl.innerHTML = `${num}<span class="card-unit">${tag.unit||''}</span>`;

    // Colora in base a soglie (opzionale)
    if (tag.max && v > tag.max * 0.9) {
      valEl.style.color = 'var(--critical)';
    } else if (tag.max && v > tag.max * 0.75) {
      valEl.style.color = 'var(--warn)';
    } else {
      valEl.style.color = 'var(--text)';
    }

    // Trend
    if (tag.trend && charts[tag.id]) {
      const d = trendData[tag.id];
      d.labels.push(new Date(ts).toLocaleTimeString('it-IT'));
      d.values.push(v);
      if (d.labels.length > TREND_POINTS) {
        d.labels.shift(); d.values.shift();
      }
      charts[tag.id].data.labels   = d.labels;
      charts[tag.id].data.datasets[0].data = d.values;
      charts[tag.id].update('none');
    }
  }
}

// ═══════════════════════════════════════════
//  MQTT CONNECTION
// ═══════════════════════════════════════════
function connectMQTT() {
  const statusEl = document.getElementById('mqtt-status');
  const url = `ws://${MQTT_HOST}:${MQTT_WS_PORT}/mqtt`;

  const client = mqtt.connect(url, {
    clientId: `openedge-dashboard-${Math.random().toString(16).slice(2,8)}`,
    clean: true,
    reconnectPeriod: 3000,
    connectTimeout: 10000
  });

  // Mappa topic → tag
  const topicMap = {};
  TAGS.forEach(tag => { topicMap[tag.topic] = tag; });

  client.on('connect', () => {
    statusEl.textContent = '● Connesso';
    statusEl.className = 'connected';
    // Sottoscrivi tutti i topic
    const topics = TAGS.map(t => t.topic);
    client.subscribe(topics, { qos: 0 });
  });

  client.on('message', (topic, message) => {
    try {
      const payload = JSON.parse(message.toString());
      const tag = topicMap[topic];
      if (tag) updateCard(tag, payload);
    } catch(e) { /* ignora messaggi malformati */ }
  });

  client.on('reconnect', () => {
    statusEl.textContent = '● Riconnessione...';
    statusEl.className = '';
  });

  client.on('error', (err) => {
    statusEl.textContent = '● Errore connessione';
    statusEl.className = 'error';
    console.error('MQTT error:', err);
  });

  client.on('offline', () => {
    statusEl.textContent = '● Offline';
    statusEl.className = 'error';
  });
}

// ═══════════════════════════════════════════
//  INIT
// ═══════════════════════════════════════════
buildGrid();
connectMQTT();
</script>
</body>
</html>
```

---

## Widget types — riferimento rapido

| Tipo tag | Widget generato | Descrizione |
|---|---|---|
| `REAL` + `trend: true` | Valore numerico + grafico linea | Portata, pressione, temperatura |
| `REAL` senza trend | Solo valore numerico grande | KPI singolo |
| `BOOL` | LED verde/rosso + ON/OFF | Pompa, valvola, contatto |
| `INT` / `DINT` | Valore numerico | Contatori, interi |

---

## Come generare la dashboard — prompt esempi

### Dashboard di un gateway
```
Leggi i tag del gateway "PLC-Serbatoio1" da OpenEdge
(host: localhost:8081, admin/admin123, org 1).
Genera una dashboard HTML5 con tutti i tag:
- REAL → card con trend se è portata/pressione/livello
- BOOL → card LED ON/OFF
- Titolo: "Serbatoio 1 — Real-Time"
- MQTT host: localhost, WebSocket port: 9001
Salva come dashboard_serbatoio1.html
```

### Dashboard personalizzata
```
Genera una dashboard HTML5 OpenEdge per:
- Tag 5 "Portata Ingresso" (REAL, m³/h, 0-100) con trend
- Tag 6 "Pompa 1" (BOOL)
- Tag 7 "Pompa 2" (BOOL)
- Tag 8 "Pressione Rete" (REAL, bar, 0-10) con trend
MQTT WebSocket: ws://localhost:9001
Salva come dashboard_pompe.html
```

---

## Costruzione automatica dei topic MQTT

Quando recuperi i tag dall'API, ottieni `gateway_name`, `site_name` ecc.
Trasformali in slug per costruire il topic:

```python
import re

def slugify(s):
    s = s.lower().strip()
    s = re.sub(r'[\s_]+', '-', s)
    s = re.sub(r'[^a-z0-9-]', '', s)
    return s

# Dati dalla API OpenEdge (da /api/tags con gerarchia)
topic = f"data/{slugify(org_name)}/{slugify(site_name)}/{slugify(area_name)}/{slugify(gateway_name)}/{slugify(tag_alias)}"
```

---

## Allarmi in tempo reale (opzionale)

Aggiungi al client MQTT la sottoscrizione al topic allarmi:

```javascript
// Sottoscrivi allarmi org
client.subscribe(`sys/alarms/${ORG_ID}/#`, { qos: 0 });

client.on('message', (topic, message) => {
  if (topic.startsWith(`sys/alarms/${ORG_ID}/`)) {
    const alarm = JSON.parse(message.toString());
    const bar = document.getElementById('alarm-bar');
    bar.textContent = `🔴 ALLARME: ${alarm.tag_alias || topic} — ${alarm.message || alarm.v}`;
    bar.className = 'active';
    setTimeout(() => bar.className = '', 30000); // sparisce dopo 30s
  }
});
```

---

## Note operative

- **Porta MQTT WebSocket:** 9001 (esposta da docker-compose, anonima)
- **Porta MQTT standard:** 18830 (non WebSocket — per client MQTT nativi)
- **Il file HTML è standalone** — aprilo direttamente nel browser, nessun server
- **Cross-origin:** il broker è anonimo, nessun CORS da gestire
- **Scalabilità:** ogni tab del browser apre una connessione MQTT separata

*Aggiornare questo file quando cambiano i topic MQTT o le porte di OpenEdge.*
