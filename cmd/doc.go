package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
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

var docExportCmd = &cobra.Command{
	Use:   "export <doc-id>",
	Short: "Export a stored document to stdout or a file",
	Long: `Export recomposes the document from the database and writes the result
to stdout (default) or to the file specified with -o.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocExport,
}

var docShowCmd = &cobra.Command{
	Use:   "show <doc-id>",
	Short: "Show document metadata",
	Long:  `Show prints the document node metadata (ID, src_hash, size, etc.) for the given document ID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDocShow,
}

var docVerifyCmd = &cobra.Command{
	Use:   "verify <doc-id>",
	Short: "Verify byte-identity of a stored document",
	Long: `Verify recomposes the document from the database, computes sha256 of
the result, and compares it to the src_hash stored in the document node metadata.
Exits with status 0 on match, 1 on mismatch.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocVerify,
}

var docExportOutput string

func init() {
	rootCmd.AddCommand(docCmd)
	docCmd.AddCommand(docImportCmd)
	docCmd.AddCommand(docExportCmd)
	docCmd.AddCommand(docShowCmd)
	docCmd.AddCommand(docVerifyCmd)

	docExportCmd.Flags().StringVarP(&docExportOutput, "output", "o", "", "Output file path (default: stdout)")
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
	composed, err := doc.ComposeDoc(docID, store)
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

func runDocExport(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := openDB()
	if err != nil {
		return err
	}
	defer store.Close()

	composed, err := doc.ComposeDoc(docID, store)
	if err != nil {
		return fmt.Errorf("doc export: %w", err)
	}

	if docExportOutput != "" {
		if err := os.WriteFile(docExportOutput, composed, 0644); err != nil {
			return fmt.Errorf("doc export: write file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Exported %d bytes to %s\n", len(composed), docExportOutput)
		return nil
	}

	// Write to stdout.
	if _, err := os.Stdout.Write(composed); err != nil {
		return fmt.Errorf("doc export: write stdout: %w", err)
	}
	return nil
}

func runDocShow(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := openDB()
	if err != nil {
		return err
	}
	defer store.Close()

	node, err := store.GetNode(docID)
	if err != nil {
		return fmt.Errorf("doc show: %w", err)
	}

	var meta map[string]interface{}
	if node.Metadata != "" {
		if err := json.Unmarshal([]byte(node.Metadata), &meta); err != nil {
			meta = map[string]interface{}{"raw": node.Metadata}
		}
	}

	fmt.Printf("ID:         %s\n", node.ID)
	fmt.Printf("Kind:       %s\n", node.Kind)
	fmt.Printf("Created:    %s\n", node.CreatedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Printf("Updated:    %s\n", node.UpdatedAt.Format("2006-01-02T15:04:05Z"))
	if srcHash, ok := meta["src_hash"].(string); ok {
		fmt.Printf("src_hash:   %s\n", srcHash)
	}

	return nil
}

func runDocVerify(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := openDB()
	if err != nil {
		return err
	}
	defer store.Close()

	// Get stored src_hash from the document node metadata.
	node, err := store.GetNode(docID)
	if err != nil {
		return fmt.Errorf("doc verify: get node: %w", err)
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(node.Metadata), &meta); err != nil {
		return fmt.Errorf("doc verify: parse metadata: %w", err)
	}

	storedHash, ok := meta["src_hash"].(string)
	if !ok || storedHash == "" {
		return fmt.Errorf("doc verify: no src_hash in metadata for %q", docID)
	}

	// Recompose and compute hash.
	composed, err := doc.ComposeDoc(docID, store)
	if err != nil {
		return fmt.Errorf("doc verify: compose: %w", err)
	}

	actualHash := fmt.Sprintf("%x", sha256.Sum256(composed))

	if actualHash == storedHash {
		fmt.Printf("OK  %s  (sha256=%s, %d bytes)\n", docID, actualHash, len(composed))
		return nil
	}

	return fmt.Errorf("doc verify: MISMATCH for %q\n  stored:   %s\n  computed: %s\n  composed size: %d bytes",
		docID, storedHash, actualHash, len(composed))
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
