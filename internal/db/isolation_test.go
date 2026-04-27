package db_test

// Memory-path surfaces must exclude doc/content nodes. Insert kind='document'
// and kind='content' nodes via raw SQL, then assert ListMemoryNodes, Search,
// ListAllTags, and ListTagsByPrefix all filter them out.

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// insertRawNode inserts a node with an explicit kind bypassing CreateNode
// (which always defaults to memory). Used to simulate pre-existing doc/content nodes.
func insertRawNode(t *testing.T, d db.Store, id, nodeType, kind, content string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO nodes (id, type, kind, content, summary, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, NULL, 10, ?, ?, '{}')`,
		id, nodeType, kind, content, now, now,
	)
	require.NoError(t, err)
}

func openTestDB(t *testing.T) db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "iso_test.db")
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

// TestIsolation_ListMemoryNodes ensures doc/content nodes are excluded.
func TestIsolation_ListMemoryNodes(t *testing.T) {
	d := openTestDB(t)

	// Create a normal memory node via the API
	memNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "memory node content",
	})
	require.NoError(t, err)

	// Insert doc and content nodes via raw SQL
	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "document node body")
	insertRawNode(t, d, "DOC0000000000000000000002", "fact", db.NodeKindContent, "content chunk body")

	nodes, err := d.ListMemoryNodes(db.ListOptions{})
	require.NoError(t, err)
	require.Len(t, nodes, 1, "ListMemoryNodes should return only the memory node")
	assert.Equal(t, memNode.ID, nodes[0].ID)
}

// TestIsolation_ListNodes_ReturnsAll ensures ListNodes (doc subsystem) returns all.
func TestIsolation_ListNodes_ReturnsAll(t *testing.T) {
	d := openTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "mem"})
	require.NoError(t, err)

	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	insertRawNode(t, d, "DOC0000000000000000000002", "fact", db.NodeKindContent, "content chunk")

	nodes, err := d.ListNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, nodes, 3, "ListNodes (unrestricted) should return all kinds")
}

// TestIsolation_SearchExcludesDocContent verifies FTS only indexes kind='memory'.
func TestIsolation_SearchExcludesDocContent(t *testing.T) {
	d := openTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "unique_memory_term_xyzzy",
	})
	require.NoError(t, err)

	// Insert doc/content with the same term via raw SQL (bypassing FTS trigger)
	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "unique_memory_term_xyzzy document")
	insertRawNode(t, d, "DOC0000000000000000000002", "fact", db.NodeKindContent, "unique_memory_term_xyzzy content")

	results, err := d.Search("unique_memory_term_xyzzy")
	require.NoError(t, err)
	for _, n := range results {
		assert.Equal(t, db.NodeKindMemory, n.Kind,
			"Search must not return non-memory node %s (kind=%s)", n.ID, n.Kind)
	}
}

// TestIsolation_ListAllTagsExcludesDocContent verifies tag listing is memory-scoped.
func TestIsolation_ListAllTagsExcludesDocContent(t *testing.T) {
	d := openTestDB(t)

	memNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "mem", Tags: []string{"tier:pinned"},
	})
	require.NoError(t, err)

	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	// Add a tag to the doc node
	_, err = d.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
		"DOC0000000000000000000001", "doc-only-tag", time.Now().UTC().Format(time.RFC3339))
	require.NoError(t, err)

	tags, err := d.ListAllTags()
	require.NoError(t, err)

	tagSet := make(map[string]bool)
	for _, tg := range tags {
		tagSet[tg] = true
	}

	// Memory node tag should appear
	assert.True(t, tagSet["tier:pinned"], "memory node tag should appear")
	// Doc node tag must NOT appear
	assert.False(t, tagSet["doc-only-tag"], "doc node tag must not appear in ListAllTags")
	_ = memNode
}

// TestIsolation_ListTagsByPrefixExcludesDocContent verifies prefix-filtered tag listing.
func TestIsolation_ListTagsByPrefixExcludesDocContent(t *testing.T) {
	d := openTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "mem", Tags: []string{"doc:memory-tag"},
	})
	require.NoError(t, err)

	insertRawNode(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	_, err = d.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
		"DOC0000000000000000000001", "doc:document-only-tag", time.Now().UTC().Format(time.RFC3339))
	require.NoError(t, err)

	tags, err := d.ListTagsByPrefix("doc:")
	require.NoError(t, err)

	for _, tg := range tags {
		assert.NotEqual(t, "doc:document-only-tag", tg, "document node tag must not appear")
	}
}

// TestIsolation_MigrationV5_KindColumnExists verifies the schema has the kind column.
func TestIsolation_MigrationV5_KindColumnExists(t *testing.T) {
	d := openTestDB(t)

	var colName string
	err := d.QueryRow(`SELECT name FROM pragma_table_info('nodes') WHERE name = 'kind'`).Scan(&colName)
	require.NoError(t, err)
	assert.Equal(t, "kind", colName)
}

// TestIsolation_MigrationV5_EdgeColumnsExist verifies document_id and position on edges.
func TestIsolation_MigrationV5_EdgeColumnsExist(t *testing.T) {
	d := openTestDB(t)

	for _, col := range []string{"document_id", "position"} {
		var colName string
		err := d.QueryRow(
			fmt.Sprintf(`SELECT name FROM pragma_table_info('edges') WHERE name = '%s'`, col),
		).Scan(&colName)
		require.NoError(t, err, "column %s should exist in edges", col)
		assert.Equal(t, col, colName)
	}
}

// TestIsolation_CreateNode_DefaultsToMemory verifies CreateNode always produces kind='memory'.
func TestIsolation_CreateNode_DefaultsToMemory(t *testing.T) {
	d := openTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "hello"})
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindMemory, node.Kind)

	// Re-fetch to confirm persisted value
	fetched, err := d.GetNode(node.ID)
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindMemory, fetched.Kind)
}
