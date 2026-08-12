# OpenEdge — Developer Context

Industrial IoT Platform. Go + Gin + PostgreSQL + React + TypeScript + Docker.

## Architecture

```
services/
  core-api/          Go backend — REST API, WebSocket, MQTT, business logic
  driver-manager/    Go — spawns driver containers, polls OTA updates, heartbeat
  web-ui/            React + TypeScript + Vite — shadcn/ui + Tailwind + Zustand
internal/
  auth/              JWT HS256, account lockout, SSO stubs
  db/                PostgreSQL connection + ALL migrations (runAutoMigrations)
  handlers/          One file per domain (tags, alarms, synoptics, security, ...)
  middleware/        RequireAuth, RequireRole, OrganizationContext
  notifications/     Email, Telegram, Slack, Teams, PagerDuty
  connectors/        InfluxDB v2 export
  scaling/           EU (engineering unit) conversion
```

## Key Patterns

### Multi-Tenancy
- All DB queries scoped by `org_id` WHERE clause
- JWT has `org_id` claim (null = global admin)
- `X-Organization-ID` header enforced by `middleware.OrganizationContext()`
- Global admin check: `middleware.IsGlobalAdmin(c)` → role=admin AND org_id IS NULL
- `middleware.GetOrganizationID(c)` returns org_id from JWT (not from header — prevents spoofing)

### Authentication
- `POST /api/auth/login` → JWT 24h
- Account lockout: 5 failures → 30-min lock (`users.locked_until`)
- `middleware.RequireAuth` → validates JWT, sets `user_id`, `org_id`, `role` in context
- `middleware.RequireRole(models.RoleAdmin)` for admin-only routes

### Database Migrations
ALL migrations live in `internal/db/db.go` → `runAutoMigrations()`.
Run at startup automatically. Pattern:

> There is a second path that is **not** a migration system: `migrations/*.sql`
> is mounted at `/docker-entrypoint-initdb.d` and runs once, on an empty data
> directory. It seeds the base schema for a NEW database and never reaches an
> existing one. Adding a file there produces a change that works locally and
> never ships. See `migrations/README.md`.

```go
if _, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS ...`); err != nil {
    log.Printf("Warning: ...")
}
```
No separate migration files needed (auto-migrate on startup).

### Route Registration
All in `services/core-api/main.go`. Groups:
```go
api := router.Group("/api", middleware.RequireAuth)
api.GET("/tags", ...)  // standard auth
api.POST("/tags", middleware.RequireRole(models.RoleAdmin), ...)  // admin only
```

### Frontend Patterns
- API clients: `services/web-ui/src/api/*.ts` — all use `apiClient` (axios with JWT interceptor)
- State: Zustand stores in `src/stores/`
- Pages: `src/pages/*.tsx` — one per route
- Routes: `src/App.tsx`
- Sidebar nav: `src/components/layout/Sidebar.tsx`
- Auth guard: `<RequireAuth>` and `<RequireAdmin>` wrapper components

## Deployment Modes

### On-Prem (customer machine)
```bash
make start              # build + run (first time)
make onprem-tls         # add HTTPS via Caddy internal CA
sudo make install-service   # systemd service (auto-start at boot)
```

### SaaS / Cloud
```bash
make vps-up             # docker-compose.vps.yml (Traefik + Let's Encrypt)
# Or: Coolify → New Resource → Docker Compose → paste docker-compose.coolify.yml
```

### Monitoring
```bash
docker-compose -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d
# Grafana: http://localhost:3030
# Prometheus: http://localhost:9090
# Core API metrics: http://localhost:8081/metrics
```

## CI / Local Testing

```bash
# Go build + tests
JWT_SECRET=local-test-secret-key-minimum-32-chars go build ./...
JWT_SECRET=local-test-secret-key-minimum-32-chars go test -race ./internal/...

# Lint (only new code since master)
golangci-lint run --timeout=5m --new-from-rev=origin/master ./internal/...

# Frontend
cd services/web-ui && npx tsc --noEmit
cd services/web-ui && npm run lint
```

## Important Files

| File | Purpose |
|------|---------|
| `internal/db/db.go` | ALL DB migrations, connection pool |
| `services/core-api/main.go` | Route registration, startup |
| `services/web-ui/src/App.tsx` | Frontend routes |
| `services/web-ui/src/components/layout/Sidebar.tsx` | Nav items |
| `.env.example` | All env vars documented |
| `docker-compose.yml` | Main stack |
| `docker-compose.onprem-tls.yml` | TLS overlay |
| `docker-compose.monitoring.yml` | Prometheus + Grafana + Loki |
| `scripts/backup.sh` | pg_dump + rotation |
| `postgres/postgresql.conf` | Tuned for historian workload |

## Gateway Join Pattern

Gateways have no direct `org_id` — join via:
```sql
gateways g
JOIN areas a ON a.id = g.area_id
JOIN sites s ON s.id = a.site_id
JOIN organizations o ON o.id = s.org_id
WHERE o.id = $1
```

## Default Credentials (change after first login!)
- Username: `admin`
- Password: `admin123`
- Or set `OPENEDGE_INITIAL_ADMIN_PASSWORD` in `.env` before first start
