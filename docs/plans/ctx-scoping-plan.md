# ctx Scoping Plan: Repo-Local + Global Memories

## Goal

Most memories should be repo-specific by default, but some should be global (cross-repo). The system should auto-detect repo context and handle both seamlessly.

## Current State

- Single global database: `~/.ctx/store.db`
- `--db` flag or `CTX_DB` env can override
- No automatic repo detection

## Proposed Design

### Database Locations

```
~/.ctx/store.db                      <- global database (always exists)
<repo-root>/.ctx/store.db            <- repo-local database (created on first use)
```

### Scope Tags

New tag namespace: `scope:`

| Tag | Meaning |
|-----|---------|
| `scope:local` | Repo-specific only (default, implicit) |
| `scope:global` | Stored in global DB, loaded everywhere |

### Hook Behavior Changes

**session-start:**
1. Open repo-local DB (if in repo)
2. Open global DB
3. Compose from repo-local (all tiers)
4. Merge in global nodes tagged `scope:global`
5. Render combined context

**stop (remember command):**
1. If `scope:global` tag present -> write to global DB
2. Otherwise -> write to repo-local DB (or global if not in repo)

**CLI commands:**
- Add `--global` flag to force global DB operations
- Add `--local` flag to force repo-local operations
- Default: auto-detect based on cwd

### gitignore Recommendation

```
.ctx/
```

## Implementation Phases

### Phase 1: Repo Detection
1. Add `internal/repo/detect.go` with `FindGitRoot()` function
2. Update `cmd/root.go` to auto-resolve DB path based on repo context
3. Add `--global` and `--local` flags to override

### Phase 2: Dual Database Support
4. Update hooks to open both databases when in a repo
5. session-start: merge global `scope:global` nodes into context
6. stop: route writes based on `scope:` tag

### Phase 3: CLI Enhancements
7. `ctx list --global` / `ctx list --local`
8. `ctx promote <id>` - copy node from local to global with `scope:global`
9. `ctx status` shows both databases when in repo

### Phase 4: Skill & Output Updates
10. Update session-start output to explain scoping
11. Update skill file with scope tag documentation

## Example Workflows

### Repo-Specific Decision (default)
```xml
<ctx:remember type="decision" tags="tier:reference">
This repo uses sqlc for database access.
</ctx:remember>
```
Stored in `<repo>/.ctx/store.db`, only visible in this repo.

### Global Preference
```xml
<ctx:remember type="fact" tags="tier:pinned,scope:global">
User prefers Go for backend, TypeScript for frontend.
</ctx:remember>
```
Stored in `~/.ctx/store.db`, visible in all repos.

### Promote Local to Global
```bash
ctx promote 01HQXXXX
```

## devloop Integration

When devloop completes a task:
1. Decision made -> store with tier:reference (repo-local by default)
2. Cross-repo pattern discovered -> store with scope:global
3. On `/devloop:ship` -> review working context, archive or promote

Potential hook into devloop plan completion:
- Inject reminder: "This session made N decisions. Consider storing key ones."
- Auto-suggest ctx:remember for plan outcomes

## Success Criteria

1. New repo -> empty local + global context loaded
2. In repo -> sees repo-local + global memories
3. `scope:global` -> persists across repos
4. Default -> repo-local only
5. `ctx status` shows both databases

## Estimated Effort

- Phase 1: 2 hours
- Phase 2: 3 hours
- Phase 3: 2 hours
- Phase 4: 1 hour

Total: ~8 hours
