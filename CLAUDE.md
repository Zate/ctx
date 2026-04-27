# CLAUDE.md

Claude-specific addendum. For codebase layout, conventions, schema, testing, and critical paths, read [AGENTS.md](AGENTS.md) — it's the canonical contributor guide for any agent.

## You're Probably Using This Right Now

This is `ctx`, the persistent memory system for Claude Code. If a session-start hook injected stored knowledge above (look for the `<!-- ctx:end -->` block), then you're already running on top of the tool you're now editing. **Be careful with changes to the parser, the hook I/O contract, and the schema** — breaking those breaks your own memory loop.

## Memory vs. `ctx doc`

There are two subsystems in this repo and they are deliberately isolated:

- **Memory** (`kind='memory'` nodes): what `<ctx:remember>`, `<ctx:recall>`, `ctx search`, `ctx list`, `ctx status`, and the session-start composer all operate on. This is what gives you persistent knowledge across sessions.
- **`ctx doc`** (`kind='document'` and `kind='content'` nodes): an opt-in subsystem for decomposing/recomposing markdown documents. **Invisible to memory queries by default.** Only reach for it when the user explicitly asks to decompose, edit sections of, or recompose a markdown file.

Doc nodes will not appear in memory commands, will not be composed into your context on session-start, and `<ctx:remember>` will never create them. Promotion from doc → memory requires `--into-memory` as a safety gate.

## Using `ctx` In Your Responses

When working in any project that has ctx active, follow the skill: `Skill: using-ctx` for the full reference, or `ctx --agent-help` for a token-minimal command index. Key rules:

- Tag every node with a `tier:` (pinned / reference / working / off-context) and a `project:NAME`.
- `<ctx:remember>` commands inside fenced code blocks or inline code are ignored — safe for examples.
- Verify before recommending. A memory that names a function/flag/file is a claim it existed *when written*; it may have been renamed or removed.

## When You're Editing This Repo

Run `make test` before committing. The parser test suite (`internal/hook/parser_test.go`) and the executor tests are the most load-bearing — they protect the memory loop you depend on. The hook I/O contract (`session-start` outputs `{"hookSpecificOutput": ...}`, `stop` outputs `{}`) must not change without coordinated changes to the Claude Code plugin.

If you're touching the schema, mirror SQLite changes (`internal/db/db.go`) into PostgreSQL (`internal/db/postgres.go`).

For everything else — repo layout, code style, how to add commands/predicates/migrations, testing, environment — see [AGENTS.md](AGENTS.md).
