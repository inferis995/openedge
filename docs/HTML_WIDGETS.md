# Widget HTML nei sinottici

Pagine HTML5 importate nei sinottici, con custom properties collegate ai tag.
Stesso modello dei custom widget AVEVA: la pagina dichiara le proprietà,
la piattaforma le alimenta con i valori live.

## Creare una pagina

Singolo file `.html` (CSS e JS inline). Struttura minima:

```html
<meta name="oe-props" content="pressione1, batteria, ts">
<script>
cwidget.on('pressione1', () => render());
function render() {
  document.getElementById('p').textContent = Number(cwidget.pressione1 ?? 0).toFixed(2);
}
</script>
```

Regole:

- Dichiara le proprietà con `<meta name="oe-props" content="a, b, ts">`
  (oppure un commento `<!-- oe-props: a, b, ts -->`). Senza dichiarazione,
  vengono lette dalle chiamate `cwidget.on('x')` / `cwidget['x']` nel codice.
- API disponibile nella pagina: `cwidget.on(prop, cb)`, `cwidget.off(prop, cb)`,
  lettura `cwidget[prop]`. Niente altro: gli `<script src=".../resources/apis/...">`
  di AVEVA vengono rimossi all'import, il bridge li sostituisce.
- La pagina gira in `iframe` isolato (`sandbox="allow-scripts"`): niente
  navigazione, niente accesso al resto. Solo lettura in v1 (scritture dal
  JS restano locali).
- `ts` non è un tag: la piattaforma passa l'ora dell'ultimo campione
  (unix secondi) senza configurazione.

## Importare e collegare (UI)

1. Sinottico → modifica → aggiungi widget **Pagina HTML** (ridimensionabile).
2. Pannello destro → **Importa file .html**: le properties vengono rilevate.
3. Per ogni property scegli il tag; per `ts` scegli **⏱ Timestamp automatico**.
4. Salva: in vista i valori arrivano live via socket come gli altri widget.

## Dashboard e TV

- Spillo 📌 dalla lista sinottici o dalla vista: la pagina appare in
  dashboard, live, click per ingrandire.
- **Modalità TV** (`/tv/synoptics`): rotazione a tutto schermo ogni 15 s,
  come il kiosk OEE.

## CLI

```bash
openedge synoptics list
openedge synoptics import-html --name "Centrale P-8" --file monitor_p1.html \
  --bind pressione1=331 --bind batteria=333 --ts-prop ts \
  --width 1280 --height 720 --site 2
```

## MCP (agenti)

- `list_synoptics` — elenca le pagine.
- `import_html_widget` — crea una pagina da `html_source` + `bindings`
  (`[{prop, tag_id}]`) + `ts_prop` opzionale.
