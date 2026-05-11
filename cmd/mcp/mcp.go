// Package mcp implements the `ctx mcp` subcommand: an MCP server exposing
// ctx tools to Claude Desktop and other MCP clients.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
	"github.com/zate/ctx/internal/view"
)

// dbPath is captured from the root --db flag in runMCP. Tests set it directly.
var dbPath string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run MCP server for Claude Desktop and other MCP clients",
	RunE:  runMCP,
}

// Register adds the mcp command to the given root.
func Register(root *cobra.Command) {
	root.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, args []string) error {
	if f := cmd.Root().PersistentFlags().Lookup("db"); f != nil {
		dbPath = f.Value.String()
	}

	s := server.NewMCPServer(
		"ctx",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	registerTools(s)

	return server.ServeStdio(s)
}

func mcpOpenDB() (db.Store, error) {
	path := dbPath
	if envDB := os.Getenv("CTX_DB"); envDB != "" && path == "" {
		path = envDB
	}
	return db.Open(path)
}

func registerTools(s *server.MCPServer) {
	// Core tools
	s.AddTool(mcp.NewTool("ctx_remember",
		mcp.WithDescription("Store a knowledge node in persistent memory"),
		mcp.WithString("type",
			mcp.Required(),
			mcp.Description("Node type"),
			mcp.Enum("fact", "decision", "pattern", "observation", "hypothesis", "task", "summary", "source", "open-question"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("Content to store"),
		),
		mcp.WithString("tags",
			mcp.Description("Comma-separated tags (e.g. 'tier:reference,project:foo')"),
		),
		mcp.WithString("summary",
			mcp.Description("Optional short summary"),
		),
	), handleRemember)

	s.AddTool(mcp.NewTool("ctx_recall",
		mcp.WithDescription("Query stored knowledge using the ctx query language. Examples: 'type:fact', 'tag:project:X', 'type:decision AND tag:tier:reference'"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Query expression (e.g. 'type:fact', 'tag:project:X AND type:decision')"),
		),
	), handleRecall)

	s.AddTool(mcp.NewTool("ctx_status",
		mcp.WithDescription("Show database statistics: node counts by type, tier breakdown, token usage"),
	), handleStatus)

	s.AddTool(mcp.NewTool("ctx_compose",
		mcp.WithDescription("Compose a markdown document from stored knowledge. Supports query, explicit node IDs, or graph traversal from a seed node."),
		mcp.WithString("query",
			mcp.Description("Query expression to filter nodes"),
		),
		mcp.WithString("ids",
			mcp.Description("Comma-separated node IDs to compose (supports short prefixes)"),
		),
		mcp.WithString("seed",
			mcp.Description("Seed node ID for graph traversal (follows edges to related nodes)"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Traversal depth for seed mode (default: 1)"),
		),
		mcp.WithNumber("budget",
			mcp.Description("Token budget (default: 50000)"),
		),
		mcp.WithString("template",
			mcp.Description("Render template: 'default' or 'document'"),
		),
		mcp.WithBoolean("edges",
			mcp.Description("Include relationships between composed nodes (default: false)"),
		),
	), handleCompose)

	// CRUD tools
	s.AddTool(mcp.NewTool("ctx_show",
		mcp.WithDescription("Show a specific node by ID with full content and metadata"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Node ID"),
		),
	), handleShow)

	s.AddTool(mcp.NewTool("ctx_list",
		mcp.WithDescription("List recent nodes with optional filters"),
		mcp.WithString("type",
			mcp.Description("Filter by node type"),
		),
		mcp.WithString("tag",
			mcp.Description("Filter by tag"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max results to return (default: 20)"),
		),
	), handleList)

	s.AddTool(mcp.NewTool("ctx_search",
		mcp.WithDescription("Full-text search across all stored knowledge"),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search text"),
		),
	), handleSearch)

	s.AddTool(mcp.NewTool("ctx_link",
		mcp.WithDescription("Create a directed edge between two nodes"),
		mcp.WithString("from",
			mcp.Required(),
			mcp.Description("Source node ID"),
		),
		mcp.WithString("to",
			mcp.Required(),
			mcp.Description("Target node ID"),
		),
		mcp.WithString("type",
			mcp.Description("Edge type (default: RELATES_TO)"),
			mcp.Enum("DERIVED_FROM", "DEPENDS_ON", "SUPERSEDES", "RELATES_TO", "CHILD_OF"),
		),
	), handleLink)

	s.AddTool(mcp.NewTool("ctx_unlink",
		mcp.WithDescription("Remove an edge between two nodes"),
		mcp.WithString("from",
			mcp.Required(),
			mcp.Description("Source node ID"),
		),
		mcp.WithString("to",
			mcp.Required(),
			mcp.Description("Target node ID"),
		),
		mcp.WithString("type",
			mcp.Description("Edge type to remove (removes all types if not specified)"),
		),
	), handleUnlink)

	s.AddTool(mcp.NewTool("ctx_tag",
		mcp.WithDescription("Add tags to a node"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Node ID"),
		),
		mcp.WithString("tags",
			mcp.Required(),
			mcp.Description("Comma-separated tags to add"),
		),
	), handleTag)

	s.AddTool(mcp.NewTool("ctx_untag",
		mcp.WithDescription("Remove tags from a node"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Node ID"),
		),
		mcp.WithString("tags",
			mcp.Required(),
			mcp.Description("Comma-separated tags to remove"),
		),
	), handleUntag)

	s.AddTool(mcp.NewTool("ctx_tags",
		mcp.WithDescription("List all tags in the database, optionally filtered by prefix"),
		mcp.WithString("prefix",
			mcp.Description("Filter tags by prefix (e.g. 'tier:', 'project:')"),
		),
	), handleTags)

	// Advanced tools
	s.AddTool(mcp.NewTool("ctx_summarize",
		mcp.WithDescription("Create a summary node from existing nodes, optionally archiving the sources"),
		mcp.WithString("nodes",
			mcp.Required(),
			mcp.Description("Comma-separated source node IDs"),
		),
		mcp.WithString("content",
			mcp.Required(),
			mcp.Description("Summary content"),
		),
		mcp.WithBoolean("archive",
			mcp.Description("Archive source nodes to tier:off-context (default: false)"),
		),
	), handleSummarize)

	s.AddTool(mcp.NewTool("ctx_supersede",
		mcp.WithDescription("Mark an old node as superseded by a new one"),
		mcp.WithString("old",
			mcp.Required(),
			mcp.Description("ID of the node being superseded"),
		),
		mcp.WithString("new",
			mcp.Required(),
			mcp.Description("ID of the replacement node"),
		),
	), handleSupersede)

	s.AddTool(mcp.NewTool("ctx_task",
		mcp.WithDescription("Start or end a task context. Starting adds tier:working tag, ending removes it."),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Task name"),
		),
		mcp.WithString("action",
			mcp.Required(),
			mcp.Description("Action to perform"),
			mcp.Enum("start", "end"),
		),
	), handleTask)

	s.AddTool(mcp.NewTool("ctx_related",
		mcp.WithDescription("Find nodes related to a given node via edges"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Node ID"),
		),
		mcp.WithNumber("depth",
			mcp.Description("Traversal depth (default: 1)"),
		),
	), handleRelated)

	s.AddTool(mcp.NewTool("ctx_trace",
		mcp.WithDescription("Trace the provenance chain of a node (DERIVED_FROM and DEPENDS_ON edges)"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Node ID to trace from"),
		),
		mcp.WithBoolean("reverse",
			mcp.Description("Trace what depends on this node instead of what it derives from"),
		),
	), handleTrace)
}

// Core tool handlers

func handleRemember(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	nodeType, err := req.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var tags []string
	if t := req.GetString("tags", ""); t != "" {
		tags = splitAndTrim(t)
	}

	input := db.CreateNodeInput{
		Type:    nodeType,
		Content: content,
		Tags:    tags,
	}

	if s := req.GetString("summary", ""); s != "" {
		input.Summary = &s
	}

	// Check for existing node with same type and content to avoid duplicates
	existing, err := d.FindByTypeAndContent(nodeType, content)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to check duplicates: %v", err)), nil
	}
	if existing != nil {
		// Merge any new tags onto the existing node
		for _, tag := range tags {
			_ = d.AddTag(existing.ID, tag)
		}
		return mcp.NewToolResultText(fmt.Sprintf("Node %s already exists (type: %s, %d tokens) — tags merged", existing.ID, existing.Type, existing.TokenEstimate)), nil
	}

	node, err := d.CreateNode(input)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create node: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Stored node %s (type: %s, %d tokens)", node.ID, node.Type, node.TokenEstimate)), nil
}

func handleRecall(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	queryStr, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	nodes, err := query.ExecuteQuery(d, queryStr, false)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}

	if len(nodes) == 0 {
		return mcp.NewToolResultText("No nodes found matching query."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d node(s):\n\n", len(nodes))
	for _, n := range nodes {
		fmt.Fprintf(&b, "### [%s] %s\n", n.ID, n.Type)
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n", strings.Join(n.Tags, ", "))
		}
		fmt.Fprintf(&b, "\n%s\n\n---\n\n", n.Content)
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	var totalNodes, totalTokens, edgeCount, tagCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM nodes WHERE superseded_by IS NULL").Scan(&totalNodes)
	_ = d.QueryRow("SELECT COALESCE(SUM(token_estimate), 0) FROM nodes WHERE superseded_by IS NULL").Scan(&totalTokens)
	_ = d.QueryRow("SELECT COUNT(*) FROM edges").Scan(&edgeCount)
	_ = d.QueryRow("SELECT COUNT(DISTINCT tag) FROM tags").Scan(&tagCount)

	type typeCount struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
	}
	rows, err := d.Query("SELECT type, COUNT(*) FROM nodes WHERE superseded_by IS NULL GROUP BY type ORDER BY type")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer rows.Close()

	var typeCounts []typeCount
	for rows.Next() {
		var tc typeCount
		_ = rows.Scan(&tc.Type, &tc.Count)
		typeCounts = append(typeCounts, tc)
	}

	type tierInfo struct {
		Tier   string `json:"tier"`
		Nodes  int    `json:"nodes"`
		Tokens int    `json:"tokens"`
	}
	tierRows, err := d.Query(`SELECT t.tag, COUNT(DISTINCT t.node_id), COALESCE(SUM(n.token_estimate), 0)
		FROM tags t JOIN nodes n ON t.node_id = n.id
		WHERE t.tag LIKE 'tier:%' AND n.superseded_by IS NULL
		GROUP BY t.tag ORDER BY t.tag`)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
	}
	defer tierRows.Close()

	var tiers []tierInfo
	for tierRows.Next() {
		var ti tierInfo
		_ = tierRows.Scan(&ti.Tier, &ti.Nodes, &ti.Tokens)
		tiers = append(tiers, ti)
	}

	out := map[string]interface{}{
		"total_nodes":  totalNodes,
		"total_tokens": totalTokens,
		"total_edges":  edgeCount,
		"unique_tags":  tagCount,
		"types":        typeCounts,
		"tiers":        tiers,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleCompose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	queryStr := req.GetString("query", "")
	budget := req.GetInt("budget", 50000)
	idsStr := req.GetString("ids", "")
	seedID := req.GetString("seed", "")
	depth := req.GetInt("depth", 1)
	templateName := req.GetString("template", "")
	edges := req.GetBool("edges", false)

	opts := view.ComposeOptions{
		Query:        queryStr,
		Budget:       budget,
		SeedID:       seedID,
		Depth:        depth,
		IncludeEdges: edges,
	}

	if idsStr != "" {
		ids := strings.Split(idsStr, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		opts.IDs = ids
	}

	result, err := view.Compose(d, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("compose error: %v", err)), nil
	}

	if templateName != "" {
		return mcp.NewToolResultText(view.RenderTemplate(result, templateName)), nil
	}

	return mcp.NewToolResultText(view.RenderMarkdown(result)), nil
}

// CRUD tool handlers

func handleShow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	idArg, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := d.ResolveID(idArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve ID %q: %v", idArg, err)), nil
	}

	node, err := d.GetNode(id)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("node not found: %v", err)), nil
	}

	out := map[string]interface{}{
		"id":             node.ID,
		"type":           node.Type,
		"content":        node.Content,
		"token_estimate": node.TokenEstimate,
		"created_at":     node.CreatedAt,
		"updated_at":     node.UpdatedAt,
		"tags":           node.Tags,
	}
	if node.Summary != nil {
		out["summary"] = *node.Summary
	}
	if node.SupersededBy != nil {
		out["superseded_by"] = *node.SupersededBy
	}

	edges, _ := d.GetEdges(node.ID, "both")
	if len(edges) > 0 {
		out["edges"] = edges
	}

	data, _ := json.MarshalIndent(out, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	opts := db.ListOptions{
		Type:  req.GetString("type", ""),
		Tag:   req.GetString("tag", ""),
		Limit: req.GetInt("limit", 20),
	}

	nodes, err := d.ListNodes(opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("list error: %v", err)), nil
	}

	if len(nodes) == 0 {
		return mcp.NewToolResultText("No nodes found."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d node(s):\n\n", len(nodes))
	for _, n := range nodes {
		preview := n.Content
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		tags := ""
		if len(n.Tags) > 0 {
			tags = " [" + strings.Join(n.Tags, ", ") + "]"
		}
		fmt.Fprintf(&b, "- **%s** (%s): %s%s\n", n.ID, n.Type, preview, tags)
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleSearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	queryStr, err := req.RequireString("query")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	nodes, err := d.Search(queryStr)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search error: %v", err)), nil
	}

	if len(nodes) == 0 {
		return mcp.NewToolResultText("No results found."), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d result(s):\n\n", len(nodes))
	for _, n := range nodes {
		fmt.Fprintf(&b, "### [%s] %s\n", n.ID, n.Type)
		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n", strings.Join(n.Tags, ", "))
		}
		fmt.Fprintf(&b, "\n%s\n\n---\n\n", n.Content)
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleLink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	fromArg, err := req.RequireString("from")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	toArg, err := req.RequireString("to")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	fromID, err := d.ResolveID(fromArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve from ID %q: %v", fromArg, err)), nil
	}
	toID, err := d.ResolveID(toArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve to ID %q: %v", toArg, err)), nil
	}

	edgeType := req.GetString("type", "RELATES_TO")

	edge, err := d.CreateEdge(fromID, toID, edgeType)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create edge: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Linked %s → %s (%s) [edge: %s]", fromID, toID, edgeType, edge.ID)), nil
}

func handleUnlink(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	fromArg, err := req.RequireString("from")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	toArg, err := req.RequireString("to")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	fromID, err := d.ResolveID(fromArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve from ID %q: %v", fromArg, err)), nil
	}
	toID, err := d.ResolveID(toArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve to ID %q: %v", toArg, err)), nil
	}

	edgeType := req.GetString("type", "")

	if err := d.DeleteEdge(fromID, toID, edgeType); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to delete edge: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Unlinked %s → %s", fromID, toID)), nil
}

func handleTag(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	idArg, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := d.ResolveID(idArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve ID %q: %v", idArg, err)), nil
	}
	tagsStr, err := req.RequireString("tags")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tags := splitAndTrim(tagsStr)
	for _, tag := range tags {
		if err := d.AddTag(id, tag); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to add tag %s: %v", tag, err)), nil
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Tagged %s with: %s", id, strings.Join(tags, ", "))), nil
}

func handleUntag(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	idArg, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := d.ResolveID(idArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve ID %q: %v", idArg, err)), nil
	}
	tagsStr, err := req.RequireString("tags")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tags := splitAndTrim(tagsStr)
	for _, tag := range tags {
		if err := d.RemoveTag(id, tag); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to remove tag %s: %v", tag, err)), nil
		}
	}

	return mcp.NewToolResultText(fmt.Sprintf("Removed tags from %s: %s", id, strings.Join(tags, ", "))), nil
}

func handleTags(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	prefix := req.GetString("prefix", "")

	var tags []string
	if prefix != "" {
		tags, err = d.ListTagsByPrefix(prefix)
	} else {
		tags, err = d.ListAllTags()
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list tags: %v", err)), nil
	}

	if len(tags) == 0 {
		return mcp.NewToolResultText("No tags found."), nil
	}

	return mcp.NewToolResultText(strings.Join(tags, "\n")), nil
}

// Advanced tool handlers

func handleSummarize(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	nodesStr, err := req.RequireString("nodes")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	content, err := req.RequireString("content")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	archive := req.GetBool("archive", false)
	sourceIDs := splitAndTrim(nodesStr)

	// Resolve short ID prefixes
	for i, sid := range sourceIDs {
		resolved, resolveErr := d.ResolveID(sid)
		if resolveErr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("cannot resolve node ID %q: %v", sid, resolveErr)), nil
		}
		sourceIDs[i] = resolved
	}

	summary, err := d.CreateNode(db.CreateNodeInput{
		Type:    "summary",
		Content: content,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create summary: %v", err)), nil
	}

	for _, sourceID := range sourceIDs {
		if _, err := d.CreateEdge(summary.ID, sourceID, "DERIVED_FROM"); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to link to %s: %v", sourceID, err)), nil
		}
		if archive {
			_ = d.RemoveTag(sourceID, "tier:working")
			_ = d.RemoveTag(sourceID, "tier:reference")
			_ = d.RemoveTag(sourceID, "tier:pinned")
			_ = d.AddTag(sourceID, "tier:off-context")
		}
	}

	result := fmt.Sprintf("Created summary %s from %d source(s)", summary.ID, len(sourceIDs))
	if archive {
		result += " (sources archived)"
	}
	return mcp.NewToolResultText(result), nil
}

func handleSupersede(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	oldArg, err := req.RequireString("old")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	newArg, err := req.RequireString("new")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	oldID, err := d.ResolveID(oldArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve old ID %q: %v", oldArg, err)), nil
	}
	newID, err := d.ResolveID(newArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve new ID %q: %v", newArg, err)), nil
	}

	_, execErr := d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", newID, oldID)
	if execErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to supersede: %v", execErr)), nil
	}

	if _, err := d.CreateEdge(newID, oldID, "SUPERSEDES"); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create SUPERSEDES edge: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Node %s superseded by %s", oldID, newID)), nil
}

func handleTask(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	action, err := req.RequireString("action")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	taskTag := "task:" + name

	switch action {
	case "start":
		node, err := d.CreateNode(db.CreateNodeInput{
			Type:    "task",
			Content: fmt.Sprintf("Task: %s", name),
			Tags:    []string{"tier:working", taskTag},
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to create task node: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Task '%s' started (node: %s, tagged tier:working)", name, node.ID)), nil

	case "end":
		// Find task nodes with this tag and move to reference
		nodes, err := d.GetNodesByTag(taskTag)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to find task nodes: %v", err)), nil
		}
		for _, n := range nodes {
			_ = d.RemoveTag(n.ID, "tier:working")
			_ = d.AddTag(n.ID, "tier:reference")
		}
		return mcp.NewToolResultText(fmt.Sprintf("Task '%s' ended (%d node(s) moved to tier:reference)", name, len(nodes))), nil

	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown action: %s (use 'start' or 'end')", action)), nil
	}
}

func handleRelated(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	idArg, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := d.ResolveID(idArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve ID %q: %v", idArg, err)), nil
	}
	depth := req.GetInt("depth", 1)

	visited := map[string]bool{id: true}
	type relatedNode struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Edge    string `json:"edge_type"`
	}
	var results []relatedNode

	current := []string{id}
	for i := 0; i < depth; i++ {
		var next []string
		for _, cid := range current {
			edges, _ := d.GetEdges(cid, "both")
			for _, e := range edges {
				targetID := e.ToID
				if targetID == cid {
					targetID = e.FromID
				}
				if visited[targetID] {
					continue
				}
				visited[targetID] = true
				next = append(next, targetID)

				node, err := d.GetNode(targetID)
				if err != nil {
					continue
				}
				results = append(results, relatedNode{
					ID:      node.ID,
					Type:    node.Type,
					Content: node.Content,
					Edge:    e.Type,
				})
			}
		}
		current = next
	}

	if len(results) == 0 {
		return mcp.NewToolResultText("No related nodes found."), nil
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

func handleTrace(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	d, err := mcpOpenDB()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("database error: %v", err)), nil
	}
	defer d.Close()

	idArg, err := req.RequireString("id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	id, err := d.ResolveID(idArg)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("cannot resolve ID %q: %v", idArg, err)), nil
	}
	reverse := req.GetBool("reverse", false)

	visited := map[string]bool{}
	type traceNode struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Depth   int    `json:"depth"`
	}
	var results []traceNode

	var walk func(nodeID string, depth int)
	walk = func(nodeID string, depth int) {
		if visited[nodeID] {
			return
		}
		visited[nodeID] = true

		node, err := d.GetNode(nodeID)
		if err != nil {
			return
		}
		results = append(results, traceNode{
			ID:      node.ID,
			Type:    node.Type,
			Content: node.Content,
			Depth:   depth,
		})

		if reverse {
			edges, _ := d.GetEdgesTo(nodeID)
			for _, e := range edges {
				if e.Type == "DERIVED_FROM" || e.Type == "DEPENDS_ON" {
					walk(e.FromID, depth+1)
				}
			}
		} else {
			edges, _ := d.GetEdgesFrom(nodeID)
			for _, e := range edges {
				if e.Type == "DERIVED_FROM" || e.Type == "DEPENDS_ON" {
					walk(e.ToID, depth+1)
				}
			}
		}
	}

	walk(id, 0)

	if len(results) == 0 {
		return mcp.NewToolResultText("No trace found."), nil
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return mcp.NewToolResultText(string(data)), nil
}

// helpers

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
