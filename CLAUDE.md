# CLAUDE.md

This is `ctx` — a persistent memory system for Claude Code agents. It's built, it works, and you're probably already using it (check if the session-start hook injected knowledge above).

## What This Is

A Go CLI tool that gives you persistent, structured memory across conversations. You store knowledge by writing `<ctx:*>` XML commands in your responses. Hooks parse those commands after you respond and persist them to a SQLite graph database. On the next session, your stored knowledge is automatically injected into context.

**The project is functional.** The core loop works:
1. Session starts → `ctx hook session-start` composes and injects stored knowledge
2. You work and include `<ctx:remember>`, `<ctx:recall>`, etc. in your responses
3. Session ends → `ctx hook stop` parses your commands and updates the database
4. Next session starts → your knowledge is there

## Project Structure

```
cmd/                       CLI commands (Cobra)
  hook/                    Hook subcommands (session-start, prompt-submit, stop)
  install.go               Automated installer (binary, db, skill, hooks, CLAUDE.md)
  add.go, show.go, ...     Node/edge/tag/view management commands
internal/
  db/                      SQLite layer — nodes, edges, tags, pending, migrations
  hook/                    <ctx:*> command parser + executor
  query/                   Query language parser (AST) + executor
  token/                   Token count estimation
  view/                    Context composition, budget management, rendering
testutil/                  Shared test helpers
```

## Technical Stack

- **Go 1.24** with modules
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGO)
- **Cobra** for CLI
- **ULID** (`oklog/ulid/v2`) for time-sortable IDs
- **testify** for assertions

Database lives at `~/.ctx/store.db`. WAL mode, foreign keys enabled.

## Schema

Six tables: `nodes`, `edges`, `tags`, `views`, `pending`, `schema_version`. Plus `nodes_fts` (FTS5 virtual table) for full-text search. Migrations are version-tracked. See `internal/db/db.go` for the full schema.

**Node types:** `fact`, `decision`, `pattern`, `observation`, `hypothesis`, `task`, `summary`, `source`, `open-question`

**Tier tags** control what gets composed into context: `tier:pinned` (always loaded), `tier:reference` (on-demand via recall), `tier:working` (current task), `tier:off-context` (archived).

## Key Subsystems

### Command Parser (`internal/hook/parser.go`)
Parses `<ctx:*>` XML commands from agent responses. Correctly ignores commands inside fenced code blocks and inline code. Handles self-closing tags, multi-line content, and attribute parsing. This is the most critical piece — if parsing breaks, the memory loop breaks.

### Command Executor (`internal/hook/executor.go`)
Executes parsed commands against the database. Handles: `remember`, `recall`, `summarize`, `link`, `status`, `task`, `expand`, `supersede`. Each has specific validation rules.

### Query Language (`internal/query/parser.go`)
Custom query parser supporting predicates (`type:fact`, `tag:project:X`), boolean operators (`AND`, `OR`, `NOT`), grouping with parentheses, and comparison operators (`created:>2025-01-01`, `tokens:<1000`). Has fuzz tests.

### Context Composer (`internal/view/composer.go`)
Selects nodes matching a query, sorts by tier priority then recency, applies a token budget, and renders as markdown for injection. The default view query is `tag:tier:pinned OR tag:tier:working` with a 50,000-token budget. Reference nodes are not auto-loaded but their availability is reported in the session-start output.

### Installer (`cmd/install.go`)
`ctx install` is deprecated in favor of the plugin-based installation. `ctx init` handles database creation only. The plugin (`cc-plugins/plugins/ctx/`) handles binary auto-download, hook registration, and skill injection.

### ctx doc (`cmd/doc.go`, `internal/doc/`)
An opt-in subsystem for decomposing, editing, and recomposing markdown documents. Completely separate from the memory subsystem.

**Critical isolation rule:** `ctx doc` nodes are invisible to memory queries by default. Specifically:
- `ctx recall`, `ctx search`, `ctx query`, `ctx list`, `ctx status`: filter to `kind=memory` nodes; content and document nodes are excluded.
- `ctx hook session-start`: composes only `kind=memory` nodes matching the tier query; doc nodes never appear.
- `ctx hook stop` / `<ctx:remember>`: only creates `kind=memory` nodes.

**When to use:** Only when the user explicitly asks to decompose, edit sections of, or recompose a markdown document. Do not reach for `ctx doc` during ordinary memory operations.

**How it works:**
1. `ctx doc import <file>` — decomposes the file at heading boundaries into a `kind=document` node + `kind=content` nodes linked by CONTAINS edges. Byte-identity is verified immediately (rolls back on failure).
2. Edit structure via `ctx doc scaffold` (emit XML) + `ctx doc apply` (apply diff) or individual commands (`mv`, `insert`, `remove`, `split`, `fork`).
3. `ctx doc export <id>` — recomposes and emits the original bytes.
4. `ctx doc promote <node-id> --into-memory --type <type>` — selectively promotes a content node to a memory node (requires `--into-memory` safety gate).
5. `ctx doc inline <doc-id> --memory <memory-id>` — injects a memory node's body into a document's composed output without changing its kind.

**Agent-help:** All `ctx doc *` subcommands are hidden from the tier-1 `ctx --agent-help` index (opt-in posture). Access via `ctx --agent-help doc <subcommand>`.

See `docs/doc-subsystem.md` for full command reference, scaffold XML format, corpus fixture layout, and byte-identity contract details.

## Working on This Project

### Running Tests
```bash
make test          # All tests
make test-unit     # internal/ only
make test-fuzz     # Fuzz the query parser
make test-coverage # Generate coverage report
```

### Building
```bash
make build         # Produces ./ctx binary
make install       # Build + full installation
```

### Design Documents
The `ctx-*.md` files in the repo root are the original spec and design docs:
- `ctx-specification.md` — Full technical spec
- `ctx-implementation-prompt.md` — 8-phase implementation roadmap
- `ctx-details.md` — Edge cases, implementation decisions (20 detailed Q&As)
- `ctx-testing.md` — Testing strategy with example code
- `ctx-skill-SKILL.md` — The skill file content

These were the build instructions. The implementation followed them closely. They remain useful as reference for understanding design decisions, but the source code is now the source of truth.

## Things to Watch Out For

1. **Code block handling in parser:** Commands in fenced blocks (```) or inline code (`` ` ``) must be ignored. The parser handles this, but changes to regex patterns need careful testing — see `internal/hook/parser_test.go`.

2. **Transcript reading in Stop hook:** The Stop hook reads Claude's response from the JSONL transcript file. The `--response` flag bypasses this for testing. The transcript format may change between Claude Code versions.

3. **Pending state:** `recall` and `status` commands store results in the `pending` table for injection on the next prompt-submit hook. `expand` stores node IDs the same way for injection on next session-start.

4. **Token budgets:** The composer skips nodes that would exceed the budget rather than truncating them. A node that's too large for the remaining budget is skipped entirely.

5. **Superseded nodes:** Nodes marked with `superseded_by` are excluded from default queries. The `SUPERSEDES` edge preserves the history chain.

## What's Next

Potential improvements (not started):
- Richer transcript parsing (Claude Code may structure content blocks differently)
- Auto-summarization when working context grows large
- Token budget tuning based on actual model context windows
- Export/import for backup and sharing
- Better error reporting back to the agent via hook output
