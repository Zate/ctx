package doc_test

// Task 3.2: Corpus harness
// For each fixture in testdata/corpus/:
//   1. Import (Decompose + Persist) the fixture.
//   2. ComposeDoc the result.
//   3. Assert sha256(composed) == sha256(fixture).
//
// Each fixture is a separate subtest so failures are isolated.

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/doc"
	"github.com/zate/ctx/testutil"
)

func TestCorpusRoundTrip(t *testing.T) {
	corpusDir := filepath.Join("testdata", "corpus")
	entries, err := os.ReadDir(corpusDir)
	require.NoError(t, err, "testdata/corpus must be readable")
	require.NotEmpty(t, entries, "testdata/corpus must contain at least one fixture")

	var count int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(corpusDir, name)

		t.Run(name, func(t *testing.T) {
			// Each fixture gets its own isolated DB.
			store := testutil.SetupTestDB(t)

			src, err := os.ReadFile(path)
			require.NoError(t, err)

			tree, err := doc.Decompose(src)
			require.NoError(t, err)

			docID, err := doc.Persist(tree, src, store)
			require.NoError(t, err)

			composed, err := doc.ComposeDoc(docID, store)
			require.NoError(t, err)

			srcHash := fmt.Sprintf("%x", sha256.Sum256(src))
			gotHash := fmt.Sprintf("%x", sha256.Sum256(composed))

			assert.Equal(t, srcHash, gotHash,
				"sha256 mismatch for %q: original %d bytes, composed %d bytes, first diff at byte %d",
				name, len(src), len(composed), firstDiffOffsetCorpus(src, composed))

			// Also assert byte equality for a precise failure message.
			assert.Equal(t, src, composed,
				"byte-identity failed for %q", name)
		})
		count++
	}

	// Sanity check: we expect at least 10 fixtures.
	assert.GreaterOrEqual(t, count, 10, "corpus must contain at least 10 fixtures")
}

// firstDiffOffsetCorpus returns the offset of the first differing byte.
// Defined here to avoid import cycle (same package test, but separate file).
func firstDiffOffsetCorpus(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
