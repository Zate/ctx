package query_test

// Query parser/executor isolation between memory and doc/content kinds:
//   - type:fact implicitly scopes to kind='memory'
//   - kind:content and kind:document predicates work
//   - omitted kind defaults to memory

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
)

func openQueryTestDB(t *testing.T) db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "q_iso.db")
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func insertRawNodeQ(t *testing.T, d db.Store, id, nodeType, kind, content string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO nodes (id, type, kind, content, summary, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, NULL, 10, ?, ?, '{}')`,
		id, nodeType, kind, content, now, now,
	)
	require.NoError(t, err)
}

// TestQueryParser_KindPredicateParsesOK verifies kind:* parses without error.
func TestQueryParser_KindPredicateParsesOK(t *testing.T) {
	for _, q := range []string{"kind:memory", "kind:document", "kind:content"} {
		ast, err := query.Parse(q)
		require.NoError(t, err, "query %q should parse", q)
		require.NotNil(t, ast)
		assert.Equal(t, "predicate", ast.Type)
		assert.Equal(t, "kind", ast.Key)
	}
}

// TestQueryExecutor_ImplicitMemoryFilter verifies that type:fact excludes non-memory nodes.
func TestQueryExecutor_ImplicitMemoryFilter(t *testing.T) {
	d := openQueryTestDB(t)

	memNode, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "a memory fact"})
	require.NoError(t, err)

	insertRawNodeQ(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "a document fact")
	insertRawNodeQ(t, d, "DOC0000000000000000000002", "fact", db.NodeKindContent, "a content fact")

	results, err := query.ExecuteQuery(d, "type:fact", false)
	require.NoError(t, err)
	require.Len(t, results, 1, "type:fact must only return memory nodes")
	assert.Equal(t, memNode.ID, results[0].ID)
}

// TestQueryExecutor_EmptyQueryDefaultsToMemory verifies that no query also defaults to memory.
func TestQueryExecutor_EmptyQueryDefaultsToMemory(t *testing.T) {
	d := openQueryTestDB(t)

	memNode, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "mem"})
	require.NoError(t, err)

	insertRawNodeQ(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc")

	results, err := query.ExecuteQuery(d, "", false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, memNode.ID, results[0].ID)
}

// TestQueryExecutor_KindDocumentPredicate verifies kind:document returns doc nodes.
func TestQueryExecutor_KindDocumentPredicate(t *testing.T) {
	d := openQueryTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "memory node"})
	require.NoError(t, err)

	insertRawNodeQ(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc node")

	results, err := query.ExecuteQuery(d, "kind:document", false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "DOC0000000000000000000001", results[0].ID)
}

// TestQueryExecutor_KindContentPredicate verifies kind:content returns content nodes.
func TestQueryExecutor_KindContentPredicate(t *testing.T) {
	d := openQueryTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "memory node"})
	require.NoError(t, err)

	insertRawNodeQ(t, d, "DOC0000000000000000000001", "fact", db.NodeKindContent, "chunk text")

	results, err := query.ExecuteQuery(d, "kind:content", false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "DOC0000000000000000000001", results[0].ID)
}

// TestQueryExecutor_TagPredicateDefaultsToMemory verifies tag:tier:pinned excludes non-memory.
func TestQueryExecutor_TagPredicateDefaultsToMemory(t *testing.T) {
	d := openQueryTestDB(t)

	memNode, err := d.CreateNode(db.CreateNodeInput{
		Type: "fact", Content: "pinned mem", Tags: []string{"tier:pinned"},
	})
	require.NoError(t, err)

	insertRawNodeQ(t, d, "DOC0000000000000000000001", "fact", db.NodeKindDocument, "doc body")
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
		"DOC0000000000000000000001", "tier:pinned", now)
	require.NoError(t, err)

	results, err := query.ExecuteQuery(d, "tag:tier:pinned", false)
	require.NoError(t, err)
	require.Len(t, results, 1, "tag query must only return memory nodes")
	assert.Equal(t, memNode.ID, results[0].ID)
}
