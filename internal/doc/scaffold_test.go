package doc_test

// Phase 4 scaffold tests (tasks 4.1, 4.2, 4.3).
//
// 4.1 Marshal tests: edge graph → <ctx:doc> XML, deterministic order, no bodies, correct nesting.
// 4.2 Unmarshal tests: valid XML → Scaffold; error on malformed XML; error on unresolved refs.
// 4.3 Apply tests: minimal mutation set; sha256(compose) unchanged after pure rearrangement.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/doc"
	"github.com/zate/ctx/testutil"
)

// ---------------------------------------------------------------------------
// 4.1 Marshal tests
// ---------------------------------------------------------------------------

// TestMarshalScaffold_Basic verifies that a simple imported document produces
// well-formed <ctx:doc> XML with one <ctx:node> per content section and no
// embedded content bodies.
func TestMarshalScaffold_Basic(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# First\n\nBody.\n\n# Second\n\nBody2.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)
	require.NotEmpty(t, xmlBytes)

	xmlStr := string(xmlBytes)

	// Must contain ctx:doc with correct id.
	assert.Contains(t, xmlStr, `<ctx:doc`, "must have ctx:doc element")
	assert.Contains(t, xmlStr, fmt.Sprintf(`id="%s"`, docID), "must contain doc id")

	// Must have ctx:node elements.
	assert.Contains(t, xmlStr, `<ctx:node`, "must have ctx:node elements")

	// Must NOT embed body content.
	assert.NotContains(t, xmlStr, "Body.", "must not embed content bodies")

	// Count the node refs — should be 2 for 2 headings.
	count := strings.Count(xmlStr, "<ctx:node")
	assert.Equal(t, 2, count, "expect 2 ctx:node elements for 2 headings")
}

// TestMarshalScaffold_DeterministicOrder verifies that marshaling the same
// document twice produces identical output (deterministic position ordering).
func TestMarshalScaffold_DeterministicOrder(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# A\n\n# B\n\n# C\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	first, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	second, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	assert.Equal(t, first, second, "marshal output must be deterministic")
}

// TestMarshalScaffold_NodeOrder verifies that nodes appear in position order
// and that unmarshal recovers the correct count.
func TestMarshalScaffold_NodeOrder(t *testing.T) {
	store := testutil.SetupTestDB(t)
	// Import a document with 3 headings; positions 10, 20, 30.
	src := []byte("# One\n\n# Two\n\n# Three\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	s, err := doc.UnmarshalScaffold(xmlBytes)
	require.NoError(t, err)
	assert.Equal(t, docID, s.DocID)
	assert.Equal(t, 3, len(s.Children), "expect 3 top-level children")
}

// TestMarshalScaffold_EmptyDocument verifies that an empty document marshals
// to a <ctx:doc> with no <ctx:node> children.
func TestMarshalScaffold_EmptyDocument(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte{}

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	xmlStr := string(xmlBytes)
	assert.Contains(t, xmlStr, `<ctx:doc`, "must have ctx:doc")
	assert.NotContains(t, xmlStr, `<ctx:node`, "empty document must have no nodes")
}

// TestMarshalScaffold_UnknownDocID verifies that MarshalScaffold errors on an
// unknown document ID.
func TestMarshalScaffold_UnknownDocID(t *testing.T) {
	store := testutil.SetupTestDB(t)

	_, err := doc.MarshalScaffold("01NONEXISTENT00000000000000", store)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// 4.2 Unmarshal tests
// ---------------------------------------------------------------------------

// TestUnmarshalScaffold_Valid verifies round-trip: marshal then unmarshal
// produces a Scaffold with the correct DocID and node refs.
func TestUnmarshalScaffold_Valid(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# Alpha\n\n# Beta\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	s, err := doc.UnmarshalScaffold(xmlBytes)
	require.NoError(t, err)

	assert.Equal(t, docID, s.DocID, "DocID must survive round-trip")
	assert.Equal(t, 2, len(s.Children), "must have 2 children for 2 headings")
	for _, child := range s.Children {
		assert.NotEmpty(t, child.Ref, "each child must have a non-empty Ref")
	}
}

// TestUnmarshalScaffold_MalformedXML verifies that malformed XML returns an error.
func TestUnmarshalScaffold_MalformedXML(t *testing.T) {
	bad := []byte(`<ctx:doc id="X"><ctx:node ref="Y"`)
	_, err := doc.UnmarshalScaffold(bad)
	assert.Error(t, err, "malformed XML must return error")
}

// TestUnmarshalScaffold_MissingDocID verifies that XML without an id attribute
// returns an error.
func TestUnmarshalScaffold_MissingDocID(t *testing.T) {
	noID := []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<ctx:doc></ctx:doc>`)
	_, err := doc.UnmarshalScaffold(noID)
	assert.Error(t, err, "missing doc id must return error")
}

// ---------------------------------------------------------------------------
// 4.3 Apply tests
// ---------------------------------------------------------------------------

// TestApplyScaffold_UnresolvedRefs verifies that applying a scaffold with a
// non-existent ref ID returns an error listing the missing ID.
func TestApplyScaffold_UnresolvedRefs(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# One\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	// Hand-craft a scaffold with a non-existent ref.
	fakeRef := "01FAKEREFID000000000000000"
	badXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ctx:doc id="%s">
  <ctx:node ref="%s"/>
</ctx:doc>
`, docID, fakeRef)

	s, err := doc.UnmarshalScaffold([]byte(badXML))
	require.NoError(t, err)

	err = doc.ApplyScaffold(s, store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fakeRef, "error must list missing ref ID")
}

// TestApplyScaffold_NoOp verifies that applying an unchanged scaffold is a
// no-op: sha256(compose) is identical before and after.
func TestApplyScaffold_NoOp(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# First\n\nBody.\n\n# Second\n\nBody2.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	beforeHash := sha256Compose(t, docID, store)

	// Marshal then immediately apply — no structural change.
	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	s, err := doc.UnmarshalScaffold(xmlBytes)
	require.NoError(t, err)

	err = doc.ApplyScaffold(s, store)
	require.NoError(t, err)

	afterHash := sha256Compose(t, docID, store)
	assert.Equal(t, beforeHash, afterHash, "no-op apply must not change sha256(compose)")
}

// TestApplyScaffold_Reorder verifies that reordering two nodes produces the
// correct composition output.
func TestApplyScaffold_Reorder(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# First\n\nFirst body.\n\n# Second\n\nSecond body.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	// Get the original scaffold (children in order: [ref0, ref1]).
	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	s, err := doc.UnmarshalScaffold(xmlBytes)
	require.NoError(t, err)
	require.Len(t, s.Children, 2)

	// Swap the two children.
	s.Children[0], s.Children[1] = s.Children[1], s.Children[0]

	err = doc.ApplyScaffold(s, store)
	require.NoError(t, err)

	// Recompose — content should be in swapped order.
	composed, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)

	secondIdx := strings.Index(string(composed), "# Second")
	firstIdx := strings.Index(string(composed), "# First")
	assert.Less(t, secondIdx, firstIdx, "after reorder, Second should appear before First")
}

// TestApplyScaffold_PureRearrangementPreservesContent verifies that a
// rearrangement never modifies content node bodies.
func TestApplyScaffold_PureRearrangementPreservesContent(t *testing.T) {
	store := testutil.SetupTestDB(t)
	src := []byte("# A\n\nA body.\n\n# B\n\nB body.\n\n# C\n\nC body.\n")

	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	docID, err := doc.Persist(tree, src, store)
	require.NoError(t, err)

	// Capture content bodies before.
	beforeBodies := contentBodies(t, docID, store)

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	require.NoError(t, err)

	s, err := doc.UnmarshalScaffold(xmlBytes)
	require.NoError(t, err)
	require.Len(t, s.Children, 3)

	// Rotate: [A, B, C] → [C, A, B]
	s.Children = append([]*doc.ScaffoldNode{s.Children[2]}, s.Children[:2]...)

	err = doc.ApplyScaffold(s, store)
	require.NoError(t, err)

	// Bodies must be identical (only positions changed).
	afterBodies := contentBodies(t, docID, store)
	assert.ElementsMatch(t, beforeBodies, afterBodies, "rearrangement must not modify content bodies")
}

// TestScaffoldCorpusRoundTrip round-trips every corpus fixture through
// marshal → unmarshal → apply and asserts sha256(compose) is unchanged.
func TestScaffoldCorpusRoundTrip(t *testing.T) {
	corpusDir := filepath.Join("testdata", "corpus")
	entries, err := os.ReadDir(corpusDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(corpusDir, name)

		t.Run(name, func(t *testing.T) {
			store := testutil.SetupTestDB(t)

			src, err := os.ReadFile(path)
			require.NoError(t, err)

			tree, err := doc.Decompose(src)
			require.NoError(t, err)

			docID, err := doc.Persist(tree, src, store)
			require.NoError(t, err)

			beforeHash := sha256Compose(t, docID, store)

			xmlBytes, err := doc.MarshalScaffold(docID, store)
			require.NoError(t, err)

			s, err := doc.UnmarshalScaffold(xmlBytes)
			require.NoError(t, err)

			err = doc.ApplyScaffold(s, store)
			require.NoError(t, err)

			afterHash := sha256Compose(t, docID, store)
			assert.Equal(t, beforeHash, afterHash,
				"corpus fixture %q: sha256 must be unchanged after no-op apply", name)
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sha256Compose computes sha256 of ComposeDoc for the given document.
func sha256Compose(t *testing.T, docID string, store db.Store) string {
	t.Helper()
	composed, err := doc.ComposeDoc(docID, store)
	require.NoError(t, err)
	return fmt.Sprintf("%x", sha256.Sum256(composed))
}

// contentBodies returns the content (body) of all kind='content' nodes for a
// document, sorted by position. Used to verify rearrangement doesn't mutate bodies.
func contentBodies(t *testing.T, docID string, store db.Store) []string {
	t.Helper()
	rows, err := store.Query(
		`SELECT n.content FROM edges e
		 JOIN nodes n ON n.id = e.to_id
		 WHERE e.from_id = ? AND e.type = 'CONTAINS' AND e.document_id = ?
		 ORDER BY e.position ASC`,
		docID, docID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var bodies []string
	for rows.Next() {
		var body string
		require.NoError(t, rows.Scan(&body))
		bodies = append(bodies, body)
	}
	require.NoError(t, rows.Err())
	return bodies
}
