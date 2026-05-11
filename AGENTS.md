# AGENTS.md

Guidance for AI agents working on this codebase. If you're Claude, see [CLAUDE.md](CLAUDE.md) for the Claude-specific addendum first, then read this.

## Quick Orientation

This is `ctx`, a Go CLI that provides persistent memory for Claude Code agents. The module path is `github.com/zate/ctx`. It stores knowledge as nodes in a graph database (SQLite local, PostgreSQL when running as a remote server) and integrates with Claude Code via hooks. There is also an opt-in `ctx doc` subsystem for decomposing/recomposing markdown documents.

**Meta warning:** if `<ctx:remember>` commands work in your responses, this project built the tool you're using. Be careful with changes — you're editing your own memory system.

## Repository Layout

| Path | What It Does |
|------|-------------|
| `cmd/root.go` | Root Cobra command, global flags, subpackage registration. |
| `cmd/version.go` | Forwards `SetVersionInfo` to `cmd/system` (keeps public surface stable). |
| `cmd/node/` | Node CRUD commands: `add`, `show`, `list`, `update`, `delete`, `search`, `query`. |
| `cmd/tag/` | Tagging commands: `tag`, `tags`, `untag`. |
| `cmd/graph/` | Graph commands: `link`, `unlink`, `related`, `trace`, plus the `edges` listing. |
| `cmd/view/` | Context composition: `compose`, `summarize`. |
| `cmd/io/` | Import/export/ingest commands. |
| `cmd/doc/` | `ctx doc` markdown decomposition subsystem commands. |
| `cmd/system/` | System commands: `init`, `install`, `status`, `accessed`, `version`. |
| `cmd/server/` | Remote-server commands: `serve`, `auth`, `sync`, `remote`, `device`. |
| `cmd/hook/` | Hook subcommands: `session-start`, `prompt-submit`, `stop`, plus `autosync.go`. |
| `cmd/mcp/` | MCP server entry point. |
| `cmd/internal/cmdutil/` | Shared helpers for cmd subpackages (not part of public CLI surface). |
| `internal/db/` | Database layer. `store.go` defines the `Store` interface; `db.go` is SQLite, `postgres.go` is PostgreSQL. Read `db.go` first for schema. |
| `internal/db/access_log.go` | Per-agent access logging. Inserts gated on `kind='memory'` so doc/content nodes are silently skipped. |
| `internal/hook/` | `<ctx:*>` XML command parser and executor. Parser correctly ignores fenced/inline code blocks. |
| `internal/query/` | Query language: tokenizer, recursive-descent parser, AST, SQL executor. Has fuzz tests. |
| `internal/view/composer.go` | Selects, sorts, budgets, and renders nodes for context injection. |
| `internal/token/` | Token count estimation (chars/4 heuristic). |
| `internal/server/` | HTTP server, auth middleware, admin UI. |
| `internal/auth/` | Device-flow state, token hashing. |
| `internal/sync/` | Sync logic, state tracking, URL normalization. |
| `internal/doc/` | `ctx doc` decomposition/recomposition. Strictly isolated from memory queries. |
| `internal/agent/`, `internal/agenthelp/` | Agent identity (`$CTX_AGENT`) and `--agent-help` rendering. |
| `pkg/db/` | Public re-export shim of `internal/db` types for external consumers embedding the store. |
| `testutil/` | Shared test helpers (temp database creation). |

## Schema

Eleven tables plus an FTS5 virtual table. SQLite source of truth is `internal/db/db.go`; PostgreSQL mirror is `internal/db/postgres.go`. Migrations are version-tracked and run automatically on database open.

Tables: `schema_version`, `nodes`, `edges`, `tags`, `views`, `pending`, `users`, `devices`, `repo_mappings`, `sync_log`, `access_log`. SQLite additionally maintains `nodes_fts` (FTS5).

**Node types:** `fact`, `decision`, `pattern`, `observation`, `hypothesis`, `task`, `summary`, `source`, `open-question`.

**Node kinds:** `memory` (default — what the memory subsystem operates on), `document`, `content` (used by `ctx doc`). Memory commands filter to `kind='memory'`; doc nodes are invisible to memory queries by default.

**Tier tags** control composition: `tier:pinned` (always loaded), `tier:reference` (on-demand via recall), `tier:working` (current task), `tier:off-context` (archived).

## Conventions

### Code Style
- Standard Go conventions, `gofmt` formatted.
- Return errors, don't panic. Hooks fail gracefully (log to stderr, output `{}` on stdout).
- IDs are ULIDs (time-sortable, globally unique). Short ID prefixes resolve uniquely for all node operations.
- Multi-write database operations use transactions.
- Table-driven tests with `testify` assertions.

### Adding a New CLI Command
1. Create a new file in the appropriate `cmd/<group>/` subpackage (e.g. `cmd/node/`, `cmd/system/`), or create a new subpackage if it doesn't fit an existing group.
2. Define a `cobra.Command` with `Use`, `Short`, and `RunE`.
3. Register the command via the subpackage's `Register(root)` function, which is called from `cmd/root.go`. Do **not** use `init()` — subpackages export a `Register` function instead.
4. Use the shared DB helpers from `cmd/internal/cmdutil` for a database handle.
5. Honor the `--format` flag (`text`, `json`, `markdown`) where output is shown.
6. Add `--agent-help` content via the `agenthelp` package so the command is discoverable to agents.

### Adding a New `<ctx:*>` Command
1. Add a case to `executeCommand()` in `internal/hook/executor.go`.
2. Implement the handler in the same file.
3. Add parser tests in `internal/hook/parser_test.go`.
4. The parser handles all `<ctx:*>` tags generically — only execution needs custom code.

### Adding a New Query Predicate
1. Add the key to `validKeys` in `internal/query/parser.go`.
2. Add the SQL translation in `internal/query/executor.go`.
3. Add parser tests and fuzz corpus entries.

### Database Migrations
Append a new entry to the `migrations` slice in `internal/db/db.go` with the next version number. Mirror the change in `postgres.go` if the schema element exists there. Migrations run automatically on open.

### Access Logging
Retrieval surfaces (`show`, `query`, `search`, `list`, `compose`, `related`, `trace`, `session-start`, recall execution) call `LogAccess` / `LogAccessBatch`. These helpers gate inserts on `kind='memory'`, so doc/content nodes are silently skipped — call sites do not need to filter. Writes do not log. Logging failures are swallowed and never propagate. See `docs/access-logging.md`.

## Testing

```bash
make test              # Fast unit tests — run before committing
make test-integration  # Adds tests that exec the built binary
make test-fuzz         # Fuzz the query parser (30s default)
make test-coverage     # Coverage report (includes integration)
```

Tests that shell out to a built `ctx` binary (currently the hook lifecycle
suite and the `--agent-help` end-to-end test) live behind the `integration`
build tag so the default loop stays fast.

Tests use temporary databases via `testutil.NewTestDB()`. No cleanup needed.

## Critical Paths — Handle With Care

### Command Parser (`internal/hook/parser.go`)
The core of the memory loop. Regex patterns and code-block detection are subtle — commands inside fenced blocks (```) and inline code (`` ` ``) must be ignored. Always run the full parser test suite after changes.

### Hook I/O Contract
Hooks read JSON from stdin and write JSON to stdout. `session-start` outputs `{"hookSpecificOutput": {"hookEventName": "SessionStart", "additionalContext": "..."}}`. `stop` outputs `{}`. The `Stop` hook reads the agent's response from the JSONL transcript file (the `--response` flag bypasses this for testing). Changing the I/O format breaks Claude Code integration.

### `ctx doc` Isolation
`ctx doc` nodes (kind=document, kind=content) are invisible to memory queries. Specifically:
- `ctx recall`, `search`, `query`, `list`, `status`: filter to `kind='memory'`.
- `ctx hook session-start`: composes only `kind='memory'` nodes matching the tier query.
- `ctx hook stop` / `<ctx:remember>`: only creates `kind='memory'` nodes.

Promotion from doc → memory requires `--into-memory` as a safety gate. See `docs/doc-subsystem.md`.

### Pending State
`recall` and `status` commands store results in the `pending` table for injection on the next `prompt-submit` hook. `expand` stores node IDs for injection on the next `session-start`.

### Token Budgets
The composer skips nodes that would exceed the budget rather than truncating. A node too large for remaining budget is dropped entirely.

### Superseded Nodes
Nodes with `superseded_by` set are excluded from default queries. The `SUPERSEDES` edge preserves history.

## Environment

- **Go 1.24+** required.
- **No CGO** — pure-Go SQLite (`modernc.org/sqlite`) and PostgreSQL (`jackc/pgx/v5`) drivers.
- **Database location:** `~/.ctx/store.db` (override with `CTX_DB` env var or `--db` flag).
- **Agent identity:** `$CTX_AGENT` scopes per-agent access logs.
- **Hook config (Claude Code):** `~/.claude/settings.json` under `hooks.SessionStart`, `hooks.UserPromptSubmit`, `hooks.Stop`. Plugin-managed by default.

## Design Documents

The original specification lives in `docs/design/`:
- `ctx-specification.md` — Authoritative technical spec.
- `ctx-details.md` — 20 detailed Q&As on implementation decisions.
- `ctx-testing.md` — Testing strategy with copy-paste test code.
- `ctx-implementation-prompt.md` — The 8-phase build roadmap (completed).

Plus subsystem-specific docs:
- `docs/access-logging.md`
- `docs/doc-subsystem.md`

These remain the best reference for "why was it built this way?" The source code is the source of truth for "how does it work now?"
