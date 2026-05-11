package doc

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/doc"
)

var docSearchLimit int

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

var docScaffoldCmd = &cobra.Command{
	Use:   "scaffold <doc-id>",
	Short: "Emit <ctx:doc> XML scaffold for a document",
	Long: `Scaffold emits the pure-structure XML for the document's CONTAINS edge graph.
The XML contains only node refs — no content bodies are embedded.
Output is deterministic (nodes ordered by position).`,
	Args: cobra.ExactArgs(1),
	RunE: runDocScaffold,
}

var docApplyCmd = &cobra.Command{
	Use:   "apply <xml-file>",
	Short: "Apply a scaffold XML to reorder/add/remove document edges",
	Long: `Apply parses the scaffold XML (previously produced by 'ctx doc scaffold'),
diffs it against the current CONTAINS edge graph, and applies a minimal set of
mutations (reorder, add, remove) transactionally. Content node bodies are never
modified. Unresolved refs cause an error listing the missing IDs.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocApply,
}

var docSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search content node bodies (doc-path only)",
	Long: `Search performs a substring match over kind='content' node bodies.
This is strictly separate from 'ctx search' which queries the memory FTS surface.
Content nodes are NOT indexed in nodes_fts, so this uses LIKE-based matching.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocSearch,
}

var docExportOutput string

// --- 5.6: mv, insert, remove flags ---
var docMvDocID string
var docMvToPos int
var docInsertDocID string
var docInsertPos int
var docInsertMemory bool
var docRemoveDocID string
var docRemoveRecursive bool

// --- 5.7: fork, split flags ---
var docSplitDocID string
var docSplitOffset int

// --- 6.3: promote flags ---
var docPromoteIntoMemory bool
var docPromoteType string

// --- 6.4: inline flags ---
var docInlineMemoryID string
var docInlineParentID string
var docInlinePos int

var docMvCmd = &cobra.Command{
	Use:   "mv <node-id>",
	Short: "Reparent (reorder) a node within a document",
	Long: `mv reorders a content node within its document. The node is moved to the
specified position (1-indexed); sibling positions are renumbered accordingly.
Cross-document moves are rejected.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocMv,
}

var docInsertCmd = &cobra.Command{
	Use:   "insert <node-id>",
	Short: "Insert a node into a document at a specified position",
	Long: `insert adds an existing content node (or memory node with --memory) to a
document at the given position (1-indexed). Existing nodes at that position and
beyond are shifted forward. The node must already exist in the store.

Note: --memory allows inserting a kind='memory' node. Its kind is not changed;
use 'ctx doc promote' or 'ctx doc inline' to promote a memory node to content.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocInsert,
}

var docRemoveCmd = &cobra.Command{
	Use:   "remove <node-id>",
	Short: "Remove a node from a document (drops CONTAINS edge, preserves content node)",
	Long: `remove drops the CONTAINS edge for the given node in the specified document.
The content node itself is NOT deleted — it may be referenced by other documents.
If the node has descendant CONTAINS edges in the document, --recursive is required.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocRemove,
}

var docForkCmd = &cobra.Command{
	Use:   "fork <doc-id>",
	Short: "Fork a document to share content nodes with independent structure",
	Long: `fork creates a new kind='document' node with independent CONTAINS edges
pointing to the same content nodes as the original. The two documents diverge
independently after the fork — editing one's structure does not affect the other.
Content node bodies are shared and immutable.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocFork,
}

var docSplitCmd = &cobra.Command{
	Use:   "split <node-id>",
	Short: "Split a content node at a byte offset into two siblings",
	Long: `split replaces the CONTAINS edge for node-id with two new content nodes:
the first containing body[:offset] and the second body[offset:]. The original
content node is preserved but no longer referenced by the document.

Rejections:
  - offset == 0 or offset == len(body): would produce an empty half.
  - offset lands on a UTF-8 continuation byte.

sha256(compose) of the parent document is unchanged after split.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocSplit,
}

var docPromoteCmd = &cobra.Command{
	Use:   "promote <node-id>",
	Short: "Promote a content node to a memory node",
	Long: `promote changes a kind='content' node to kind='memory', making it available
to the memory recall surface and FTS search (ctx search).

Requires --into-memory (safety gate — no accidental promotions) and --type
(the memory node type to assign, e.g. fact, decision, pattern).

The node's body is unchanged; all CONTAINS edges are preserved; sha256(compose)
of the parent document is byte-identical before and after promotion.

Valid types: fact, decision, pattern, observation, hypothesis, task, summary,
source, open-question.`,
	Args: cobra.ExactArgs(1),
	RunE: runDocPromote,
}

var docInlineCmd = &cobra.Command{
	Use:   "inline <doc-id>",
	Short: "Inline a memory node into a document",
	Long: `inline creates a CONTAINS edge from doc-id to an existing kind='memory' node,
making its body appear in the composed document output at the specified position.

The memory node's kind is NOT changed — it remains available to the memory
recall surface. To permanently promote a content node, use 'ctx doc promote'.

Flags:
  --memory <memory-id>   The kind='memory' node to inline (required).
  --parent <parent-id>   Parent node ID (reserved for future hierarchical use).
  --pos <n>              Position to insert at (1-indexed, default: append).`,
	Args: cobra.ExactArgs(1),
	RunE: runDocInline,
}

func init() {

	docCmd.AddCommand(docImportCmd)
	docCmd.AddCommand(docExportCmd)
	docCmd.AddCommand(docShowCmd)
	docCmd.AddCommand(docVerifyCmd)
	docCmd.AddCommand(docScaffoldCmd)
	docCmd.AddCommand(docApplyCmd)
	docCmd.AddCommand(docSearchCmd)
	docCmd.AddCommand(docMvCmd)
	docCmd.AddCommand(docInsertCmd)
	docCmd.AddCommand(docRemoveCmd)
	docCmd.AddCommand(docForkCmd)
	docCmd.AddCommand(docSplitCmd)
	docCmd.AddCommand(docPromoteCmd)
	docCmd.AddCommand(docInlineCmd)

	docExportCmd.Flags().StringVarP(&docExportOutput, "output", "o", "", "Output file path (default: stdout)")
	docSearchCmd.Flags().IntVarP(&docSearchLimit, "limit", "n", 50, "Maximum number of results")

	// mv flags
	docMvCmd.Flags().StringVar(&docMvDocID, "doc", "", "Document ID containing the node (required)")
	docMvCmd.Flags().IntVar(&docMvToPos, "pos", 0, "Target position (1-indexed, required)")
	_ = docMvCmd.MarkFlagRequired("doc")
	_ = docMvCmd.MarkFlagRequired("pos")

	// insert flags
	docInsertCmd.Flags().StringVar(&docInsertDocID, "doc", "", "Document ID to insert into (required)")
	docInsertCmd.Flags().IntVar(&docInsertPos, "pos", 1, "Position to insert at (1-indexed, default: 1)")
	docInsertCmd.Flags().BoolVar(&docInsertMemory, "memory", false, "Allow inserting a kind='memory' node")
	_ = docInsertCmd.MarkFlagRequired("doc")

	// remove flags
	docRemoveCmd.Flags().StringVar(&docRemoveDocID, "doc", "", "Document ID to remove from (required)")
	docRemoveCmd.Flags().BoolVar(&docRemoveRecursive, "recursive", false, "Also remove descendant CONTAINS edges")
	_ = docRemoveCmd.MarkFlagRequired("doc")

	// split flags
	docSplitCmd.Flags().StringVar(&docSplitDocID, "doc", "", "Document ID containing the node (required)")
	docSplitCmd.Flags().IntVar(&docSplitOffset, "at", 0, "Byte offset to split at (required)")
	_ = docSplitCmd.MarkFlagRequired("doc")
	_ = docSplitCmd.MarkFlagRequired("at")

	// promote flags
	docPromoteCmd.Flags().BoolVar(&docPromoteIntoMemory, "into-memory", false, "Confirm promotion to memory (required safety gate)")
	docPromoteCmd.Flags().StringVar(&docPromoteType, "type", "", "Memory node type to assign (required): fact, decision, pattern, observation, hypothesis, task, summary, source, open-question")
	_ = docPromoteCmd.MarkFlagRequired("type")

	// inline flags
	docInlineCmd.Flags().StringVar(&docInlineMemoryID, "memory", "", "ID of the kind='memory' node to inline (required)")
	docInlineCmd.Flags().StringVar(&docInlineParentID, "parent", "", "Parent node ID (reserved for future use)")
	docInlineCmd.Flags().IntVar(&docInlinePos, "pos", 0, "Insert position 1-indexed (default: append to end)")
	_ = docInlineCmd.MarkFlagRequired("memory")
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

	store, err := cmdutil.OpenDB(cmd)
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

	store, err := cmdutil.OpenDB(cmd)
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

	store, err := cmdutil.OpenDB(cmd)
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

	store, err := cmdutil.OpenDB(cmd)
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

func runDocScaffold(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	xmlBytes, err := doc.MarshalScaffold(docID, store)
	if err != nil {
		return fmt.Errorf("doc scaffold: %w", err)
	}

	if _, err := os.Stdout.Write(xmlBytes); err != nil {
		return fmt.Errorf("doc scaffold: write: %w", err)
	}
	return nil
}

func runDocApply(cmd *cobra.Command, args []string) error {
	xmlFile := args[0]

	xmlBytes, err := os.ReadFile(xmlFile)
	if err != nil {
		return fmt.Errorf("doc apply: read file: %w", err)
	}

	s, err := doc.UnmarshalScaffold(xmlBytes)
	if err != nil {
		return fmt.Errorf("doc apply: parse scaffold: %w", err)
	}

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := doc.ApplyScaffold(s, store); err != nil {
		return fmt.Errorf("doc apply: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Applied scaffold for document %s\n", s.DocID)
	return nil
}

func runDocSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	nodes, err := doc.SearchContent(query, docSearchLimit, store)
	if err != nil {
		return fmt.Errorf("doc search: %w", err)
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(nodes, "", "  ")
		fmt.Println(string(data))
	default:
		if len(nodes) == 0 {
			fmt.Println("No results found.")
			return nil
		}
		for _, n := range nodes {
			preview := n.Content
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			fmt.Printf("[%s] %s\n", n.ID, preview)
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// 5.6: mv, insert, remove handlers
// ---------------------------------------------------------------------------

func runDocMv(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := doc.MvNode(docMvDocID, nodeID, docMvToPos, store); err != nil {
		return fmt.Errorf("doc mv: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Moved node %s to position %d in document %s\n", nodeID, docMvToPos, docMvDocID)
	return nil
}

func runDocInsert(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	var insertErr error
	if docInsertMemory {
		insertErr = doc.InsertMemoryNode(docInsertDocID, nodeID, docInsertPos, store)
	} else {
		insertErr = doc.InsertNode(docInsertDocID, nodeID, docInsertPos, store)
	}
	if insertErr != nil {
		return fmt.Errorf("doc insert: %w", insertErr)
	}

	fmt.Fprintf(os.Stderr, "Inserted node %s at position %d in document %s\n", nodeID, docInsertPos, docInsertDocID)
	return nil
}

func runDocRemove(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := doc.RemoveNode(docRemoveDocID, nodeID, docRemoveRecursive, store); err != nil {
		return fmt.Errorf("doc remove: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Removed node %s from document %s (content node preserved)\n", nodeID, docRemoveDocID)
	return nil
}

// ---------------------------------------------------------------------------
// 5.7: fork, split handlers
// ---------------------------------------------------------------------------

func runDocFork(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	forkID, err := doc.ForkDoc(docID, store)
	if err != nil {
		return fmt.Errorf("doc fork: %w", err)
	}

	fmt.Printf("Forked: %s → %s\n", docID, forkID)
	return nil
}

func runDocSplit(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := doc.SplitNode(docSplitDocID, nodeID, docSplitOffset, store); err != nil {
		return fmt.Errorf("doc split: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Split node %s at offset %d in document %s\n", nodeID, docSplitOffset, docSplitDocID)
	return nil
}

// ---------------------------------------------------------------------------
// 6.3: promote handler
// ---------------------------------------------------------------------------

func runDocPromote(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	if !docPromoteIntoMemory {
		return fmt.Errorf("doc promote: --into-memory flag is required to confirm promotion (this flag prevents accidental promotions)")
	}

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := doc.PromoteNode(nodeID, docPromoteType, store); err != nil {
		return fmt.Errorf("doc promote: %w", err)
	}

	fmt.Printf("Promoted %s → memory/%s\n", nodeID, docPromoteType)
	return nil
}

// ---------------------------------------------------------------------------
// 6.4: inline handler
// ---------------------------------------------------------------------------

func runDocInline(cmd *cobra.Command, args []string) error {
	docID := args[0]

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	// If pos == 0 (not set), append to end by passing a large position (will be clamped).
	pos := docInlinePos
	if pos == 0 {
		pos = 999999 // will be clamped to len(siblings)+1 by InsertMemoryNode
	}

	if err := doc.InlineNode(docID, docInlineMemoryID, pos, store); err != nil {
		return fmt.Errorf("doc inline: %w", err)
	}

	fmt.Printf("Inlined %s into %s at pos %d\n", docInlineMemoryID, docID, pos)
	return nil
}
