# ctx doc subsystem

## 1. Overview and When to Use

`ctx doc` is an opt-in subsystem for decomposing, editing, and recomposing markdown documents inside the ctx store. It is completely separate from the memory subsystem (`ctx add`, `ctx recall`, `ctx search`, etc.).

**Use `ctx doc` when the user explicitly asks to:**
- Decompose a markdown file so its sections can be individually edited.
- Reorder, insert, remove, or split sections of a stored document.
- Export a modified document back to disk with byte-identical round-trip verification.
- Inline a memory node into a document's composed output.
- Promote a document section to a memory node for recall.

**Do not use `ctx doc` for ordinary memory operations.** Document content nodes are invisible to `ctx recall`, `ctx search`, `ctx status`, `ctx list`, `ctx tags`, and the session-start hook. Importing a document does not pollute the memory context.

---

## 2. Core Primitives

| Primitive | Command | Description |
|-----------|---------|-------------|
| Decompose | `ctx doc import <path>` | Parse markdown into a document node + content nodes + CONTAINS edges. Byte-identity verified on import. |
| Compose | `ctx doc export <doc-id>` | Recompose all content nodes in position order and emit the original bytes. |
| Inspect | `ctx doc show <doc-id>` | Print document node metadata (ID, src_hash, size, timestamps). |
| Verify | `ctx doc verify <doc-id>` | Recompose and compare sha256 against stored src_hash. Exits 0 on match. |
| Scaffold | `ctx doc scaffold <doc-id>` | Emit pure-structure XML (`<ctx:doc>`) representing the CONTAINS edge graph. No content bodies embedded. |
| Edit | `ctx doc apply <xml-file>` | Apply a scaffold XML diff to the live edge graph (reorder, add, remove). |
| Search | `ctx doc search <query>` | LIKE-based substring search over content node bodies. Separate from `ctx search` (FTS). |
| Move | `ctx doc mv <node-id>` | Reposition a content node within its document. |
| Insert | `ctx doc insert <node-id>` | Add an existing node to a document at a given position. |
| Remove | `ctx doc remove <node-id>` | Drop a CONTAINS edge (content node is preserved). |
| Fork | `ctx doc fork <doc-id>` | Create a new document sharing the same content nodes. Structures diverge independently. |
| Split | `ctx doc split <node-id>` | Split a content node at a byte offset into two sibling nodes. |
| Promote | `ctx doc promote <node-id>` | Change a content node to kind=memory, making it available to recall. |
| Inline | `ctx doc inline <doc-id>` | Create a CONTAINS edge from a document to an existing memory node. |

---

## 3. Isolation Guarantees

The doc subsystem stores two kinds of nodes:

- `kind=document` — one per imported file, holds `src_hash` and `size` in metadata.
- `kind=content` — one per section, holds the raw source bytes in the node body.

**These nodes are excluded from all memory-surface queries by default:**
- `ctx recall`, `ctx search`, `ctx query`, `ctx list`, `ctx status`: filter to `kind=memory` nodes only.
- `ctx hook session-start`: composes only `kind=memory` nodes that match `tag:tier:pinned OR tag:tier:working`.
- `ctx hook stop`: the `<ctx:remember>` executor only creates `kind=memory` nodes.

The only way a document node appears in memory queries is via `ctx doc promote`, which explicitly changes `kind=content` to `kind=memory`. Promotion requires `--into-memory` (safety gate) and `--type` (memory node type).

After promotion, the node's body is unchanged and all CONTAINS edges are preserved, so `ctx doc verify` still passes and `ctx doc export` still produces byte-identical output.

---

## 4. Scaffold XML Format

The scaffold XML represents the structure (CONTAINS edges and their positions) of a document. No content bodies are embedded — the XML is purely structural.

### Shape

```xml
<ctx:doc id="DOCID">
  <ctx:node ref="ULID1">
    <ctx:node ref="ULID2"/>
    <ctx:node ref="ULID3"/>
  </ctx:node>
  <ctx:node ref="ULID4"/>
</ctx:doc>
```

### Rules

- The `id` attribute on `<ctx:doc>` is the document node ID.
- Each `<ctx:node ref="...">` corresponds to one content node (by ULID).
- Nesting mirrors the heading hierarchy: a `<ctx:node>` with children means those children are nested under that node in the CONTAINS edge graph.
- Sibling order is the composed position order (1-indexed, ascending).
- `ctx doc scaffold` output is deterministic: nodes ordered by position at each level.
- `ctx doc apply` diffs the XML against the live edge graph. It applies a minimal set of mutations (reorder, add, remove) transactionally. Unresolved refs (IDs not in the store) cause an error listing the missing IDs; no partial mutations are applied.

### Example Round-Trip

```sh
# Emit scaffold
ctx doc scaffold 01DOCID > scaffold.xml

# Edit scaffold.xml in your editor (reorder nodes, add/remove refs)

# Apply edits
ctx doc apply scaffold.xml

# Verify byte-identity still holds after edits that preserve all refs
ctx doc verify 01DOCID
```

Note: if `ctx doc apply` removes a ref from the scaffold (i.e., drops a CONTAINS edge), the composed output will no longer contain that section and `ctx doc verify` will fail. This is expected — `verify` checks the current composed state against the stored `src_hash` from the original import.

---

## 5. Command Reference

### ctx doc import

```
ctx doc import <path>
```

Reads the markdown file at `<path>`, decomposes it at heading boundaries, persists a `kind=document` node and `kind=content` nodes with CONTAINS edges, then recomposes and verifies byte-identity. Rolls back the entire transaction if the round-trip check fails.

Output: `Imported: <path>  (doc=<id>, <N> bytes)`

### ctx doc export

```
ctx doc export <doc-id> [-o <output-path>]
```

Recomposes the document from the database and writes the result to stdout (default) or to the file specified with `-o`. The output is byte-identical to the original import assuming no edits have removed content.

### ctx doc show

```
ctx doc show <doc-id>
```

Prints the document node metadata: ID, kind, created/updated timestamps, and `src_hash` (sha256 of the original import).

### ctx doc verify

```
ctx doc verify <doc-id>
```

Recomposes the document, computes sha256 of the result, and compares it to the stored `src_hash`. Exits 0 on match, 1 on mismatch.

### ctx doc scaffold

```
ctx doc scaffold <doc-id>
```

Emits the `<ctx:doc>` XML for the document's CONTAINS edge graph to stdout. Output is deterministic (nodes ordered by position). No content bodies are embedded.

### ctx doc apply

```
ctx doc apply <xml-file>
```

Parses the scaffold XML and diffs it against the current CONTAINS edge graph. Applies a minimal set of mutations (reorder, add, remove) transactionally. Unresolved refs cause an error with a list of missing IDs; no partial changes are written.

### ctx doc search

```
ctx doc search <query> [-n <limit>]
```

Performs a LIKE-based substring match over `kind=content` node bodies. Content nodes are not indexed in `nodes_fts`, so this uses SQL `LIKE`. Default limit: 50. Completely separate from `ctx search` which queries the memory FTS surface.

### ctx doc mv

```
ctx doc mv <node-id> --doc <doc-id> --pos <n>
```

Moves the content node to position `n` (1-indexed) within its document. Sibling positions are renumbered. Cross-document moves are rejected.

### ctx doc insert

```
ctx doc insert <node-id> --doc <doc-id> [--pos <n>] [--memory]
```

Inserts an existing node into the document at position `n` (default: 1). Existing nodes at that position and beyond are shifted forward. Use `--memory` to insert a `kind=memory` node.

### ctx doc remove

```
ctx doc remove <node-id> --doc <doc-id> [--recursive]
```

Drops the CONTAINS edge for the node in the document. The content node itself is NOT deleted. If the node has descendant CONTAINS edges in the document, `--recursive` is required.

### ctx doc fork

```
ctx doc fork <doc-id>
```

Creates a new `kind=document` node with independent CONTAINS edges pointing to the same content nodes as the original. The two documents diverge independently after the fork — editing one's structure does not affect the other. Content node bodies are shared and immutable.

Output: `Forked: <original-id> → <fork-id>`

### ctx doc split

```
ctx doc split <node-id> --doc <doc-id> --at <offset>
```

Replaces the CONTAINS edge for `node-id` with two new content nodes: the first containing `body[:offset]` and the second `body[offset:]`. The original content node is preserved but no longer referenced by the document.

Rejections:
- `offset == 0` or `offset == len(body)`: would produce an empty half.
- `offset` lands on a UTF-8 continuation byte.

`sha256(compose)` of the parent document is unchanged after split.

### ctx doc promote

```
ctx doc promote <node-id> --into-memory --type <memory-type>
```

Changes a `kind=content` node to `kind=memory`, making it available to the memory recall surface and FTS search (`ctx search`). `--into-memory` is a required safety gate preventing accidental promotion. `--type` sets the memory node type.

Valid types: `fact`, `decision`, `pattern`, `observation`, `hypothesis`, `task`, `summary`, `source`, `open-question`.

The node's body is unchanged; all CONTAINS edges are preserved; `sha256(compose)` is byte-identical before and after promotion.

### ctx doc inline

```
ctx doc inline <doc-id> --memory <memory-id> [--pos <n>]
```

Creates a CONTAINS edge from `doc-id` to an existing `kind=memory` node, making its body appear in the composed document output at position `n` (default: append). The memory node's kind is NOT changed — it remains available to the memory recall surface.

To permanently change a content node to memory, use `ctx doc promote`.

---

## 6. Corpus Fixture Layout

Integration and round-trip tests use a fixture corpus at:

```
internal/doc/testdata/corpus/
```

### Files

| Fixture | What it tests |
|---------|---------------|
| `01-simple-heading.md` | Single heading with body text |
| `02-nested-headings.md` | Multi-level heading hierarchy |
| `03-gfm-table.md` | GFM table inside a section |
| `04-code-block.md` | Fenced code blocks |
| `05-html-passthrough.md` | Raw HTML in a markdown file |
| `06-yaml-frontmatter.md` | YAML front matter as preamble |
| `07-crlf-only.md` | Windows-style CRLF line endings |
| `08-no-eof-newline.md` | File with no trailing newline |
| `09-empty-file.md` | Zero-byte file |
| `10-repeated-headings.md` | Multiple headings at the same level |
| `11-preamble-only.md` | Content before the first heading only |
| `12-preamble-then-headings.md` | Preamble followed by headings |

### Why `-text` in `.gitattributes`

The corpus files use `.gitattributes`:

```
internal/doc/testdata/corpus/** -text
```

This disables Git's line-ending normalization for corpus files. The byte-identity contract requires that `ctx doc import` followed by `ctx doc export` produces bit-for-bit identical output. If Git normalizes CRLF to LF on checkout, fixture `07-crlf-only.md` would lose its CRLF endings and the test would fail on a clean clone. The `-text` attribute preserves the exact bytes committed.

---

## 7. Byte-Identity Contract

The fundamental guarantee of the doc subsystem is:

> `sha256(ctx doc export <id>)` == `sha256(original file at import time)`

This holds as long as no structural edits have dropped content nodes from the CONTAINS edge graph. Specifically:

- `ctx doc import` verifies byte-identity immediately after persist (rolls back on failure).
- `ctx doc verify` re-checks the contract on demand.
- `ctx doc split` preserves the contract: the two halves concatenate to the original body.
- `ctx doc promote` preserves the contract: the node body is unchanged.
- `ctx doc inline` adds a memory node's body to the composed output, breaking the contract for the original document. This is expected and intentional.
- `ctx doc apply` with a scaffold that removes refs will break the contract. `ctx doc verify` will report the mismatch.
- `ctx doc fork` produces a new document with its own src_hash derived from the current composed output.

The corpus test (`internal/doc/corpus_test.go`) runs all 12 fixtures through import → export and asserts `bytes.Equal(original, exported)`.
