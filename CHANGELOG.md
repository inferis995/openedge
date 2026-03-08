# Changelog

All notable changes to OpenEdge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

## [Unreleased]

### Planned for 1.1.0
- Predictive maintenance features
- Machine learning integration
- OPC UA method calls support
- Enhanced analytics dashboard
- Mobile applications (iOS/Android)
- Advanced reporting module

---

## Version Summary

| Version | Date | Status | Key Features |
|---------|------|--------|--------------|
| 1.0.0 | 2026-03-08 | Production | First stable release with complete feature set |
| 0.x | 2024-2025 | Development | Development versions (not documented) |
