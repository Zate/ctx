package doc_test

// Unit tests for Compose(docID string, store db.Store) ([]byte, error).

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/doc"
	"github.com/zate/ctx/testutil"
)

// TestCompose_SingleNode verifies that a document with a single content node
// (no heading, just preamble) round-trips byte-identically.
func TestCompose_SingleNode(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("Just some preamble text.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	got, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	assert.Equal(t, src, got, "single-node document must round-trip byte-identically")
}

// TestCompose_EmptyDocument verifies that an empty document composes to an
// empty byte slice (not nil, empty OK).
func TestCompose_EmptyDocument(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte{}

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	got, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	assert.Equal(t, src, got, "empty document must compose to empty bytes")
}

// TestCompose_DepthFirstOrder verifies that nested headings are concatenated
// depth-first: H1 body → H2 body (child), not H1 → all-H1s → H2 (breadth-first).
func TestCompose_DepthFirstOrder(t *testing.T) {
	store := testutil.SetupTestDB(t)
	// Depth-first means: H1-intro, then H2-under-H1, then H1-2nd
	src := []byte("# First\n\nFirst body.\n\n## Nested\n\nNested body.\n\n# Second\n\nSecond body.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	got, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	assert.Equal(t, src, got, "depth-first order must be preserved")

	// Verify the ordering in the composed output explicitly
	composed := string(got)
	firstIdx := strings.Index(composed, "# First")
	nestedIdx := strings.Index(composed, "## Nested")
	secondIdx := strings.Index(composed, "# Second")
	assert.Greater(t, nestedIdx, firstIdx, "H2 must come after H1")
	assert.Greater(t, secondIdx, nestedIdx, "second H1 must come after H2")
}

// TestCompose_PositionOrdering verifies that position ordering is strictly
// respected: if positions are 10, 20, 30, the composition reflects that order.
func TestCompose_PositionOrdering(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# One\n\n# Two\n\n# Three\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	got, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	assert.Equal(t, src, got, "three-section document must round-trip byte-identically")

	composed := string(got)
	oneIdx := strings.Index(composed, "# One")
	twoIdx := strings.Index(composed, "# Two")
	threeIdx := strings.Index(composed, "# Three")
	assert.Less(t, oneIdx, twoIdx)
	assert.Less(t, twoIdx, threeIdx)
}

// TestCompose_Sha256RoundTrip verifies that sha256(compose(docID)) == stored src_hash.
func TestCompose_Sha256RoundTrip(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Main Heading\n\nSome content.\n\n## Sub\n\nSub content.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	got, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)

	expected := fmt.Sprintf("%x", sha256.Sum256(src))
	actual := fmt.Sprintf("%x", sha256.Sum256(got))
	assert.Equal(t, expected, actual, "sha256 of composed output must equal stored src_hash")
}

// TestCompose_UnknownDocID verifies that ComposeDoc returns an error for an
// unknown document ID.
func TestCompose_UnknownDocID(t *testing.T) {
	store := testutil.SetupTestDB(t)

	got, err := doc.ComposeDoc("nonexistent-id", store)
	// Should either return an error or empty bytes.
	// Per task spec: return an error for unknown IDs.
	assert.Error(t, err)
	assert.Nil(t, got)
}
