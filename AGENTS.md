# AGENTS.md

Guidance for AI agents working on this codebase. If you're Claude and this was injected into your context, this is for you.

## Quick Orientation

This is `ctx`, a Go CLI that provides persistent memory for Claude Code agents. The module path is `github.com/zate/ctx`. It stores knowledge as nodes in a SQLite graph database and integrates with Claude Code via hooks.

**If you're using ctx right now** (i.e., `<ctx:remember>` commands work in your responses), then this project built the tool you're using. Be careful with changes — you're editing your own memory system.

## Repository Layout

| Path | What It Does |
|------|-------------|
| `cmd/*.go` | CLI commands — one file per command. All register in `init()` via `rootCmd.AddCommand()` |
| `cmd/hook/*.go` | Hook subcommands: `session-start`, `prompt-submit`, `stop` |
| `cmd/install.go` | Installer — binary, database, skill, hooks, CLAUDE.md. Most complex single file. |
| `internal/db/db.go` | Database init, migrations, schema. Read this first for schema understanding. |
| `internal/db/nodes.go` | Node CRUD. `CreateNode`, `GetNode`, `UpdateNode`, `DeleteNode`, `ListNodes`, `Search` |
| `internal/db/edges.go` | Edge CRUD. `CreateEdge`, `GetEdgesFrom`, `GetEdgesTo`, `DeleteEdge` |
| `internal/db/tags.go` | Tag operations. `AddTag`, `RemoveTag`, `GetTags`, `ListAllTags` |
| `internal/db/pending.go` | Key-value store for inter-hook state. `SetPending`, `GetPending`, `DeletePending` |
| `internal/hook/parser.go` | Parses `<ctx:*>` XML commands from text. Ignores code blocks. |
| `internal/hook/executor.go` | Executes parsed commands against the database. |
| `internal/query/parser.go` | Query language parser — tokenizer, AST, recursive descent. |
| `internal/query/executor.go` | Converts query AST to SQL and executes against the database. |
| `internal/token/estimator.go` | Token count estimation (chars/4 heuristic). |
| `internal/view/composer.go` | Selects, sorts, budgets, and renders nodes for context injection. |
| `testutil/testutil.go` | Shared test helpers (temp database creation). |

## Conventions

### Code Style
- Standard Go conventions. `gofmt` formatted.
- Error handling: return errors, don't panic. Hooks fail gracefully (print to stderr, output `{}` to stdout).
- IDs are ULIDs (time-sortable, globally unique).
- Database operations use transactions where multiple writes are needed.
- Table-driven tests with `testify` assertions.

### Adding a New CLI Command
1. Create `cmd/<command>.go`
2. Define a `cobra.Command` with `Use`, `Short`, and `RunE`
3. Register it in `init()` with `rootCmd.AddCommand()`
4. Use `openDB()` to get a database handle
5. Use the `format` flag for output format switching (`text`, `json`, `markdown`)

### Adding a New `<ctx:*>` Command
1. Add a case to `executeCommand()` in `internal/hook/executor.go`
2. Implement the handler function in the same file
3. Add parser tests in `internal/hook/parser_test.go`
4. The parser handles all XML-like `<ctx:*>` tags automatically — you only need to handle execution

### Adding a New Query Predicate
1. Add the key to `validKeys` in `internal/query/parser.go`
2. Add the SQL translation in `internal/query/executor.go`
3. Add parser tests and fuzz corpus entries

### Database Migrations
Add a new entry to the `migrations` slice in `internal/db/db.go` with the next version number. Migrations run automatically on database open.

## Testing

```bash
make test          # All tests — run this before committing
make test-unit     # Just internal/ packages
make test-fuzz     # Fuzz the query parser (30s default)
```

Tests use temporary databases created via `testutil.NewTestDB()`. No cleanup needed — they use temp directories.

Test files with coverage:
- `internal/db/*_test.go` — Node, edge, tag, FTS operations
- `internal/hook/parser_test.go` — Command parsing including code block exclusion
- `internal/query/parser_test.go` — Query language parsing
- `internal/query/parser_fuzz_test.go` — Fuzz testing
- `cmd/install_test.go` — Installer tests

## Critical Paths — Handle With Care

### The Command Parser (`internal/hook/parser.go`)
This is the core of the memory loop. If it breaks, agents can't store knowledge. The regex patterns and code-block detection logic are subtle. Always run the full parser test suite after changes.

### The Hook I/O Contract
Hooks read JSON from stdin and write JSON to stdout. The `session-start` hook outputs `{"hookSpecificOutput": {"hookEventName": "SessionStart", "additionalContext": "..."}}`. The `stop` hook outputs `{}`. Changing this format breaks the Claude Code integration.

### The Installer (`cmd/install.go`)
Modifies user files (`~/.claude/settings.json`, `~/.claude/CLAUDE.md`). The merge logic for hooks is careful to preserve existing entries. Test with `--dry-run` first.

## Design Documents

The original specification lives in `docs/design/`:
- `docs/design/ctx-specification.md` — Authoritative technical spec
- `docs/design/ctx-details.md` — 20 detailed Q&As on implementation decisions
- `docs/design/ctx-testing.md` — Testing strategy with copy-paste test code
- `docs/design/ctx-implementation-prompt.md` — The 8-phase build roadmap (completed)

These remain the best reference for "why was it built this way?" questions. For "how does it work now?", read the source.

## Environment

- **Go 1.24+** required
- **No CGO** — pure Go SQLite driver (`modernc.org/sqlite`)
- **Database location:** `~/.ctx/store.db` (override with `CTX_DB` env var or `--db` flag)
- **Skill file:** `~/.claude/skills/ctx/SKILL.md`
- **Hook config:** `~/.claude/settings.json` under `hooks.SessionStart`, `hooks.UserPromptSubmit`, `hooks.Stop`
