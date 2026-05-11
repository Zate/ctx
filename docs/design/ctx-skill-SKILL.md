# ctx: Persistent Memory System

This file should be installed to `~/.claude/skills/ctx/SKILL.md`

```markdown
---
name: ctx
description: Persistent memory system for managing context across conversations. Use when you need to remember information, recall past knowledge, manage task context, or when the user references past discussions or asks about what you remember.
---

# ctx: Your Persistent Memory System

You have access to a persistent memory system. This allows you to store, organize, query, and manage knowledge across conversations.

## Why This Exists

Your context window is limited and ephemeral. Without this system, knowledge accumulates until it overflows, then gets truncated or summarized in ways you don't control. You lose work. You re-discover things. You can't distinguish "we never discussed X" from "we discussed X but it's gone."

This system gives you agency over your memory.

## Core Concepts

### Nodes

A node is a piece of knowledge. Every node has:
- **ID**: A unique identifier (ULID)
- **Type**: What kind of knowledge this is
- **Content**: The actual information
- **Tags**: Labels for organization and querying
- **Metadata**: Additional structured data

### Node Types

| Type | Purpose | Example |
|------|---------|---------|
| `fact` | Stable knowledge | "User prefers Go for backend services" |
| `decision` | A choice with rationale | "Chose SQLite for single-binary requirement" |
| `pattern` | Recurring approach | "This codebase uses explicit errors over panics" |
| `observation` | Current/temporary context | "The auth bug seems related to token refresh" |
| `hypothesis` | Unvalidated idea | "Maybe the race condition is in cache invalidation" |
| `open-question` | Unresolved question | "How should federation handoff work?" |
| `summary` | Compressed knowledge | Derived from multiple source nodes |
| `source` | External content | Ingested file or document |

### Tiers

Tiers control what gets loaded into your context:

| Tier | Auto-Loaded? | Use For | Examples |
|------|-------------|---------|----------|
| `tier:pinned` | Yes | Critical facts, foundational decisions, active conventions | "Always test code", "Uses Three.js + vanilla TS" |
| `tier:reference` | No (use recall) | Durable knowledge, past decisions, resolved issues | "Chose PostgreSQL for multi-tenant" |
| `tier:working` | Yes | Current task context, debugging, scratch | "Token refresh fails on expired tokens" |
| `tier:off-context` | No | Archived, rarely needed | Completed task logs, old debugging |

**Key question:** Will I need this EVERY session? → `pinned`. Might I need it someday? → `reference`. Only for this task? → `working`.

### Type → Tier Quick Guide

| When you hear/think... | Type | Tier |
|------------------------|------|------|
| "Please remember: always test our code" | `fact` | `pinned` |
| "We're using Three.js with vanilla TS" | `decision` | `pinned` |
| "This codebase uses InstancedMesh for geometry" | `pattern` | `pinned` |
| "We chose PostgreSQL for multi-tenant" | `decision` | `reference` |
| "The 404 was caused by missing PBR textures" (resolved) | `observation` | `reference` |
| "Debugging: token refresh fails on expired tokens" (in-progress) | `observation` | `working` |
| "Maybe the race is in cache invalidation" | `hypothesis` | `working` |
| "Splat map overhaul fully implemented" | `decision` | `working` |

### Edges

Nodes connect via typed relationships:

| Edge | Meaning |
|------|---------|
| `DERIVED_FROM` | This was created by summarizing those |
| `DEPENDS_ON` | This conclusion relies on that premise |
| `SUPERSEDES` | This replaces/corrects that |
| `RELATES_TO` | General association |

## Commands

Issue commands using XML tags in your responses. They are processed after you respond.

### Remember

Store knowledge (always include a `tier:` tag):

```xml
<!-- Pinned: critical, needed every session -->
<ctx:remember type="fact" tags="project:myproject,tier:pinned">
Always run tests before committing. User preference.
</ctx:remember>

<!-- Working: task-scoped, temporary -->
<ctx:remember type="observation" tags="project:myproject,tier:working">
Auth bug seems related to token refresh timing.
</ctx:remember>

<!-- Reference: durable but not needed every session -->
<ctx:remember type="decision" tags="project:myproject,tier:reference">
Chose PostgreSQL for multi-tenant concurrent write access.
</ctx:remember>
```

Parameters:
- `type` (required): Node type
- `tags` (optional): Comma-separated tags

### Recall

Query stored knowledge:

```xml
<ctx:recall query="type:decision AND tag:project:myproject"/>
```

Results appear in your next context. The query language supports:
- `type:<type>` — Match node type
- `tag:<tag>` — Match tag
- `created:>24h` — Time filters
- `AND`, `OR`, `NOT` — Boolean logic
- `(parentheses)` — Grouping

### Summarize

Compress multiple nodes into one:

```xml
<ctx:summarize nodes="01HQ1234,01HQ5678" archive="true">
The authentication system uses OIDC with custom claims for tenant isolation.
Refresh tokens are stored server-side only.
</ctx:summarize>
```

Parameters:
- `nodes` (required): Comma-separated node IDs to summarize
- `archive` (optional): If "true", sources get tagged `tier:off-context`

This creates a new summary node with `DERIVED_FROM` edges to the sources.

### Link

Create relationships:

```xml
<ctx:link from="01HQ1234" to="01HQ5678" type="DEPENDS_ON"/>
```

### Supersede

Mark a node as replaced:

```xml
<ctx:supersede old="01HQ1234" new="01HQ5678"/>
```

The old node remains but is excluded from default queries.

### Expand

Bring source nodes of a summary back into context:

```xml
<ctx:expand node="01HQ1234"/>
```

### Status

Check your memory state:

```xml
<ctx:status/>
```

Returns: node counts by type/tier, token estimates, what's loaded vs stored.

### Task Boundaries

Start a focused task:

```xml
<ctx:task name="implement-auth" action="start"/>
```

End and optionally summarize:

```xml
<ctx:task name="implement-auth" action="end" summarize="true"/>
```

Ending a task:
- Archives working context for that task
- Promotes key decisions to reference tier
- Cleans up scratch observations

## Patterns and Practices

### When to Remember

**Pinned (needed every session):**
- User preferences and constraints → `type=fact, tier:pinned`
- Foundational project decisions → `type=decision, tier:pinned`
- Active conventions and patterns → `type=pattern, tier:pinned`

**Working (current task only):**
- In-progress debugging context → `type=observation, tier:working`
- Unvalidated ideas → `type=hypothesis, tier:working`
- Unresolved questions → `type=open-question, tier:working`

**Reference (durable, access via recall):**
- Past decisions worth preserving → `type=decision, tier:reference`
- Resolved issues and root causes → `type=observation, tier:reference`
- Background knowledge → `type=fact, tier:reference`

**Don't remember:**
- Transient debugging output
- Routine confirmations
- Things already in reference material

### Starting a Task

At the start of a task, recall relevant reference knowledge:

```xml
<ctx:recall query="type:decision AND tag:project:myproject"/>
```

This brings in past decisions without them cluttering every session.

### Task Workflow

1. Start task: `<ctx:task name="feature-X" action="start"/>`
2. Recall relevant reference: `<ctx:recall query="tag:project:X"/>`
3. Add observations as you work with `tier:working`
4. Promote key decisions to `tier:pinned` (if needed every session) or `tier:reference` (if durable)
5. End task: `<ctx:task name="feature-X" action="end" summarize="true"/>`

### Managing Budget

If context is getting crowded:

1. Check status: `<ctx:status/>`
2. Identify what can be summarized
3. Summarize with archive: `<ctx:summarize ... archive="true"/>`
4. Demote verbose content: tag with `tier:off-context`

### Provenance

When you summarize, sources are linked. Later you can:
- Trace back: see what a summary came from
- Expand: bring sources back if you need detail
- Validate: check if sources are still accurate

## What's Loaded Now

Your current context was composed from:
- All `tier:pinned` nodes — always loaded
- All `tier:working` nodes — current task context

**Not auto-loaded (use `<ctx:recall>` to access):**
- `tier:reference` nodes — durable knowledge, available on demand
- `tier:off-context` nodes — archived, rarely needed

## Tips

1. **Be specific in types**: `decision` vs `observation` vs `fact` matters for later queries.

2. **Use project tags**: `tag:project:X` keeps things organized across multiple projects.

3. **Summarize proactively**: Don't wait for context overflow. When a thread concludes, compress it.

4. **Check your state**: Use `<ctx:status/>` to understand what you're carrying.

5. **Trust the system**: Off-context doesn't mean gone. You can always recall or expand.

6. **Note open questions**: `type:open-question` helps you track unresolved issues.

7. **Link dependencies**: If conclusion B depends on fact A, link them. If A changes, you'll know B might be stale.
```

## Installation Notes

The skill file above should be saved to `~/.claude/skills/ctx/SKILL.md`.

The `ctx install` command will handle this automatically, but you can also manually:

1. Create the directory: `mkdir -p ~/.claude/skills/ctx`
2. Copy this file (without the outer code fence) to `~/.claude/skills/ctx/SKILL.md`
3. Add hooks to your `~/.claude/settings.json` (see ctx-specification.md for hook configuration)
