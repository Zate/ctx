package query_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
	"github.com/zate/ctx/testutil"
)

func createNode(t *testing.T, d db.Store, nodeType, content string, tags []string) *db.Node {
	t.Helper()
	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    nodeType,
		Content: content,
		Tags:    tags,
	})
	require.NoError(t, err)
	return node
}

func nodeContents(nodes []*db.Node) []string {
	var contents []string
	for _, n := range nodes {
		contents = append(contents, n.Content)
	}
	return contents
}

func TestExecuteQuery_ByType(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "a fact", nil)
	createNode(t, d, "fact", "another fact", nil)
	createNode(t, d, "decision", "a decision", nil)

	nodes, err := query.ExecuteQuery(d, "type:fact", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
	for _, n := range nodes {
		assert.Equal(t, "fact", n.Type)
	}
}

func TestExecuteQuery_ByTag(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "pinned fact", []string{"tier:pinned"})
	createNode(t, d, "fact", "reference fact", []string{"tier:reference"})

	nodes, err := query.ExecuteQuery(d, "tag:tier:pinned", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "pinned fact", nodes[0].Content)
}

func TestExecuteQuery_AND(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "pinned fact", []string{"tier:pinned", "project:alpha"})
	createNode(t, d, "decision", "pinned decision", []string{"tier:pinned", "project:alpha"})
	createNode(t, d, "fact", "reference fact", []string{"tier:reference", "project:alpha"})

	nodes, err := query.ExecuteQuery(d, "type:fact AND tag:tier:pinned", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "pinned fact", nodes[0].Content)
}

func TestExecuteQuery_OR(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "pinned fact", []string{"tier:pinned"})
	createNode(t, d, "observation", "working obs", []string{"tier:working"})
	createNode(t, d, "decision", "reference dec", []string{"tier:reference"})

	nodes, err := query.ExecuteQuery(d, "tag:tier:pinned OR tag:tier:working", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
	contents := nodeContents(nodes)
	assert.Contains(t, contents, "pinned fact")
	assert.Contains(t, contents, "working obs")
}

func TestExecuteQuery_NOT(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "a fact", nil)
	createNode(t, d, "decision", "a decision", nil)
	createNode(t, d, "pattern", "a pattern", nil)

	nodes, err := query.ExecuteQuery(d, "NOT type:fact", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
	for _, n := range nodes {
		assert.NotEqual(t, "fact", n.Type)
	}
}

func TestExecuteQuery_EmptyQuery_ReturnsAll(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "one", nil)
	createNode(t, d, "decision", "two", nil)

	nodes, err := query.ExecuteQuery(d, "", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestExecuteQuery_ExcludesSuperseded(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1 := createNode(t, d, "fact", "old fact", nil)
	n2 := createNode(t, d, "fact", "new fact", nil)

	_, err := d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", n2.ID, n1.ID)
	require.NoError(t, err)

	nodes, err := query.ExecuteQuery(d, "type:fact", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, n2.ID, nodes[0].ID)
}

func TestExecuteQuery_IncludeSuperseded(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1 := createNode(t, d, "fact", "old fact", nil)
	n2 := createNode(t, d, "fact", "new fact", nil)

	_, err := d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", n2.ID, n1.ID)
	require.NoError(t, err)

	nodes, err := query.ExecuteQuery(d, "type:fact", true)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestExecuteQuery_HasSummary(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1 := createNode(t, d, "fact", "has summary", nil)
	createNode(t, d, "fact", "no summary", nil)

	_, err := d.UpdateNode(n1.ID, db.UpdateNodeInput{Summary: testutil.Ptr("short version")})
	require.NoError(t, err)

	nodes, err := query.ExecuteQuery(d, "has:summary", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "has summary", nodes[0].Content)
}

func TestExecuteQuery_HasEdges(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1 := createNode(t, d, "fact", "linked node", nil)
	n2 := createNode(t, d, "fact", "other linked node", nil)
	createNode(t, d, "fact", "isolated node", nil)

	_, err := d.CreateEdge(n1.ID, n2.ID, "RELATES_TO")
	require.NoError(t, err)

	nodes, err := query.ExecuteQuery(d, "has:edges", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
	contents := nodeContents(nodes)
	assert.Contains(t, contents, "linked node")
	assert.Contains(t, contents, "other linked node")
}

func TestExecuteQuery_TokensFilter(t *testing.T) {
	d := testutil.SetupTestDB(t)

	// Token estimates are len(content)/4
	createNode(t, d, "fact", "short", nil)                  // ~1 token
	createNode(t, d, "fact", strings.Repeat("x", 400), nil) // ~100 tokens

	nodes, err := query.ExecuteQuery(d, "tokens:<50", false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "short", nodes[0].Content)
}

func TestExecuteQuery_FromEdge(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1 := createNode(t, d, "fact", "source node", nil)
	n2 := createNode(t, d, "fact", "target node", nil)
	createNode(t, d, "fact", "unrelated", nil)

	_, err := d.CreateEdge(n1.ID, n2.ID, "DERIVED_FROM")
	require.NoError(t, err)

	// from:n1 should find nodes that n1 points to
	nodes, err := query.ExecuteQuery(d, "from:"+n1.ID, false)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "target node", nodes[0].Content)
}

func TestExecuteQuery_TagsLoaded(t *testing.T) {
	d := testutil.SetupTestDB(t)

	createNode(t, d, "fact", "tagged node", []string{"tier:pinned", "project:alpha"})

	nodes, err := query.ExecuteQuery(d, "type:fact", false)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Contains(t, nodes[0].Tags, "tier:pinned")
	assert.Contains(t, nodes[0].Tags, "project:alpha")
}

func TestExecuteQuery_InvalidQuery(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := query.ExecuteQuery(d, "invalid:field", false)
	assert.Error(t, err)
}
