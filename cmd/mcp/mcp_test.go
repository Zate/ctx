package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

func setupMCPTest(t *testing.T) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "test.db")
	// Open once to run migrations
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	d.Close()
}

func makeReq(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func TestHandleRemember(t *testing.T) {
	setupMCPTest(t)

	result, err := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "fact",
		"content": "Go is a compiled language",
		"tags":    "tier:reference,project:test",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "Stored node")
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "fact")
}

func TestHandleRemember_MissingType(t *testing.T) {
	setupMCPTest(t)

	result, err := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"content": "some content",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleRemember_WithSummary(t *testing.T) {
	setupMCPTest(t)

	result, err := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "decision",
		"content": "We chose SQLite for the database",
		"summary": "SQLite choice",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestHandleRecall(t *testing.T) {
	setupMCPTest(t)

	// Store something first
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "fact",
		"content": "Testing recall functionality",
		"tags":    "tier:reference",
	}))

	result, err := handleRecall(context.Background(), makeReq(map[string]interface{}{
		"query": "type:fact",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "Testing recall functionality")
}

func TestHandleRecall_NoResults(t *testing.T) {
	setupMCPTest(t)

	result, err := handleRecall(context.Background(), makeReq(map[string]interface{}{
		"query": "type:hypothesis",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "No nodes found")
}

func TestHandleStatus(t *testing.T) {
	setupMCPTest(t)

	// Add some nodes
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "fact",
		"content": "fact one",
		"tags":    "tier:reference",
	}))
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "decision",
		"content": "decision one",
		"tags":    "tier:pinned",
	}))

	result, err := handleStatus(context.Background(), makeReq(map[string]interface{}{}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "\"total_nodes\": 2")
	assert.Contains(t, text, "fact")
	assert.Contains(t, text, "decision")
}

func TestHandleCompose(t *testing.T) {
	setupMCPTest(t)

	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "fact",
		"content": "composed fact",
		"tags":    "tier:reference",
	}))

	result, err := handleCompose(context.Background(), makeReq(map[string]interface{}{}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "composed fact")
}

func TestHandleShow(t *testing.T) {
	setupMCPTest(t)

	// Create a node and get its ID
	remResult, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type":    "fact",
		"content": "show me",
	}))
	// Extract ID from result text
	text := remResult.Content[0].(mcp.TextContent).Text
	// "Stored node XXXX (type: fact, N tokens)"
	nodeID := extractNodeID(text)

	result, err := handleShow(context.Background(), makeReq(map[string]interface{}{
		"id": nodeID,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "show me")
}

func TestHandleShow_NotFound(t *testing.T) {
	setupMCPTest(t)

	result, err := handleShow(context.Background(), makeReq(map[string]interface{}{
		"id": "nonexistent",
	}))
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleList(t *testing.T) {
	setupMCPTest(t)

	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "list item 1",
	}))
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "list item 2",
	}))
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "decision", "content": "list item 3",
	}))

	// List all
	result, err := handleList(context.Background(), makeReq(map[string]interface{}{}))
	require.NoError(t, err)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "3 node(s)")

	// Filter by type
	result, err = handleList(context.Background(), makeReq(map[string]interface{}{
		"type": "fact",
	}))
	require.NoError(t, err)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "2 node(s)")

	// Limit
	result, err = handleList(context.Background(), makeReq(map[string]interface{}{
		"limit": float64(1),
	}))
	require.NoError(t, err)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "1 node(s)")
}

func TestHandleSearch(t *testing.T) {
	setupMCPTest(t)

	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "SQLite uses WAL mode for concurrency",
	}))
	_, _ = handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "Redis is an in-memory store",
	}))

	result, err := handleSearch(context.Background(), makeReq(map[string]interface{}{
		"query": "SQLite",
	}))
	require.NoError(t, err)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "WAL mode")
}

func TestHandleLink_Unlink(t *testing.T) {
	setupMCPTest(t)

	r1, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "node A",
	}))
	r2, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "node B",
	}))
	id1 := extractNodeID(r1.Content[0].(mcp.TextContent).Text)
	id2 := extractNodeID(r2.Content[0].(mcp.TextContent).Text)

	// Link
	result, err := handleLink(context.Background(), makeReq(map[string]interface{}{
		"from": id1,
		"to":   id2,
		"type": "RELATES_TO",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "Linked")

	// Unlink
	result, err = handleUnlink(context.Background(), makeReq(map[string]interface{}{
		"from": id1,
		"to":   id2,
		"type": "RELATES_TO",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestHandleTag_Untag_Tags(t *testing.T) {
	setupMCPTest(t)

	r, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "tagging test",
	}))
	id := extractNodeID(r.Content[0].(mcp.TextContent).Text)

	// Tag
	result, err := handleTag(context.Background(), makeReq(map[string]interface{}{
		"id":   id,
		"tags": "project:test,tier:working",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Tags list
	result, err = handleTags(context.Background(), makeReq(map[string]interface{}{}))
	require.NoError(t, err)
	text := result.Content[0].(mcp.TextContent).Text
	assert.Contains(t, text, "project:test")
	assert.Contains(t, text, "tier:working")

	// Untag
	result, err = handleUntag(context.Background(), makeReq(map[string]interface{}{
		"id":   id,
		"tags": "tier:working",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
}

func TestHandleSummarize(t *testing.T) {
	setupMCPTest(t)

	r1, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "source 1", "tags": "tier:reference",
	}))
	r2, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "source 2", "tags": "tier:reference",
	}))
	id1 := extractNodeID(r1.Content[0].(mcp.TextContent).Text)
	id2 := extractNodeID(r2.Content[0].(mcp.TextContent).Text)

	result, err := handleSummarize(context.Background(), makeReq(map[string]interface{}{
		"nodes":   id1 + "," + id2,
		"content": "Summary of sources",
		"archive": true,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "Created summary")
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "sources archived")
}

func TestHandleSupersede(t *testing.T) {
	setupMCPTest(t)

	r1, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "old fact",
	}))
	r2, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "new fact",
	}))
	oldID := extractNodeID(r1.Content[0].(mcp.TextContent).Text)
	newID := extractNodeID(r2.Content[0].(mcp.TextContent).Text)

	result, err := handleSupersede(context.Background(), makeReq(map[string]interface{}{
		"old": oldID,
		"new": newID,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "superseded by")
}

func TestHandleTask(t *testing.T) {
	setupMCPTest(t)

	// Start task
	result, err := handleTask(context.Background(), makeReq(map[string]interface{}{
		"name":   "test-task",
		"action": "start",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "started")

	// End task
	result, err = handleTask(context.Background(), makeReq(map[string]interface{}{
		"name":   "test-task",
		"action": "end",
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "ended")
}

func TestHandleRelated(t *testing.T) {
	setupMCPTest(t)

	r1, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "related A",
	}))
	r2, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "related B",
	}))
	id1 := extractNodeID(r1.Content[0].(mcp.TextContent).Text)
	id2 := extractNodeID(r2.Content[0].(mcp.TextContent).Text)

	_, _ = handleLink(context.Background(), makeReq(map[string]interface{}{
		"from": id1, "to": id2, "type": "RELATES_TO",
	}))

	result, err := handleRelated(context.Background(), makeReq(map[string]interface{}{
		"id": id1,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "related B")
}

func TestHandleTrace(t *testing.T) {
	setupMCPTest(t)

	r1, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "fact", "content": "trace source",
	}))
	r2, _ := handleRemember(context.Background(), makeReq(map[string]interface{}{
		"type": "summary", "content": "trace derived",
	}))
	id1 := extractNodeID(r1.Content[0].(mcp.TextContent).Text)
	id2 := extractNodeID(r2.Content[0].(mcp.TextContent).Text)

	_, _ = handleLink(context.Background(), makeReq(map[string]interface{}{
		"from": id2, "to": id1, "type": "DERIVED_FROM",
	}))

	result, err := handleTrace(context.Background(), makeReq(map[string]interface{}{
		"id": id2,
	}))
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "trace source")
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b, c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"a,,b", []string{"a", "b"}},
		{"", nil},
	}

	for _, tt := range tests {
		result := splitAndTrim(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}

// extractNodeID parses "Stored node XXXX (type: ...)" to get the ID
func extractNodeID(text string) string {
	// "Stored node 01ABC123... (type: fact, 5 tokens)"
	const prefix = "Stored node "
	start := len(prefix)
	end := start
	for end < len(text) && text[end] != ' ' {
		end++
	}
	return text[start:end]
}
