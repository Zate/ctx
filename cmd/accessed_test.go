package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

func seedAccessEntry(t *testing.T, nodeID, accessType, agent, ctx string) {
	t.Helper()
	d := openTestDB(t)
	require.NoError(t, d.LogAccess(nodeID, accessType, agent, ctx))
}

// 4.1 text output renders columns
func TestCLI_Accessed_TextOutput(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "fact body")
	seedAccessEntry(t, id, "get", "alice", "show:abc")

	out, err := runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, id[:8])
	assert.Contains(t, out, "show:abc")
}

// 4.2 filters: --node prefix, --type, --since, --limit
func TestCLI_Accessed_Filters(t *testing.T) {
	setupCLI(t)
	id1 := seedNode(t, "fact", "a")
	id2 := seedNode(t, "fact", "b")

	seedAccessEntry(t, id1, "get", "alice", "show:a")
	seedAccessEntry(t, id1, "explicit_query", "alice", "query:type:fact")
	seedAccessEntry(t, id2, "explicit_query", "alice", "query:type:fact")

	// --type filter
	out, err := runCLI(t, "accessed", "--all-agents", "--type", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "get")
	assert.NotContains(t, out, "explicit_query")

	// --node prefix (use full id to avoid time-prefix collision in shortID display)
	out, err = runCLI(t, "accessed", "--all-agents", "--node", id2, "--type", "explicit_query")
	require.NoError(t, err)
	// only id2's explicit_query row
	assert.Contains(t, out, "1 entries shown")

	// --limit
	out, err = runCLI(t, "accessed", "--all-agents", "--limit", "1")
	require.NoError(t, err)
	assert.Contains(t, out, "1 entries shown")

	// --since (future date filters everything out)
	out, err = runCLI(t, "accessed", "--all-agents", "--since", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	require.NoError(t, err)
	assert.Contains(t, out, "No access entries found")
}

// 4.3 --json emits valid JSON
func TestCLI_Accessed_JSON(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "json target")
	seedAccessEntry(t, id, "get", "alice", "show:x")

	format = "json"
	defer func() { format = "text" }()
	out, err := runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)

	var entries []db.AccessEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, id, entries[0].NodeID)
	assert.Equal(t, "get", entries[0].AccessType)
	assert.Equal(t, "alice", entries[0].Agent)
}

// 4.4 agent isolation: default scopes to --agent; --all-agents opts out
func TestCLI_Accessed_AgentIsolation(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "shared")
	seedAccessEntry(t, id, "get", "alice", "show:a")
	seedAccessEntry(t, id, "get", "bob", "show:b")

	// agent=alice -> only alice's row
	agent = "alice"
	defer func() { agent = "" }()
	out, err := runCLI(t, "accessed")
	require.NoError(t, err)
	assert.Contains(t, out, "alice")
	assert.NotContains(t, out, "bob")

	// --all-agents -> both
	out, err = runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "bob")
}

// 4.5 memory-only invariant: rows for non-memory nodes are not displayed
func TestCLI_Accessed_HidesNonMemoryRows(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)

	// Memory node + access entry
	mem, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "mem"})
	require.NoError(t, err)
	require.NoError(t, d.LogAccess(mem.ID, "get", "alice", "show:m"))

	// Document node + raw-inserted access entry (bypasses LogAccess gate)
	doc, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "doc", Kind: db.NodeKindDocument})
	require.NoError(t, err)
	_, err = d.Exec(
		`INSERT INTO access_log (node_id, accessed_at, agent, access_type, query_context) VALUES (?, ?, ?, ?, ?)`,
		doc.ID, time.Now().UTC().Format(time.RFC3339), "alice", "get", "show:d",
	)
	require.NoError(t, err)

	out, err := runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)
	// Only the memory row remains; the doc row is filtered out by QueryAccess.
	assert.Contains(t, out, "show:m")
	assert.NotContains(t, out, "show:d")
	assert.Contains(t, out, "1 entries shown")
}
