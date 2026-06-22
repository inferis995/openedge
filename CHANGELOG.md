# Changelog

All notable changes to OpenEdge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
