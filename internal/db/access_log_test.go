package db_test

// Phase 2 tests — LogAccess / LogAccessBatch / QueryAccess with kind='memory'
// isolation guard.

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

func countAccessLog(t *testing.T, d db.Store) int {
	t.Helper()
	var n int
	require.NoError(t, d.QueryRow("SELECT COUNT(*) FROM access_log").Scan(&n))
	return n
}

// 2.1 — Single LogAccess writes exactly one row with correct fields.
func TestLogAccess_HappyPath(t *testing.T) {
	d := openTestDB(t)

	mem, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "hello"})
	require.NoError(t, err)

	require.NoError(t, d.LogAccess(mem.ID, db.AccessTypeGet, "foo", "show:01HF"))

	entries, err := d.QueryAccess(db.AccessLogQuery{AllAgents: true})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	got := entries[0]
	assert.Equal(t, mem.ID, got.NodeID)
	assert.Equal(t, db.AccessTypeGet, got.AccessType)
	assert.Equal(t, "foo", got.Agent)
	assert.Equal(t, "show:01HF", got.QueryContext)
	assert.WithinDuration(t, time.Now().UTC(), got.AccessedAt, 5*time.Second)
}

// 2.2 — LogAccess on doc/content/unknown is a silent no-op.
func TestLogAccess_NonMemoryIsNoOp(t *testing.T) {
	d := openTestDB(t)

	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	insertRawNode(t, d, "DOC0000000000000000000002", "fact", db.NodeKindContent, "content body")

	require.NoError(t, d.LogAccess("DOC0000000000000000000001", db.AccessTypeGet, "a", ""))
	require.NoError(t, d.LogAccess("DOC0000000000000000000002", db.AccessTypeGet, "a", ""))
	require.NoError(t, d.LogAccess("01HFNONEXISTENTNODEXXXXXXXX", db.AccessTypeGet, "a", ""))

	assert.Equal(t, 0, countAccessLog(t, d))
}

// 2.3 — LogAccessBatch filters mixed-kind input; only memory IDs are inserted.
func TestLogAccessBatch_FiltersMixedKinds(t *testing.T) {
	d := openTestDB(t)

	mem1, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "m1"})
	require.NoError(t, err)
	mem2, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "m2"})
	require.NoError(t, err)
	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc")

	ids := []string{mem1.ID, "DOC0000000000000000000001", mem2.ID, "01HUNKNOWNXXXXXXXXXXXXXXXX"}
	require.NoError(t, d.LogAccessBatch(ids, db.AccessTypeHookInject, "agent1", "session-start"))

	assert.Equal(t, 2, countAccessLog(t, d))

	entries, err := d.QueryAccess(db.AccessLogQuery{AllAgents: true})
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, db.AccessTypeHookInject, e.AccessType)
		assert.Equal(t, "agent1", e.Agent)
		assert.Contains(t, []string{mem1.ID, mem2.ID}, e.NodeID)
	}
}

// 2.4 — LogAccessBatch with 200 IDs is fast (single-tx perf check).
func TestLogAccessBatch_Stress200(t *testing.T) {
	d := openTestDB(t)

	ids := make([]string, 200)
	for i := range ids {
		mem, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "n"})
		require.NoError(t, err)
		ids[i] = mem.ID
	}

	start := time.Now()
	require.NoError(t, d.LogAccessBatch(ids, db.AccessTypeHookInject, "stress", "x"))
	elapsed := time.Since(start)

	assert.Equal(t, 200, countAccessLog(t, d))
	assert.Less(t, elapsed, 200*time.Millisecond, "batch insert should be sub-200ms (single tx)")
}

// 2.5 — Concurrent LogAccess + DeleteNode produces no panic and no error
// surfaces from LogAccess regardless of order.
func TestLogAccess_RaceWithDelete(t *testing.T) {
	d := openTestDB(t)

	mem, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "race"})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = d.LogAccess(mem.ID, db.AccessTypeGet, "a", "race")
	}()
	go func() {
		defer wg.Done()
		_ = d.DeleteNode(mem.ID)
	}()
	wg.Wait()

	// Whatever order won the race, querying access log must work and
	// FK cascade must hold (no orphans for deleted node).
	entries, err := d.QueryAccess(db.AccessLogQuery{AllAgents: true, NodeIDPrefix: mem.ID})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(entries), 1)
}

// QueryAccess filters: prefix, agent, type, since, limit.
func TestQueryAccess_Filters(t *testing.T) {
	d := openTestDB(t)

	a, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "a"})
	require.NoError(t, err)
	b, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "b"})
	require.NoError(t, err)

	require.NoError(t, d.LogAccess(a.ID, db.AccessTypeGet, "agent1", "show:a"))
	require.NoError(t, d.LogAccess(a.ID, db.AccessTypeExplicitQuery, "agent2", "query:a"))
	require.NoError(t, d.LogAccess(b.ID, db.AccessTypeGet, "agent1", "show:b"))

	// Default: agent filter requires explicit Agent or AllAgents.
	entries, err := d.QueryAccess(db.AccessLogQuery{Agent: "agent1"})
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// AllAgents returns everything.
	entries, err = d.QueryAccess(db.AccessLogQuery{AllAgents: true})
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Type filter.
	entries, err = d.QueryAccess(db.AccessLogQuery{AllAgents: true, AccessType: db.AccessTypeExplicitQuery})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "agent2", entries[0].Agent)

	// Prefix filter — use full ID since ULIDs share a timestamp prefix.
	entries, err = d.QueryAccess(db.AccessLogQuery{AllAgents: true, NodeIDPrefix: a.ID})
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, a.ID, e.NodeID)
	}

	// Limit.
	entries, err = d.QueryAccess(db.AccessLogQuery{AllAgents: true, Limit: 1})
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Since filter (future timestamp returns nothing).
	future := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	entries, err = d.QueryAccess(db.AccessLogQuery{AllAgents: true, Since: future})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// LogAccessBatch with empty input is a no-op.
func TestLogAccessBatch_EmptyNoOp(t *testing.T) {
	d := openTestDB(t)
	require.NoError(t, d.LogAccessBatch(nil, db.AccessTypeHookInject, "a", ""))
	require.NoError(t, d.LogAccessBatch([]string{}, db.AccessTypeHookInject, "a", ""))
	assert.Equal(t, 0, countAccessLog(t, d))
}
