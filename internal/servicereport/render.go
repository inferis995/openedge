package servicereport

import (
	"fmt"
	"html/template"
	"io"
	"time"
)

// Render writes the report as a self-contained HTML document.
//
// HTML rather than PDF, and self-contained rather than a page in the app: this
// is a file that gets saved, attached to an email, and printed. No external
// stylesheet, no script, no font to fetch — it has to look the same on a
// machine that has never heard of OpenEdge, including one with no network.
// Printing it to PDF is one keystroke in every browser, which is a cheaper
// dependency than a headless renderer in the container.
//
// html/template, not text/template: every name in here — an organization, a
// gateway, a tag alias — is typed by a user, and this document is opened in a
// browser. The distinction is one import and the whole difference between an
// escaped document and an injected one.
func Render(w io.Writer, r *Report) error {
	return reportTemplate.Execute(w, r)
}

var reportFuncs = template.FuncMap{
	"date": func(t time.Time) string { return t.Format("02/01/2006") },
	"datetime": func(t time.Time) string {
		return t.Format("02/01/2006 15:04")
	},
	"pct": func(f float64) string { return fmt.Sprintf("%.2f%%", f*100) },
	"dur": func(d time.Duration) string {
		switch {
		case d <= 0:
			return "—"
		case d < time.Minute:
			return fmt.Sprintf("%d s", int(d.Seconds()))
		case d < time.Hour:
			return fmt.Sprintf("%d min", int(d.Minutes()))
		case d < 48*time.Hour:
			return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
		}
		return fmt.Sprintf("%d giorni %d h", int(d.Hours())/24, int(d.Hours())%24)
	},
	// endOfPeriod renders the last instant a reader would call part of the
	// period. The range is half-open — [1 Aug, 1 Sep) — and a heading that says
	// "1 agosto — 1 settembre" reads as if a day were counted twice.
	"endOfPeriod": func(t time.Time) string { return t.Add(-time.Second).Format("02/01/2006") },
}

var reportTemplate = template.Must(template.New("report").Funcs(reportFuncs).Parse(`<!DOCTYPE html>
<html lang="it">
<head>
<meta charset="utf-8">
<title>Rapporto di servizio — {{.Organization}} — {{date .From}}</title>
<style>
  :root { color-scheme: light; }
  body { font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
         color: #18181b; background: #fff; margin: 0; padding: 32px; line-height: 1.5; }
  .wrap { max-width: 900px; margin: 0 auto; }
  h1 { font-size: 22px; margin: 0 0 4px; }
  h2 { font-size: 15px; margin: 28px 0 8px; padding-bottom: 4px;
       border-bottom: 1px solid #e4e4e7; text-transform: uppercase;
       letter-spacing: .04em; color: #52525b; }
  .sub { color: #71717a; font-size: 13px; margin: 0 0 4px; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid #f4f4f5; }
  th { color: #71717a; font-weight: 600; font-size: 11px;
       text-transform: uppercase; letter-spacing: .04em; }
  td.n, th.n { text-align: right; font-variant-numeric: tabular-nums; }
  .tiles { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 8px; }
  .tile { flex: 1 1 140px; border: 1px solid #e4e4e7; border-radius: 8px; padding: 10px 12px; }
  .tile .k { font-size: 11px; color: #71717a; text-transform: uppercase; letter-spacing: .04em; }
  .tile .v { font-size: 20px; font-weight: 600; font-variant-numeric: tabular-nums; }
  .note { font-size: 12px; color: #71717a; margin-top: 6px; }
  footer { margin-top: 36px; padding-top: 10px; border-top: 1px solid #e4e4e7;
           font-size: 11px; color: #a1a1aa; }
  @media print {
    body { padding: 0; }
    h2 { break-after: avoid; }
    table { break-inside: auto; }
    tr { break-inside: avoid; }
  }
</style>
</head>
<body><div class="wrap">

<h1>Rapporto di servizio — {{.Organization}}</h1>
<p class="sub">Periodo dal {{date .From}} al {{endOfPeriod .To}} · generato il {{datetime .GeneratedAt}}</p>

<h2>Impianto</h2>
<div class="tiles">
  <div class="tile"><div class="k">Dispositivi</div><div class="v">{{.Plant.Devices}}</div></div>
  <div class="tile"><div class="k">Online</div><div class="v">{{.Plant.Online}}</div></div>
  <div class="tile"><div class="k">Non raggiungibili</div><div class="v">{{.Plant.Offline}}</div></div>
  <div class="tile"><div class="k">Disabilitati</div><div class="v">{{.Plant.Disabled}}</div></div>
  <div class="tile"><div class="k">Tag</div><div class="v">{{.Plant.Tags}}</div></div>
</div>
{{if .Plant.ByProtocol}}
<table>
  <thead><tr><th>Protocollo</th><th class="n">Dispositivi</th></tr></thead>
  <tbody>{{range $protocol, $n := .Plant.ByProtocol}}<tr><td>{{$protocol}}</td><td class="n">{{$n}}</td></tr>{{end}}</tbody>
</table>
{{end}}

<h2>Continuità di servizio</h2>
<div class="tiles">
  <div class="tile"><div class="k">Disponibilità</div><div class="v">{{pct .Service.FleetAvailability}}</div></div>
  <div class="tile"><div class="k">Interruzioni</div><div class="v">{{.Service.Interruptions}}</div></div>
  <div class="tile"><div class="k">Fermo totale</div><div class="v">{{dur .Service.Downtime}}</div></div>
</div>
<p class="note">
  La disponibilità è la quota del tempo-dispositivo del periodo in cui i gateway
  hanno risposto. Interruzioni sovrapposte sullo stesso gateway contano una
  volta sola; i dispositivi disabilitati sono esclusi, perché nessuno sta
  chiedendo loro niente.
</p>
{{if .Service.Gateways}}
<table>
  <thead><tr><th>Gateway</th><th class="n">Interruzioni</th><th class="n">Fermo</th><th class="n">Disponibilità</th></tr></thead>
  <tbody>{{range .Service.Gateways}}<tr>
    <td>{{.GatewayName}}</td>
    <td class="n">{{.Interruptions}}</td>
    <td class="n">{{dur .Downtime}}</td>
    <td class="n">{{pct .Availability}}</td>
  </tr>{{end}}</tbody>
</table>
{{end}}

<h2>Allarmi</h2>
<div class="tiles">
  <div class="tile"><div class="k">Totali</div><div class="v">{{.Alarms.Total}}</div></div>
  <div class="tile"><div class="k">Ancora aperti</div><div class="v">{{.Alarms.StillOpen}}</div></div>
  <div class="tile"><div class="k">Presi in carico</div><div class="v">{{.Alarms.Acknowledged}}</div></div>
  <div class="tile"><div class="k">Tempo medio di presa in carico</div><div class="v">{{dur .Alarms.MeanAck}}</div></div>
</div>
{{if .Alarms.TopTags}}
<table>
  <thead><tr><th>Tag con più allarmi</th><th class="n">Allarmi</th></tr></thead>
  <tbody>{{range .Alarms.TopTags}}<tr><td>{{.Alias}}</td><td class="n">{{.Count}}</td></tr>{{end}}</tbody>
</table>
{{end}}

<h2>Storico</h2>
<div class="tiles">
  <div class="tile"><div class="k">Campioni registrati</div><div class="v">{{.History.Samples}}</div></div>
  <div class="tile"><div class="k">Tag con dati</div><div class="v">{{.History.Tags}}</div></div>
</div>

{{with .Backups}}
<h2>Copie di sicurezza</h2>
<div class="tiles">
  <div class="tile"><div class="k">Eseguite</div><div class="v">{{.Taken}}</div></div>
  <div class="tile"><div class="k">Fallite</div><div class="v">{{.Failed}}</div></div>
  <div class="tile"><div class="k">Ultima</div><div class="v">{{if .LastTaken}}{{datetime .LastTaken}}{{else}}—{{end}}</div></div>
</div>
{{end}}

<footer>OpenEdge · rapporto generato automaticamente dai dati registrati nel periodo indicato.</footer>
</div></body>
</html>
`))
