<div align="center">

<table border="0" cellspacing="0" cellpadding="0">
  <tr>
    <td align="center" valign="middle" width="160">
      <img src="img/foto.png" alt="OpenEdge" width="130"/>
    </td>
    <td align="center" valign="middle" width="40"></td>
    <td align="center" valign="middle" width="220">
      <img src="img/icona.png" alt="OpenEdge Logo" width="180"/>
    </td>
  </tr>
</table>

# OpenEdge Industrial Edge Middleware

**Production-Ready Industrial IoT Edge Computing Platform**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![CESMII i3X](https://img.shields.io/badge/CESMII-i3X%20v1-orange?style=flat)](https://www.cesmii.org/)

⚡ **High-Performance** • 🏭 **Industrial-Grade** • 🔒 **Secure** • 📊 **Real-Time Analytics**

</div>

---

## Screenshots

<table>
  <tr>
    <td align="center"><b>Gateway Dashboard</b></td>
    <td align="center"><b>Tags Management</b></td>
  </tr>
  <tr>
    <td><img src="img/gatway.PNG" alt="Gateway Dashboard" width="400"/></td>
    <td><img src="img/tag.PNG" alt="Tags Management" width="400"/></td>
  </tr>
  <tr>
    <td align="center"><b>Alarm System</b></td>
    <td align="center"><b>Trend & History</b></td>
  </tr>
  <tr>
    <td><img src="img/allarm.PNG" alt="Alarm System" width="400"/></td>
    <td><img src="img/trend.PNG" alt="Trend & History" width="400"/></td>
  </tr>
  <tr>
    <td align="center"><b>MQTT Publisher</b></td>
    <td align="center"><b>MQTT Monitor</b></td>
  </tr>
  <tr>
    <td><img src="img/mqtt.PNG" alt="MQTT Configuration" width="400"/></td>
    <td><img src="img/mqttpub.PNG" alt="MQTT Monitor" width="400"/></td>
  </tr>
</table>

---

## Overview

**OpenEdge** is a professional-grade edge computing middleware for industrial IoT. It bridges the gap between field devices (PLCs, sensors) and cloud systems, providing real-time data collection, alarm management, and a vendor-neutral REST API compatible with the **CESMII i3X v1** standard.

### Key Features

- **Multi-Protocol Drivers** — Modbus TCP, Siemens S7, OPC UA, MQTT, Redis
- **Real-Time Processing** — Sub-second latency with Redis caching
- **Advanced Alarm System** — Configurable thresholds, delays, hysteresis
- **CESMII i3X Access API** — Standard vendor-neutral REST interface for equipment and properties
- **AI-Ops Endpoints** — Optimized for AI agents: anomaly detection, alarm digest, org snapshot
- **Cloud Sync** — Bidirectional MQTT forwarding with Sparkplug B support
- **Multi-Organization** — Org-scoped access control with global and org-scoped admins
- **Time-Series Database** — TimescaleDB with automatic retention policies
- **Modern Web UI** — React dashboard with dark/light themes, real-time updates
- **Zero-Config Start** — `.env` and `JWT_SECRET` auto-generated on first run

---

## Table of Contents

- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [How Drivers Work](#how-drivers-work)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [API Reference](#api-reference)
  - [Authentication](#authentication)
  - [i3X Access API](#i3x-access-api-cesmii-standard)
  - [AI-Ops Endpoints](#ai-ops-endpoints)
  - [Standard Endpoints](#standard-endpoints)
- [AI Agent Skills](#ai-agent-skills)
- [Database Migrations](#database-migrations)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

### Prerequisites

**Docker Desktop** — [download here](https://www.docker.com/products/docker-desktop/)

**`make`** (Linux/Mac only — Windows uses `openedge.bat` instead):

| OS | Command |
|----|---------|
| Ubuntu / Debian | `sudo apt install make` |
| RHEL / Fedora | `sudo dnf install make` |
| Mac | `xcode-select --install` |
| Windows | Use `openedge.bat` — no make needed |

### Start

```bash
# Clone
git clone https://github.com/inferis995/openedge.git
cd openedge

# Build + start  (.env and JWT_SECRET created automatically)
make start

# Wait ~30s, then verify
curl http://localhost:8081/ready
# Expected: {"status":"ready","db":"ok","redis":"ok"}

# Verify all 5 driver images were built
docker images | grep industrial-driver
# Must show: modbus, s7, opcua, mqtt, redis
```

Open **http://localhost:3000** — Login: `admin` / `admin123`

> Change the default password after first login.

### Services

| Service | Port |
|---------|------|
| Web UI | 3000 |
| Core API | 8081 |
| MQTT Broker | 18830 |
| PostgreSQL | 5432 |
| Redis | 6379 |

---

## Architecture

```
┌────────────────────────────────────────────────────────────┐
│                        OpenEdge                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  ┌─────────────┐   ┌──────────────┐   ┌──────────────┐   │
│  │   Drivers   │──▶│   Historian  │──▶│  Cloud Sync  │   │
│  │             │   │              │   │              │   │
│  │ • Modbus    │   │ • Real-time  │   │ • Sparkplug B│   │
│  │ • S7        │   │ • History    │   │ • Forwarding │   │
│  │ • OPC UA    │   │ • Alarms     │   │ • Commands   │   │
│  │ • MQTT      │   │ • Deadband   │   │              │   │
│  │ • Redis     │   │              │   │              │   │
│  └─────────────┘   └──────────────┘   └──────────────┘   │
│         │                                                  │
│         ▼                                                  │
│  ┌─────────────┐   ┌──────────────┐   ┌──────────────┐   │
│  │  PostgreSQL │   │    Redis     │   │   Core API   │   │
│  │ TimescaleDB │◀─▶│    Cache     │   │  i3X + REST  │   │
│  └─────────────┘   └──────────────┘   └──────────────┘   │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

---

## How Drivers Work

Drivers are Docker images that `driver-manager` spawns **on demand** when you create a Gateway in the UI.

```
Create Gateway  →  driver-manager starts driver container with GATEWAY_ID
Delete Gateway  →  driver-manager stops and removes the container
```

- **0 driver containers** when no gateways exist — this is normal
- Each gateway gets its own isolated driver container
- Driver images must be built before use — handled by `make start`

---

## Configuration

`make start` automatically creates `.env` from `.env.example` and generates a secure `JWT_SECRET`. No manual setup needed for development.

For production, create `.env` before first run:

```bash
cp .env.example .env
# Edit the values you want to change
```

### Key Variables

```bash
# Auto-generated — do not set manually unless you know what you're doing
JWT_SECRET=<hex_64_chars>

# Data storage (default: ./data/ inside the repo)
POSTGRES_DATA_PATH=./data/postgres
REDIS_DATA_PATH=./data/redis

# Database
POSTGRES_DB=industrial_edge
POSTGRES_USER=industrial_user
POSTGRES_PASSWORD=CHANGE_ME_IN_PRODUCTION

# API port
PORT=8081

# Cloud sync (optional)
CLOUD_SYNC_ENABLED=false
CLOUD_MQTT_HOST=
CLOUD_MQTT_PORT=1883
CLOUD_MQTT_USERNAME=
CLOUD_MQTT_PASSWORD=
CLOUD_MQTT_TOPIC=
```

### Data Storage Path

Data is persisted via bind mount (already wired in `docker-compose.yml`). Default is `./data/` inside the repo. For production, set absolute paths in `.env` **before** the first `make start`:

```bash
# Linux/Mac
POSTGRES_DATA_PATH=/opt/openedge/data/postgres
REDIS_DATA_PATH=/opt/openedge/data/redis

# Windows
POSTGRES_DATA_PATH=D:/openedge-data/postgres
REDIS_DATA_PATH=D:/openedge-data/redis
```

> Changing these paths after data exists requires a backup + restore.

---

## Deployment

### Linux / Mac

```bash
make start    # First run: build images + start
make up       # Start (images already built)
make down     # Stop all services
make restart  # Stop + start
make logs     # Follow logs
make clean    # Stop + delete all data (irreversible)
```

### Windows

Double-click **`openedge.bat`** for the interactive menu, or from cmd/PowerShell:

```cmd
openedge.bat start    :: build + launch
openedge.bat stop
openedge.bat restart
openedge.bat logs
openedge.bat status
openedge.bat clean    :: delete all data (asks confirmation)
```

`openedge.bat` handles `.env` and `JWT_SECRET` generation automatically — no extra tools needed beyond Docker Desktop.

### System Requirements

| | Minimum | Recommended (100+ tags) |
|-|---------|--------------------------|
| CPU | 2 cores | 4 cores |
| RAM | 4 GB | 8 GB |
| Disk | 20 GB SSD | 100 GB SSD |

---

## API Reference

Base URL: `http://localhost:8081`

### Authentication

All endpoints except `/health` and `/ready` require a JWT token:

```bash
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

# Use in every request
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment
```

---

### i3X Access API (CESMII Standard)

Base path: `/api/i3x/v1/`

Vendor-neutral REST interface compatible with the **CESMII i3X v1** specification. Use this API for integrations with external systems (SCADA, MES, cloud platforms) and AI agents.

#### ID Format

All i3X IDs are prefixed strings:

| Type | Format | Example |
|------|--------|---------|
| Organization | `org-{n}` | `org-1` |
| Site | `site-{n}` | `site-3` |
| Area | `area-{n}` | `area-7` |
| Gateway | `gw-{n}` | `gw-2` |
| Tag / Property | `tag-{n}` | `tag-42` |

#### Quality Codes

These codes apply to **all protocols** (Modbus, S7, OPC UA, MQTT, Redis) — the i3X standard uses OPC-UA numeric encoding regardless of the underlying driver.

| Value | Meaning |
|-------|---------|
| `192` | Good |
| `64` | Uncertain |
| `0` | Bad |

> The standard REST API uses `0=Good, 1=Bad`. The i3X API uses `192=Good, 0=Bad`.

#### Endpoints

```
GET  /api/i3x/v1/equipment                        # Full asset hierarchy (org→site→area→gateway)
GET  /api/i3x/v1/equipment/:id                    # Single equipment node
GET  /api/i3x/v1/equipment/:id/properties         # Tags for a gateway, with live values
GET  /api/i3x/v1/equipment/:id/properties/:propId # Single property with live value
GET  /api/i3x/v1/properties                       # All tags in the organization
GET  /api/i3x/v1/properties/:id                   # Single property with live value
PUT  /api/i3x/v1/properties/:id/value             # Write value to tag (requires i3x_write or admin)
GET  /api/i3x/v1/alarms                           # Active alarms in i3X format
GET  /api/i3x/v1/alarms/history                   # Alarm history in i3X format
```

#### Example — List equipment

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment
```

```json
{
  "items": [
    {
      "id": "gw-2",
      "name": "PLC-Serbatoio1",
      "type": "Equipment",
      "parentId": "area-7",
      "path": "MyOrg/Sito-Crotone/Zona-A/PLC-Serbatoio1",
      "attributes": {"driver_type": "MODBUS_TCP", "connection_status": "online"}
    }
  ],
  "total": 5
}
```

#### Example — Read tag values

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/equipment/gw-2/properties
```

```json
{
  "items": [
    {
      "id": "tag-42",
      "name": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "dataType": "Float",
      "historize": true,
      "current": {"value": 42.5, "quality": 192, "timestamp": "2026-04-27T10:30:00Z"}
    }
  ],
  "total": 12
}
```

#### Example — Write to PLC

```bash
curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "X-Organization-ID: 1" \
  -H "Content-Type: application/json" \
  -d '{"value": 1}' \
  http://localhost:8081/api/i3x/v1/properties/tag-43/value
# Response: {"message": "Write command sent"}
```

Write requires `i3x_write: true` in the user's JWT claims, or admin role. The command is sent asynchronously to the driver via MQTT.

#### Example — Active alarms

```bash
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/i3x/v1/alarms
```

```json
{
  "items": [
    {
      "id": "alarm-45",
      "propertyId": "tag-42",
      "propertyName": "Portata_Ingresso",
      "equipmentId": "gw-2",
      "equipmentName": "PLC-Serbatoio1",
      "severity": "Critical",
      "status": "Active",
      "alarmType": "high",
      "message": "Portata massima superata",
      "value": 98.7,
      "triggerTime": "2026-04-27T08:15:00Z"
    }
  ],
  "total": 1
}
```

---

### AI-Ops Endpoints

Optimized for AI agents and automation — one call returns everything needed.

```
GET /api/aiops/summary?hours=24
    Org-wide snapshot: tag stats (avg/min/max/sample_count), alarm counts, gateway totals

GET /api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30
    Z-score anomaly detection. Threshold: |z_score| >= 2.5

GET /api/aiops/alarms/digest?hours=24
    Alarm digest grouped by severity — for reports and notifications
```

---

### Standard Endpoints

```
GET  /health                         # Server health
GET  /ready                          # Full readiness (DB + Redis)

GET  /api/tags                       # List tags
GET  /api/tags/:id/current           # Real-time value (quality: 0=Good, 1=Bad)
POST /api/tags/import                # Bulk import from PLC address format
GET  /api/tags/export?gateway_id=3   # Export tags as PLC address text

GET  /api/gateways                   # Gateways with connection_status (online/offline/unknown)
GET  /api/alarms/active              # Active & acknowledged alarms
GET  /api/history/stats              # Aggregated historical statistics

GET  /api/system                     # System settings
POST /api/backup                     # Create DB backup
POST /api/restore                    # Restore from backup
```

### Tag Import Format

```
POST /api/tags/import
{"gateway_id": 3, "historize": true, "content": "..."}
```

Format: `Alias : DataType AT Address;`

**Modbus TCP:**
```
Portata_Ingresso : REAL AT 40001;
Livello_Vasca    : REAL AT 40003;
Pompa_On         : BOOL AT 00001.0;
```

**Siemens S7:**
```
DB1_REAL4 : REAL AT DB1.DBD4;
DB1_INT0  : INT  AT DB1.DBW0;
M0_0      : BOOL AT M0.0;
```

Supported types: `BOOL`, `INT`, `UINT`, `DINT`, `UDINT`, `REAL`, `STRING`, `WORD`

### WebSocket

```
ws://localhost:8081/ws/realtime
```

Real-time tag values, alarm notifications, system events.

---

## AI Agent Skills

OpenEdge ships two skill files for AI agents (Claude Code, OpenClaw, and compatible frameworks).

Install from the repo:

```bash
# Clone and the skills are in .claude/skills/
git clone https://github.com/inferis995/openedge.git

# Or download individually with curl
curl -o .claude/skills/openedge.md \
  https://raw.githubusercontent.com/inferis995/openedge/master/.claude/skills/openedge.md

curl -o .claude/skills/openedge-ops.md \
  https://raw.githubusercontent.com/inferis995/openedge/master/.claude/skills/openedge-ops.md
```

### `openedge-ops` — Deploy & Configure

Gives the agent everything needed to deploy OpenEdge from scratch, create gateways, and import tags.

```
"Installa OpenEdge e crea un gateway Modbus su 192.168.1.10"
"Importa questi tag S7: DB1_REAL4:REAL:DB1.DBD4, M0_0:BOOL:M0.0"
"Il driver del gateway non parte — diagnostica e risolvi"
```

### `openedge` — Monitor & i3X

Gives the agent read/write access via both the standard API and the i3X Access API.

```
"Leggi il valore corrente di tutti i tag del gateway PLC-1 via i3X"
"Ci sono allarmi Critical attivi?"
"Scrivi valore 1 sul tag Pompa_On del gateway PLC-Serbatoio1"
"Genera un digest degli allarmi delle ultime 24 ore"
"Rileva anomalie sul tag Pressione_Rete dell'ultima settimana"
```

---

## Database Migrations

```bash
make migrate-status   # Check pending migrations
make migrate          # Apply pending migrations
make migrate-down     # Rollback last migration
```

Migrations run automatically on startup. The `migrations_archive/` folder contains the full schema history for reference.

---

## Troubleshooting

**Application refuses to start — "JWT_SECRET environment variable is required"**

```bash
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env
make restart
```

**Driver container doesn't start after creating a gateway**

Driver images weren't built. Use `make start` instead of `docker-compose up -d`:

```bash
docker images | grep industrial-driver  # Check if images exist
make start                              # Builds images + starts services
```

**Login fails with "Invalid Credentials"**

```bash
make migrate-status
make migrate
```

**Check logs**

```bash
make logs
docker-compose logs -f core-api
docker-compose logs -f driver-manager
```

**Full reset (deletes all data)**

```bash
make clean && make start
```

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Commit and push
4. Open a Pull Request

Issues: https://github.com/inferis995/openedge/issues

---

## License

MIT License — see [LICENSE](LICENSE).

---

<div align="center">
Made with ❤️ for the Industrial IoT Community
</div>
