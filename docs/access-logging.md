# Access Logging

`ctx` records every retrieval of a memory node so you can answer "is this node
actually being used?" The data lives in the `access_log` table and is queried
via `ctx accessed`.

## What gets logged

| Surface                                    | Access type      | `query_context`              |
| ------------------------------------------ | ---------------- | ---------------------------- |
| `ctx hook session-start` (composed nodes)  | `hook_inject`    | `session-start`              |
| `ctx show <id>`                            | `get`            | `show:<arg>`                 |
| `ctx query <expr>`                         | `explicit_query` | `query:<expr>`               |
| `ctx search <term>`                        | `explicit_query` | `search:<term>`              |
| `ctx list`                                 | `explicit_query` | `list`                       |
| `ctx compose ...`                          | `explicit_query` | `compose:<query\|seed\|ids>` |
| `<ctx:recall>` (executed in prompt-submit) | `explicit_query` | `recall:<query>`             |
| `ctx related`, `ctx trace`                 | `graph_walk`     | `related:<arg>` / `trace:<arg>` |

Writes (`add`, `remember`, `update`, `link`, `tag`) do **not** log access.

## Isolation rules

- **Memory-only.** `LogAccess` and `LogAccessBatch` are gated at the DB layer
  by a `kind='memory'` subquery. Doc and content nodes never produce log rows.
  `QueryAccess` enforces the same filter on read, so even a raw-inserted row
  for a non-memory node will not appear in `ctx accessed`.
- **Agent partitioning.** Every entry records the agent at write time
  (`cmd.agent` or `$CTX_AGENT`). `ctx accessed` defaults to that agent;
  `--all-agents` opts out.
- **Failures are silent.** Logging errors are swallowed — they never propagate
  to the caller and never affect compose output.

## `ctx accessed`

```text
ctx accessed [--node ID] [--type T] [--since RFC3339] [--limit N]
             [--all-agents] [--format json]

--node         prefix match on node_id (resolves short prefixes)
--type         hook_inject | explicit_query | get | graph_walk
--since        RFC3339 timestamp; entries with accessed_at >= since
--limit        defaults to 50
--all-agents   ignore the agent filter
--format json  emit JSON array of AccessEntry
```

Examples:

```bash
ctx accessed --since 2026-04-20 --type hook_inject
ctx accessed --node 01HF
ctx accessed --all-agents --limit 100 --format json
```

## Schema

Migration v6 adds the `access_log` table:

```
access_log(
  id            INTEGER PRIMARY KEY,
  node_id       TEXT  NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  accessed_at   TEXT  NOT NULL,   -- RFC3339, UTC
  agent         TEXT  NOT NULL,   -- empty string when no agent set
  access_type   TEXT  NOT NULL,
  query_context TEXT  NOT NULL
);
```

The migration runs idempotently and transactionally on both the SQLite and
PostgreSQL backends. FK cascade ensures access entries are removed when their
node is deleted.

## Out of scope

- Retention or auto-pruning policies — the table grows monotonically.
- Hook-output stale-node reports.
- Query predicates beyond the four flags above.
