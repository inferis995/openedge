# Post-Restore Scripts for OpenEdge

After restoring a backup, you need to restart services in the correct order to ensure proper reconnection to databases, Redis, and MQTT broker.

## Automatic Restart

### Windows (PowerShell)
```powershell
.\scripts\post-restore.ps1
```

### Linux/Mac (Bash)
```bash
./scripts/post-restore.ps1
```

## What This Script Does

1. **PostgreSQL** - Restarts the database service
2. **Redis** - Clears cache and ensures fresh state
3. **Mosquitto** - Restarts MQTT broker
4. **Core API** - Reconnects to all services
5. **Driver Manager** - Recreates all driver containers
6. **Engine Historian** - Restarts history service
7. **Web UI** - Restarts web interface

Each service waits for a "healthy" status before proceeding to the next.

## Why Is This Necessary?

After a database restore:
- Services may have stale connections to the database
- Redis cache may contain inconsistent data
- Core API needs to reestablish connections to all services
- Driver containers need to be recreated with fresh configuration

## Manual Restart (If Scripts Fail)

```bash
docker restart industrial-postgres
docker restart industrial-redis
docker restart industrial-mosquitto
docker restart industrial-core-api
docker restart industrial-driver-manager
docker restart industrial-engine-historian
docker restart industrial-web-ui
```

## Verify All Services Are Running

```bash
docker ps
```

All services should show "healthy" status.
