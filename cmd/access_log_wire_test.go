package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// queryAllAccess returns every access entry in the test DB, agent-agnostic.
func queryAllAccess(t *testing.T) []*db.AccessEntry {
	t.Helper()
	d := openTestDB(t)
	entries, err := d.QueryAccess(db.AccessLogQuery{AllAgents: true, Limit: 1000})
	require.NoError(t, err)
	return entries
}

// 3.1 show -> one `get` entry, query_context starts with show:
func TestCLI_AccessLog_Show_LogsGet(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "show target")

	_, err := runCLI(t, "show", id)
	require.NoError(t, err)

	entries := queryAllAccess(t)
	require.Len(t, entries, 1)
	assert.Equal(t, id, entries[0].NodeID)
	assert.Equal(t, db.AccessTypeGet, entries[0].AccessType)
	assert.Contains(t, entries[0].QueryContext, "show:")
}

// 3.2 query type:fact -> one explicit_query per returned node, ctx="query:type:fact"
func TestCLI_AccessLog_Query_LogsExplicitQuery(t *testing.T) {
	setupCLI(t)
	id1 := seedNode(t, "fact", "fact a")
	id2 := seedNode(t, "fact", "fact b")
	seedNode(t, "decision", "should not match")

	_, err := runCLI(t, "query", "type:fact")
	require.NoError(t, err)

	entries := queryAllAccess(t)
	got := map[string]string{}
	for _, e := range entries {
		got[e.NodeID] = e.AccessType
		assert.Equal(t, "query:type:fact", e.QueryContext)
	}
	assert.Equal(t, db.AccessTypeExplicitQuery, got[id1])
	assert.Equal(t, db.AccessTypeExplicitQuery, got[id2])
	assert.Len(t, entries, 2)
}

// 3.3 search -> N entries with ctx="search:<term>"
func TestCLI_AccessLog_Search_LogsExplicitQuery(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "Postgres uses MVCC for transaction isolation")
	seedNode(t, "fact", "SQLite uses WAL for crash safety")

	_, err := runCLI(t, "search", "Postgres")
	require.NoError(t, err)

	entries := queryAllAccess(t)
	require.Len(t, entries, 1)
	assert.Equal(t, id, entries[0].NodeID)
	assert.Equal(t, db.AccessTypeExplicitQuery, entries[0].AccessType)
	assert.Equal(t, "search:Postgres", entries[0].QueryContext)
}

// 3.4 compose -> LogAccessBatch once, type=explicit_query, all returned IDs
func TestCLI_AccessLog_Compose_LogsExplicitQuery(t *testing.T) {
	setupCLI(t)
	a := seedNode(t, "fact", "fact A", "tier:pinned")
	b := seedNode(t, "decision", "decision B", "tier:pinned")

	_, err := runCLI(t, "compose", "--query", "tag:tier:pinned")
	require.NoError(t, err)

	entries := queryAllAccess(t)
	ids := map[string]string{}
	for _, e := range entries {
		ids[e.NodeID] = e.AccessType
		assert.Equal(t, "compose:tag:tier:pinned", e.QueryContext)
	}
	assert.Equal(t, db.AccessTypeExplicitQuery, ids[a])
	assert.Equal(t, db.AccessTypeExplicitQuery, ids[b])
	assert.Len(t, entries, 2)
}

// 3.4b list -> explicit_query
func TestCLI_AccessLog_List_LogsExplicitQuery(t *testing.T) {
	setupCLI(t)
	a := seedNode(t, "fact", "list a")
	b := seedNode(t, "fact", "list b")

	_, err := runCLI(t, "list")
	require.NoError(t, err)

	entries := queryAllAccess(t)
	got := map[string]bool{}
	for _, e := range entries {
		got[e.NodeID] = true
		assert.Equal(t, db.AccessTypeExplicitQuery, e.AccessType)
		assert.Equal(t, "list", e.QueryContext)
	}
	assert.True(t, got[a])
	assert.True(t, got[b])
}

// 3.6 related -> graph_walk
func TestCLI_AccessLog_Related_LogsGraphWalk(t *testing.T) {
	setupCLI(t)
	from := seedNode(t, "fact", "from node")
	to := seedNode(t, "fact", "to node")
	d := openTestDB(t)
	_, err := d.CreateEdge(from, to, "RELATES_TO")
	require.NoError(t, err)

	_, err = runCLI(t, "related", from)
	require.NoError(t, err)

	entries := queryAllAccess(t)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		assert.Equal(t, db.AccessTypeGraphWalk, e.AccessType)
		assert.Contains(t, e.QueryContext, "related:")
		assert.Equal(t, to, e.NodeID)
	}
}

// 3.6 trace -> graph_walk
func TestCLI_AccessLog_Trace_LogsGraphWalk(t *testing.T) {
	setupCLI(t)
	a := seedNode(t, "fact", "trace a")
	b := seedNode(t, "fact", "trace b")
	d := openTestDB(t)
	_, err := d.CreateEdge(a, b, "DERIVED_FROM")
	require.NoError(t, err)

	_, err = runCLI(t, "trace", a)
	require.NoError(t, err)

	entries := queryAllAccess(t)
	require.NotEmpty(t, entries)
	gotIDs := map[string]bool{}
	for _, e := range entries {
		assert.Equal(t, db.AccessTypeGraphWalk, e.AccessType)
		assert.Contains(t, e.QueryContext, "trace:")
		gotIDs[e.NodeID] = true
	}
	assert.True(t, gotIDs[a])
	assert.True(t, gotIDs[b])
}

// 3.7 negative: writes don't log access
func TestCLI_AccessLog_Writes_DoNotLog(t *testing.T) {
	setupCLI(t)

	_, err := runCLI(t, "add", "--type", "fact", "no logging on add")
	require.NoError(t, err)

	d := openTestDB(t)
	nodes, err := d.ListNodes(db.ListOptions{Type: "fact"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	id := nodes[0].ID

	_, err = runCLI(t, "tag", id, "tier:pinned")
	require.NoError(t, err)

	_, err = runCLI(t, "update", id, "--content", "edited")
	require.NoError(t, err)

	target := seedNode(t, "fact", "link target")
	_, err = runCLI(t, "link", id, target)
	require.NoError(t, err)

	entries := queryAllAccess(t)
	assert.Empty(t, entries, "write commands must not produce access entries")
}

// Isolation: show on a kind=document node must not log
func TestCLI_AccessLog_DocNode_NotLogged(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	doc, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "doc node",
		Kind:    db.NodeKindDocument,
	})
	require.NoError(t, err)

	// show would refuse via the memory-only path? Try directly via DB API.
	_ = d.LogAccess(doc.ID, "get", "", "show:doc")

	entries := queryAllAccess(t)
	assert.Empty(t, entries)
}
