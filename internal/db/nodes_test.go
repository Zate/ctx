package db_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/testutil"
)

func TestNodeCreate(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "test content",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, node.ID)
	assert.Equal(t, "fact", node.Type)
	assert.Equal(t, "test content", node.Content)
	assert.Greater(t, node.TokenEstimate, 0)
	assert.False(t, node.CreatedAt.IsZero())
}

func TestNodeCreate_AllTypes(t *testing.T) {
	validTypes := []string{"fact", "decision", "pattern", "observation",
		"hypothesis", "task", "summary", "source", "open-question"}

	for _, nodeType := range validTypes {
		t.Run(nodeType, func(t *testing.T) {
			d := testutil.SetupTestDB(t)

			node, err := d.CreateNode(db.CreateNodeInput{
				Type:    nodeType,
				Content: "test",
			})

			require.NoError(t, err)
			assert.Equal(t, nodeType, node.Type)
		})
	}
}

func TestNodeCreate_InvalidType(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{
		Type:    "invalid-type",
		Content: "test",
	})

	assert.Error(t, err)
}

func TestNodeCreate_EmptyContent(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "",
	})
	assert.Error(t, err)

	_, err = d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "   ",
	})
	assert.Error(t, err)
}

func TestNodeCreate_WithTags(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "test",
		Tags:    []string{"tier:reference", "project:test"},
	})

	require.NoError(t, err)
	assert.Contains(t, node.Tags, "tier:reference")
	assert.Contains(t, node.Tags, "project:test")
}

func TestNodeGet(t *testing.T) {
	d := testutil.SetupTestDB(t)

	created, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "test content",
	})
	require.NoError(t, err)

	fetched, err := d.GetNode(created.ID)

	require.NoError(t, err)
	assert.Equal(t, created.ID, fetched.ID)
	assert.Equal(t, created.Content, fetched.Content)
}

func TestNodeGet_NotFound(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.GetNode("nonexistent-id")

	assert.Error(t, err)
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestNodeUpdate(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "original",
	})
	require.NoError(t, err)

	updated, err := d.UpdateNode(node.ID, db.UpdateNodeInput{
		Content: testutil.Ptr("updated content"),
	})

	require.NoError(t, err)
	assert.Equal(t, "updated content", updated.Content)
	assert.False(t, updated.UpdatedAt.IsZero())
}

func TestNodeUpdate_Type(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "observation",
		Content: "test",
	})
	require.NoError(t, err)

	updated, err := d.UpdateNode(node.ID, db.UpdateNodeInput{
		Type: testutil.Ptr("decision"),
	})

	require.NoError(t, err)
	assert.Equal(t, "decision", updated.Type)
}

func TestNodeDelete(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "to delete",
	})
	require.NoError(t, err)

	err = d.DeleteNode(node.ID)
	assert.NoError(t, err)

	_, err = d.GetNode(node.ID)
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestNodeDelete_NotFound(t *testing.T) {
	d := testutil.SetupTestDB(t)

	err := d.DeleteNode("nonexistent")
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestNodeList(t *testing.T) {
	d := testutil.SetupTestDB(t)

	for i := 0; i < 5; i++ {
		_, err := d.CreateNode(db.CreateNodeInput{
			Type:    "fact",
			Content: fmt.Sprintf("node %d", i),
		})
		require.NoError(t, err)
	}

	nodes, err := d.ListNodes(db.ListOptions{})

	require.NoError(t, err)
	assert.Len(t, nodes, 5)
}

func TestNodeList_FilterByType(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "a"})
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "b"})
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "decision", Content: "c"})

	nodes, err := d.ListNodes(db.ListOptions{Type: "fact"})

	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestNodeList_Limit(t *testing.T) {
	d := testutil.SetupTestDB(t)

	for i := 0; i < 10; i++ {
		_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: fmt.Sprintf("%d", i)})
	}

	nodes, err := d.ListNodes(db.ListOptions{Limit: 3})

	require.NoError(t, err)
	assert.Len(t, nodes, 3)
}

func TestNodeList_ExcludesSuperseded(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "old"})
	n2, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "new"})

	_, _ = d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", n2.ID, n1.ID)

	nodes, err := d.ListNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, n2.ID, nodes[0].ID)
}

func TestNodeList_FilterByMultipleTags(t *testing.T) {
	d := testutil.SetupTestDB(t)

	// Node with both tags
	n1, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "book pinned", Tags: []string{"project:Book", "tier:pinned"}})
	// Node with only project:Book
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "book ref", Tags: []string{"project:Book", "tier:reference"}})
	// Node with only tier:pinned but different project
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "other pinned", Tags: []string{"project:other", "tier:pinned"}})

	nodes, err := d.ListNodes(db.ListOptions{Tags: []string{"project:Book", "tier:pinned"}})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, n1.ID, nodes[0].ID)
}

func TestNodeList_FilterBySingleTag(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "a", Tags: []string{"project:Book"}})
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "b", Tags: []string{"project:Book"}})
	_, _ = d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "c", Tags: []string{"project:other"}})

	// Single tag via Tags slice
	nodes, err := d.ListNodes(db.ListOptions{Tags: []string{"project:Book"}})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	// Backwards compat: single tag via Tag field
	nodes, err = d.ListNodes(db.ListOptions{Tag: "project:Book"})
	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestResolveID_FullID(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "test content",
	})
	require.NoError(t, err)

	resolved, err := d.ResolveID(node.ID)
	require.NoError(t, err)
	assert.Equal(t, node.ID, resolved)
}

func TestResolveID_FullID_NotFound(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.ResolveID("01AAAAAAAABBBBBBBBCCCCCCCC")
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestResolveID_Prefix(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: "test content",
	})
	require.NoError(t, err)

	// Use first 8 chars as prefix
	prefix := node.ID[:8]
	resolved, err := d.ResolveID(prefix)
	require.NoError(t, err)
	assert.Equal(t, node.ID, resolved)
}

func TestResolveID_Prefix_NotFound(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.ResolveID("ZZZZZZZZ")
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestResolveID_EmptyPrefix(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.ResolveID("")
	assert.Error(t, err)
}

func TestFindByTypeAndContent(t *testing.T) {
	d := testutil.SetupTestDB(t)

	node, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "unique content"})

	// Should find the existing node
	found, err := d.FindByTypeAndContent("fact", "unique content")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, node.ID, found.ID)

	// Different type should not match
	found, err = d.FindByTypeAndContent("decision", "unique content")
	require.NoError(t, err)
	assert.Nil(t, found)

	// Different content should not match
	found, err = d.FindByTypeAndContent("fact", "other content")
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestFindByTypeAndContent_IgnoresSuperseded(t *testing.T) {
	d := testutil.SetupTestDB(t)

	n1, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "old fact"})
	n2, _ := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "new fact"})

	// Mark n1 as superseded
	_, _ = d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", n2.ID, n1.ID)

	// Should not find the superseded node
	found, err := d.FindByTypeAndContent("fact", "old fact")
	require.NoError(t, err)
	assert.Nil(t, found)

	// Should still find the active node
	found, err = d.FindByTypeAndContent("fact", "new fact")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, n2.ID, found.ID)
}
