# Changelog

All notable changes to OpenEdge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **A tag import that failed halfway left the gateway half configured, and
  reloaded the driver onto it.** The import walked the lines writing as it went,
  collected failures into a list, and then sent the reload command whatever had
  happened. A thousand-line file failing at line five hundred left 499 tags
  written, 501 missing, and a driver restarted onto that gateway polling
  addresses that no longer matched the tag list. That does not present as a
  failed import; it presents as a plant reading wrong.

  Every line is now parsed before anything is written. A single unparseable line
  means nothing is written and the errors come back with created and updated at
  zero. What does get written goes in one transaction, and the reload only
  follows a commit. The web UI says explicitly that nothing landed, because
  "Created: 0" next to a list of errors otherwise reads as a half-done import.

- **The import set a change filter nobody asked for.** It hardcoded
  `historize_deadband = 0.1` while the column default is 0 and a tag created
  through the UI gets 0. On a temperature living inside a narrow band that
  silently drops the readings the tag was imported to show. It now defaults to 0
  and accepts an explicit `historize_deadband` in the request.

- **`tag_history` and `system_events` could exist without their foreign keys.**
  `EnsureTimescaleDBStructures` created both without `REFERENCES`, while
  `migrations/20250308_schema.sql` declared them with `ON DELETE CASCADE`: the
  same table had two shapes depending on which path created it. And
  `ensureCriticalConstraints`, which repairs foreign keys after a restore,
  covered `tags`, `alarm_definitions` and `alarm_events` but neither of these
  two. Without the cascade, every tag an operator deletes leaves its samples
  behind — invisible, unreachable from the UI, and still aged and compressed as
  if they mattered. Both definitions now carry the key, and both are repaired
  after a restore.

- **`validateRestoreIntegrity` reported success over an empty database.** It
  checked that eight tables EXISTED and then printed "All 8 critical tables
  verified". Table existence is the one thing a restore cannot really fail at:
  the schema is at the top of the dump and lands long before any data. It now
  counts rows, puts the numbers in front of the operator, and says so
  explicitly when `tag_history` comes back empty — without turning that into a
  failure, because a restore into a new installation legitimately has no
  history, and a check that cries wolf gets switched off within a week.

### Added

- **A test for the backup button in the web UI.** `scripts/backup.sh` and
  `scripts/restore.sh` are covered end to end by `TestBackupCanActuallyBeRestored`.
  `GET /api/system/backup` is a different route — it runs pg_dump with
  `--exclude-table=_timescaledb_internal.*`, and in TimescaleDB the rows of a
  hypertable physically live in chunk tables under exactly that schema — and
  nothing was checking it. `TestTheInAppBackupContainsTheHistory` seeds one
  history row with a value unique to the run, calls the endpoint, and reads the
  dump back out of the ZIP looking for that row. It answers the question without
  restoring anything: restoring into the live database to find out would be a
  destructive test of a destructive path.

- **Two tests for the tag import**, covering the two fixes above: a file with one
  bad line must leave the database untouched, and a clean file must land whole
  with `historize_deadband` at 0.

- **Store-and-forward on every driver.** A driver reads the PLC on a fixed tick
  and publishes over MQTT. When the broker was unreachable — network down,
  container restarted, maintenance — that sample went into paho's in-memory
  queue, which starts from a volatile store, and on process restart it was gone.
  What remained in the historian was a hole, and a hole in the history is the
  first thing a shift supervisor notices and the one thing there is no excuse
  for.

  Samples that fail to publish now go to an append-only file on a Docker volume
  and are resent, in order, on reconnect. Set per driver via `SPOOL_DIR` and
  `SPOOL_MAX_BYTES` (64 MB default, roughly a day for a mid-sized gateway).

  Replay works because of a property the platform already had: the driver stamps
  the timestamp when it READS the PLC and carries it in the payload, and the
  historian writes that value into the `time` column. A sample held for twenty
  minutes lands at the instant it was read, not the instant it was resent —
  otherwise store-and-forward would draw a wrong curve instead of a gap, which
  is worse than the gap.

  When the ceiling is reached the OLDEST records are dropped, because when the
  link returns the first thing anyone needs is the state of the plant now. The
  lost stretch is counted and logged, so the gap is declared rather than
  discovered from a chart. A line truncated by a crash is skipped and counted.
  A failed resend stops the replay and leaves the remainder on disk; the file is
  rewritten only after a send succeeds, so a crash mid-replay produces
  duplicates rather than losses — for a time series a point written twice with
  the same timestamp is harmless, a missing point is not.

  Eight tests, each verified by reintroducing the defect and watching it fail:
  replay order, survival across a process restart, the remainder staying on disk
  after a failed send, oldest-dropped-and-counted when full, a corrupt line
  skipped rather than fatal, draining an absent spool, and — at the client
  level, with no broker running — that a publish with the broker down queues
  instead of losing, while a client with no `SpoolPath` still returns the error
  as it did before.

### Fixed

- **`Publish` did not check whether it was connected.** It handed the message to
  paho and reported whatever came back. `publishNow` now fails explicitly when
  the client is nil or disconnected, which is what makes the spool decision
  possible at all.

### Removed

- `Client.connectOnce`, a `sync.Once` nothing had used.

## [3.1.0] - 2026-09-05

> **No breaking change. Nothing to do on upgrade.** `nis2_checks_passed`,
> `nis2_checks_total` and `passed` are all still served — a client written
> against 3.0.0 keeps working untouched. What changed is the values behind them,
> which are now true. `nis2_checks_total` carries the number of checks actually
> *evaluated* rather than a fixed twelve, so a client printing "X/Y" prints a
> fraction that means something instead of a denominator nobody could reach.

### Fixed

- **The security posture screen reported checks it had never performed.** Twelve
  checks were shown as an NIS2 compliance figure; eight of them were constants
  in the source — five hardcoded `true`, three hardcoded `false`. A deployment
  saw 8/12 and could not raise that number by fixing anything, because three of
  the twelve fail unconditionally, and five points were awarded without anything
  being looked at. A boolean has no way to say "I don't know", so it was wrong in
  both directions at once.

  Six checks are real platform state and are read for real: business continuity
  (backup freshness), network security (MQTT TLS), MFA, access control, audit
  logging, account security. The other six — risk management, incident handling,
  supply chain, cryptography, vulnerability management, data protection — are
  organizational measures that depend on company procedure outside this process.
  They now report `not_assessed`, and the denominator counts only what was
  actually evaluated.

- **A detail line that contradicted its own verdict.** The network security row
  read "MQTT non cifrato (TLS assente)" next to a green tick whenever TLS *was*
  enabled: the detail string was a constant independent of the outcome. Each
  check now carries the text of the branch it is in.

### Changed

- **The product stopped claiming NIS2 compliance in the interface.** The Terms
  of Service already stated the platform does not provide or substitute NIS2
  compliance, while the Security Center header said "conformità NIS2" and the
  report was titled "Stato conformità NIS2 Art. 21" — the UI promised what the
  contract denied. The screens now say what the feature is: a self-assessment of
  automated checks modelled on the Article 21 measures. The exported JSON is
  `SECURITY_POSTURE_SELF_ASSESSMENT`, carries an explicit disclaimer for whoever
  opens it out of context months later, and is named `security-posture-*.json`.

- **NIS2 is no longer named on the security screens at all.** The box is called
  "Controlli di sicurezza", each row shows the check and its verdict, and the
  article reference next to it is gone — a legal citation beside a row of green
  ticks reads as "compliant" whatever the heading says. The exported report
  carries the checks without it too, because that file is what somebody ends up
  handing to a customer. `article` is still served by the API for compatibility
  and is deprecated for removal in 4.0.0.

  The provenance stays in `internal/handlers/security.go`, where a developer
  reads it and a customer does not. The Terms of Service still name the
  directive, and must: the sentence there is "non fornisce, né sostituisce, gli
  adempimenti previsti dalla direttiva NIS2", which is the disclaimer that
  protects the seller.

### Deprecated

- **`nis2_checks_passed` and `nis2_checks_total`** on `GET /api/security/overview`,
  and **`passed`** on `GET /api/security/compliance`. Superseded by
  `checks_passed` / `checks_evaluated` / `checks_not_assessed` and by a
  three-valued `state` (`pass` / `fail` / `not_assessed`) — a boolean has no way
  to say "not assessed", which is what half these checks honestly are. The old
  fields keep being served and keep agreeing with the new ones: both
  representations are computed in one place (`SecurityOverview.setChecks`), and
  `TestTheDeprecatedFieldsMirrorTheCurrentOnes` fails if they ever drift. A
  compatibility field that quietly stops matching the thing it mirrors is worse
  than no compatibility at all. `passed` is `false` for a check that was never
  assessed — an old client understates its posture rather than overstating it,
  which is the safe direction to be wrong in. Scheduled for removal in 4.0.0.

### Added

- **Three tests over the security checks**, all verified the same way — by
  reintroducing the defect and watching them fail.
  `TestTheDeprecatedFieldsMirrorTheCurrentOnes` catches the deprecated fields
  drifting from the current ones. `TestOnlyMeasuredChecksAreCounted` catches a
  constant put back where a measurement belongs: with every platform measure
  off, no check may report as passed. `TestEachComplianceDetailFollowsItsVerdict`
  catches a detail string shared between the pass and fail branches — the exact
  shape of the "MQTT non cifrato (TLS assente)" line that sat next to a green
  tick. `securityChecks` and `complianceChecks` were extracted as pure functions
  so all three run without a database.

## [3.0.0] - 2026-09-05

> **Breaking — the NIS2 compliance module is gone.** Thirty-eight endpoints
> under `/api/compliance/*` no longer answer, six pages are removed, and the
> Terms of Service no longer state that the platform provides NIS2/IEC 62443
> compliance. Anything integrating with those endpoints must be updated. If you
> need the module, stay on 2.3.0 or `git revert 235885f` — the code is in
> history, not deleted from the world.
>
> **Upgrading an existing installation is safe and needs no action.** The
> migrations stop *creating* the ten tables; nothing drops them. Existing rows —
> including `csirt_incidents`, which holds incident records with statutory
> filing deadlines somebody may be required to retain — stay where they are and
> are simply no longer read. Removing them is an operator's deliberate act.

### Removed

- **The NIS2 / OT compliance module.** Six pages, five handlers, ten tables, the
  whole `internal/sync` package, and an hourly background worker that walked
  every organization on every installation whether or not anyone had ever opened
  those pages. The directive binds companies in regulated sectors; every
  deployment carried the weight, most have no obligation under it.

  What remains, deliberately: `GET /api/security/compliance` still runs its
  twelve checks against the deployment's own data — MFA on admins, audit
  logging, MQTT over TLS, edge versions — mapped to NIS2 Art. 21(2) points, and
  still feeds the Security and Reports pages. It reads users and settings, never
  the removed tables. It is a posture report, not a compliance suite: no asset
  register, no incident filing workflow, no supplier scoring, and README and the
  Terms of Service now say exactly that.

### Security

- **Four CVEs were compiled into every Go binary this project ships.** Found on
  the first run of the rebuilt image scan (see *Changed*), which looks at the
  artifact instead of the base image it started from. The Alpine layer came back
  clean; the binary did not.

  | module | fixed in | advisory |
  |---|---|---|
  | `golang.org/x/crypto` | v0.55.0 | CVE-2026-56854 — **CRITICAL**, `ssh`: authentication bypass, source-address restrictions not enforced |
  | `golang.org/x/mod` | v0.40.0 | CVE-2026-56864 — a malicious GOSUMDB could serve arbitrary module content |
  | `golang.org/x/mod` | v0.40.0 | CVE-2026-56865 — `sumdb/tlog`: supply-chain compromise via tile verification bypass |
  | `golang.org/x/text` | v0.41.0 | CVE-2026-56852 — denial of service on invalid UTF-8 |

  No scan of a base image could ever have seen these. `govulncheck` did not
  report them either, and is right not to: it reports what the code can *reach*,
  and it passes on this tree. Trivy reports what is *present* — which is what a
  customer's own scanner will find in the image and ask about.

- **Shipped images now take OS security patches at build time.** Not one runtime
  stage ran `apk upgrade`, so every image froze whatever its base tag happened to
  hold on build day. The nightly scan found openssl 3.5.7-r0 (CVE-2026-14456,
  HIGH) while Alpine had already published 3.5.8-r0, and the only remedy on offer
  — "bump the FROM tag" — does not exist until somebody else rebuilds one. All
  eleven runtime stages now upgrade their OS packages themselves.

- **Frontend advisories.** `browserslist` (HIGH, unbounded memory growth) and
  `@humanfs/node` (symlink traversal in recursive copy), both taken by
  `npm audit fix` without touching `package.json`.

### Changed

- **The image scan looks at the product, not at its base.** It scanned the base
  image *reference* — `alpine:3.22` as it stood on the registry. With the
  upgrade above in place those two answers diverge: the artifact a customer
  receives can be clean while the tag it started from is not, and the job would
  have stayed red over a finding that no longer applied to anything shipped. The
  runtime matrix now builds each service and scans what came out.

- **Builder images report, and only CRITICAL stops the build.** `golang:1.25-alpine`
  and `node:22-alpine` carried the same openssl with no tag to move to, turning
  the whole nightly scan red with no action available — the state that teaches
  people to ignore a job. Nothing in a builder reaches a customer, so what is in
  there is always printed and only a CRITICAL halts.

- **CI runs on every branch.** The push trigger listed `[main, master, develop,
  "claude/*"]` — the branch naming one tool happened to use. A branch called
  anything else got no CI until somebody opened a pull request.

- **Accepted findings now carry an expiry.** `.trivyignore` holds seven
  util-linux CVEs in the web-ui image: fixed in `libuuid 2.42.3-r0`, which is not
  in the Alpine 3.24 branch that image builds on, so `apk upgrade` has nothing
  newer to install. They are privilege escalations through `mount(8)`, `nsenter`
  and X-mount hooks, and this container runs nginx as a non-root user under
  `no-new-privileges` and mounts nothing — present on the filesystem, unreachable
  by anything the image executes. **The exception expires 2026-12-01**, after
  which the build goes red again on purpose.

### Fixed

- `services/web-ui/dist/` was in `.gitignore` and tracked anyway — `.gitignore`
  does not untrack what git already knows. `tsconfig.tsbuildinfo` was tracked and
  not ignored at all, so any `tsc` run dirtied the working tree and the file kept
  being swept into unrelated commits.

## [2.3.0] - 2026-08-19

### Security

- **The web UI's MQTT connection was anonymous, and on the cloud deployment that
  meant the broker was on the public internet.** The browser watches live values
  over a WebSocket straight to Mosquitto (`mqtt-client.ts` → nginx `/mqtt` →
  `mosquitto:9001`) and sent no credentials at all. To make that work the broker
  config had been switched to `per_listener_settings true` with
  `allow_anonymous true` on 9001 — which also detached the dynamic-security
  plugin from that listener, leaving it with no ACLs whatsoever. nginx is
  published by Traefik on 443, so anyone who could open the site could subscribe
  to `#` and read every tenant's live plant data, and publish
  `cmd/write/{gateway_id}`, which the S7 and Modbus drivers execute as a
  setpoint write on real machinery.

  The broker is authenticated again on both listeners, with one global policy and
  the plugin governing both. The UI now signs in: `GET /api/mqtt/ui-credentials`
  issues a per-organization identity to an already-authenticated session, bound
  to a new role (`org-{id}-ui-role`, `uiViewerACLs`) that is **read-only by
  construction** — subscribe and receive, not one `publishClientSend`. It is
  deliberately NOT the organization's existing MQTT role: that one is an edge
  identity and may publish tag data, so handing it to a browser would let any
  signed-in user, including a read-only one, inject readings indistinguishable
  from a PLC's. Command traffic (NCMD/DCMD, `cmd/`, `sys/command/`) is not
  readable by the viewer either — reading a setpoint discloses what is about to
  be written.

  Existing organizations are provisioned on first use, so no backfill is needed.
  The three places that opened MQTT connections separately — the client service,
  `useSparkplugListener` and `MqttMonitorPage` — now go through one
  `connectAuthenticatedMqtt`, because a rule written in three places is one that
  gets missed in the fourth.

- **One tenant could watch another tenant's Sparkplug data.** Sparkplug puts the
  organization and the site in a single topic level
  (`spBv1.0/{org-slug}-{site-slug}/…`) and MQTT wildcards match whole levels, so
  `+` cannot prefix-match `acme-*`: an organization's namespace can only be
  granted by naming each of its sites. Both broker roles were built once, at
  organization creation, when no site exists yet — and never rebuilt. So every
  deployment ran on the fallback grant, whose group level is a `+`, which is
  every other tenant's group as well. The code said the hole "closes as soon as
  siteNames is supplied"; nothing ever supplied them.

  `refreshOrgMQTTRoles` now rebuilds both roles from the organization's actual
  sites whenever a site is created, renamed or deleted, and when the
  organization itself is renamed — which previously called `UpdateOrgRole` with
  no sites and quietly widened the grant back on every rename. A test asserts
  the wildcard is present without sites and gone with them, in both roles, so it
  cannot pass by granting nothing.

## [2.2.0] - 2026-08-18

### Security

> **One behaviour change for anything that calls the API.** `POST /api/users`,
> `POST /api/auth/accept-invite`, `POST /api/auth/change-password` and
> `POST /api/auth/reset-password` now refuse a password shorter than 12
> characters with `400`. Scripts or integrations that create accounts with
> shorter ones need updating. Nobody is locked out: existing passwords keep
> working, and login is not affected.

- **A spent invite blamed the username.** `POST /api/auth/accept-invite` read
  the invite on the pooled connection, decided it was unused, hashed the
  password — bcrypt at the default cost, tens of milliseconds — and only then
  opened the transaction that inserted the user and marked the invite consumed.
  Two requests carrying the same token both passed that check and both drove on.
  No duplicate account was ever created: both insert the invite's own email
  address, so the unique index on `users(email)` refuses the second — the
  database was holding the line the handler had let go. What the user got was
  that constraint surfacing as `409 username already taken`. The username was
  fine. Someone was told to pick another one, it failed again, and nothing in
  the response or the logs pointed at the invite. The lookup now runs inside the
  transaction with `FOR UPDATE`, so the loser waits for the winner to commit and
  is told the truth: the invite is spent.
- **A six-character password was enough to hold an account.** Every path that
  sets a password — accepting an invite, creating a user, changing your own,
  resetting by email — declared `binding:"required,min=6"` separately. Six
  characters is a short offline guessing run against the bcrypt hash beside it,
  and the invite endpoint is public. There is now one rule,
  `auth.MinPasswordLength` = 12, matching what `scripts/preflight.sh` already
  demanded of the initial admin, with a test that fails if any handler
  reintroduces a minimum of its own. Existing sessions and logins are
  unaffected: every path changed is one that STORES a password, never one that
  checks an existing one. The security dashboard's `strong_password_policy`,
  hardcoded `false` because the old minimum did not deserve better, is now true.

### Fixed

- **The web UI's health probe had never passed, and it cost the whole site
  behind a proxy.** The container runs as `USER nginx`, so the base image's
  `10-listen-on-ipv6-by-default.sh` cannot rewrite a config it does not own —
  it says so at every start — and nginx listens on `0.0.0.0:80` only. The probe
  asked `http://localhost/nginx-health`; `localhost` resolves to `::1` ahead of
  `127.0.0.1`, so it was refused every time. nginx served perfectly throughout
  and the only symptom was the word `(unhealthy)` in `docker ps`, which nothing
  read. The bill came due on the deployments with a reverse proxy: **Traefik
  skips unhealthy containers**, so `make vps-up` produced a site that answered
  **404 to every request**, and `make onprem-tls` never started Caddy, which
  waits for this container to be healthy. Fixed by probing `127.0.0.1`; both
  end-to-end jobs now fail if any container reports unhealthy.

- **The cloud overlay published every internal service to the internet.**
  `docker-compose.vps.yml` removed the host port bindings with `ports: []`,
  which does nothing: Compose *concatenates* `ports` across overlay files, so an
  empty list merges with the base one and leaves it intact. A VPS started with
  `make vps-up` was therefore serving `0.0.0.0:3000` — the web UI's nginx, and
  through it the whole API, in plaintext with none of Traefik's security
  headers — plus `0.0.0.0:18830` and `0.0.0.0:9001`, unencrypted MQTT and
  MQTT-over-WebSocket. PostgreSQL, Redis and core-api were spared only by the
  `127.0.0.1` bind defaults in the base file. The `ufw` rules in
  `deploy/cloud-init.sh` did not help: Docker's iptables rules are consulted
  before ufw's. Fixed with `ports: !override []` (Compose 2.24+, checked by
  `scripts/preflight.sh`), and asserted on every push by
  `test/config/compose_overlay_test.go`, which reads the merged configuration
  rather than either file.
- **`preflight.sh` refused a correctly filled cloud configuration.** It compared
  `ALLOWED_ORIGINS` against `PUBLIC_HOST` as raw text, so the template's own
  `ALLOWED_ORIGINS=https://${PUBLIC_HOST}` — the line an operator is meant to
  leave alone — failed the check that gates `make vps-up`, with a message
  claiming CORS was broken when it was not.

### Added

- **CI runs the acceptance suite against the cloud deployment.** The existing
  end-to-end job proves the application in the on-prem shape only: every service
  published on the host. `docker-compose.vps.yml` — Traefik in front, host ports
  removed, everything arriving through the proxy — had never been brought up by
  anything. The new `e2e-vps` job assembles it, runs `scripts/preflight.sh`
  against a `.env` built from `.env.cloud.example`, and runs the whole suite
  through Traefik and the nginx behind it, plus assertions only that deployment
  has: the redirect off port 80, the security headers, the OAuth issuer, MQTT
  over TLS on 8883, and every internal port being shut. It does not prove
  certificate issuance, which needs a public domain and a real CA.
- `ACME_CA_SERVER` — point Traefik at the Let's Encrypt staging directory while
  DNS is still moving, instead of burning the production failed-validation rate
  limit on a domain that does not resolve yet.

## [2.1.0] - 2026-06-22

### Added

- **MFA / Two-Factor Authentication (TOTP)**
  - TOTP-based 2FA compatible with Google Authenticator, Authy, and any RFC 6238 app
  - QR code setup flow in Profile → Security section
  - 8 single-use recovery codes generated at activation (format `XXXX-XXXX-XXXX`, bcrypt-hashed)
  - Recovery code fallback at login (when phone is unavailable)
  - `POST /api/auth/mfa/recovery-codes` — regenerate codes (invalidates old ones)
  - Org-level MFA enforcement: `PUT /api/organizations/:id/mfa-required` — blocks login for users without MFA configured
  - SSO users (Google/Azure AD) bypass TOTP — MFA delegated to identity provider
  - CLI `openedge login` handles MFA step automatically (TOTP or recovery code prompt)

- **NIS2 Compliance Suite (Art. 21/23/18)**
  - **OT Asset Inventory** — auto-sync from configured gateways (hourly worker), risk score 0–10 based on protocol type, TLS, online status
  - **Risk Posture** — 30-item NIS2 checklist covering Art.21(a–j) with auto-assessment from real org data
  - **CSIRT Incident Management (Art.23)** — create/track incidents with legal deadline countdown: 24h early warning, 72h notification, 30d final report
  - **Vendor Risk (Art.18)** — supplier scoring 0–100 (ISO27001/SOC2/IEC62443/audit/access level/country), auto-import from gateway `connection_config`
  - **Threat Monitor** — log and track security threats, auto-link to CSIRT incidents
  - **Compliance Reports** — generate audit-ready PDF/CSV reports for NIS2 and IEC 62443
  - 6 new UI pages: Asset Discovery, Risk Posture, CSIRT Art.23, Vendor Risk, Threat Monitor, Compliance Reports
  - `GET /api/compliance/auto-assess` — auto-values 12 requirements from live org data
  - `POST /api/compliance/sync-assets` — on-demand asset sync from gateways

- **GDPR / Legal Pages**
  - Cookie consent banner (localStorage, shown once, links to privacy/terms)
  - Privacy Policy page at `/privacy` (GDPR Art.13 compliant, Italian)
  - Terms of Service page at `/terms` (Italian, SLA tiers, limitation of liability)
  - Support/Privacy/Terms links in Sidebar footer

- **UX & Production Hardening**
  - React `ErrorBoundary` wraps the entire app — friendly crash page instead of blank screen
  - Favicon updated from Vite default to OpenEdge logo
  - Full SEO/OG/Twitter meta tags in `index.html` (`lang=it`, description, og:image)
  - `INFLUX_*` env vars documented in `.env.example`

- **SCADA Widget Editor Improvements**
  - Clock widget: `clockFormat` (24h/12h AM-PM), `showDate` toggle, text color picker
  - Setpoint widget: `unit` label, `decimals`, `spMin`/`spMax` range limits (validated on write), `spStep` increment, `confirmWrite` confirmation dialog

### Fixed

- **Sparkplug B JSON decoder** was a stub returning an error — replaced with `encoding/json` (breaks Sparkplug B JSON mode in all previous versions)
- **Docker resource limits** (NanoCPUs/Memory) were commented out — re-enabled; driver containers now have per-type CPU/RAM caps
- **Health stats handler** silently discarded `rows.Scan` errors — now logs and continues
- Debug `console.log` removed from `TagSearch` and `App.tsx`

---

## [1.0.0] - 2026-03-08

### Added
- **Multi-Protocol Drivers**
  - Modbus TCP driver with configurable scan rates
  - Siemens S7 driver supporting S7-200/300/400/1200/1500
  - OPC UA driver with secure encryption support
  - MQTT driver for message consumption
  - Redis driver for cache synchronization

- **Real-Time Data Historian**
  - High-performance data ingestion from MQTT topics
  - TimescaleDB integration with automatic compression
  - Configurable retention policies (1-3650 days)
  - Real-time value caching with Redis
  - Deadband filtering to reduce storage

- **Advanced Alarm System**
  - Per-tag threshold alarms (High/Low limits)
  - Configurable alarm delays (0-86400 seconds)
  - Hysteresis support to prevent alarm chatter
  - Real-time alarm notifications via WebSocket
  - Complete alarm history and audit trail
  - Alarm severity levels (Critical, Warning, Info)

- **Cloud Synchronization**
  - Sparkplug B protocol support (dual format: JSON + Sparkplug)
  - Configurable topic prefix for cloud forwarding
  - Bidirectional command forwarding (Cloud → Edge)
  - Automatic reconnection with exponential backoff
  - Support for multiple cloud brokers

- **Security Features**
  - AES-256 encryption utilities for sensitive credentials
  - Built-in user authentication system
  - Password masking in logs and exports
  - CORS configuration

- **Web Interface**
  - Modern React dashboard with shadcn/ui components
  - Dark/Light theme support
  - Real-time tag browser with tree view
  - Advanced trend chart with offline gap detection
  - Alarm configuration and monitoring
  - System configuration pages
  - Responsive design for desktop and tablet

- **Production Features**
  - Automatic retry logic for all connections (30 attempts, 2s-30s backoff)
  - Graceful degradation on service failures
  - Health monitoring for all services
  - Docker Compose deployment with health checks
  - Structured logging with production format
  - Auto-reconnection for cloud MQTT (background retry every 30s)

- **Documentation**
  - Comprehensive README with quick start
  - Environment configuration template
  - Deployment scripts with full automation
  - API documentation
  - Architecture diagrams

### Changed
- Renamed Docker images from `ralph-wiggum-claude-code--main-*` to `openedge-*`
- Updated alarm system with improved race condition handling
- Enhanced error handling across all services
- Improved MQTT client with better connection management

### Fixed
- **Critical** - Race condition in alarm manager's tickDelays() function
- **Critical** - SQL injection risk in retention policy handlers
- **Critical** - All services now use retry logic instead of crashing on connection failure
- Fixed Sparkplug B death message handling
- Fixed tag ordering with drag-and-drop
- Fixed historical data quality codes (STALE data now marked correctly)
- Fixed Web UI build artifacts

### Security
- Added input validation for retention policy (1-3650 day range)
- Implemented parameterized queries to prevent SQL injection
- Added AES-256 encryption for credential storage
- Improved password masking in logs and exports

### Performance
- TimescaleDB compression enabled by default
- Configurable deadband filtering reduces storage by up to 90%
- Redis caching reduces database load
- Optimized Sparkplug B parsing

### Removed
- Removed Claude Code development artifacts
- Removed temporary documentation files
- Removed unused branches
- Cleaned up development utilities

## [2.0.0] - 2026-06-20

### Added

- **Enterprise Identity & Access**
  - SSO / OIDC support — Google (OAuth2) and Azure AD (Microsoft Entra) with automatic user provisioning by email domain
  - Granular RBAC — per-user permission flags: `write_tags`, `ack_alarms`, `export_data`, `manage_recipes`, `manage_shifts`, `view_audit`, `download_installer`
  - Permissions embedded in JWT to avoid per-request DB queries
  - Account lockout after 5 failed attempts (30-min lock), last-login IP tracking

- **Tag Shadows / Digital Twin**
  - `GET /api/tags/:id/shadow` — last-known value always available even when edge is offline
  - `GET /api/tags/shadows?gateway_id=X` — batch endpoint for all tags of a gateway
  - `source` field: `"live"` (edge online) | `"historic"` (edge offline, value from Redis/DB)
  - Dashboard and trend UI show `LIVE` / `HISTORIC` badge per tag

- **Enterprise Notifications**
  - Slack — webhook HTTP POST with Block Kit rich messages
  - Microsoft Teams — Incoming Webhook with Adaptive Card (Teams 2.0 compatible)
  - PagerDuty — Events API v2 with severity mapping (critical/error/warning/info)
  - Test button per channel in System → Settings → Notifications

- **OTA Fleet Management**
  - `POST /api/organizations/:id/edge-update` — publish OTA update to org edge via MQTT
  - `POST /api/organizations/:id/edge-restart` — remote restart all org drivers
  - `GET /api/fleet/status` — global admin fleet view: all orgs with edge online/offline, last ping, agent version
  - driver-manager subscribes to `sys/update/#` and `sys/restart/#`; SHA256 checksum verify before apply; auto-rollback on health check failure

- **InfluxDB v2 Export Connector**
  - Continuous push of tag values to InfluxDB using line protocol
  - Watermark-based (zero data loss, no duplicates) via Redis key `influx_watermark:{org_id}`
  - Configurable batch size (default 500) and flush interval (default 10 s)
  - System → Integrations → InfluxDB with config form and last-push indicator

- **Professional Observability**
  - Prometheus + Grafana + AlertManager + Loki + Promtail stack
  - 4 exporters: postgres-exporter, redis-exporter, node-exporter, mosquitto-exporter
  - 10 alert rules (CoreAPIDown, APIHighLatency, DiskSpaceCritical, MQTTBrokerDown, …)
  - 2 auto-provisioned Grafana dashboards: OpenEdge Operations + Infrastructure
  - VPS compose: monitoring always on; on-prem: `make monitoring-up`

- **Deployment Flexibility**
  - `--profile edge` on VPS and Coolify composes — adds driver-manager for all-in-one deployments
  - `make vps-up-edge` / `make coolify-up-edge` for single-machine cloud deployments
  - LoRaWAN driver included in all builds (no separate compose file)
  - 3 compose files total: `docker-compose.yml` · `docker-compose.vps.yml` · `docker-compose.coolify.yml`

- **Security Hardening**
  - Security Center: NIS2 Art. 21 compliance dashboard, 0–100 security score, 12-point checklist
  - Infrastructure Dashboard: real-time inventory with IP, port, TLS status per gateway
  - Full audit log with IP, user-agent, action, success/failure (filterable)
  - MQTT DynSec per-org provisioning — isolated credentials and ACL topic prefix per organization
  - Rootless containers, no root processes in production

- **Operational Tools**
  - `make setup-env` — auto-generates JWT_SECRET, POSTGRES_PASSWORD, MQTT_ADMIN_PASSWORD, GRAFANA_ADMIN_PASSWORD with `openssl rand`
  - `make update` — safe upgrade: snapshot → pull → build → health check → optional rollback
  - `make backup-now` / `make backup-to-usb` / `make restore BACKUP=...`
  - Windows Service installer (`windows/install-service.ps1`) with WinSW, SHA256 verified
  - Linux systemd installer (`sudo make install-service`)
  - HMI kiosk mode for operator workstations (`make kiosk-linux URL=...`)

- **CESMII i3X v1 API**
  - Vendor-neutral REST interface: equipment hierarchy, properties, live values, write commands, alarms
  - Quality codes: 192 (Good) / 64 (Uncertain) / 0 (Bad)

- **AI-Ops Endpoints**
  - `GET /api/aiops/summary` — org-wide snapshot: tag stats, alarm counts, gateway totals
  - `GET /api/aiops/anomalies` — Z-score anomaly detection (threshold: |z| ≥ 2.5)
  - `GET /api/aiops/alarms/digest` — alarm digest grouped by severity

- **Multi-Tenant Self-Service**
  - Email invite flow with one-time link (7-day TTL) — no admin involvement after initial setup
  - Password reset via email (1-hour token)
  - HMAC-SHA256 signed webhooks on 5 event types (alarm.active, alarm.cleared, tag.write, edge.online, edge.offline)
  - Edge Installer ZIP — pre-configured docker-compose + .env per org, downloadable from UI

### Changed
- Renamed all Docker container names and volumes from `industrial-*` to `openedge-*`
- Renamed Docker network from `industrial-network` to `openedge-net`
- Consolidated from 6 compose files to 3

### Removed
- `docker-compose.monitoring.yml` — merged into main compose as `--profile monitoring`
- `docker-compose.build.yml` — merged into main compose as `--profile drivers`
- `docker-compose.onprem-tls.yml` — merged into main compose as `--profile tls`
- Stale planning docs, migration archive, deprecated Windows Task Scheduler installer

---

## Version Summary

| Version | Date | Status | Key Features |
|---------|------|--------|--------------|
| 2.1.0 | 2026-06-22 | Production | MFA TOTP + recovery codes, NIS2 full suite (CSIRT/Vendor/Assets), GDPR pages, SCADA improvements |
| 2.0.0 | 2026-06-20 | Production | Enterprise: SSO, RBAC, Tag Shadows, InfluxDB, Fleet, Monitoring, Notifications |
| 1.0.0 | 2026-03-08 | Production | First stable release — drivers, historian, alarms, multi-tenant SaaS |
| 0.x | 2024-2025 | Development | Development versions (not documented) |
