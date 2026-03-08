# OpenEdge Release 1.0

## 🎉 First Production Release

**Release Date:** March 8, 2026
**Version:** 1.0.0
**Status:** Production Ready

---

## 📦 What's Included

This release contains the complete OpenEdge Industrial Edge Middleware platform with all features production-ready and fully tested.

### Core Components

- ✅ **Multi-Protocol Drivers** - Modbus TCP, Siemens S7, OPC UA, MQTT, Redis
- ✅ **Real-Time Data Historian** - TimescaleDB integration with compression
- ✅ **Advanced Alarm System** - Thresholds, delays, hysteresis, notifications
- ✅ **Cloud Synchronization** - Sparkplug B support, bidirectional forwarding
- ✅ **Modern Web UI** - React dashboard with dark/light themes
- ✅ **Production-Ready Error Handling** - Auto-reconnection, retry logic
- ✅ **Security Features** - AES-256 encryption, authentication

---

## 🚀 Quick Installation

### Option 1: Docker Compose (Recommended)

```bash
# Extract the release
tar -xzf openedge-1.0.0.tar.gz
cd openedge

# Copy environment template
cp .env.example .env

# Edit configuration
nano .env

# Start all services
docker-compose up -d

# Access the web UI
open http://localhost:80
```

### Option 2: Manual Installation

See [INSTALLATION.md](INSTALLATION.md) for detailed manual installation instructions.

---

## 🔧 System Requirements

### Minimum Requirements
- **CPU:** 2 cores
- **RAM:** 4 GB
- **Disk:** 20 GB SSD
- **OS:** Linux (Ubuntu 20.04+, Debian 11+, RHEL 8+) or Windows 10/11 with WSL2

### Recommended Requirements (100+ tags)
- **CPU:** 4 cores
- **RAM:** 8 GB
- **Disk:** 100 GB SSD

---

## 📋 Breaking Changes from Development Branch

### Renamed Components
- `ralph-wiggum-claude-code--main-core-api` → `openedge-core-api`
- `ralph-wiggum-claude-code--main-web-ui` → `openedge-web-ui`

### Removed Features
- None - all features from development branch are included

### Configuration Changes
- New environment variable: `ENCRYPTION_KEY` (optional, for password encryption)
- Cloud sync now supports configurable topic prefix
- Database retention policy now configurable via UI

---

## ✨ New Features in 1.0

### Alarm System
- **Threshold Alarms** - High/Low limits with configurable hysteresis
- **Delayed Alarms** - Configurable delay before triggering
- **Alarm Notifications** - Real-time UI updates, MQTT publishing
- **Alarm History** - Complete audit trail with state changes

### Cloud Sync
- **Sparkplug B Support** - Native industrial IoT protocol
- **Configurable Prefix** - Add custom topic prefix for cloud forwarding
- **Bidirectional Communication** - Forward commands from cloud to edge
- **Auto-Reconnection** - Automatic retry with exponential backoff

### Production Improvements
- **Connection Retry Logic** - All services retry on connection failure
- **Graceful Degradation** - Services continue operating during partial failures
- **Health Monitoring** - Built-in health checks for all services
- **Structured Logging** - Production-ready logging format

---

## 🔒 Security Features

- **AES-256 Encryption** - Optional encryption for sensitive credentials
- **Password Masking** - Automatic masking in logs and exports
- **Authentication** - Built-in user authentication system
- **CORS Configuration** - Configurable cross-origin resource sharing

---

## 📚 Documentation

- [User Manual](docs/USER_MANUAL.md)
- [Installation Guide](docs/INSTALLATION.md)
- [API Reference](docs/API_REFERENCE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Migration Guide](docs/MIGRATION.md)

---

## 🐛 Known Issues

None - this is a clean production release.

---

## 🔄 Upgrading from Previous Versions

This is the first production release. If you were running from the development branch, see the [Migration Guide](docs/MIGRATION.md).

---

## 📞 Support

- **Documentation:** https://docs.openedge.io
- **Issues:** https://github.com/your-org/openedge/issues
- **Discussions:** https://github.com/your-org/openedge/discussions

---

## 🙏 Acknowledgments

This release incorporates contributions from the industrial IoT community and builds upon excellent open-source projects:
- TimescaleDB
- Eclipse Mosquitto
- Sparkplug B standard
- Go programming language
- React framework

---

## 📜 License

MIT License - see [LICENSE](LICENSE) file for details.

---

**Next Release Preview:** 1.1.0 (Q2 2026) - Predictive Maintenance, Machine Learning Integration
