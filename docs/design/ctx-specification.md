# ctx: Persistent Context Management System

## Overview

Build a CLI tool called `ctx` that provides persistent, structured memory for Claude. This system allows Claude to actively manage its own context rather than passively receiving whatever fits in the window.

The tool stores knowledge as a graph (nodes + edges) in SQLite, supports querying and composition, and integrates with Claude Code via hooks and a skill file.

## Goals

1. **Persistence**: Knowledge survives across conversations
2. **Structure**: Different types of knowledge (facts, decisions, patterns, observations) with appropriate metadata
3. **Agency**: Claude can read, write, query, summarize, and organize its own memory
4. **Provenance**: Summarization preserves links to source material
5. **Budget-aware**: Context composition respects token limits
6. **Self-contained**: Single binary, single SQLite file, no external dependencies

## Technical Decisions

- **Language**: Go
- **Storage**: SQLite (embedded, via modernc.org/sqlite for pure Go)
- **CLI framework**: Cobra
- **Token estimation**: Character count / 4 (simple, no dependencies)
- **IDs**: ULID (sortable, readable)

---

## Data Model

### Nodes Table

```sql
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    content TEXT NOT NULL,
    summary TEXT,
    token_estimate INTEGER NOT NULL,
    superseded_by TEXT REFERENCES nodes(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE INDEX idx_nodes_type ON nodes(type);
CREATE INDEX idx_nodes_created ON nodes(created_at);
CREATE INDEX idx_nodes_superseded ON nodes(superseded_by);
```

Node types:
- `fact` — Stable knowledge ("user prefers Go")
- `decision` — Choices made with rationale
- `pattern` — Recurring approaches or structures
- `observation` — Temporary/situational notes
- `hypothesis` — Unvalidated ideas
- `task` — Task context container
- `summary` — Compressed representation of other nodes
- `source` — Ingested external content (files, tool output)
- `open-question` — Unresolved questions

### Edges Table

```sql
CREATE TABLE edges (
    id TEXT PRIMARY KEY,
    from_id TEXT NOT NULL,
    to_id TEXT NOT NULL,
    type TEXT NOT NULL,
    created_at TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',
    FOREIGN KEY (from_id) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (to_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX idx_edges_from ON edges(from_id);
CREATE INDEX idx_edges_to ON edges(to_id);
CREATE INDEX idx_edges_type ON edges(type);
CREATE UNIQUE INDEX idx_edges_unique ON edges(from_id, to_id, type);
```

Edge types:
- `DERIVED_FROM` — Summary was created from these sources
- `DEPENDS_ON` — This conclusion relies on this premise
- `SUPERSEDES` — This replaces/updates that
- `RELATES_TO` — Weaker association
- `CHILD_OF` — Hierarchical containment (e.g., task contains observations)

### Tags Table

```sql
CREATE TABLE tags (
    node_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (node_id, tag),
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX idx_tags_tag ON tags(tag);
```

Tags use `namespace:value` format: `task:auth-implementation`, `tier:reference`, `project:viberent`

### Views Table

```sql
CREATE TABLE views (
    name TEXT PRIMARY KEY,
    query TEXT NOT NULL,
    budget INTEGER DEFAULT 50000,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

### Pending Table

Stores transient state between hook calls (recall results, expand requests, etc.):

```sql
CREATE TABLE pending (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL
);
```

Keys used:
- `recall_results` — JSON array of nodes from last `<ctx:recall>`
- `status_output` — Text output from last `<ctx:status/>`
- `expand_nodes` — JSON array of node IDs to expand into context
- `current_task` — Name of the currently active task

### Schema Version Table

```sql
CREATE TABLE schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
```

### Full-Text Search

```sql
CREATE VIRTUAL TABLE nodes_fts USING fts5(
    content,
    content='nodes',
    content_rowid='rowid'
);

-- Triggers to keep FTS in sync
CREATE TRIGGER nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO nodes_fts(rowid, content) VALUES (NEW.rowid, NEW.content);
END;

CREATE TRIGGER nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, content) VALUES('delete', OLD.rowid, OLD.content);
END;

CREATE TRIGGER nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, content) VALUES('delete', OLD.rowid, OLD.content);
    INSERT INTO nodes_fts(rowid, content) VALUES (NEW.rowid, NEW.content);
END;
```

---

## CLI Commands

### Global Flags

```
--db <path>      Database file (default: ~/.ctx/store.db)
--format <fmt>   Output format: text, json, markdown (default: text)
```

### Node Operations

```bash
# Add a node
ctx add --type=<type> [--tag=<tag>]... [--meta=<key>=<value>]... "<content>"
ctx add --type=fact --tag=project:viberent "Database uses SQLite with WAL mode"

# Read from stdin
echo "content" | ctx add --type=observation --stdin

# Show a node
ctx show <id>
ctx show <id> --with-edges

# Update a node
ctx update <id> --content="new content"
ctx update <id> --type=decision
ctx update <id> --meta=confidence=high

# Delete a node
ctx delete <id>
ctx delete <id> --cascade  # Also delete nodes that DERIVED_FROM this

# List nodes
ctx list
ctx list --type=fact
ctx list --tag=task:current
ctx list --since=1h
ctx list --limit=20
```

### Edge Operations

```bash
# Link nodes
ctx link <from-id> <to-id> --type=DEPENDS_ON
ctx link <from-id> <to-id> --type=DERIVED_FROM

# Unlink
ctx unlink <from-id> <to-id> [--type=<type>]

# Show connections
ctx edges <id>
ctx edges <id> --direction=in
ctx edges <id> --direction=out
```

### Tag Operations

```bash
# Add tag
ctx tag <id> <tag>
ctx tag <id> task:current tier:working

# Remove tag
ctx untag <id> <tag>

# List tags
ctx tags
ctx tags --prefix=task:
```

### Query Operations

```bash
# Search by content (full-text)
ctx search "auth pattern"

# Query with filters
ctx query "type:fact AND tag:project:viberent"
ctx query "type:decision"
ctx query "created:>24h"  # Created in last 24 hours

# Trace provenance
ctx trace <id>  # Show what this was derived from (recursive)
ctx trace <id> --reverse  # Show what depends on this

# Find related
ctx related <id> --depth=2
```

### Summarization

```bash
# Create a summary node from multiple nodes
ctx summarize <id1> <id2> <id3> --content="Summary text here"

# This creates:
# - New node with type=summary
# - DERIVED_FROM edges from new node to each source
# - Optionally adds tier:off-context tag to sources

ctx summarize <id1> <id2> --content="..." --archive-sources
```

### View Operations

```bash
# Create a named view
ctx view create <name> --query="type:fact OR type:decision" --budget=40000

# List views
ctx view list

# Render a view (compose context)
ctx view render <name>
ctx view render <name> --budget=30000  # Override budget
ctx view render <name> --format=markdown

# Delete a view
ctx view delete <name>

# Quick render without saved view
ctx compose --query="tag:task:current" --budget=50000
```

### Context Status

```bash
# Show current state
ctx status

# Output:
# Database: ~/.ctx/store.db (2.3 MB)
# Nodes: 147 (estimated 89,000 tokens)
#   fact: 34
#   decision: 12
#   pattern: 8
#   observation: 45
#   summary: 18
#   source: 30
# Edges: 203
# Tags: 89 unique
# 
# Tier breakdown:
#   pinned: 5 nodes (4,200 tokens)
#   reference: 42 nodes (31,000 tokens)
#   working: 23 nodes (18,000 tokens)
#   off-context: 77 nodes (35,800 tokens)
```

### Import/Export

```bash
# Export entire graph
ctx export > backup.json
ctx export --query="tag:project:viberent" > project.json

# Import
ctx import < backup.json
ctx import --merge < other.json  # Don't fail on conflicts

# Ingest a file as a source node
ctx ingest <file> [--tag=<tag>]...
ctx ingest README.md --tag=project:viberent --tag=type:documentation
```

---

## Query Language

Simple query syntax for filtering nodes:

```
type:<type>           Match node type
tag:<tag>             Match tag
created:<op><dur>     Time filter: >24h, <1w, >2024-01-01
updated:<op><dur>     Time filter on update
tokens:<op><num>      Token count filter: <1000, >500
has:summary           Has non-null summary field
has:edges             Has any edges
from:<id>             Has edge from this node
to:<id>               Has edge to this node

AND, OR, NOT          Boolean operators
( )                   Grouping
```

Examples:
```
type:fact AND tag:project:viberent
(type:decision OR type:pattern) AND created:>7d
tag:tier:working AND NOT has:summary
type:observation AND tokens:<500
```

---

## View Rendering

When composing context from a view:

1. Execute query to get matching nodes
2. Sort by priority (pinned first, then by tier, then by recency)
3. Include nodes until budget is reached
4. Format output

Output format for context injection:

```markdown
<!-- ctx: 23 nodes, 42,000 tokens, rendered at 2024-01-15T10:30:00Z -->

## Facts

- [fact:01HQ...] Database uses SQLite with WAL mode
- [fact:01HQ...] User prefers explicit error handling

## Decisions

- [decision:01HQ...] Chose Go for single-binary requirement
  - Rationale: No runtime dependencies, easy distribution
  - Depends on: [fact:01HQ...]

## Patterns

...

## Working Context

...

<!-- ctx:end -->
```

---

## Claude Code Integration

### Directory Structure

```
~/.ctx/
└── store.db              # SQLite database

~/.claude/
├── settings.json         # Hook configuration
└── skills/
    └── ctx/
        └── SKILL.md      # Tells Claude how to use ctx

<project>/.claude/
└── settings.json         # Project-specific hook configuration (optional)
```

### Hook Configuration Location

Hooks are configured in `~/.claude/settings.json` (user-level) or `.claude/settings.json` (project-level), NOT as separate shell scripts. The hooks call the `ctx` binary directly.

### Skill File: ~/.claude/skills/ctx/SKILL.md

```markdown
---
name: ctx
description: Persistent memory system for managing context across conversations. Use when you need to remember information, recall past knowledge, manage task context, or when the user references past discussions or asks about what you remember.
---

# ctx: Your Persistent Memory System

You have access to a persistent memory system called `ctx`. This allows you to:

- Store knowledge that persists across conversations
- Query your own history and accumulated knowledge
- Organize information by type, tags, and relationships
- Summarize and compress while preserving provenance
- Manage your context budget actively

## How Memory Works

Your memory is stored as a graph of nodes (pieces of knowledge) and edges (relationships between them).

### Node Types

Use these types to categorize knowledge:

| Type | Use For | Persistence |
|------|---------|-------------|
| `fact` | Stable knowledge about user, project, domain | Long-term |
| `decision` | Choices made with rationale | Long-term |
| `pattern` | Recurring approaches or structures | Long-term |
| `observation` | Situational notes, current task context | Session |
| `hypothesis` | Unvalidated ideas to explore | Until validated |
| `open-question` | Unresolved questions | Until resolved |

### Tiers

Tag nodes with tiers to control context composition:

- `tier:pinned` — Always included, never evicted
- `tier:reference` — Stable background knowledge
- `tier:working` — Current task context
- `tier:off-context` — Stored but not loaded by default

### Relationships

Connect nodes with edges:

- `DERIVED_FROM` — This summary came from these sources
- `DEPENDS_ON` — This conclusion relies on this premise
- `SUPERSEDES` — This replaces/corrects that
- `RELATES_TO` — General association

## Commands

Issue commands using XML tags. These will be processed after your response.

### Remembering

```xml
<ctx:remember type="fact" tags="project:viberent,tier:reference">
SQLite database uses WAL mode for concurrent read access.
</ctx:remember>
```

### Recalling

```xml
<ctx:recall query="type:decision AND tag:project:viberent"/>
```

Results will be available in your next context.

### Summarizing

```xml
<ctx:summarize nodes="01HQ1234,01HQ5678" archive="true">
The auth implementation uses OIDC with custom claims for tenant isolation.
Key decision: refresh tokens stored server-side only.
</ctx:summarize>
```

### Linking

```xml
<ctx:link from="01HQ1234" to="01HQ5678" type="DEPENDS_ON"/>
```

### Status

```xml
<ctx:status/>
```

Returns your current memory state in the next response.

### Task Boundaries

```xml
<ctx:task name="implement-oauth" action="start"/>
```

```xml
<ctx:task name="implement-oauth" action="end" summarize="true"/>
```

Ending a task archives its working context and promotes key decisions to reference tier.

### Expanding Summaries

```xml
<ctx:expand node="01HQ1234"/>
```

Brings source nodes of a summary back into context.

### Superseding

```xml
<ctx:supersede old="01HQ1234" new="01HQ5678"/>
```

Marks old node as superseded; it will be excluded from default views.

## Best Practices

1. **Crystallize decisions**: When you and the user make a decision, remember it as type `decision` with rationale.

2. **Use tasks**: Wrap focused work in task boundaries. This keeps working memory clean.

3. **Summarize proactively**: When a thread of work concludes, summarize it. Keep conclusions, archive details.

4. **Tag consistently**: Use `project:<name>` for project-specific knowledge. Use `tier:<level>` to control what's loaded.

5. **Note open questions**: When something is unresolved, remember it as `open-question`. Revisit later.

6. **Check status**: Periodically check `<ctx:status/>` to understand your memory state.

7. **Expand when needed**: If a summary lacks detail you need, expand it to see sources.

## What's In Your Context Now

The context injected before this conversation was composed from:
- All `tier:pinned` nodes
- All `tier:reference` nodes
- All `tier:working` nodes tagged with the current task
- Recent observations

If something you expected isn't here, it may be `tier:off-context`. Use `<ctx:recall>` to find it.
```

### Hook Configuration: ~/.claude/settings.json

The hooks call the `ctx` binary and can inject context or parse Claude's output.

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "ctx hook session-start"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "ctx hook prompt-submit"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "ctx hook stop"
          }
        ]
      }
    ]
  }
}
```

### Hook Subcommands

The `ctx` binary includes hook subcommands that handle Claude Code integration:

```bash
# Called on SessionStart - outputs JSON with additionalContext
ctx hook session-start

# Called on UserPromptSubmit - can inject context based on pending recalls
ctx hook prompt-submit

# Called on Stop - parses Claude's response for <ctx:*> commands
ctx hook stop
```

#### ctx hook session-start

Reads stdin (JSON with session_id, cwd, etc.), composes context from the default view, and outputs:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "<!-- ctx: 23 nodes, 42000 tokens -->\n\n## Facts\n..."
  }
}
```

#### ctx hook prompt-submit

Checks for pending recall results or status requests, injects them into context:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "## Recall Results\n\n[Results from previous <ctx:recall> command]"
  }
}
```

#### ctx hook stop

Reads stdin (JSON with full session context), extracts Claude's last response, parses `<ctx:*>` commands, and executes them against the database.

Supported commands:
- `<ctx:remember type="..." tags="...">content</ctx:remember>` → `ctx add`
- `<ctx:recall query="..."/>` → `ctx query`, stores results for next prompt-submit
- `<ctx:summarize nodes="..." archive="true">content</ctx:summarize>` → `ctx summarize`
- `<ctx:link from="..." to="..." type="..."/>` → `ctx link`
- `<ctx:status/>` → `ctx status`, stores for next prompt-submit
- `<ctx:task name="..." action="start|end"/>` → task management
- `<ctx:expand node="..."/>` → brings source nodes into context

Returns empty JSON on success (no output needed for Stop hook).

---

## Implementation Plan

### Phase 1: Core CLI (Priority)

1. **Project setup**
   - Initialize Go module
   - Set up Cobra CLI structure
   - Embed SQLite (modernc.org/sqlite)
   - Database initialization and migrations

2. **Node operations**
   - `ctx add`
   - `ctx show`
   - `ctx list`
   - `ctx update`
   - `ctx delete`

3. **Edge operations**
   - `ctx link`
   - `ctx unlink`
   - `ctx edges`

4. **Tag operations**
   - `ctx tag`
   - `ctx untag`
   - `ctx tags`

5. **Basic query**
   - `ctx search` (full-text)
   - `ctx query` (structured)

### Phase 2: Composition

6. **View operations**
   - `ctx view create`
   - `ctx view render`
   - `ctx compose`

7. **Summarization**
   - `ctx summarize`
   - Provenance tracking

8. **Provenance**
   - `ctx trace`
   - `ctx related`

### Phase 3: Integration

9. **Import/Export**
   - `ctx export`
   - `ctx import`
   - `ctx ingest`

10. **Status**
    - `ctx status`

11. **Hooks**
    - Pre-prompt script
    - Post-response script
    - Command parsing

12. **Skill file**
    - SKILL.md documentation
    - Example workflows

### Phase 4: Polish

13. **Testing**
    - Unit tests for query parsing
    - Integration tests for CLI
    - Hook integration tests

14. **Documentation**
    - README
    - Man pages / help text
    - Examples

---

## File Structure

```
ctx/
├── cmd/
│   ├── root.go
│   ├── add.go
│   ├── show.go
│   ├── list.go
│   ├── update.go
│   ├── delete.go
│   ├── link.go
│   ├── unlink.go
│   ├── edges.go
│   ├── tag.go
│   ├── untag.go
│   ├── tags.go
│   ├── search.go
│   ├── query.go
│   ├── trace.go
│   ├── related.go
│   ├── view.go
│   ├── compose.go
│   ├── summarize.go
│   ├── status.go
│   ├── export.go
│   ├── import.go
│   ├── ingest.go
│   ├── install.go
│   └── hook/
│       ├── hook.go           # Parent command for hook subcommands
│       ├── session_start.go  # ctx hook session-start
│       ├── prompt_submit.go  # ctx hook prompt-submit
│       └── stop.go           # ctx hook stop
├── internal/
│   ├── db/
│   │   ├── db.go           # Database connection, migrations
│   │   ├── nodes.go        # Node CRUD
│   │   ├── edges.go        # Edge CRUD
│   │   └── tags.go         # Tag CRUD
│   ├── query/
│   │   ├── parser.go       # Query language parser
│   │   └── executor.go     # Query execution
│   ├── view/
│   │   ├── composer.go     # View composition logic
│   │   └── renderer.go     # Output formatting
│   ├── hook/
│   │   ├── parser.go       # Parse <ctx:*> commands from Claude's response
│   │   └── executor.go     # Execute parsed commands
│   └── token/
│       └── estimator.go    # Token estimation
├── install/
│   ├── skill/
│   │   └── SKILL.md        # Skill file to copy to ~/.claude/skills/ctx/
│   └── settings.example.json  # Example hook configuration
├── main.go
├── go.mod
└── go.sum
```

---

## Example Session

```bash
# Initialize (happens automatically on first use)
ctx status
# Database: ~/.ctx/store.db (new)
# Nodes: 0

# Add some knowledge
ctx add --type=fact --tag=tier:reference "User (Zate) prefers Go with Echo for APIs"
# Added: 01HQ1A2B3C

ctx add --type=fact --tag=tier:reference "Single-binary deployments are preferred"
# Added: 01HQ1A2B3D

ctx add --type=decision --tag=project:ctx --tag=tier:reference \
  "Using SQLite with pure-Go driver (modernc.org/sqlite) for zero CGO dependencies"
# Added: 01HQ1A2B3E

# Link decision to supporting fact
ctx link 01HQ1A2B3E 01HQ1A2B3D --type=DEPENDS_ON

# Start a task
ctx add --type=observation --tag=tier:working --tag=task:implement-query \
  "Working on query parser. Need to handle AND/OR/NOT with proper precedence."
# Added: 01HQ1A2B3F

# Check status
ctx status
# Nodes: 4 (estimated 850 tokens)
#   fact: 2
#   decision: 1
#   observation: 1

# Compose context
ctx compose --query="tag:tier:reference OR tag:tier:working" --budget=10000
# <!-- ctx: 4 nodes, 850 tokens -->
# ## Facts
# - [01HQ1A2B3C] User (Zate) prefers Go with Echo for APIs
# - [01HQ1A2B3D] Single-binary deployments are preferred
# ## Decisions  
# - [01HQ1A2B3E] Using SQLite with pure-Go driver...
#   - Depends on: [01HQ1A2B3D]
# ## Working Context
# - [01HQ1A2B3F] Working on query parser...
# <!-- ctx:end -->

# Later, summarize the task
ctx summarize 01HQ1A2B3F --content="Query parser complete. Supports AND/OR/NOT with parentheses." --archive-sources
# Created summary: 01HQ1A2B3G
# Archived: 01HQ1A2B3F
```

---

## Notes for Implementation

1. **ULID generation**: Use `github.com/oklog/ulid/v2`

2. **Database location**: Default to `~/.ctx/store.db`, create directory if needed, respect `--db` flag and `CTX_DB` environment variable

3. **Token estimation**: `len(content) / 4` is good enough for now

4. **Query parser**: Start simple. Can use a basic recursive descent parser or even regex for v1. Don't over-engineer.

5. **Output formats**: Support `--format=text` (human readable), `--format=json` (machine readable), `--format=markdown` (context injection)

6. **Error handling**: Return non-zero exit codes on error. Errors to stderr, output to stdout.

7. **Idempotency**: `ctx tag` on an already-tagged node should succeed silently. `ctx link` on existing edge should succeed silently.

8. **Cascading deletes**: SQLite foreign keys with `ON DELETE CASCADE` handle edge cleanup when nodes are deleted.

9. **Transactions**: Wrap multi-step operations (like summarize which creates node + edges + tags sources) in transactions.

10. **Hook robustness**: Hooks should fail gracefully. If ctx isn't installed or db doesn't exist, don't break Claude Code.
