# ctx MCP Server Implementation Plan

## Goal

Create an MCP (Model Context Protocol) server that exposes ctx functionality as tools for Claude Desktop and other MCP-compatible clients. This provides the same persistent memory capabilities that Claude Code agents get via hooks, but through explicit tool calls.

## Why MCP

The hook-based approach works for Claude Code because:
1. Hooks can parse XML commands from response text
2. Session lifecycle events (start/stop) trigger context injection

Claude Desktop and claude.ai don't have hooks. MCP provides:
1. Explicit tool calls instead of response parsing
2. Tool results injected directly into context
3. Same database, different interface

## Architecture

```
┌─────────────────┐     stdio      ┌─────────────────┐
│  Claude Desktop │ ◄────────────► │   ctx-mcp       │
│  or MCP Client  │                │   (Go binary)   │
└─────────────────┘                └────────┬────────┘
                                            │
                                   uses existing packages
                                            │
                    ┌───────────────────────┼───────────────────────┐
                    │                       │                       │
              internal/db            internal/query          internal/view
              (nodes, edges,         (parser, executor)      (composer)
               tags, pending)
```

## MCP Tools to Implement

### Core Memory Operations

| Tool | Description | Parameters | Returns |
|------|-------------|------------|---------|
| `ctx_remember` | Store a knowledge node | `type`, `content`, `tags?`, `summary?` | Node ID and confirmation |
| `ctx_recall` | Query stored knowledge | `query` | Matching nodes as markdown |
| `ctx_status` | Database statistics | none | Node counts, tier breakdown |
| `ctx_show` | Show a specific node | `id` | Full node content with tags |
| `ctx_list` | List recent nodes | `limit?`, `type?`, `tag?` | Node list |
| `ctx_search` | Full-text search | `query` | Matching nodes |

### Relationship Operations

| Tool | Description | Parameters | Returns |
|------|-------------|------------|---------|
| `ctx_link` | Create edge between nodes | `from`, `to`, `type` | Confirmation |
| `ctx_unlink` | Remove edge | `from`, `to`, `type?` | Confirmation |
| `ctx_related` | Find related nodes | `id`, `depth?` | Related nodes |
| `ctx_trace` | Trace provenance chain | `id` | Provenance path |

### Advanced Operations

| Tool | Description | Parameters | Returns |
|------|-------------|------------|---------|
| `ctx_summarize` | Create summary from nodes | `nodes`, `content`, `archive?` | Summary node ID |
| `ctx_supersede` | Mark node as superseded | `old`, `new` | Confirmation |
| `ctx_task` | Start/end task context | `name`, `action` | Task status |
| `ctx_compose` | Get composed context | `query?`, `budget?` | Markdown context |

### Tag Operations

| Tool | Description | Parameters | Returns |
|------|-------------|------------|---------|
| `ctx_tag` | Add tags to node | `id`, `tags` | Updated tags |
| `ctx_untag` | Remove tags from node | `id`, `tags` | Updated tags |
| `ctx_tags` | List all tags | none | Tag list with counts |

## File Structure

```
cmd/
  mcp/
    main.go           # MCP server entry point
    tools.go          # Tool definitions
    handlers.go       # Tool handler implementations
```

Or as a subcommand of the main binary:
```
cmd/
  mcp.go              # ctx mcp - runs MCP server
```

**Recommendation:** Subcommand approach (`ctx mcp`) keeps single binary, simpler distribution.

## Implementation Details

### MCP Library

Use `github.com/mark3labs/mcp-go` which provides:
- Tool registration with JSON Schema
- stdio transport
- Request/response handling

### Tool Registration Pattern

```go
func registerTools(s *server.MCPServer) {
    s.AddTool(mcp.NewTool("ctx_remember",
        mcp.WithDescription("Store a knowledge node in persistent memory"),
        mcp.WithString("type", 
            mcp.Required(),
            mcp.Description("Node type: fact, decision, pattern, observation, hypothesis, task, summary, source, open-question"),
        ),
        mcp.WithString("content",
            mcp.Required(), 
            mcp.Description("Content to store"),
        ),
        mcp.WithString("tags",
            mcp.Description("Comma-separated tags (e.g., 'project:foo,tier:reference')"),
        ),
        mcp.WithString("summary",
            mcp.Description("Optional short summary"),
        ),
    ), handleRemember)
    
    // ... more tools
}
```

### Handler Pattern

```go
func handleRemember(args map[string]interface{}) (*mcp.CallToolResult, error) {
    d, err := db.Open(getDBPath())
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
    }
    defer d.Close()
    
    nodeType := args["type"].(string)
    content := args["content"].(string)
    
    var tags []string
    if t, ok := args["tags"].(string); ok && t != "" {
        tags = strings.Split(t, ",")
    }
    
    var summary *string
    if s, ok := args["summary"].(string); ok && s != "" {
        summary = &s
    }
    
    node, err := d.CreateNode(db.CreateNodeInput{
        Type:    nodeType,
        Content: content,
        Tags:    tags,
        Summary: summary,
    })
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("failed to create node: %v", err)), nil
    }
    
    return mcp.NewToolResultText(fmt.Sprintf("Stored node %s (type: %s, %d tokens)", 
        node.ID, node.Type, node.TokenEstimate)), nil
}
```

### Database Path

Same as CLI: `--db` flag > `CTX_DB` env > `~/.ctx/store.db`

For MCP server, environment variable is most practical since Claude Desktop config doesn't easily support flags.

### Error Handling

MCP tools should return errors as tool results, not crash:
```go
if err != nil {
    return mcp.NewToolResultError(fmt.Sprintf("error: %v", err)), nil
}
```

## Claude Desktop Configuration

After building, add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or equivalent:

```json
{
  "mcpServers": {
    "ctx": {
      "command": "/path/to/ctx",
      "args": ["mcp"],
      "env": {
        "CTX_DB": "/home/user/.ctx/store.db"
      }
    }
  }
}
```

## Implementation Order

### Phase 1: Minimal Viable MCP
1. Add `cmd/mcp.go` with basic server setup
2. Implement `ctx_remember` tool
3. Implement `ctx_recall` tool  
4. Implement `ctx_status` tool
5. Test with Claude Desktop

### Phase 2: Full CRUD
6. Implement `ctx_show`, `ctx_list`, `ctx_search`
7. Implement `ctx_link`, `ctx_unlink`, `ctx_related`
8. Implement `ctx_tag`, `ctx_untag`, `ctx_tags`

### Phase 3: Advanced
9. Implement `ctx_summarize`, `ctx_supersede`
10. Implement `ctx_task`
11. Implement `ctx_compose`, `ctx_trace`

### Phase 4: Polish
12. Add `ctx install --mcp` to output Claude Desktop config snippet
13. Add MCP section to README
14. Test full workflow end-to-end

## Testing Strategy

### Unit Tests
- Each handler function tested with mock database
- Parameter validation tests
- Error handling tests

### Integration Tests
- Start MCP server as subprocess
- Send JSON-RPC requests via stdin
- Verify responses

### Manual Testing
1. Build: `go build -o ctx .`
2. Configure Claude Desktop
3. Restart Claude Desktop
4. Ask Claude to remember something
5. New conversation: ask Claude to recall
6. Verify with `ctx list` CLI

## Differences from Hook-Based Approach

| Aspect | Hooks | MCP |
|--------|-------|-----|
| Invocation | Implicit (XML in response) | Explicit (tool call) |
| Context injection | session-start hook | Manual via `ctx_compose` or per-tool |
| Session awareness | Automatic via hooks | None (stateless tools) |
| Nudges | prompt-submit injection | Not applicable |
| Best for | Claude Code | Claude Desktop, API |

## Open Questions

1. **Auto-compose on startup?** MCP doesn't have session events. Options:
   - Claude manually calls `ctx_compose` when starting
   - Create a `ctx_init` tool that returns composed context (like session-start)
   - Add a resource that Claude Desktop can auto-load

2. **Task context?** The `ctx_task` tool can work, but without session lifecycle, "end task" needs explicit invocation.

3. **Nudges?** No equivalent in MCP. Could add to tool results: "Note: 5 nodes stored this session" but less elegant.

## Success Criteria

1. `ctx mcp` starts and responds to MCP protocol
2. All core tools work (remember, recall, status, show, list)
3. Claude Desktop can successfully store and retrieve knowledge
4. Same database works with both CLI and MCP (interop)
5. Documentation for setup

## Dependencies

Add to go.mod:
```
github.com/mark3labs/mcp-go v0.17.0
```

## Estimated Effort

- Phase 1: 2-3 hours (minimal viable)
- Phase 2: 2-3 hours (full CRUD)
- Phase 3: 2 hours (advanced)
- Phase 4: 1 hour (polish)

Total: ~8 hours for complete implementation
