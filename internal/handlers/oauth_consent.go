package handlers

import (
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// The sign-in and consent page.
//
// It is served from here rather than from the React app on purpose. This page
// is the one place a user is asked to grant a third party access to their
// plant, and it must not depend on a separate build being deployed, on routes
// the SPA might rename, or on a bundle loading at all. It is one self-contained
// document with no scripts and no external requests.
//
// Everything variable on it — the client's name above all, which whoever
// registered the client chose — goes through html/template, so a client called
// `<script>` renders as text.

type consentView struct {
	Request  *authzRequest
	Action   string
	Error    string
	Username string
	// Set once the password was accepted and only the second factor is left.
	MFAToken string
}

// Scopes describes, in plain words, what the client is asking for. The user is
// deciding, so the decision has to be legible without knowing what a scope is.
func (v consentView) Scopes() []string {
	var out []string
	for _, s := range strings.Fields(v.Request.Scope) {
		switch s {
		case ScopeRead:
			out = append(out, "Leggere la configurazione e i valori degli impianti")
		case ScopeWrite:
			out = append(out, "Scrivere: comandare i tag, creare gateway, tipi e sinottici")
		default:
			out = append(out, s)
		}
	}
	return out
}

var consentTmpl = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="it"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Autorizza {{.Request.ClientName}} — OpenEdge</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font: 15px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
         background:#f4f5f7; color:#111827; padding:24px; }
  @media (prefers-color-scheme: dark) { body { background:#0b1220; color:#e5e7eb; } }
  .card { width:100%; max-width:420px; background:#fff; border-radius:14px; padding:28px;
          box-shadow:0 1px 3px rgba(0,0,0,.12), 0 8px 28px rgba(0,0,0,.08); }
  @media (prefers-color-scheme: dark) { .card { background:#111a2e; box-shadow:none;
          border:1px solid #1f2a44; } }
  h1 { font-size:19px; margin:0 0 4px; }
  .sub { color:#6b7280; font-size:13px; margin:0 0 20px; }
  @media (prefers-color-scheme: dark) { .sub { color:#9aa4b8; } }
  ul { margin:0 0 20px; padding-left:20px; }
  li { margin:4px 0; }
  label { display:block; font-size:13px; font-weight:600; margin:12px 0 4px; }
  input[type=text], input[type=password] { width:100%; box-sizing:border-box; padding:9px 11px;
          border:1px solid #d1d5db; border-radius:8px; font-size:15px; background:transparent;
          color:inherit; }
  @media (prefers-color-scheme: dark) { input { border-color:#334155; } }
  .row { display:flex; gap:10px; margin-top:22px; }
  button { flex:1; padding:10px 14px; border-radius:8px; font-size:15px; cursor:pointer;
           border:1px solid transparent; }
  .allow { background:#1d4ed8; color:#fff; }
  .deny { background:transparent; border-color:#d1d5db; color:inherit; }
  .err { background:#fee2e2; color:#991b1b; padding:9px 11px; border-radius:8px;
         font-size:13px; margin-bottom:14px; }
  @media (prefers-color-scheme: dark) { .err { background:#3f1d1d; color:#fecaca; } }
  .uri { font-size:12px; color:#6b7280; word-break:break-all; margin-top:18px; }
</style>
</head><body>
<div class="card">
  <h1>{{.Request.ClientName}}</h1>
  <p class="sub">chiede di accedere a OpenEdge per tuo conto.</p>

  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}

  <ul>{{range .Scopes}}<li>{{.}}</li>{{end}}</ul>

  <form method="post" action="{{.Action}}">
    <input type="hidden" name="client_id" value="{{.Request.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.Request.RedirectURI}}">
    <input type="hidden" name="response_type" value="code">
    <input type="hidden" name="scope" value="{{.Request.Scope}}">
    <input type="hidden" name="state" value="{{.Request.State}}">
    <input type="hidden" name="code_challenge" value="{{.Request.Challenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.Request.Method}}">
    <input type="hidden" name="resource" value="{{.Request.Resource}}">

    {{if .MFAToken}}
      <input type="hidden" name="mfa_token" value="{{.MFAToken}}">
      <label for="mfa_code">Codice di verifica</label>
      <input id="mfa_code" name="mfa_code" type="text" inputmode="numeric"
             autocomplete="one-time-code" autofocus required>
    {{else}}
      <label for="username">Utente</label>
      <input id="username" name="username" type="text" value="{{.Username}}"
             autocomplete="username" autofocus required>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
    {{end}}

    <div class="row">
      <button class="deny" type="submit" name="action" value="deny">Annulla</button>
      <button class="allow" type="submit" name="action" value="allow">Autorizza</button>
    </div>
  </form>

  <p class="uri">Al termine verrai reindirizzato a<br>{{.Request.RedirectURI}}</p>
</div>
</body></html>`))

var authzErrorTmpl = template.Must(template.New("authzError").Parse(`<!doctype html>
<html lang="it"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Richiesta non valida — OpenEdge</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font: 15px/1.55 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
         background:#f4f5f7; color:#111827; padding:24px; }
  @media (prefers-color-scheme: dark) { body { background:#0b1220; color:#e5e7eb; } }
  .card { max-width:420px; background:#fff; border-radius:14px; padding:28px;
          box-shadow:0 1px 3px rgba(0,0,0,.12); }
  @media (prefers-color-scheme: dark) { .card { background:#111a2e; box-shadow:none;
          border:1px solid #1f2a44; } }
  h1 { font-size:18px; margin:0 0 8px; }
  p { color:#6b7280; margin:0; }
</style>
</head><body>
<div class="card">
  <h1>Non posso completare questa richiesta</h1>
  <p>{{.}}</p>
  <p style="margin-top:14px">Non ti reindirizzo indietro: l'indirizzo di ritorno fa parte
     di ciò che non torna, e mandarti lì sarebbe il problema stesso.</p>
</div>
</body></html>`))

func renderConsent(c *gin.Context, view consentView) {
	// A page that asks for a password must not be cached or framed.
	c.Header("Cache-Control", "no-store")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := consentTmpl.Execute(c.Writer, view); err != nil {
		log.Printf("[OAUTH] render consent: %v", err)
	}
}

func renderAuthzError(c *gin.Context, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Frame-Options", "DENY")
	c.Status(http.StatusBadRequest)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := authzErrorTmpl.Execute(c.Writer, message); err != nil {
		log.Printf("[OAUTH] render error: %v", err)
	}
}
