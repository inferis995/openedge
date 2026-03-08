# OpenEdge Industrial Edge Middleware

<div align="center">

**Production-Ready Industrial IoT Edge Computing Platform**

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat&logo=docker)](https://www.docker.com/)

⚡ **High-Performance** • 🏭 **Industrial-Grade** • 🔒 **Secure** • 📊 **Real-Time Analytics**

</div>

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

- [Plug-and-Play Setup](#-plug-and-play-setup)
- [Quick Start](#-quick-start)
- [Architecture](#-architecture)
- [Database Migrations](#-database-migrations)
- [Configuration](#-configuration)
- [Deployment](#-deployment)
- [API Documentation](#-api-documentation)
- [Development](#-development)
- [Troubleshooting](#-troubleshooting)
- [Contributing](#-contributing)
- [License](#-license)

---

## 🎯 Plug-and-Play Setup

**Get OpenEdge running in under 5 minutes with zero configuration:**

```bash
# 1. Clone from GitHub
git clone https://github.com/your-org/openedge.git
cd openedge

# 2. Start with default configuration
docker-compose up -d

# 3. Access the web UI
# Open http://localhost in your browser
# Login with admin/admin123
```

**That's it!** The system automatically:
- ✅ Initializes PostgreSQL with TimescaleDB
- ✅ Creates all database tables and indexes
- ✅ Sets up default admin user
- ✅ Configures MQTT broker
- ✅ Starts Redis caching
- ✅ Launches all microservices
- ✅ Applies database migrations

No manual database setup, no configuration files to edit, no dependencies to install.

---

## ⚡ Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.21+ (for development)
- Node.js 20+ (for UI development)

### Start with Docker

```bash
# Clone the repository
git clone https://github.com/your-org/openedge.git
cd openedge

# Copy environment template
cp .env.example .env

# Start all services
docker-compose up -d

# Access the web UI
open http://localhost:80
```

**Default Credentials:**
- Username: `admin`
- Password: `admin123` (change immediately after first login)

### Services Started

| Service | Port | Description |
|---------|------|-------------|
| Web UI | 80 | React dashboard |
| Core API | 8081 | REST API & WebSocket |
| Mosquitto | 18830 | MQTT broker |
| PostgreSQL | 5432 | TimescaleDB |
| Redis | 6379 | Cache & real-time data |

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         OpenEdge                            │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   Drivers   │───▶│ Engine Historian│───▶│  Cloud Sync  │  │
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

## 🗄️ Database Migrations

### Professional Migration System

This project uses **goose**, a professional database migration tool that provides:
- Version tracking of all database schema changes
- Safe rollback capabilities
- Support for complex migration scripts
- Easy status tracking and debugging

### Migration Strategy

We use a professional two-stage approach:

**1. Initial Setup (First Run)**
- PostgreSQL automatically runs initialization scripts via `docker-entrypoint-initdb.d`
- All 24 migrations are applied on first container start
- Creates complete schema with TimescaleDB hypertables, indexes, and default data

**2. Future Updates (Production)**
- Goose tracks applied migrations in `goose_db_version` table
- New migrations are versioned and applied incrementally
- Supports rollback for safe schema changes

### Migration Commands

```bash
# Check migration status
make migrate-status
# Or: go run cmd/migrate/main.go -cmd=status

# Apply pending migrations
make migrate
# Or: go run cmd/migrate/main.go -cmd=up

# Rollback last migration
make migrate-down
# Or: go run cmd/migrate/main.go -cmd=down

# Build standalone migration binary
make migrate-build
```

### Migration Files

All migration files are located in `migrations/` directory:
- `001_init_schema.sql` - Core tables (organizations, users, tags, gateways, drivers)
- `002_add_users.sql` - Authentication and default admin user
- `003_add_timescaledb.sql` - TimescaleDB extension and time-series functions
- `004_remove_alarms_table.sql` - Legacy alarm system removal
- `005-013` - Feature additions (MQTT, OPC UA, audit logs, TimescaleDB hypertables)
- `014-024` - Advanced features (continuous aggregates, data cleanup, nullable markers)

Each migration follows the goose format:
```sql
-- +goose Up
[Your SQL here]
-- +goose Down
[Rollback SQL here]
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

### Alarm Configuration

Alarms are configured per tag with:
- **Thresholds**: High/Limit limits with hysteresis
- **Delays**: Time delay before triggering (seconds)
- **Severity**: Critical, Warning, Info
- **Actions**: MQTT publish, database logging, UI notifications

### Multi-Tenant Organization System

OpenEdge supports multi-tenant deployments with organization-based data isolation:

**Organization Types:**
- **Global Admin** (`org_id=NULL`): Access to all organizations and system-wide configuration
- **Organization Members**: Access only to their assigned organization's data

**Default Setup:**
- Default organization named "Default" is created automatically
- Admin user has global access (no org assignment)
- Additional users can be assigned to specific organizations

**API Organization Isolation:**
All API endpoints automatically filter data by organization based on the authenticated user:
```bash
# Global admin sees all organizations
GET /api/organizations

# Regular users see only their organization
GET /api/organizations  # Returns only their org
GET /api/tags           # Only tags from their org
GET /api/gateways       # Only gateways from their org
```

---

## 🚀 Deployment

### Production Deployment

```bash
# Build production images
docker-compose -f docker-compose.yml build

# Start with production configuration
docker-compose --env-file .env.prod up -d

# Check service health
docker-compose ps
```

### System Requirements

**Minimum:**
- CPU: 2 cores
- RAM: 4 GB
- Disk: 20 GB SSD

**Recommended (100+ tags):**
- CPU: 4 cores
- RAM: 8 GB
- Disk: 100 GB SSD

---

## 📚 API Documentation

### REST API Endpoints

```
GET  /api/health          # Health check
GET  /api/tags            # List all tags
GET  /api/tags/:id        # Get tag details
POST /api/tags/:id/write  # Write tag value
GET  /api/alarms          # List alarms
GET  /api/history         # Historical data
GET  /api/system          # System settings
PUT  /api/system          # Update settings
```

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

### Makefile Commands

The project includes a Makefile for common development tasks:

```bash
# Database migrations
make migrate          # Apply pending migrations
make migrate-status   # Show migration status
make migrate-down     # Rollback last migration
make migrate-build    # Build migration binary
make migrate-docker   # Run migrations in Docker
make migrate-reset    # Reset all migrations (WARNING: destroys data)
```

### Build Services

```bash
# Build all Go services
go build ./services/...

# Build UI
cd services/web-ui
npm run build
```

### Run Locally

```bash
# Start PostgreSQL, Redis, Mosquitto
docker-compose up -d postgres redis mosquitto

# Start services
./services/core-api/core-api &
./services/engine-historian/engine-historian &
./services/driver-modbus/driver-modbus &

# Start UI
cd services/web-ui && npm run dev
```

---

## 📖 Documentation

- [Alarm System Guide](docs/alarms.md)
- [Driver Configuration](docs/drivers.md)
- [API Reference](docs/api.md)
- [Deployment Guide](docs/deployment.md)
- [Troubleshooting](docs/troubleshooting.md)

---

## 🔧 Troubleshooting

### Common Issues

**1. Login Fails with "Invalid Credentials"**

Even with correct admin/admin123 credentials, login may fail if:
- Database schema is outdated
- Run: `docker-compose exec postgres goose -dir ./migrations postgres "user=industrial_user password=industrial_pass dbname=industrial_edge" status`

**2. Services Cannot Connect to Database**

Symptom: `failed to ping database: pq: SSL is not enabled on the server`

Solution: Ensure `DB_SSLMODE=disable` is set in docker-compose.yml for all services

**3. Migration Errors**

If migrations fail to apply:
```bash
# Check current migration status
make migrate-status

# View PostgreSQL logs
docker-compose logs postgres

# Reset database (WARNING: destroys all data)
docker-compose down -v
docker-compose up -d
```

**4. Mosquitto Connection Issues**

If drivers cannot connect to MQTT broker:
- Check Mosquitto is running: `docker-compose ps mosquitto`
- Verify port 18830 is not in use: `netstat -an | grep 18830`
- Check logs: `docker-compose logs mosquitto`

**5. Time-Series Data Not Appearing**

If historical data is missing:
- Verify TimescaleDB extension: `docker-compose exec postgres psql -U industrial_user -d industrial_edge -c "SELECT * FROM pg_extension WHERE extname='timescaledb'"`
- Check hypertable status: `docker-compose exec postgres psql -U industrial_user -d industrial_edge -c "SELECT * FROM timescaledb_information.hypertables"`
- Verify compression policy: `docker-compose exec postgres psql -U industrial_user -d industrial_edge -c "SELECT * FROM timescaledb_information.jobs"`

### Getting Help

- Check logs: `docker-compose logs -f [service-name]`
- Review database status: `make migrate-status`
- Open an issue on GitHub with detailed error messages

---

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development Workflow

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## 📜 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 🙏 Acknowledgments

Built with:
- [Golang](https://golang.org/) - Core platform
- [TimescaleDB](https://www.timescale.com/) - Time-series database
- [Eclipse Mosquitto](https://mosquitto.org/) - MQTT broker
- [React](https://reactjs.org/) - Frontend framework
- [Sparkplug B](https://sparkplug.eclipse.org/) - Industrial IoT standard

---

## 📧 Support

- **Documentation**: [https://docs.openedge.io](https://docs.openedge.io)
- **Issues**: [GitHub Issues](https://github.com/your-org/openedge/issues)
- **Discussions**: [GitHub Discussions](https://github.com/your-org/openedge/discussions)

---

<div align="center">

**Made with ❤️ for the Industrial IoT Community**

</div>
