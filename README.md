# ctx

Persistent memory for Claude Code. A CLI tool that gives AI agents the ability to store, organize, query, and manage knowledge across conversations — locally or synced to a remote server.

## The Problem

Claude Code's context is a bucket that fills up and overflows. Agents can't choose what stays, don't know what they've lost, and rediscover the same things across sessions. There's no way to checkpoint, roll back, or hand off context between tasks.

## The Solution

`ctx` provides a graph-based knowledge store backed by SQLite (local) or PostgreSQL (remote), integrated into Claude Code via hooks. Agents write XML commands in their responses to store knowledge. Hooks parse those commands and persist them. On the next session, stored knowledge is automatically injected into context.

The core loop:

```
Session starts → ctx injects stored knowledge (+ auto-sync pull if configured)
    ↓
Agent works, writes <ctx:remember> commands in responses
    ↓
Session ends → ctx parses commands, updates database (+ auto-sync push if configured)
    ↓
Next session → stored knowledge is there
```

## Installation

### As a Claude Code Plugin (Recommended)

The ctx plugin handles binary download, hook registration, and skill injection automatically. See the [plugin repository](https://github.com/Zate/cc-plugins) for installation.

### Manual Build

```bash
# Build and install
make install

# Or manually:
go build -o ctx .
./ctx init       # Creates ~/.ctx/store.db
```

`ctx init` creates the SQLite database at `~/.ctx/store.db`. Hook registration is handled by the plugin.

## How It Works

### Knowledge as a Graph

Knowledge is stored as **nodes** (facts, decisions, patterns, observations) connected by **edges** (relationships). Each node has a type, content, tags, and a token estimate for budget management.

**Node types:**

| Type | Purpose |
|------|---------|
| `fact` | Stable knowledge ("User prefers Go for backend services") |
| `decision` | A choice with rationale ("Chose SQLite for single-binary deployment") |
| `pattern` | Recurring approach ("This codebase uses explicit errors over panics") |
| `observation` | Current/temporary context ("The auth bug seems related to token refresh") |
| `hypothesis` | Unvalidated idea |
| `open-question` | Unresolved question |
| `summary` | Compressed knowledge derived from multiple nodes |
| `source` | Ingested external content |

### Tiers Control What Gets Loaded

Nodes are tagged with tiers that control context composition:

- **`tier:pinned`** — Always loaded into context
- **`tier:reference`** — Stored for on-demand recall (not auto-loaded)
- **`tier:working`** — Current task context (auto-loaded)
- **`tier:off-context`** — Archived, not loaded

The default view query is `tag:tier:pinned OR tag:tier:working`. Use `<ctx:recall>` to pull reference-tier nodes into context on demand.

### XML Commands

Agents interact with ctx by writing XML commands in their responses:

```xml
<!-- Store knowledge -->
<ctx:remember type="decision" tags="project:auth,tier:reference">
Chose JWT over sessions for stateless API authentication.
</ctx:remember>

<!-- Query stored knowledge -->
<ctx:recall query="type:decision AND tag:project:auth"/>

<!-- Check memory status -->
<ctx:status/>

<!-- Mark task boundaries -->
<ctx:task name="implement-auth" action="start"/>
<ctx:task name="implement-auth" action="end"/>

<!-- Compress old knowledge -->
<ctx:summarize nodes="01HQ1234,01HQ5678" archive="true">
Summary of authentication decisions.
</ctx:summarize>

<!-- Link related nodes -->
<ctx:link from="01HQ1234" to="01HQ5678" type="DEPENDS_ON"/>

<!-- Replace outdated knowledge -->
<ctx:supersede old="01HQ1234" new="01HQ5678"/>

<!-- Expand a summary to see source nodes -->
<ctx:expand node="01HQ1234"/>
```

Commands inside code blocks are ignored (so agents can safely show examples without triggering them).

### Hook Integration

ctx integrates with Claude Code through three hooks:

| Hook | Trigger | Purpose |
|------|---------|---------|
| `SessionStart` | Conversation begins | Composes and injects stored knowledge (+ auto-sync pull) |
| `UserPromptSubmit` | User sends a message | Injects pending recall results |
| `Stop` | Agent finishes responding | Parses `<ctx:*>` commands from response (+ auto-sync push) |

## CLI Reference

### Node Management

```bash
ctx add --type fact --tags "tier:reference,project:myapp" "API uses OAuth 2.0"
ctx show <node-id>
ctx update <node-id> --content "Updated content"
ctx delete <node-id>
ctx list [--type fact] [--tag tier:reference] [--limit 10]
ctx search "OAuth authentication"
```

Short ID prefixes work for all node operations (e.g., `ctx show 01HQ` instead of the full ULID).

### Graph Operations

```bash
ctx link <from-id> <to-id> --type DEPENDS_ON
ctx unlink <edge-id>
ctx edges [--from <id>] [--to <id>]
ctx related <node-id>
ctx trace <node-id>        # Trace relationship paths
```

### Tags

```bash
ctx tag <node-id> tier:reference
ctx untag <node-id> tier:working
ctx tags                   # List all tags
```

### Views and Composition

```bash
ctx compose --query "tag:tier:pinned OR tag:tier:working" --budget 50000
ctx view list
ctx view set default --query "tag:tier:pinned OR tag:tier:working"
```

### Query Language

The query language supports predicates, boolean operators, and grouping:

```
type:decision AND tag:project:auth
tag:tier:reference OR tag:tier:pinned
NOT type:observation
(type:fact OR type:decision) AND tag:project:myapp
created:>2025-01-01
tokens:<1000
```

### Other Commands

```bash
ctx status                 # Database statistics
ctx export                 # Export all data as JSON
ctx import <file>          # Import data from JSON
ctx ingest <file>          # Ingest a file as a source node
ctx version                # Show version info
```

## Remote Server

ctx can run as a self-hosted HTTP server with PostgreSQL, enabling knowledge sync across multiple devices.

### Server Setup

```bash
# Start the server (SQLite, local dev)
ctx serve

# Start with PostgreSQL
ctx serve --db-url "postgres://user:pass@host:5432/ctx?sslmode=require"

# With authentication
ctx serve --admin-password "your-secret"

# With TLS
ctx serve --tls-cert /path/to/cert.pem --tls-key /path/to/key.pem
```

**Configuration** can be set via `~/.ctx/server.yaml`, environment variables, or CLI flags:

| Setting | Flag | Env Var | YAML Key |
|---------|------|---------|----------|
| Port | `--port` | `CTX_SERVER_PORT` | `port` |
| Bind address | `--bind` | `CTX_SERVER_BIND` | `bind` |
| Database URL | `--db-url` | `CTX_SERVER_DB_URL` | `db_url` |
| TLS certificate | `--tls-cert` | `CTX_SERVER_TLS_CERT` | `tls_cert` |
| TLS key | `--tls-key` | `CTX_SERVER_TLS_KEY` | `tls_key` |
| Admin password | `--admin-password` | `CTX_SERVER_ADMIN_PASSWORD` | `admin_password` |
| Auto-sync | — | `CTX_AUTO_SYNC` | `auto_sync` |

Priority: CLI flags > environment variables > server.yaml > defaults.

Example `~/.ctx/server.yaml`:
```yaml
port: 8377
bind: 0.0.0.0
db_url: postgres://user:pass@host:5432/ctx?sslmode=require
admin_password: your-secret
auto_sync: true
```

### Admin UI

When the server is running, visit `/admin` for a web dashboard with:
- `/admin` — Dashboard with node counts, token totals, recent activity
- `/admin/nodes` — Browse, search, and filter nodes
- `/admin/repos` — View registered repository mappings
- `/admin/devices` — Manage registered devices

### Authentication (Device Flow)

ctx uses OAuth 2.0 Device Authorization Flow for CLI authentication:

```bash
# 1. Set the remote server URL
ctx remote set http://your-server:8377

# 2. Authenticate (opens browser for approval)
ctx auth

# 3. Check auth status
ctx auth status

# 4. Logout
ctx auth logout
```

The server admin approves devices via the web UI at `/device/authorize`.

### Sync

Sync knowledge between local and remote:

```bash
# Push local changes to server
ctx sync push

# Pull remote changes to local
ctx sync pull

# Check sync status
ctx sync status

# Register current git repo for project mapping
ctx sync register-repo
```

**Auto-sync:** Set `auto_sync: true` in `server.yaml` or `CTX_AUTO_SYNC=true` to automatically pull on session start and push on session end.

### Device Management

```bash
# List all registered devices
ctx device list

# Revoke a device's access
ctx device revoke <device-id>
```

### HTTP API

All endpoints are under `/api/`:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/status` | Database statistics |
| `POST` | `/api/nodes` | Create a node |
| `GET` | `/api/nodes/{id}` | Get a node (supports short ID prefix) |
| `PATCH` | `/api/nodes/{id}` | Update a node |
| `DELETE` | `/api/nodes/{id}` | Delete a node |
| `GET` | `/api/edges/{id}` | Get edges for a node |
| `POST` | `/api/edges` | Create an edge |
| `DELETE` | `/api/edges` | Delete an edge |
| `POST` | `/api/nodes/{id}/tags` | Add tags |
| `DELETE` | `/api/nodes/{id}/tags` | Remove tags |
| `POST` | `/api/query` | Query nodes |
| `POST` | `/api/compose` | Compose context |
| `POST` | `/api/sync/push` | Push changes |
| `POST` | `/api/sync/pull` | Pull changes |
| `POST` | `/api/repo-mappings` | Register repo mapping |
| `POST` | `/api/auth/device` | Initiate device flow |
| `POST` | `/api/auth/token` | Poll for token |
| `POST` | `/api/auth/refresh` | Refresh access token |
| `GET` | `/api/devices` | List devices (auth required) |
| `POST` | `/api/devices/{id}/revoke` | Revoke device (auth required) |

When `admin_password` is set, all `/api/` routes (except `/api/auth/*`) require a `Bearer` token in the `Authorization` header.

## Architecture

```
ctx
├── cmd/                   # CLI commands (Cobra)
│   ├── hook/              # Hook subcommands (session-start, prompt-submit, stop)
│   │   └── autosync.go    # Auto-sync on session start/end
│   ├── serve.go           # HTTP server command
│   ├── auth.go            # Device flow authentication
│   ├── sync.go            # Sync push/pull/status commands
│   ├── remote.go          # Remote server configuration
│   ├── device.go          # Device management commands
│   └── *.go               # Node/edge/tag/view commands
├── internal/
│   ├── db/                # Database layer (Store interface)
│   │   ├── store.go       # Store interface definition
│   │   ├── db.go          # SQLite implementation
│   │   └── postgres.go    # PostgreSQL implementation
│   ├── server/            # HTTP server, auth middleware, admin UI
│   ├── auth/              # Device flow state management, token hashing
│   ├── sync/              # Sync logic, state tracking, URL normalization
│   ├── hook/              # Command parser and executor
│   ├── query/             # Query language parser and executor
│   ├── token/             # Token estimation
│   └── view/              # Context composition and rendering
├── testutil/              # Shared test utilities
└── main.go
```

**Key dependencies:**
- `modernc.org/sqlite` — Pure-Go SQLite (no CGO required)
- `github.com/jackc/pgx/v5` — Pure-Go PostgreSQL driver
- `github.com/spf13/cobra` — CLI framework
- `github.com/oklog/ulid/v2` — Time-sortable unique IDs
- `gopkg.in/yaml.v3` — Server config parsing
- `github.com/stretchr/testify` — Test assertions

**Database:** SQLite at `~/.ctx/store.db` (local) or PostgreSQL (remote server). Schema includes `nodes`, `edges`, `tags`, `views`, `pending`, `users`, `devices`, `repo_mappings`, `sync_log`, `access_log`, `schema_version` tables and FTS5 full-text search (SQLite only).

## Cross-Platform Builds

All dependencies are pure Go (CGO_ENABLED=0). Supported targets:

- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64`

```bash
make build-all    # Build for all platforms
```

Releases are automated via GoReleaser on tagged commits.

## Development

```bash
# Run all tests
make test

# Unit tests only
make test-unit

# Fuzz testing (query parser)
make test-fuzz

# Coverage report
make test-coverage

# Build
make build

# Lint
make lint

# Clean
make clean
```

## Design Documents

The `docs/design/` directory contains the full specification and design:

- **docs/design/ctx-specification.md** — Technical spec: schema, commands, query language, hooks
- **docs/design/ctx-implementation-prompt.md** — Implementation roadmap (8 phases)
- **docs/design/ctx-details.md** — Detailed implementation decisions and edge cases
- **docs/design/ctx-testing.md** — Testing strategy with example test code
- **docs/design/ctx-skill-SKILL.md** — The skill file installed for Claude Code

## License

MIT
