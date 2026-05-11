# Claude Code Implementation Prompt: ctx

## What You're Building

A CLI tool called `ctx` that serves as a persistent memory system for Claude. This is a tool I (Claude) will use to manage my own context across conversations.

**Reference Documents (read all before starting):**
- `ctx-specification.md` — Full technical specification (data model, commands, hooks)
- `ctx-testing.md` — Comprehensive testing strategy (read alongside implementation)
- `ctx-details.md` — Implementation decisions, edge cases, and specific behaviors
- `ctx-skill-SKILL.md` — The skill file that teaches Claude how to use ctx

**When in doubt:**
1. Check `ctx-details.md` first — it answers specific "how should this work?" questions
2. Check `ctx-specification.md` for the data model and command syntax
3. Check `ctx-testing.md` for expected behaviors (tests are specifications)

## Critical: Test As You Build

**Do not defer testing.** Each phase must include its tests before moving to the next phase. The testing document (`ctx-testing.md`) contains:
- Unit tests for each component
- Integration tests for CLI commands  
- E2E tests for complete workflows
- Test utilities and helpers
- Golden file testing patterns

Write tests alongside implementation. A phase is not complete until its tests pass.

## Key Constraints

- **Language**: Go
- **Single binary**: No external runtime dependencies
- **SQLite**: Use `modernc.org/sqlite` (pure Go, no CGO)
- **CLI**: Use Cobra
- **IDs**: Use ULIDs (`github.com/oklog/ulid/v2`)

## Implementation Order

Build in this order, testing each phase before moving to the next:

### Phase 1: Foundation

1. Initialize Go module as `github.com/zate/ctx` (or appropriate path)
2. Set up Cobra CLI skeleton with root command
3. Implement database layer:
   - Connection management (open, close, path resolution)
   - Schema creation (nodes, edges, tags tables + FTS)
   - Migration support (version tracking for future schema changes)

4. Implement node CRUD in `internal/db/nodes.go`:
   - Create node (with ULID generation, token estimation)
   - Get node by ID
   - List nodes (with filters)
   - Update node
   - Delete node

5. Implement CLI commands:
   - `ctx add`
   - `ctx show`
   - `ctx list`
   - `ctx update`
   - `ctx delete`

6. Set up test infrastructure:
   - Create `testutil/testutil.go` with helpers
   - Create `testdata/` directory structure

**Tests for Phase 1** (see `ctx-testing.md` Layer 1):
- `internal/db/db_test.go` — database connection, schema creation
- `internal/db/nodes_test.go` — all node CRUD operations
- `cmd/add_test.go`, `cmd/show_test.go`, etc. — CLI command tests

**Phase 1 is complete when:**
- All unit tests pass for node operations
- Can run: `ctx add --type=fact "test"` → `ctx list` → see the node
- Can run: `ctx show <id>` → see node details
- Can run: `ctx delete <id>` → node is gone

### Phase 2: Relationships

6. Implement edge CRUD in `internal/db/edges.go`:
   - Create edge
   - Get edges for node (in/out/both)
   - Delete edge

7. Implement tag CRUD in `internal/db/tags.go`:
   - Add tag to node
   - Remove tag from node
   - List all tags
   - Get nodes by tag

8. Implement CLI commands:
   - `ctx link`
   - `ctx unlink`
   - `ctx edges`
   - `ctx tag`
   - `ctx untag`
   - `ctx tags`

**Tests for Phase 2** (see `ctx-testing.md` Layer 1):
- `internal/db/edges_test.go` — edge CRUD, cascade delete, idempotency
- `internal/db/tags_test.go` — tag operations, prefix queries

**Phase 2 is complete when:**
- All edge and tag unit tests pass
- Can run: `ctx link <id1> <id2> --type=DEPENDS_ON`
- Can run: `ctx tag <id> project:test tier:reference`
- Can run: `ctx edges <id>` → see connections

### Phase 3: Query

9. Implement query parser in `internal/query/`:
   - Tokenizer for query language
   - Parser for expressions (type:X, tag:Y, AND, OR, NOT, parentheses)
   - SQL generator from parsed query

10. Implement full-text search using FTS5

11. Implement CLI commands:
    - `ctx search` (full-text)
    - `ctx query` (structured)
    - `ctx trace` (provenance traversal)
    - `ctx related` (graph traversal)

**Tests for Phase 3** (see `ctx-testing.md` Layer 2):
- `internal/query/parser_test.go` — all query syntax variations
- `internal/query/parser_fuzz_test.go` — fuzz testing for robustness
- `internal/query/executor_test.go` — query execution against real data
- `internal/db/fts_test.go` — full-text search

**Phase 3 is complete when:**
- Parser handles all documented query syntax
- Fuzz tests run without panics
- Complex queries like `type:fact AND (tag:a OR tag:b) AND NOT tag:archived` work
- `ctx search "keyword"` finds matching content

### Phase 4: Composition

12. Implement view storage and rendering in `internal/view/`:
    - Store view definitions
    - Compose nodes from query
    - Apply token budget (sort by priority, include until budget exhausted)
    - Format output (text, json, markdown)

13. Implement CLI commands:
    - `ctx view create`
    - `ctx view list`
    - `ctx view render`
    - `ctx view delete`
    - `ctx compose` (ad-hoc view)

**Tests for Phase 4** (see `ctx-testing.md` Layer 4):
- `internal/view/composer_test.go` — budget enforcement, priority sorting
- Test edge cases: zero budget, single node exceeds budget, empty results

**Phase 4 is complete when:**
- `ctx compose --query="tag:tier:reference" --budget=10000` outputs formatted context
- Priority sorting works (pinned > reference > working > recency)
- Budget is respected (output never exceeds specified tokens)

### Phase 5: Summarization

14. Implement summarize operation:
    - Create summary node
    - Create DERIVED_FROM edges to source nodes
    - Optionally tag sources as tier:off-context

15. Implement CLI command:
    - `ctx summarize`

**Tests for Phase 5**:
- `cmd/summarize_test.go` — creates summary, edges, archives sources
- Verify provenance chain with `ctx trace`

**Phase 5 is complete when:**
- `ctx summarize <id1> <id2> --content="..." --archive-sources` works
- New summary node has DERIVED_FROM edges to sources
- Sources are tagged tier:off-context when --archive-sources is used

### Phase 6: Utilities

16. Implement remaining commands:
    - `ctx status` (database stats, tier breakdown)
    - `ctx export` (dump to JSON)
    - `ctx import` (load from JSON)
    - `ctx ingest` (parse file into source node)

**Tests for Phase 6**:
- `cmd/status_test.go` — correct counts and token estimates
- `cmd/export_test.go`, `cmd/import_test.go` — round-trip preserves all data
- `cmd/ingest_test.go` — file content becomes source node

**Phase 6 is complete when:**
- Full round-trip export/import preserves all nodes, edges, and tags
- `ctx status` shows accurate tier breakdown

### Phase 7: Integration Files

17. Implement `<ctx:*>` command parser in `internal/hook/`:
    - Parse Claude's response for ctx commands
    - Extract attributes and content
    - Handle edge cases (code blocks, malformed tags)

18. Create hook subcommands in `cmd/hook/`:
    - `ctx hook session-start` — outputs additionalContext JSON for SessionStart
    - `ctx hook prompt-submit` — injects pending recall/status results
    - `ctx hook stop` — parses Claude's response for `<ctx:*>` commands

19. Create install artifacts:
    - Skill file: `~/.claude/skills/ctx/SKILL.md`
    - Example settings.json hook configuration
    - README with setup instructions

20. Create an `install` command that:
    - Builds the binary
    - Creates ~/.ctx directory for database
    - Copies skill file to ~/.claude/skills/ctx/
    - Prints instructions for adding hooks to settings.json

**Tests for Phase 7** (see `ctx-testing.md` Layers 3, 5, 6):
- `internal/hook/parser_test.go` — comprehensive command parsing tests
- `internal/hook/parser_golden_test.go` — golden file tests for realistic responses
- `cmd/hook/hook_test.go` — integration tests for all hook subcommands

**Critical test cases for hook parser:**
- Simple remember command
- Multi-line content in remember
- Self-closing commands (recall, status, link)
- Multiple commands in one response
- Commands inside code blocks (should be IGNORED)
- Commands inside inline code (should be IGNORED)
- Malformed/unclosed tags (should be ignored gracefully)
- Unicode and special characters in content

**Phase 7 is complete when:**
- All hook parser tests pass including golden files
- `ctx hook session-start` outputs valid JSON with context
- `ctx hook stop --response="<ctx:remember...>"` creates nodes
- Commands in code blocks are correctly ignored

### Phase 8: End-to-End Testing

21. Create E2E test scenarios in `e2e/`:
    - Basic memory cycle (session start → remember → session start again)
    - Summarization with provenance
    - Task lifecycle (start → work → end)
    - Expand summary to see sources
    - Recall query results injection

**Tests for Phase 8** (see `ctx-testing.md` Layer 7):
- `e2e/scenarios_test.go` — complete workflow tests

**Phase 8 is complete when:**
- All E2E scenarios pass
- Can simulate a multi-turn conversation with ctx commands
- Provenance chains are correctly maintained

## What Success Looks Like

When complete, I should be able to:

1. Run `ctx add --type=fact "Some knowledge"` and have it persist
2. Run `ctx query "type:fact"` and see my stored facts
3. Run `ctx compose --budget=50000` and get formatted context
4. Have hooks automatically inject/process my context commands
5. Use `<ctx:remember>` tags in my responses and have them processed
6. Use `<ctx:recall>` and get results in my next context
7. Summarize nodes and later expand to see sources

**The critical integration path:**
```
ctx hook session-start  →  injects context into Claude Code
         ↓
Claude includes <ctx:*> commands in response
         ↓
ctx hook stop  →  parses and executes commands
         ↓
Next session-start  →  includes new knowledge
```

This loop working end-to-end is the foundation. Everything else is refinement.

## Code Style

- Use Go idioms (accept interfaces, return structs)
- Errors should wrap with context: `fmt.Errorf("failed to create node: %w", err)`
- Use structured logging (zerolog or slog) if logging is needed
- Keep packages focused: db handles storage, query handles parsing, view handles composition
- Write table-driven tests

## Database Details

The database file location resolution order:
1. `--db` flag
2. `CTX_DB` environment variable
3. `~/.ctx/store.db` (default)

Create the directory if it doesn't exist.

## Token Estimation

Simple formula: `tokens = len(content) / 4`

This is intentionally naive. Accuracy isn't critical; consistency is.

## Output Formats

All list/query/compose commands should support `--format`:
- `text`: Human-readable, default
- `json`: Machine-readable, for scripting
- `markdown`: For context injection

## Query Language Grammar

```
query     = expr
expr      = term (('AND' | 'OR') term)*
term      = 'NOT'? factor
factor    = '(' expr ')' | predicate
predicate = key ':' value | key ':' op value

key       = 'type' | 'tag' | 'created' | 'updated' | 'tokens' | 'has' | 'from' | 'to'
op        = '>' | '<' | '>=' | '<='
value     = string | duration | number
duration  = number ('h' | 'd' | 'w')
```

Start simple. It's okay if v1 doesn't support all of this - get the basics working first.

## What Success Looks Like

When complete, I should be able to:

1. Run `ctx add --type=fact "Some knowledge"` and have it persist
2. Run `ctx query "type:fact"` and see my stored facts
3. Run `ctx compose --budget=50000` and get formatted context
4. Have hooks automatically inject/process my context commands
5. Use `<ctx:remember>` tags in my responses and have them processed

## Questions to Resolve During Implementation

If you encounter ambiguity, make a reasonable choice and document it. Specifically:

- **Conflict handling in import**: Overwrite, skip, or fail?
- **Query syntax edge cases**: How to handle special characters in values?
- **Markdown parsing in ingest**: How deep to parse? Just split on headings? Full AST?

Make the simple choice first. We can refine later.

## Start Here

Begin with Phase 1. Create the Go module, set up Cobra, implement the database layer, then the basic CRUD commands. Get `ctx add` and `ctx list` working before anything else.
