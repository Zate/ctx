package doc_test

// Kind-promotion tests:
//   - PromoteNode changes kind from content→memory, requires --type,
//     preserves CONTAINS edges, and keeps compose output byte-identical.
//   - InlineNode adds a CONTAINS edge targeting an existing kind='memory'
//     node; the memory node's kind is unchanged and compose reads its
//     body verbatim.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/doc"
	"github.com/zate/ctx/testutil"
)

// ---------------------------------------------------------------------------
// PromoteNode tests
// ---------------------------------------------------------------------------

// TestPromote_KindChangesContentToMemory: PromoteNode changes kind from 'content'
// to 'memory' and sets the node type correctly.
func TestPromote_KindChangesContentToMemory(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Section\n\nPromotion target body.\n")
	docID := importDoc(t, store, src)

	ids := contentNodeIDs(t, docID, store)
	require.NotEmpty(t, ids)
	nodeID := ids[0]

	// Verify kind is 'content' before promotion.
	before, err := store.GetNode(nodeID)
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindContent, before.Kind, "node must start as kind='content'")

	// Promote to memory/fact.
	require.NoError(t, doc.PromoteNode(nodeID, "fact", store))

	// Verify kind is now 'memory' and type is 'fact'.
	after, err := store.GetNode(nodeID)
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindMemory, after.Kind, "promoted node must have kind='memory'")
	assert.Equal(t, "fact", after.Type, "promoted node must have type='fact'")
}

// TestPromote_PreservesContainsEdges: after promotion, the CONTAINS edges
// from the parent document to the promoted node are preserved.
func TestPromote_PreservesContainsEdges(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# A\n\nBody A.\n\n# B\n\nBody B.\n")
	docID := importDoc(t, store, src)

	ids := contentNodeIDs(t, docID, store)
	require.Len(t, ids, 2)
	nodeID := ids[0]

	// Promote first node.
	require.NoError(t, doc.PromoteNode(nodeID, "fact", store))

	// CONTAINS edge must still exist.
	assert.True(t, hasContainsEdge(t, docID, nodeID, store),
		"CONTAINS edge must be preserved after promotion")

	// Both nodes still in order.
	after := contentNodeIDs(t, docID, store)
	assert.Len(t, after, 2, "document must still have 2 content nodes in CONTAINS edges")
}

// TestPromote_ComposeByteIdentical: after promotion, sha256(compose(docID))
// is unchanged because the content body is preserved.
func TestPromote_ComposeByteIdentical(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Title\n\nContent for byte-identity test.\n")
	docID := importDoc(t, store, src)

	hashBefore := composeHash(t, docID, store)

	ids := contentNodeIDs(t, docID, store)
	require.NotEmpty(t, ids)

	require.NoError(t, doc.PromoteNode(ids[0], "decision", store))

	hashAfter := composeHash(t, docID, store)
	assert.Equal(t, hashBefore, hashAfter,
		"sha256(compose) must be byte-identical after promotion")
}

// TestPromote_AppearsInFTS: after promotion, the node's body is indexed in
// nodes_fts (the kind-conditional UPDATE trigger adds it automatically).
func TestPromote_AppearsInFTS(t *testing.T) {
	store := testutil.SetupTestDB(t)
	uniquePhrase := "zxquniquephrase99"
	src := []byte("# FTS\n\nThe " + uniquePhrase + " lives here.\n")
	docID := importDoc(t, store, src)
	_ = docID

	ids := contentNodeIDs(t, docID, store)
	require.NotEmpty(t, ids)
	nodeID := ids[0]

	// Before promotion: kind='content' — NOT in FTS.
	results, err := store.Search(uniquePhrase)
	require.NoError(t, err)
	found := false
	for _, r := range results {
		if r.ID == nodeID {
			found = true
		}
	}
	assert.False(t, found, "content node must NOT appear in FTS before promotion")

	// Promote.
	require.NoError(t, doc.PromoteNode(nodeID, "fact", store))

	// After promotion: kind='memory' — SHOULD appear in FTS via UPDATE trigger.
	results, err = store.Search(uniquePhrase)
	require.NoError(t, err)
	found = false
	for _, r := range results {
		if r.ID == nodeID {
			found = true
		}
	}
	assert.True(t, found, "promoted node must appear in FTS after promotion (via UPDATE trigger)")
}

// TestPromote_RequiresValidType: PromoteNode rejects invalid type strings.
func TestPromote_RequiresValidType(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# A\n\nBody.\n")
	docID := importDoc(t, store, src)

	ids := contentNodeIDs(t, docID, store)
	require.NotEmpty(t, ids)
	nodeID := ids[0]

	err := doc.PromoteNode(nodeID, "not-a-valid-type", store)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "type",
		"error must mention 'type'")
}

// TestPromote_RejectsNonContentNode: attempting to promote a kind='memory'
// node (already memory) must be rejected.
func TestPromote_RejectsNonContentNode(t *testing.T) {
	store := testutil.SetupTestDB(t)

	// Create a memory node directly.
	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Kind:    db.NodeKindMemory,
		Content: "already a memory node",
	})
	require.NoError(t, err)

	err = doc.PromoteNode(memNode.ID, "fact", store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content",
		"error must mention that node must be kind='content'")
}

// TestPromote_AllValidTypes: PromoteNode accepts all valid memory node types.
func TestPromote_AllValidTypes(t *testing.T) {
	validTypes := []string{
		"fact", "decision", "pattern", "observation", "hypothesis",
		"task", "summary", "source", "open-question",
	}
	for _, typ := range validTypes {
		t.Run(typ, func(t *testing.T) {
			store := testutil.SetupTestDB(t)
			src := []byte(fmt.Sprintf("# Section\n\nBody for type %s.\n", typ))
			docID := importDoc(t, store, src)

			ids := contentNodeIDs(t, docID, store)
			require.NotEmpty(t, ids)
			nodeID := ids[0]

			err := doc.PromoteNode(nodeID, typ, store)
			require.NoErrorf(t, err, "PromoteNode must accept type=%q", typ)

			n, err := store.GetNode(nodeID)
			require.NoError(t, err)
			assert.Equal(t, db.NodeKindMemory, n.Kind)
			assert.Equal(t, typ, n.Type)
		})
	}
}

// ---------------------------------------------------------------------------
// InlineNode tests
// ---------------------------------------------------------------------------

// TestInline_AddsContainsEdge: InlineNode creates a CONTAINS edge from docID
// to the memory node at the given position.
func TestInline_AddsContainsEdge(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Section\n\nDoc content.\n")
	docID := importDoc(t, store, src)

	// Create a standalone memory node.
	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Kind:    db.NodeKindMemory,
		Content: "memory content to inline",
	})
	require.NoError(t, err)

	// Inline the memory node at position 1 (before existing content).
	require.NoError(t, doc.InlineNode(docID, memNode.ID, 1, store))

	// CONTAINS edge must exist.
	assert.True(t, hasContainsEdge(t, docID, memNode.ID, store),
		"CONTAINS edge must exist after InlineNode")
}

// TestInline_MemoryKindUnchanged: after InlineNode, the referenced node's
// kind is still 'memory' (inline does NOT promote the node).
func TestInline_MemoryKindUnchanged(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Section\n\nDoc content.\n")
	docID := importDoc(t, store, src)

	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Kind:    db.NodeKindMemory,
		Content: "memory stays memory",
	})
	require.NoError(t, err)

	require.NoError(t, doc.InlineNode(docID, memNode.ID, 1, store))

	after, err := store.GetNode(memNode.ID)
	require.NoError(t, err)
	assert.Equal(t, db.NodeKindMemory, after.Kind,
		"InlineNode must NOT change node kind (promote is separate)")
}

// TestInline_ComposeIncludesMemoryBody: after InlineNode, ComposeDoc includes
// the memory node's body at the correct position.
func TestInline_ComposeIncludesMemoryBody(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Doc\n\nOriginal content.\n")
	docID := importDoc(t, store, src)

	memBody := "injected memory body\n"
	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Kind:    db.NodeKindMemory,
		Content: memBody,
	})
	require.NoError(t, err)

	// Append at position 2 (after the existing node).
	require.NoError(t, doc.InlineNode(docID, memNode.ID, 2, store))

	composed, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	assert.Contains(t, string(composed), memBody,
		"compose must include memory body verbatim after InlineNode")
}

// TestInline_RejectsNonMemoryNode: InlineNode must reject a node that is
// not kind='memory' (e.g., a content node).
func TestInline_RejectsNonMemoryNode(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Src\n\nSrc body.\n")
	docID := importDoc(t, store, src)

	ids := contentNodeIDs(t, docID, store)
	require.NotEmpty(t, ids)
	contentNodeID := ids[0]

	// Attempting to inline a kind='content' node must fail.
	err := doc.InlineNode(docID, contentNodeID, 1, store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory",
		"error must mention that node must be kind='memory'")
}

// TestInline_RejectsNonExistentNode: InlineNode must reject a node ID
// that does not exist in the store.
func TestInline_RejectsNonExistentNode(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Src\n\nSrc body.\n")
	docID := importDoc(t, store, src)

	err := doc.InlineNode(docID, "01NONEXISTENTID000000000XX", 1, store)
	require.Error(t, err)
}

// TestInline_RejectsNonExistentDoc: InlineNode must reject a docID that
// does not correspond to a document node.
func TestInline_RejectsNonExistentDoc(t *testing.T) {
	store := testutil.SetupTestDB(t)

	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Kind:    db.NodeKindMemory,
		Content: "some memory content",
	})
	require.NoError(t, err)

	err = doc.InlineNode("01BADDOCIDNOTEXISTS000000A", memNode.ID, 1, store)
	require.Error(t, err)
}

// TestInline_PositionOrdering: when inlined at position 1, the memory node
// appears first in contentNodeIDs.
func TestInline_PositionOrdering(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# A\n\nA body.\n\n# B\n\nB body.\n")
	docID := importDoc(t, store, src)

	origIDs := contentNodeIDs(t, docID, store)
	require.Len(t, origIDs, 2)

	memNode, err := store.CreateNode(db.CreateNodeInput{
		Type:    "observation",
		Kind:    db.NodeKindMemory,
		Content: "memory observation\n",
	})
	require.NoError(t, err)

	// Insert at position 1 — should appear first.
	require.NoError(t, doc.InlineNode(docID, memNode.ID, 1, store))

	allIDs := contentNodeIDs(t, docID, store)
	require.Len(t, allIDs, 3)
	assert.Equal(t, memNode.ID, allIDs[0], "inlined memory node must be at position 1")
	assert.Equal(t, origIDs[0], allIDs[1])
	assert.Equal(t, origIDs[1], allIDs[2])
}
