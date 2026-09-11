package handlers

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ralph/industrial-edge-middleware/migrations"
)

// EdgeInstallerHandler generates downloadable edge deployment packages.
type EdgeInstallerHandler struct {
	db *sql.DB
}

// NewEdgeInstallerHandler creates a new edge installer handler.
func NewEdgeInstallerHandler(db *sql.DB) *EdgeInstallerHandler {
	return &EdgeInstallerHandler{db: db}
}

type installerData struct {
	OrgID      int
	OrgName    string
	OrgSlug    string
	APIKey     string
	APIBaseURL string
	MQTTUser   string
	MQTTPass   string
	Generated  string

	// Where the box's broker forwards to. Taken from the same settings the
	// edge config endpoint hands out, so the two cannot point at different
	// brokers.
	CloudMQTTHost string
	CloudMQTTPort int

	// Credentials for the box's OWN broker. Generated per installation: this
	// broker used to allow anyone on the factory network to connect.
	EdgeMQTTUser string
	EdgeMQTTPass string
}

// Download generates and streams a ZIP installer package for the given org.
// GET /api/organizations/:id/edge-installer.
func (h *EdgeInstallerHandler) Download(c *gin.Context) {
	orgID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid org id"})
		return
	}
	// The ZIP embeds a freshly minted API key and the org's cloud MQTT
	// credentials — only the owning org (or a global admin) may download it.
	if !requireOwnOrg(c, orgID) {
		return
	}

	var orgName string
	err = h.db.QueryRowContext(c.Request.Context(),
		"SELECT name FROM organizations WHERE id = $1", orgID).Scan(&orgName)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	// Generate a dedicated API key for this edge deployment.
	pfxBytes := make([]byte, 4)
	secBytes := make([]byte, 16)
	if _, err = rand.Read(pfxBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}
	if _, err = rand.Read(secBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}
	prefix := "oe_" + hex.EncodeToString(pfxBytes)
	apiKey := prefix + "_" + hex.EncodeToString(secBytes)
	h256 := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(h256[:])
	keyName := "edge-" + time.Now().UTC().Format("20060102-150405")

	_, err = h.db.ExecContext(c.Request.Context(),
		`INSERT INTO org_api_keys (org_id, name, key_prefix, key_hash)
		 VALUES ($1, $2, $3, $4)`,
		orgID, keyName, prefix, keyHash,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create API key"})
		return
	}

	// Load MQTT credentials — may be empty if DynSec is not configured.
	var mqttUser, mqttPass string
	_ = h.db.QueryRowContext(c.Request.Context(),
		`SELECT username, password FROM org_mqtt_credentials WHERE org_id = $1`, orgID,
	).Scan(&mqttUser, &mqttPass)

	// A password for the box's own broker, different for every installation.
	localPass := make([]byte, 18)
	if _, err = rand.Read(localPass); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "key generation failed"})
		return
	}

	data := &installerData{
		OrgID:      orgID,
		OrgName:    orgName,
		OrgSlug:    slugify(orgName),
		APIKey:     apiKey,
		APIBaseURL: publicBaseURL(c),
		MQTTUser:   mqttUser,
		MQTTPass:   mqttPass,
		Generated:  time.Now().UTC().Format(time.RFC3339),

		CloudMQTTHost: mqttPublicHost(),
		CloudMQTTPort: mqttPublicPort(),

		EdgeMQTTUser: "edge",
		EdgeMQTTPass: hex.EncodeToString(localPass),
	}

	zipBuf, buildErr := buildInstallerZIP(data)
	if buildErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build installer"})
		return
	}

	fname := "openedge-edge-" + data.OrgSlug + ".zip"
	c.Header("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Data(http.StatusOK, "application/zip", zipBuf.Bytes())
}

// buildInstallerZIP creates the ZIP archive in memory.
func buildInstallerZIP(data *installerData) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	defer w.Close() //nolint:errcheck

	dir := "openedge-edge-" + data.OrgSlug + "/"

	files := map[string]string{
		dir + ".env":               envTemplate,
		dir + "docker-compose.yml": composeTemplate,
		dir + "install.sh":         installShTemplate,
		dir + "install.ps1":        installPs1Template,
		dir + "README.md":          readmeTemplate,
	}

	for name, tmplStr := range files {
		tmpl, err := template.New("").Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("template parse %s: %w", name, err)
		}
		var content bytes.Buffer
		if err = tmpl.Execute(&content, data); err != nil {
			return nil, fmt.Errorf("template exec %s: %w", name, err)
		}

		fh := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if strings.HasSuffix(name, ".sh") {
			fh.SetMode(0755)
		} else {
			fh.SetMode(0644)
		}
		fw, err := w.CreateHeader(fh)
		if err != nil {
			return nil, err
		}
		if _, err = fw.Write(content.Bytes()); err != nil {
			return nil, err
		}
	}

	if err := addFile(w, dir+"mosquitto/config/mosquitto.conf", mosquittoConf, data); err != nil {
		return nil, err
	}
	// The bridge is a file of its own so a deliberately offline box is a
	// configuration and not a patch: delete it and the broker keeps working,
	// it simply stops forwarding.
	if err := addFile(w, dir+"mosquitto/config/conf.d/bridge.conf", mosquittoBridgeConf, data); err != nil {
		return nil, err
	}

	// The schema. The box runs its own Postgres and nothing else was ever going
	// to create the tables in it: organizations, sites, areas, gateways, tags
	// and alarm_definitions exist only in these files, and the edge stack did
	// not carry them. driver-manager's own migrations run afterwards and take
	// the schema the rest of the way, exactly as they do on the central server.
	if err := addMigrations(w, dir); err != nil {
		return nil, err
	}

	return buf, nil
}

// addFile renders one template into the archive.
func addFile(w *zip.Writer, name, tmplStr string, data *installerData) error {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse %s: %w", name, err)
	}
	var content bytes.Buffer
	if execErr := tmpl.Execute(&content, data); execErr != nil {
		return fmt.Errorf("template execute %s: %w", name, execErr)
	}

	fh := &zip.FileHeader{Name: name, Method: zip.Deflate}
	fh.SetMode(0o644)
	fw, err := w.CreateHeader(fh)
	if err != nil {
		return err
	}
	_, err = fw.Write(content.Bytes())
	return err
}

// addMigrations copies the schema files into the archive, where the box's
// compose mounts them as Postgres's init directory.
func addMigrations(w *zip.Writer, dir string) error {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("reading the embedded schema: %w", err)
	}
	if len(entries) == 0 {
		// An installer that ships no schema produces a box whose database is
		// empty and whose drivers fail their first query. Better to refuse to
		// build it than to hand it to somebody.
		return fmt.Errorf("no schema files are embedded in this build")
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, readErr := migrations.Files.ReadFile(e.Name())
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", e.Name(), readErr)
		}
		fh := &zip.FileHeader{Name: dir + "migrations/" + e.Name(), Method: zip.Deflate}
		fh.SetMode(0o644)
		fw, createErr := w.CreateHeader(fh)
		if createErr != nil {
			return createErr
		}
		if _, writeErr := fw.Write(content); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// publicBaseURL returns the API base URL visible from outside the server.
func publicBaseURL(c *gin.Context) string {
	if host := os.Getenv("PUBLIC_HOST"); host != "" {
		return "https://" + host
	}
	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

// slugify converts an org name to a safe filesystem slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "org"
	}
	return result
}

// ── Templates ────────────────────────────────────────────────────────────────

// mosquittoConf is the broker inside the box.
//
// Two things it must do, and neither was configured before: keep the plant's
// own traffic private, and carry it to the central platform when there is a
// link.
//
// cleansession false on the bridge is the whole point of the second. With it,
// the broker keeps the bridge's session across disconnections and queues the
// QoS 1 messages it could not deliver; without it, everything published while
// the line is down is simply gone. A plant with no internet is the normal case
// here, not the exception.
const mosquittoConf = `listener 1883

# The plant's own traffic. Anonymous access used to be allowed, which meant
# anybody on the factory network could read every value and — where writes are
# enabled — send setpoints to the PLCs.
allow_anonymous false
password_file /mosquitto/config/passwd

persistence true
persistence_location /mosquitto/data/
autosave_interval 30

# Forwarding to the central platform. Delete bridge.conf to keep this box
# entirely offline; everything else goes on working.
include_dir /mosquitto/config/conf.d

# Queued messages survive a restart of the box, not just a drop of the link.
max_queued_messages 0
queue_qos0_messages false
`

// mosquittoBridgeConf is written separately because it is filled in from the
// organization's credentials, and because a box deliberately kept offline is a
// legitimate configuration: delete this file and the broker keeps working, it
// simply stops forwarding.
const mosquittoBridgeConf = `# Bridge to the central OpenEdge platform.
connection openedge-central
address {{.CloudMQTTHost}}:{{.CloudMQTTPort}}
bridge_protocol_version mqttv311

remote_username {{.MQTTUser}}
remote_password {{.MQTTPass}}
remote_clientid edge-{{.OrgSlug}}

# The session — and with it the queue of undelivered messages — survives the
# link going down. This is what makes a plant with intermittent internet work.
cleansession false
start_type automatic
restart_timeout 10 60
notifications false
try_private false

# Out: what the plant produces.
topic data/# out 1
topic sys/alarms/# out 1
topic sys/health/# out 1
topic spBv1.0/# out 1

# In: what the platform asks the plant to do. One direction each, deliberately:
# a bridge declared both ways echoes every message back to its own sender.
topic sys/write/# in 1
topic sys/update/# in 1
topic sys/restart/# in 1
`

const envTemplate = `# OpenEdge Edge Configuration
# Organization : {{.OrgName}} (ID: {{.OrgID}})
# Generated   : {{.Generated}}
#
# KEEP THIS FILE SECURE — it contains credentials.

# ── Central platform ─────────────────────────────────────────────────────────
# These three names are the ones driver-manager actually reads. The installer
# used to write EDGE_API_URL and EDGE_API_KEY, which nothing consumed, and never
# wrote ORG_ID at all — so the heartbeat condition was never satisfied and the
# box never appeared in the platform.
CORE_API_URL={{.APIBaseURL}}
CORE_API_TOKEN={{.APIKey}}
ORG_ID={{.OrgID}}

# ── Local Database ───────────────────────────────────────────────────────────
DB_HOST=postgres
DB_PORT=5432
DB_USER=edge_user
DB_PASSWORD=changeme_db
DB_NAME=edge_db

# ── Local MQTT Broker ────────────────────────────────────────────────────────
MQTT_HOST=mosquitto
MQTT_PORT=1883
# Credentials for the broker inside this box. install.sh turns these into the
# broker's password file; anonymous access is refused.
EDGE_MQTT_USER={{.EdgeMQTTUser}}
EDGE_MQTT_PASS={{.EdgeMQTTPass}}

# ── Cloud MQTT (data forwarding) ─────────────────────────────────────────────
{{- if .MQTTUser}}
CLOUD_MQTT_USER={{.MQTTUser}}
CLOUD_MQTT_PASS={{.MQTTPass}}
{{- else}}
# CLOUD_MQTT_USER=
# CLOUD_MQTT_PASS=
{{- end}}

# ── Misc ─────────────────────────────────────────────────────────────────────
LOG_FORMAT=json
# Passed to every driver container: it is what the timestamps an operator reads
# are rendered in.
TZ=Europe/Rome
`

const composeTemplate = `# OpenEdge Edge Stack
# Organization : {{.OrgName}}
# Generated   : {{.Generated}}
#
# Usage:
#   docker compose up -d          # start all services
#   docker compose ps             # check status
#   docker compose logs -f        # stream logs

services:

  mosquitto:
    image: eclipse-mosquitto:2
    volumes:
      # Not read-only: mosquitto_passwd writes the password file in here during
      # install, and the broker rewrites nothing afterwards.
      - ./mosquitto/config:/mosquitto/config
      - mosquitto_data:/mosquitto/data
    restart: unless-stopped

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data
    restart: unless-stopped

  postgres:
    image: timescale/timescaledb:latest-pg16
    environment:
      POSTGRES_USER: ${DB_USER:-edge_user}
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: ${DB_NAME:-edge_db}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      # The base schema. Postgres runs this once, on an empty data directory;
      # driver-manager's own migrations take it the rest of the way on every
      # start, which is how the central server works too.
      - ./migrations:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${DB_USER:-edge_user}"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  driver-manager:
    image: ghcr.io/inferis995/openedge/driver-manager:latest
    environment:
      DB_HOST: postgres
      DB_PORT: ${DB_PORT:-5432}
      DB_USER: ${DB_USER:-edge_user}
      DB_PASSWORD: ${DB_PASSWORD}
      DB_NAME: ${DB_NAME:-edge_db}
      MQTT_HOST: mosquitto
      MQTT_PORT: ${MQTT_PORT:-1883}
      CORE_API_URL: ${CORE_API_URL}
      CORE_API_TOKEN: ${CORE_API_TOKEN}
      ORG_ID: ${ORG_ID}
      AGENT_VERSION: ${AGENT_VERSION:-edge}
      MQTT_USERNAME: ${EDGE_MQTT_USER:-edge}
      MQTT_PASSWORD: ${EDGE_MQTT_PASS}
      LOG_FORMAT: ${LOG_FORMAT:-json}
    depends_on:
      postgres:
        condition: service_healthy
      mosquitto:
        condition: service_started
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    restart: unless-stopped

volumes:
  mosquitto_data:
  redis_data:
  postgres_data:
`

const installShTemplate = `#!/usr/bin/env bash
# OpenEdge Edge Installer — {{.OrgName}}
# Generated: {{.Generated}}
set -euo pipefail

GREEN='\033[0;32m'; RED='\033[0;31m'; NC='\033[0m'

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " OpenEdge Edge Installer"
echo " Organization: {{.OrgName}}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check Docker
if ! command -v docker &>/dev/null; then
  echo -e "${RED}ERROR: Docker not found.${NC}"
  echo "  Install: https://docs.docker.com/get-docker/"
  exit 1
fi

# Check Docker Compose v2
if ! docker compose version &>/dev/null; then
  echo -e "${RED}ERROR: Docker Compose v2 required.${NC}"
  echo "  Install: https://docs.docker.com/compose/install/"
  exit 1
fi

# The broker refuses anonymous connections, so it needs a password file. It is
# built with mosquitto's own tool inside the official image rather than by
# hashing the password here: the format is mosquitto's, and reimplementing it
# is how a box ends up with a broker nobody can log into.
if [ ! -f mosquitto/config/passwd ]; then
  echo "Creating the broker password file..."
  # shellcheck disable=SC1091
  set -a; . ./.env; set +a
  docker run --rm -v "$(pwd)/mosquitto/config:/mosquitto/config" \
    eclipse-mosquitto:2 \
    mosquitto_passwd -b -c /mosquitto/config/passwd "$EDGE_MQTT_USER" "$EDGE_MQTT_PASS"
  chmod 0700 mosquitto/config/passwd
fi

echo "Pulling images..."
docker compose pull

echo "Starting services..."
docker compose up -d

echo ""
echo "Waiting for the configuration to arrive from the platform..."
for _ in $(seq 1 30); do
  if docker compose logs driver-manager 2>/dev/null | grep -q "\[CONFIG-SYNC\] configuration for"; then
    echo -e "${GREEN}✓ Configuration received.${NC}"
    break
  fi
  sleep 2
done

echo ""
echo -e "${GREEN}✓ OpenEdge edge is running!${NC}"
echo ""
echo "  Status : docker compose ps"
echo "  Logs   : docker compose logs -f driver-manager"
echo "  Stop   : docker compose down"
`

const installPs1Template = `# OpenEdge Edge Installer — Windows
# Organization: {{.OrgName}}
# Generated: {{.Generated}}
#Requires -Version 5.1

$ErrorActionPreference = "Stop"

Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
Write-Host " OpenEdge Edge Installer" -ForegroundColor Cyan
Write-Host " Organization: {{.OrgName}}" -ForegroundColor Cyan
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
Write-Host ""

# Check Docker Desktop
try { docker compose version | Out-Null }
catch {
  Write-Host "ERROR: Docker Desktop with Compose v2 is required." -ForegroundColor Red
  Write-Host "  https://docs.docker.com/desktop/install/windows/"
  exit 1
}

Write-Host "Pulling images..."
docker compose pull

Write-Host "Starting services..."
docker compose up -d

Write-Host ""
Write-Host "✓ OpenEdge edge is running!" -ForegroundColor Green
Write-Host ""
Write-Host "  Status : docker compose ps"
Write-Host "  Logs   : docker compose logs -f driver-manager"
Write-Host "  Stop   : docker compose down"
`

const readmeTemplate = `# OpenEdge Edge — {{.OrgName}}

Generated: {{.Generated}}

## Prerequisites

- Docker Engine 24+ and Docker Compose v2
- Linux (x86_64 or arm64), macOS, or Windows 10+

## Quick Start

### Linux / macOS

` + "```bash" + `
chmod +x install.sh
./install.sh
` + "```" + `

### Windows (PowerShell)

` + "```powershell" + `
Set-ExecutionPolicy Bypass -Scope Process
.\install.ps1
` + "```" + `

## Configuration

Edit ` + "`" + `.env` + "`" + ` before starting to change passwords and paths.

| Variable | Description |
|---|---|
| ` + "`CORE_API_TOKEN`" + ` | API key for the central platform (pre-filled) |
| ` + "`CORE_API_URL`" + ` | Central platform base URL (pre-filled) |
| ` + "`ORG_ID`" + ` | This plant's organization (pre-filled) |
| ` + "`DB_PASSWORD`" + ` | Local PostgreSQL password — **change this** |

## Manage

` + "```bash" + `
docker compose ps              # check running services
docker compose logs -f         # stream all logs
docker compose down            # stop everything
docker compose pull && docker compose up -d  # update to latest
` + "```" + `

## Security

- Keep ` + "`" + `.env` + "`" + ` secure — it contains your edge API key.
- Change ` + "`" + `DB_PASSWORD` + "`" + ` from the default value.
- The API key grants read access to your gateway configuration only.
`
