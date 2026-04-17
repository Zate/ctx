package doc_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/doc"
	"github.com/zate/ctx/testutil"
)

// --- Task 2.3: Persist tests ---

func TestPersist_CreatesDocumentAndContentNodes(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Heading\n\nBody text.\n\n# Second\n\nMore text.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)
	assert.NotEmpty(t, docID)

	// Document node exists with kind='document'
	docNode, err := store.GetNode(docID)
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindDocument, docNode.Kind)

	// src_hash stored in metadata
	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(docNode.Metadata), &meta))
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(src))
	assert.Equal(t, expectedHash, meta["src_hash"])
}

func TestPersist_ContentNodesCreated(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Section One\n\nBody one.\n\n# Section Two\n\nBody two.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	// Check CONTAINS edges exist
	edges, err := store.Query(
		`SELECT e.id, e.from_id, e.to_id, e.position, e.document_id
		 FROM edges e
		 WHERE e.from_id = ? AND e.type = 'CONTAINS'
		 ORDER BY e.position ASC`,
		docID,
	)
	require.NoError(t, err)
	defer edges.Close()

	var edgeList []struct {
		id, fromID, toID, documentID string
		position                     int
	}
	for edges.Next() {
		var e struct {
			id, fromID, toID, documentID string
			position                     int
		}
		require.NoError(t, edges.Scan(&e.id, &e.fromID, &e.toID, &e.position, &e.documentID))
		edgeList = append(edgeList, e)
	}
	require.NoError(t, edges.Err())

	// Should have 2 content nodes
	assert.Len(t, edgeList, 2)

	// Positions are strictly increasing
	for i := 1; i < len(edgeList); i++ {
		assert.Greater(t, edgeList[i].position, edgeList[i-1].position,
			"positions must be strictly increasing")
	}

	// document_id is non-NULL and equals docID on all CONTAINS edges
	for _, e := range edgeList {
		assert.Equal(t, docID, e.fromID)
		assert.Equal(t, docID, e.documentID, "document_id must be non-NULL on CONTAINS edges")

		// Content nodes have kind='content'
		contentNode, err := store.GetNode(e.toID)
		require.NoError(t, err)
		assert.Equal(t, db.NodeKindContent, contentNode.Kind)
	}
}

func TestPersist_StrictlyIncreasingPositions(t *testing.T) {
	store := testutil.SetupTestDB(t)
	// Three sections
	src := []byte("# One\n\nBody.\n\n# Two\n\nBody.\n\n# Three\n\nBody.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	rows, err := store.Query(
		`SELECT position FROM edges WHERE from_id = ? AND type = 'CONTAINS' ORDER BY position ASC`,
		docID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var positions []int
	for rows.Next() {
		var pos int
		require.NoError(t, rows.Scan(&pos))
		positions = append(positions, pos)
	}

	require.Len(t, positions, 3)
	for i := 1; i < len(positions); i++ {
		assert.Greater(t, positions[i], positions[i-1])
	}
}

func TestPersist_SrcHashInMetadata(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Test\n\nContent here.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	docNode, err := store.GetNode(docID)
	require.NoError(t, err)

	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(docNode.Metadata), &meta))

	h := sha256.Sum256(src)
	expected := fmt.Sprintf("%x", h)
	assert.Equal(t, expected, meta["src_hash"])
}

func TestPersist_NestedNodes(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Parent\n\nParent body.\n\n## Child\n\nChild body.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	// Count total CONTAINS edges from docID (all content nodes, flattened)
	rows, err := store.Query(
		`SELECT COUNT(*) FROM edges WHERE document_id = ? AND type = 'CONTAINS'`,
		docID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var count int
	rows.Next()
	require.NoError(t, rows.Scan(&count))
	// Nested: H1 + H2 = 2 content nodes
	assert.Equal(t, 2, count)
}
