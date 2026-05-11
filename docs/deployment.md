# Deploying ctx Server

This guide covers deploying `ctx serve` as a persistent service with PostgreSQL.

## Prerequisites

- ctx binary (built from source or downloaded from [releases](https://github.com/Zate/ctx/releases))
- PostgreSQL 14+ instance
- (Optional) TLS certificate and key

## Quick Start

```bash
# 1. Create a PostgreSQL database
createdb ctx

# 2. Set environment variables
export CTX_SERVER_DB_URL="postgres://user:pass@localhost:5432/ctx?sslmode=disable"
export CTX_SERVER_ADMIN_PASSWORD="your-secret-password"

# 3. Start the server
ctx serve
```

The server will run migrations automatically on startup.

## Configuration

Configuration is resolved in priority order: CLI flags > environment variables > `~/.ctx/server.yaml` > defaults.

### Environment Variables

```bash
CTX_SERVER_PORT=8377
CTX_SERVER_BIND=0.0.0.0
CTX_SERVER_DB_URL=postgres://user:pass@host:5432/ctx?sslmode=require
CTX_SERVER_TLS_CERT=/path/to/cert.pem
CTX_SERVER_TLS_KEY=/path/to/key.pem
CTX_SERVER_ADMIN_PASSWORD=your-secret
```

### Config File (`~/.ctx/server.yaml`)

```yaml
port: 8377
bind: 0.0.0.0
db_url: postgres://user:pass@host:5432/ctx?sslmode=require
tls_cert: /path/to/cert.pem
tls_key: /path/to/key.pem
admin_password: your-secret
auto_sync: true
```

## Authentication

When `admin_password` is set, all API routes (except `/health` and `/api/auth/*`) require a Bearer token.

### Device Registration Flow

1. Client calls `POST /api/auth/device` with a device name
2. Server returns a device code and user code
3. Admin visits `/device/authorize`, enters the user code and admin password
4. Admin approves or denies the device
5. Client polls `POST /api/auth/token` until approved
6. Client receives access token, refresh token, and device ID

### Token Refresh

Access tokens can be refreshed via `POST /api/auth/refresh` with the refresh token and device ID.

## Docker Deployment

### Dockerfile

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ctx .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/ctx /usr/local/bin/ctx
EXPOSE 8377
ENTRYPOINT ["ctx", "serve"]
```

### Docker Compose

```yaml
services:
  ctx:
    build: .
    ports:
      - "8377:8377"
    environment:
      CTX_SERVER_DB_URL: postgres://ctx:ctx@postgres:5432/ctx?sslmode=disable
      CTX_SERVER_ADMIN_PASSWORD: ${CTX_ADMIN_PASSWORD:-changeme}
    depends_on:
      postgres:
        condition: service_healthy
    restart: unless-stopped

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ctx
      POSTGRES_PASSWORD: ctx
      POSTGRES_DB: ctx
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ctx"]
      interval: 5s
      timeout: 5s
      retries: 5
    restart: unless-stopped

volumes:
  pgdata:
```

```bash
# Start
docker compose up -d

# View logs
docker compose logs -f ctx

# Stop
docker compose down
```

## Systemd Service

```ini
# /etc/systemd/system/ctx-server.service
[Unit]
Description=ctx Knowledge Server
After=network.target postgresql.service

[Service]
Type=simple
User=ctx
ExecStart=/usr/local/bin/ctx serve
EnvironmentFile=/etc/ctx/server.env
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# /etc/ctx/server.env
CTX_SERVER_DB_URL=postgres://ctx:password@localhost:5432/ctx?sslmode=disable
CTX_SERVER_ADMIN_PASSWORD=your-secret
CTX_SERVER_PORT=8377
```

## TLS

For production, either:

1. **Direct TLS** — Set `tls_cert` and `tls_key` in config
2. **Reverse proxy** — Use nginx, Caddy, or similar to terminate TLS

Example Caddy config:

```
ctx.example.com {
    reverse_proxy localhost:8377
}
```

## Client Setup

On each device that should sync:

```bash
# 1. Point to the server
ctx remote set https://ctx.example.com

# 2. Authenticate
ctx auth

# 3. (Admin) Approve the device at https://ctx.example.com/device/authorize

# 4. Test sync
ctx sync status
ctx sync push
ctx sync pull
```

### Enable Auto-Sync

Add to `~/.ctx/server.yaml`:
```yaml
auto_sync: true
```

Or set: `export CTX_AUTO_SYNC=true`

With auto-sync enabled, hooks will automatically pull on session start and push on session end.

## SQLite Mode

The server also works with SQLite (the default when no `--db-url` is provided). This is suitable for single-user local development but not recommended for production or multi-device sync.

```bash
ctx serve  # Uses ~/.ctx/store.db
```

## Monitoring

- `GET /health` — Returns `{"status": "ok"}` (no auth required)
- `GET /api/status` — Returns node/edge/tag/token counts
- `GET /admin` — Web dashboard with stats and recent activity

## Backup

### PostgreSQL
```bash
pg_dump -U ctx ctx > ctx-backup.sql
```

### SQLite
```bash
cp ~/.ctx/store.db ~/.ctx/store.db.backup
```
