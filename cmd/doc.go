package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/doc"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Document decomposition and composition",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var docImportCmd = &cobra.Command{
	Use:   "import <path>",
	Short: "Import a markdown file as a decomposed document",
	Long: `Import reads a markdown file, decomposes it into a document node and
content nodes connected by CONTAINS edges, and verifies byte-identity by
recomposing and comparing the sha256 hash of the result with the original.
If the round-trip check fails, the transaction is rolled back and an error
is returned.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocImport,
}

func init() {
	rootCmd.AddCommand(docCmd)
	docCmd.AddCommand(docImportCmd)
}

func runDocImport(cmd *cobra.Command, args []string) error {
	path := args[0]

	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("doc import: read file: %w", err)
	}

	tree, err := doc.Decompose(src)
	if err != nil {
		return fmt.Errorf("doc import: decompose: %w", err)
	}

	store, err := openDB()
	if err != nil {
		return err
	}
	defer store.Close()

	docID, err := doc.Persist(tree, src, store)
	if err != nil {
		return fmt.Errorf("doc import: persist: %w", err)
	}

	// Round-trip check: recompose and assert byte-identity.
	composed, err := doc.ComposeFromStore(docID, store)
	if err != nil {
		// Best-effort cleanup: delete the document node (cascades via FK).
		_ = store.DeleteNode(docID)
		return fmt.Errorf("doc import: compose after persist: %w", err)
	}

	if !bytes.Equal(src, composed) {
		// Rollback by deleting the document node (cascades to content nodes + edges).
		_ = store.DeleteNode(docID)
		return fmt.Errorf("doc import: byte-identity check failed for %q\n"+
			"  original: %d bytes\n  composed: %d bytes\n"+
			"  First diff at byte (approx): %d",
			path, len(src), len(composed), firstDiffOffset(src, composed))
	}

	fmt.Printf("Imported: %s  (doc=%s, %d bytes)\n", path, docID, len(src))
	return nil
}

// firstDiffOffset returns the index of the first byte that differs between a and b.
func firstDiffOffset(a, b []byte) int {
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
