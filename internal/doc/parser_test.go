package doc_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/doc"
)

func TestDecompose_SimpleHeadingAndBody(t *testing.T) {
	src := []byte("# Heading\n\nSome body text.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	// Root has one child: the H1 section
	require.Len(t, tree.Children, 1)
	h1 := tree.Children[0]
	assert.Equal(t, "# Heading", strings.TrimSpace(strings.Split(string(h1.Body), "\n")[0]))

	// Concatenation equals source
	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_TwoSiblingH1s(t *testing.T) {
	src := []byte("# First\n\nBody one.\n\n# Second\n\nBody two.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 2)
	assert.True(t, strings.HasPrefix(string(tree.Children[0].Body), "# First"))
	assert.True(t, strings.HasPrefix(string(tree.Children[1].Body), "# Second"))

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_NestedH1H2(t *testing.T) {
	src := []byte("# Parent\n\nParent body.\n\n## Child\n\nChild body.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 1)
	h1 := tree.Children[0]
	require.Len(t, h1.Children, 1)
	h2 := h1.Children[0]
	assert.True(t, strings.HasPrefix(string(h2.Body), "## Child"))

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_CodeBlock(t *testing.T) {
	src := []byte("# Section\n\nText before.\n\n```go\nfunc main() {}\n```\n\nText after.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 1)
	// The entire code block and surrounding text is part of the H1 body
	body := string(tree.Children[0].Body)
	assert.Contains(t, body, "```go")
	assert.Contains(t, body, "func main() {}")

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_GFMTable(t *testing.T) {
	src := []byte("# Section\n\n| Col1 | Col2 |\n|------|------|\n| A    | B    |\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 1)
	body := string(tree.Children[0].Body)
	assert.Contains(t, body, "| Col1 |")

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_Preamble(t *testing.T) {
	src := []byte("Preamble text before any heading.\n\n# First Heading\n\nBody.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	// Preamble is a child of root with no heading prefix
	// We expect 2 children: preamble + H1
	require.Len(t, tree.Children, 2)
	assert.True(t, strings.HasPrefix(string(tree.Children[0].Body), "Preamble"))
	assert.True(t, strings.HasPrefix(string(tree.Children[1].Body), "# First Heading"))

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_EmptyInput(t *testing.T) {
	src := []byte("")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.NotNil(t, tree)
	assert.Empty(t, concatTree(tree))
}

func TestDecompose_OnlyPreamble(t *testing.T) {
	src := []byte("Just some text, no headings.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.Equal(t, src, concatTree(tree))
}

// Whitespace-ownership tests: every byte in the input must be attributed to
// exactly one tree node so round-trip composition is byte-identical.

func TestDecompose_CRLFLineEndings(t *testing.T) {
	src := []byte("# Heading\r\n\r\nBody text.\r\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.Equal(t, src, concatTree(tree), "CRLF round-trip must be byte-identical")
}

func TestDecompose_TrailingBlankLines(t *testing.T) {
	src := []byte("# Heading\n\nBody text.\n\n\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.Equal(t, src, concatTree(tree), "trailing blank lines must be preserved")
}

func TestDecompose_NoEOFNewline(t *testing.T) {
	src := []byte("# Heading\n\nBody text.")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.Equal(t, src, concatTree(tree), "no-EOF-newline must be preserved")
}

func TestDecompose_MixedLFCRLF(t *testing.T) {
	// Mixed line endings within a document
	src := []byte("# Heading\r\n\r\nFirst line.\nSecond line.\r\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)
	assert.Equal(t, src, concatTree(tree), "mixed LF/CRLF must be preserved byte-for-byte")
}

func TestDecompose_MultipleH2UnderH1(t *testing.T) {
	src := []byte("# Parent\n\n## Child One\n\nBody one.\n\n## Child Two\n\nBody two.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 1)
	h1 := tree.Children[0]
	require.Len(t, h1.Children, 2)

	assert.Equal(t, src, concatTree(tree))
}

func TestDecompose_DeepNesting(t *testing.T) {
	src := []byte("# H1\n\n## H2\n\n### H3\n\nDeep body.\n")
	tree, err := doc.Decompose(src)
	require.NoError(t, err)

	require.Len(t, tree.Children, 1) // H1
	h1 := tree.Children[0]
	require.Len(t, h1.Children, 1) // H2
	h2 := h1.Children[0]
	require.Len(t, h2.Children, 1) // H3

	assert.Equal(t, src, concatTree(tree))
}

// concatTree recursively concatenates all node bodies in depth-first order.
// This is what compose should produce; it's used to verify byte-identity.
func concatTree(n *doc.DocNode) []byte {
	var buf bytes.Buffer
	buf.Write(n.Body)
	for _, child := range n.Children {
		buf.Write(concatTree(child))
	}
	return buf.Bytes()
}
