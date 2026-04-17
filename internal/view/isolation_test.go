package view_test

// Task 1.4: Composer seed-mode isolation test.
// Insert a memory node with a RELATES_TO edge pointing to a content node;
// run the composer in seed mode and assert the content node is NOT returned.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/view"
)

func openViewIsoTestDB(t *testing.T) db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view_iso.db")
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func insertRawNodeV(t *testing.T, d db.Store, id, nodeType, kind, content string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO nodes (id, type, kind, content, summary, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, NULL, 10, ?, ?, '{}')`,
		id, nodeType, kind, content, now, now,
	)
	require.NoError(t, err)
}

// TestCompose_SeedMode_ExcludesContentNodes verifies that the composer's seed-mode
// edge traversal does not return non-memory nodes.
func TestCompose_SeedMode_ExcludesContentNodes(t *testing.T) {
	d := openViewIsoTestDB(t)

	// Create a normal memory node as seed
	seedNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "seed memory node", Tags: []string{"tier:working"},
	})
	require.NoError(t, err)

	// Insert a content node
	insertRawNodeV(t, d, "DOC0000000000000000000001", "fact", db.NodeKindContent, "content chunk")

	// Link memory → content via RELATES_TO edge
	_, err = d.CreateEdge(seedNode.ID, "DOC0000000000000000000001", "RELATES_TO")
	require.NoError(t, err)

	result, err := view.Compose(d, view.ComposeOptions{
		SeedID: seedNode.ID,
		Depth:  1,
		Budget: 50000,
	})
	require.NoError(t, err)

	for _, n := range result.Nodes {
		assert.Equal(t, db.NodeKindMemory, n.Kind,
			"composer seed-mode must not return non-memory node %s (kind=%s)", n.ID, n.Kind)
		assert.NotEqual(t, "DOC0000000000000000000001", n.ID,
			"content node must not appear in composer output")
	}
}

// TestCompose_SeedMode_IncludesMemoryNeighbors verifies that the seed-mode still
// returns memory nodes that are linked from the seed.
func TestCompose_SeedMode_IncludesMemoryNeighbors(t *testing.T) {
	d := openViewIsoTestDB(t)

	seedNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "seed node", Tags: []string{"tier:working"},
	})
	require.NoError(t, err)

	neighborNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "memory neighbor", Tags: []string{"tier:reference"},
	})
	require.NoError(t, err)

	// Link seed → memory neighbor
	_, err = d.CreateEdge(seedNode.ID, neighborNode.ID, "RELATES_TO")
	require.NoError(t, err)

	// Also insert a content node that should be excluded
	insertRawNodeV(t, d, "DOC0000000000000000000001", "fact", db.NodeKindContent, "content chunk")
	_, err = d.CreateEdge(seedNode.ID, "DOC0000000000000000000001", "RELATES_TO")
	require.NoError(t, err)

	result, err := view.Compose(d, view.ComposeOptions{
		SeedID: seedNode.ID,
		Depth:  1,
		Budget: 50000,
	})
	require.NoError(t, err)

	// Should have seed + memory neighbor, but NOT the content node
	ids := make(map[string]bool)
	for _, n := range result.Nodes {
		ids[n.ID] = true
	}
	assert.True(t, ids[seedNode.ID], "seed node should be in result")
	assert.True(t, ids[neighborNode.ID], "memory neighbor should be in result")
	assert.False(t, ids["DOC0000000000000000000001"], "content node must not be in result")
}

// TestCompose_QueryMode_ExcludesNonMemory verifies the default query mode also excludes non-memory.
func TestCompose_QueryMode_ExcludesNonMemory(t *testing.T) {
	d := openViewIsoTestDB(t)

	memNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "pinned mem", Tags: []string{"tier:pinned"},
	})
	require.NoError(t, err)

	// Insert a doc node with the same tag
	insertRawNodeV(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
		"DOC0000000000000000000001", "tier:pinned", now)
	require.NoError(t, err)

	result, err := view.Compose(d, view.ComposeOptions{
		Query:  "tag:tier:pinned",
		Budget: 50000,
	})
	require.NoError(t, err)
	require.Len(t, result.Nodes, 1)
	assert.Equal(t, memNode.ID, result.Nodes[0].ID)
}
