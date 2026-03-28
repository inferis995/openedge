<div align="center">

<img src="img/icona.png" alt="OpenEdge Logo" width="200"/>

# OpenEdge Industrial Edge Middleware

**Production-Ready Industrial IoT Edge Computing Platform**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat&logo=docker)](https://www.docker.com/)

⚡ **High-Performance** • 🏭 **Industrial-Grade** • 🔒 **Secure** • 📊 **Real-Time Analytics**

</div>

---

## 📸 Screenshots

<table>
  <tr>
    <td align="center"><b>Dashboard Gateway</b></td>
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
    <td align="center"><b>MQTT broker Pub.</b></td>
    <td align="center"><b>MQTT Monitor</b></td>
  </tr>
  <tr>
    <td><img src="img/mqtt.PNG" alt="MQTT Configuration" width="400"/></td>
    <td><img src="img/mqttpub.PNG" alt="MQTT Publishing" width="400"/></td>
  </tr>
</table>

---

## 🚀 Overview

**OpenEdge** is a professional-grade edge computing middleware designed for industrial IoT applications. It bridges the gap between industrial PLCs/field devices and cloud systems, providing real-time data collection, alarm management, and secure cloud synchronization.

### Key Features

- **🔄 Multi-Protocol Support**: Modbus TCP, Siemens S7, OPC UA, MQTT, Redis
- **⚡ Real-Time Data Processing**: Sub-millisecond latency with Redis caching
- **🚨 Advanced Alarm System**: Configurable thresholds, delays, and hysteresis
- **📡 Cloud Sync**: Bidirectional MQTT forwarding with Sparkplug B support
- **🔒 End-to-End Security**: AES-256 encryption, authentication, TLS support
- **📊 Time-Series Database**: TimescaleDB integration with automatic retention policies
- **🎨 Modern Web UI**: React-based dashboard with dark/light themes
- **🐳 Production-Ready**: Docker Compose deployment, health checks, auto-reconnection

---

## 📋 Table of Contents

- [Screenshots](#-screenshots)
- [Quick Start](#-quick-start)
- [AI Agent Skills](#-ai-agent-skills)
  - [Deploy & Configure (openedge-ops)](#deploy--configure-openedge-ops)
  - [Monitor & Query (openedge)](#monitor--query-openedge)
- [Architecture](#-architecture)
- [How Drivers Work](#-how-drivers-work)
- [Database Migrations](#-database-migrations)
- [Configuration](#-configuration)
- [Deployment](#-deployment)
- [API Documentation](#-api-documentation)
  - [Core Endpoints](#core-endpoints)
  - [AI Agent Integration](#ai-agent-integration-read-only)
- [Development](#-development)
- [Troubleshooting](#-troubleshooting)
- [Contributing](#-contributing)
- [License](#-license)

---

## 🎯 Quick Start

**Prerequisites:** Docker Desktop installed and running.

```bash
# 1. Clone
git clone https://github.com/inferis995/openedge.git
cd openedge

# 2. Build driver images + start all services
#    ⚠️ Use make start — do NOT use docker-compose up directly
make start

# 3. Verify everything is up (wait ~30s after make start)
curl http://localhost:8081/ready
# Expected: {"status":"ready","db":"ok","redis":"ok"}

# 4. Verify driver images were built (required before creating gateways)
docker images | grep industrial-driver
# Must show 5 images: modbus, s7, opcua, mqtt, redis
```

**Windows** (no `make`):
```powershell
.\scripts\start-industrial-edge.ps1
```

Open **http://localhost:3000** — Login: `admin` / `admin123`

> ⚠️ Change the default password after first login.

### Services

| Service | Port |
|---------|------|
| Web UI | 3000 |
| Core API | 8081 |
| MQTT broker | 18830 |
| PostgreSQL | 5432 |
| Redis | 6379 |

---

## 🤖 AI Agent Skills

OpenEdge ships with AI agent skills that let any compatible agent (Claude Code, OpenCode, OpenClaw and others) deploy, configure and monitor the system autonomously — without manual intervention.

Install the skills with one command:

```bash
npx github:inferis995/openedge
```

Or install individually:

```bash
npx github:inferis995/openedge openedge-ops   # deploy & configure
npx github:inferis995/openedge openedge        # monitor & query
```

---

### Deploy & Configure (`openedge-ops`)

The `openedge-ops` skill gives the agent everything it needs to deploy OpenEdge from scratch and configure gateways and tags — including the correct build sequence that ensures drivers work on first use.

**What the agent can do with this skill:**

- Clone the repo and run a verified deploy (`make start`)
- Check that all 5 driver images are built before creating any gateway
- Create gateways (Modbus TCP, Siemens S7, OPC UA, MQTT)
- Import tags in bulk from PLC address format or one by one
- Diagnose and fix container issues

**Example prompts:**

```
"Installa OpenEdge su questo server e crea un gateway Modbus su 192.168.1.10"

"Importa questi tag sul gateway PLC-1:
 Portata:REAL:40001, Livello:REAL:40003, Pompa1:BOOL:00001.0"

"Il driver del gateway non è partito — diagnostica e risolvi"
```

> The skill enforces a mandatory pre-check: all 5 driver images must exist
> before gateway creation is attempted. This prevents the most common
> first-deploy issue where gateways appear to save but the driver never starts.

---

### Monitor & Query (`openedge`)

The `openedge` skill gives the agent read-only access to the live system — alarms, tag values, anomaly detection, and historical data.

**What the agent can do with this skill:**

- Check active alarms and their severity
- Read real-time tag values
- Detect anomalies via Z-score on historical data
- Generate alarm digests for reports
- Monitor gateway connectivity

**Example prompts:**

```
"Ci sono allarmi critici attivi in questo momento?"

"Mostrami le anomalie del tag Pressione_Rete nell'ultima settimana"

"Genera un digest degli allarmi delle ultime 24 ore"

"Quali gateway sono offline da più di 30 minuti?"
```

**Use with cron jobs for autonomous monitoring:**

```bash
# Claude Code — check alarms every 5 minutes
/schedule "*/5 * * * *" "controlla allarmi OpenEdge su localhost:8081, notifica se critical"

# Shell cron with OpenClaw
*/5  * * * * openclaw run openedge "controlla allarmi attivi"
0    7 * * * openclaw run openedge "genera report giornaliero allarmi"
```

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         OpenEdge                            │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   Drivers   │───▶│Engine Historian│───▶│  Cloud Sync  │  │
│  │             │    │              │    │              │  │
│  │ • Modbus    │    │ • Real-time   │    │ • Sparkplug B│  │
│  │ • S7        │    │   History     │    │ • Forwarding │  │
│  │ • OPC UA    │    │ • Alarms      │    │ • Commands   │  │
│  │ • MQTT      │    │ • Deadband    │    │              │  │
│  │ • Redis     │    │ • Compression │    │              │  │
│  └─────────────┘    └──────────────┘    └──────────────┘  │
│         │                                      │           │
│         ▼                                      ▼           │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │  PostgreSQL │    │    Redis     │    │  Cloud MQTT  │  │
│  │ TimescaleDB │◀──▶│    Cache     │    │              │  │
│  └─────────────┘    └──────────────┘    └──────────────┘  │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

---

## ⚙️ How Drivers Work

Drivers (Modbus, S7, OPC UA, MQTT, Redis) are **not started automatically**.
They are Docker images that `driver-manager` spawns **on demand** when you
create a Gateway in the web UI.

```
You create Gateway  →  driver-manager starts driver container with GATEWAY_ID
You delete Gateway  →  driver-manager stops and removes the container
```

This means:
- **0 driver containers** when no gateways are configured — this is normal
- Each gateway gets its own isolated driver container
- Driver images must be built before the system starts (handled by `make start`)

---

## 🗄️ Database Migrations

This project uses **goose** for database migrations.

```bash
# Check migration status
make migrate-status

# Apply pending migrations
make migrate

# Rollback last migration
make migrate-down
```

---

## ⚙️ Configuration

### Environment Variables

```bash
# Database
DB_HOST=postgres
DB_PORT=5432
DB_USER=industrial_user
DB_PASSWORD=industrial_pass
DB_NAME=industrial_edge

# MQTT
MQTT_HOST=mosquitto
MQTT_PORT=1883

# Redis
REDIS_HOST=redis
REDIS_PORT=6379

# Cloud Sync (Optional)
CLOUD_SYNC_ENABLED=false
CLOUD_MQTT_HOST=
CLOUD_MQTT_PORT=1883
CLOUD_MQTT_USERNAME=
CLOUD_MQTT_PASSWORD=
CLOUD_MQTT_TOPIC=

# Encryption (Optional)
ENCRYPTION_KEY= # 32-character key for AES-256
```

---

## 🚀 Deployment

### Useful Commands

```bash
make start    # First run: build images + start services
make up       # Start services (if images already built)
make down     # Stop all services
make restart  # Stop + start
make logs     # Follow logs
make clean    # Stop + delete all data (full reset)
```

### Manual (without make)

```bash
# Build all images including drivers
docker-compose -f docker-compose.yml -f docker-compose.build.yml build

# Start services
docker-compose up -d

# Stop
docker-compose down
```

### System Requirements

**Minimum:**
- CPU: 2 cores
- RAM: 4 GB
- Disk: 20 GB SSD
- Docker Desktop installed

**Recommended (100+ tags):**
- CPU: 4 cores
- RAM: 8 GB
- Disk: 100 GB SSD

---

## 📚 API Documentation

Base URL: `http://localhost:8081`

All endpoints (except `/health` and `/ready`) require JWT authentication:

```bash
# Login
TOKEN=$(curl -s -X POST http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

# Use token in subsequent requests
curl -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: 1" \
  http://localhost:8081/api/tags
```

### Core Endpoints

```
GET  /health                      # Server health check
GET  /ready                       # Full readiness check (DB + Redis)

GET  /api/tags                    # List all tags
GET  /api/tags/:id                # Get tag details
GET  /api/tags/:id/current        # Real-time value from Redis
POST /api/tags/:id/write          # Write value to tag

GET  /api/gateways                # List gateways with connection status
GET  /api/alarms/active           # Active & acknowledged alarms
GET  /api/alarms/history          # Alarm history
GET  /api/history                 # Raw historical data
GET  /api/history/stats           # Aggregated statistics

GET  /api/system                  # System settings
PUT  /api/system                  # Update settings
POST /api/backup                  # Create DB backup
POST /api/restore                 # Restore from backup
```

### AI Agent Integration (read-only)

These endpoints provide optimized responses for AI agents and automation systems — a single call returns everything needed without N+1 queries:

```
GET  /api/aiops/summary?hours=24
     # Org-wide snapshot: tag stats, alarm counts, gateway status
     # Returns: active_alarms_count, critical_alarms_count, total_gateways_count, tags[]

GET  /api/aiops/anomalies?tag_id=5&window_hours=168&baseline_days=30
     # Z-score anomaly detection on a specific tag
     # Returns: baseline_mean, baseline_std_dev, anomaly_count, anomalies[]

GET  /api/aiops/alarms/digest?hours=24
     # Alarm digest grouped by severity — ideal for reports and notifications
     # Returns: total_fired, still_active, cleared, by_severity{}, alarms[]
```

All AI-Ops endpoints are read-only, org-scoped, and have a 30-second query timeout.

### WebSocket

```
ws://localhost:8081/ws/realtime
```

Real-time tag value updates, alarm notifications, and system events.

---

## 🛠️ Development

### Prerequisites

```bash
# Install Go dependencies
go mod download

# Install UI dependencies
cd services/web-ui
npm install
```

### Build Services

```bash
# Build all Go services
go build ./services/...

# Build UI
cd services/web-ui
npm run build
```

---

## 🔧 Troubleshooting

### Common Issues

**1. Driver containers crash with "GATEWAY_ID required"**

This is normal if you ran `docker-compose up -d` directly.
Driver images must be built before they can be used by driver-manager.
Use `make start` instead — it builds all images first, then starts services.
Driver containers are started by driver-manager only when you create a Gateway.

**2. Login fails with "Invalid Credentials"**

Database schema may be outdated. Run:
```bash
make migrate-status
make migrate
```

**3. Services cannot connect to the database**

Symptom: `failed to ping database: pq: SSL is not enabled on the server`

Solution: Ensure `DB_SSLMODE=disable` is set for all services in docker-compose.yml.

**4. Mosquitto connection issues**

```bash
docker-compose ps mosquitto
docker-compose logs mosquitto
```

**5. Full reset (clear all data)**

```bash
make clean
make start
```

### Getting Help

- Check logs: `make logs` or `docker-compose logs -f [service-name]`
- Open an issue: https://github.com/inferis995/openedge/issues

---

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes
4. Push and open a Pull Request

---

## 📜 License

MIT License — see [LICENSE](LICENSE).

---

<div align="center">

**Made with ❤️ for the Industrial IoT Community**

</div>
