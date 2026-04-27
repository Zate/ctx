# ctx: Implementation Details & Decisions

This document addresses specific implementation questions that aren't covered in the main specification. When in doubt during implementation, refer here.

---

## 1. Hook State Management

### Pending State Location

Pending state (recall results, status output, expand requests) is stored in the database, not in files.

**Add a `pending` table:**

```sql
CREATE TABLE pending (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL
);
```

Keys:
- `recall_results` — JSON array of nodes from last `<ctx:recall>`
- `status_output` — Text output from last `<ctx:status/>`
- `expand_nodes` — JSON array of node IDs to expand into context

**Lifecycle:**
1. `ctx hook stop` processes `<ctx:recall>`, stores results in `pending`
2. `ctx hook prompt-submit` reads `pending`, includes in `additionalContext`
3. `ctx hook prompt-submit` clears the pending entry after use

```go
// After injecting pending recall results
db.DeletePending("recall_results")
```

This ensures results are injected exactly once.

---

## 2. Hook Input/Output Schemas

### SessionStart Input

```json
{
  "session_id": "abc123-def456",
  "cwd": "/path/to/project",
  "hook_event_name": "SessionStart",
  "source": "startup",
  "transcript_path": "/Users/x/.claude/projects/.../session.jsonl"
}
```

Fields:
- `session_id`: Unique session identifier
- `cwd`: Current working directory
- `hook_event_name`: Always "SessionStart"
- `source`: One of "startup", "resume", "clear", "compact"
- `transcript_path`: Path to conversation JSONL file

### SessionStart Output

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "<!-- ctx: 15 nodes, 23000 tokens -->\n\n## Facts\n..."
  }
}
```

### UserPromptSubmit Input

```json
{
  "session_id": "abc123-def456",
  "cwd": "/path/to/project",
  "hook_event_name": "UserPromptSubmit",
  "prompt": "The user's message text"
}
```

### UserPromptSubmit Output

```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "## Recall Results\n\n[Previous query results...]"
  }
}
```

If no pending content, return empty or minimal JSON:

```json
{}
```

### Stop Input

```json
{
  "session_id": "abc123-def456",
  "cwd": "/path/to/project",
  "hook_event_name": "Stop",
  "transcript_path": "/Users/x/.claude/projects/.../session.jsonl"
}
```

**Note:** The Stop hook does NOT receive Claude's response directly. You must read the transcript file.

### Stop Output

Return empty JSON on success:

```json
{}
```

Or with error (logged but doesn't block):

```json
{
  "systemMessage": "ctx: failed to parse 1 command"
}
```

---

## 3. Extracting Claude's Response from Transcript

The transcript file is JSONL (JSON Lines). Each line is a separate JSON object representing a conversation event.

**Transcript entry types:**

```json
{"type": "user", "message": {"content": "user's message"}}
{"type": "assistant", "message": {"content": "claude's response"}}
{"type": "tool_use", "tool": "Bash", "input": {...}}
{"type": "tool_result", "output": "..."}
```

**To get Claude's last response:**

```go
func GetLastAssistantResponse(transcriptPath string) (string, error) {
    file, err := os.Open(transcriptPath)
    if err != nil {
        return "", err
    }
    defer file.Close()
    
    var lastResponse string
    scanner := bufio.NewScanner(file)
    
    for scanner.Scan() {
        var entry map[string]interface{}
        if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
            continue
        }
        
        if entry["type"] == "assistant" {
            if msg, ok := entry["message"].(map[string]interface{}); ok {
                if content, ok := msg["content"].(string); ok {
                    lastResponse = content
                }
            }
        }
    }
    
    return lastResponse, scanner.Err()
}
```

**Alternative: --response flag**

For testing and simpler integration, also support a `--response` flag:

```bash
ctx hook stop --response "Claude's response text here"
```

If `--response` is provided, use it directly. Otherwise, read from transcript.

---

## 4. Default View

**Auto-created on first database access:**

```sql
INSERT INTO views (name, query, budget, created_at, updated_at)
VALUES (
    'default',
    'tag:tier:pinned OR tag:tier:reference OR tag:tier:working',
    50000,
    datetime('now'),
    datetime('now')
);
```

**Default budget: 50,000 tokens**

This can be overridden:
- `CTX_DEFAULT_BUDGET` environment variable
- `--budget` flag on compose commands
- Editing the view: `ctx view update default --budget=80000`

---

## 5. Error Handling Strategy

### Hooks Should Not Fail Loudly

Hooks run in Claude Code's critical path. Failures should:
- Log to stderr (visible in debug mode)
- Return valid JSON (empty `{}` if nothing to add)
- Never return non-zero exit code unless absolutely critical

**Error hierarchy:**

| Severity | Action | Example |
|----------|--------|---------|
| Warning | Log to stderr, continue | 1 of 3 commands failed to parse |
| Error | Log to stderr, return empty | Database locked |
| Critical | Log to stderr, exit 1 | Database corrupted |

**Logging:**

```go
// Log to stderr, not stdout (stdout is for hook output)
fmt.Fprintf(os.Stderr, "ctx warning: %v\n", err)
```

### CLI Commands Can Fail Normally

Non-hook commands (`ctx add`, `ctx query`, etc.) should:
- Return non-zero exit code on error
- Print error message to stderr
- Print nothing to stdout on error

---

## 6. Task State Tracking

### Current Task Storage

Add to the `pending` table:

```
key: "current_task"
value: "implement-auth"
```

### Task Lifecycle

**On `<ctx:task name="X" action="start"/>`:**

1. Store current task: `pending["current_task"] = "X"`
2. Tag all existing `tier:working` nodes with `task:X`

**On new `<ctx:remember>` with `tier:working`:**

If `pending["current_task"]` exists, automatically add `task:{current_task}` tag.

**On `<ctx:task name="X" action="end"/>`:**

1. Query all nodes with `tag:task:X AND tag:tier:working`
2. If `summarize="true"`:
   - These nodes should be summarized (but we can't generate the summary)
   - Mark them as needing summarization or just archive them
3. Retag from `tier:working` to `tier:off-context`
4. Clear `pending["current_task"]`

**If no explicit task:**

Working memory operates without task isolation. Nodes get `tier:working` but no `task:X` tag.

---

## 7. Supersede Mechanics

### Implementation

Add a `superseded_by` field to the nodes table:

```sql
ALTER TABLE nodes ADD COLUMN superseded_by TEXT REFERENCES nodes(id);
```

**On `<ctx:supersede old="X" new="Y"/>`:**

```sql
UPDATE nodes SET superseded_by = 'Y' WHERE id = 'X';
```

Also create an edge:

```go
db.CreateEdge(Y, X, "SUPERSEDES")
```

### Query Behavior

Default queries automatically exclude superseded nodes:

```sql
SELECT * FROM nodes WHERE superseded_by IS NULL AND ...
```

To include superseded nodes:

```bash
ctx query "type:fact" --include-superseded
```

---

## 8. Expand Behavior

### On `<ctx:expand node="X"/>`

1. Find node X (must be type `summary`)
2. Find all nodes that X was `DERIVED_FROM`
3. Store their IDs in `pending["expand_nodes"]`

### On next `ctx hook session-start` or `ctx hook prompt-submit`

1. Read `pending["expand_nodes"]`
2. Include those nodes in context (temporarily override their tier)
3. Clear `pending["expand_nodes"]`

**Expansion is one-shot.** After the next context composition, the expansion is forgotten. If Claude needs them again, issue another `<ctx:expand/>`.

---

## 9. Concurrent Access

### SQLite Configuration

Enable WAL mode for concurrent read access:

```go
func Open(path string) (*DB, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }
    
    // Enable WAL mode
    _, err = db.Exec("PRAGMA journal_mode=WAL")
    if err != nil {
        return nil, err
    }
    
    // Set busy timeout (5 seconds)
    _, err = db.Exec("PRAGMA busy_timeout=5000")
    if err != nil {
        return nil, err
    }
    
    return &DB{db: db}, nil
}
```

### Concurrent Writes

SQLite with WAL allows:
- Multiple concurrent readers
- One writer at a time (others wait)

With busy_timeout, writes will retry for 5 seconds before failing. This should be sufficient for hook operations.

**Do not** keep long-running transactions. Each hook call should:
1. Open database
2. Do work (single transaction if multiple operations)
3. Close database

---

## 10. Specific Defaults

| Setting | Default | Override |
|---------|---------|----------|
| Database path | `~/.ctx/store.db` | `--db` flag, `CTX_DB` env |
| Token budget | 50,000 | `--budget` flag, `CTX_DEFAULT_BUDGET` env |
| Default tier for new nodes | None (untagged) | `--tag` flag |
| Default query (compose) | `tag:tier:pinned OR tag:tier:reference OR tag:tier:working` | `--query` flag |
| Token estimation | `len(content) / 4` | Not configurable |
| Transcript read timeout | 5 seconds | `CTX_TRANSCRIPT_TIMEOUT` env |

### Tier Assignment

New nodes have NO tier by default. Tiers must be explicitly assigned:

```bash
ctx add --type=fact --tag=tier:reference "Important fact"
```

Or via Claude's commands:

```xml
<ctx:remember type="fact" tags="tier:reference">...</ctx:remember>
```

If Claude omits tier in tags, the node is created without a tier and won't appear in default context composition (which filters on tiers).

**Recommendation for skill file:** Instruct Claude to always include a tier tag.

---

## 11. Content Parsing Details

### Whitespace Handling

- Trim leading/trailing whitespace from parsed content
- Preserve internal whitespace (newlines, indentation)

```go
content = strings.TrimSpace(content)
```

### Empty Content

Empty content is valid for some commands:

```xml
<ctx:status/>           <!-- No content, valid -->
<ctx:recall query="x"/> <!-- No content, valid -->
<ctx:remember type="fact"></ctx:remember>  <!-- Empty content, INVALID -->
```

For `<ctx:remember>` and `<ctx:summarize>`, empty content should be rejected:

```go
if strings.TrimSpace(content) == "" {
    // Log warning, skip this command
    continue
}
```

### Maximum Content Length

No hard limit, but log a warning for very large content:

```go
const MaxRecommendedContentLength = 50000 // ~12,500 tokens

if len(content) > MaxRecommendedContentLength {
    fmt.Fprintf(os.Stderr, "ctx warning: large content (%d bytes) in remember command\n", len(content))
}
```

---

## 12. Skill Configuration

### Recommended Frontmatter

```yaml
---
name: ctx
description: >
  Persistent memory system for managing context across conversations.
  Use when you need to remember information, recall past knowledge,
  manage task context, or when the user references past discussions.
---
```

**Do not use:**
- `disable-model-invocation: true` — Claude should auto-invoke this skill
- `user-invocable: false` — Users should be able to trigger `/ctx` manually if needed
- `allowed-tools` — Not needed, ctx commands are in response text, not tool calls

### Skill Detection

Claude Code uses the `description` field to determine when to auto-invoke the skill. The description should mention key trigger phrases:
- "remember"
- "recall"
- "past knowledge"
- "past discussions"
- "what do you remember"
- "context"
- "memory"

---

## 13. Context Output Format

### Markdown Format (for hook injection)

```markdown
<!-- ctx: {node_count} nodes, {token_count} tokens, rendered at {timestamp} -->

## Pinned

- [{type}:{short_id}] {content_preview}
  - Tags: {tags}

## Reference

### Facts

- [fact:{short_id}] {content}

### Decisions

- [decision:{short_id}] {content}
  - Rationale: {from metadata if present}
  - Depends on: [{dependency_ids}]

### Patterns

- [pattern:{short_id}] {content}

## Working Context

- [{type}:{short_id}] {content}

<!-- ctx:end -->
```

**Short ID:** First 8 characters of the ULID (enough to identify, not overwhelming).

**Content preview:** For long content, show first 100 characters + "..."

### JSON Format (for scripting)

```json
{
  "meta": {
    "node_count": 23,
    "token_count": 42000,
    "rendered_at": "2024-01-15T10:30:00Z"
  },
  "nodes": [
    {
      "id": "01HQ1A2B3C4D5E6F",
      "type": "fact",
      "content": "...",
      "tags": ["tier:reference", "project:ctx"],
      "token_estimate": 150,
      "created_at": "2024-01-14T08:00:00Z",
      "edges": {
        "depends_on": ["01HQ..."],
        "derived_from": []
      }
    }
  ]
}
```

---

## 14. ULID Generation

Use `github.com/oklog/ulid/v2`:

```go
import (
    "crypto/rand"
    "time"
    
    "github.com/oklog/ulid/v2"
)

func NewID() string {
    return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}
```

ULIDs are:
- Lexicographically sortable (newer IDs sort after older)
- URL-safe
- 26 characters (vs 36 for UUID)
- Contain timestamp (first 10 chars)

---

## 15. Database Migrations

### Version Tracking

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
```

### Migration Strategy

On database open:
1. Check current version
2. Apply any unapplied migrations in order
3. Update version

```go
var migrations = []struct {
    version int
    sql     string
}{
    {1, initialSchema},
    {2, `ALTER TABLE nodes ADD COLUMN superseded_by TEXT`},
    {3, `CREATE TABLE pending (...)`},
}

func (db *DB) migrate() error {
    currentVersion := db.getSchemaVersion()
    
    for _, m := range migrations {
        if m.version > currentVersion {
            if _, err := db.Exec(m.sql); err != nil {
                return fmt.Errorf("migration %d failed: %w", m.version, err)
            }
            db.setSchemaVersion(m.version)
        }
    }
    
    return nil
}
```

---

## 16. Code Block Detection

To avoid parsing `<ctx:*>` commands inside code blocks:

```go
func removeCodeBlocks(text string) string {
    // Remove fenced code blocks
    fencedPattern := regexp.MustCompile("(?s)```.*?```")
    text = fencedPattern.ReplaceAllString(text, "")
    
    // Remove indented code blocks (4+ spaces or tab at line start)
    // This is trickier, may want to skip for v1
    
    // Remove inline code
    inlinePattern := regexp.MustCompile("`[^`]+`")
    text = inlinePattern.ReplaceAllString(text, "")
    
    return text
}

func ParseCtxCommands(response string) []CtxCommand {
    // First, remove code blocks
    cleanedResponse := removeCodeBlocks(response)
    
    // Then parse commands from cleaned response
    return parseCommands(cleanedResponse)
}
```

**Important:** Only remove code blocks for the purpose of command detection. The actual content extraction should use the original response text.

Actually, better approach — detect if a command's position falls within a code block:

```go
func isInsideCodeBlock(text string, position int) bool {
    // Count ``` occurrences before position
    before := text[:position]
    fenceCount := strings.Count(before, "```")
    
    // Odd count means we're inside a fenced block
    return fenceCount%2 == 1
}
```

---

## 17. Recall Results Format

When `<ctx:recall query="..."/>` results are stored and then injected:

```markdown
## Recall Results

Query: `type:fact AND tag:auth`

Found 3 nodes:

- [fact:01HQ1234] The API uses OAuth 2.0 with PKCE for public clients.
  - Tags: tier:reference, project:auth
  
- [fact:01HQ5678] Refresh tokens are stored server-side only.
  - Tags: tier:reference, project:auth
  
- [fact:01HQ9ABC] Rate limiting uses token bucket algorithm.
  - Tags: tier:reference, project:auth

---
```

If no results:

```markdown
## Recall Results

Query: `type:fact AND tag:auth`

No matching nodes found.

---
```

---

## 18. Install Command Behavior

`ctx install` should:

1. Create `~/.ctx/` directory if not exists
2. Initialize database with schema
3. Create default view
4. Create `~/.claude/skills/ctx/` directory if not exists
5. Copy SKILL.md to that directory
6. Print instructions for hook configuration

**Output:**

```
ctx installed successfully!

Database: ~/.ctx/store.db
Skill: ~/.claude/skills/ctx/SKILL.md

To enable hooks, add to ~/.claude/settings.json:

{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "ctx hook session-start"}]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "ctx hook prompt-submit"}]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "ctx hook stop"}]
      }
    ]
  }
}

Then restart Claude Code to load the new configuration.
```

---

## 19. Testing Against Real Claude Code

### Manual Integration Test Checklist

After implementation, test with actual Claude Code:

1. [ ] Run `ctx install`
2. [ ] Add hooks to `~/.claude/settings.json`
3. [ ] Restart Claude Code
4. [ ] Start new conversation
5. [ ] Check: Does context get injected? (May be empty initially)
6. [ ] Ask Claude to remember something
7. [ ] Check: Does Claude include `<ctx:remember>` in response?
8. [ ] Check: Does `ctx list` show the new node?
9. [ ] Start new conversation
10. [ ] Check: Does the remembered fact appear in context?
11. [ ] Ask Claude to recall something specific
12. [ ] Check: Do recall results appear in next prompt?
13. [ ] Test summarize workflow
14. [ ] Test task start/end workflow

### Debugging Hooks

If hooks aren't working:

```bash
# Check if ctx is in PATH
which ctx

# Test hook manually
echo '{"session_id":"test","hook_event_name":"SessionStart"}' | ctx hook session-start

# Check Claude Code logs
# (Location varies by OS)
tail -f ~/.claude/logs/claude-code.log
```

---

## 20. Future Considerations (Out of Scope for v1)

These are noted but NOT implemented in v1:

- **Semantic search:** Embedding nodes for similarity search
- **Auto-summarization:** Calling an LLM to generate summaries
- **Conflict detection:** Identifying contradictions between nodes
- **Node expiration:** Auto-archiving old observations
- **Multi-user:** Separate databases per user
- **Sync:** Cloud backup/sync of database
- **Encryption:** Encrypting sensitive content at rest
