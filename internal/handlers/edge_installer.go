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

	data := &installerData{
		OrgID:      orgID,
		OrgName:    orgName,
		OrgSlug:    slugify(orgName),
		APIKey:     apiKey,
		APIBaseURL: publicBaseURL(c),
		MQTTUser:   mqttUser,
		MQTTPass:   mqttPass,
		Generated:  time.Now().UTC().Format(time.RFC3339),
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

	// Mosquitto minimal config.
	mqFH := &zip.FileHeader{Name: dir + "mosquitto/config/mosquitto.conf", Method: zip.Deflate}
	mqFH.SetMode(0644)
	mqFW, err := w.CreateHeader(mqFH)
	if err != nil {
		return nil, err
	}
	if _, err = mqFW.Write([]byte(mosquittoConf)); err != nil {
		return nil, err
	}

	return buf, nil
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

const mosquittoConf = `listener 1883
allow_anonymous true
persistence true
persistence_location /mosquitto/data/
`

const envTemplate = `# OpenEdge Edge Configuration
# Organization : {{.OrgName}} (ID: {{.OrgID}})
# Generated   : {{.Generated}}
#
# KEEP THIS FILE SECURE — it contains credentials.

# ── Cloud API ────────────────────────────────────────────────────────────────
EDGE_API_URL={{.APIBaseURL}}
EDGE_API_KEY={{.APIKey}}

# ── Local Database ───────────────────────────────────────────────────────────
DB_HOST=postgres
DB_PORT=5432
DB_USER=edge_user
DB_PASSWORD=changeme_db
DB_NAME=edge_db

# ── Local MQTT Broker ────────────────────────────────────────────────────────
MQTT_HOST=mosquitto
MQTT_PORT=1883

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
      - ./mosquitto/config:/mosquitto/config:ro
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
      EDGE_API_URL: ${EDGE_API_URL}
      EDGE_API_KEY: ${EDGE_API_KEY}
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

echo "Pulling images..."
docker compose pull

echo "Starting services..."
docker compose up -d

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
| ` + "`EDGE_API_KEY`" + ` | API key for cloud authentication (pre-filled) |
| ` + "`EDGE_API_URL`" + ` | Cloud API base URL (pre-filled) |
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
