package doc

// Phase 6: Kind Promotion — PromoteNode and InlineNode.
//
// PromoteNode promotes a kind='content' node to kind='memory' with the given
// memory type. The node's body is unchanged; its CONTAINS edges are preserved;
// the FTS index is updated automatically via the AFTER UPDATE trigger from
// migration v5 (which conditionally adds rows for kind='memory' on UPDATE).
//
// InlineNode inserts a kind='memory' node into a document at a given position
// by creating a CONTAINS edge. The memory node's kind is NOT changed. This is
// a thin wrapper over InsertMemoryNode, with an extra validation that the
// referenced node is actually kind='memory'.

import (
	"fmt"
	"time"

	"github.com/zate/ctx/internal/db"
)

// validMemoryTypes is the set of allowed type values for promoted memory nodes.
// Mirrors the validNodeTypes map in internal/db/nodes.go.
var validMemoryTypes = map[string]bool{
	"fact":          true,
	"decision":      true,
	"pattern":       true,
	"observation":   true,
	"hypothesis":    true,
	"task":          true,
	"summary":       true,
	"source":        true,
	"open-question": true,
}

// PromoteNode promotes the node identified by nodeID from kind='content' to
// kind='memory', setting its type to memType.
//
// Preconditions:
//   - nodeID must exist and have kind='content'.
//   - memType must be a valid memory node type.
//
// Effects:
//   - Updates nodes SET kind='memory', type=memType, updated_at=now.
//   - The AFTER UPDATE FTS trigger (migration v5) automatically adds the node
//     to nodes_fts because NEW.kind = 'memory'.
//   - All existing CONTAINS edges for this node are preserved (only the node
//     row is updated, not the edges).
//
// Returns an error if any precondition fails or if the DB update fails.
func PromoteNode(nodeID, memType string, store db.Store) error {
	// Validate type.
	if !validMemoryTypes[memType] {
		return fmt.Errorf("doc.PromoteNode: invalid memory type %q (must be one of: fact, decision, pattern, observation, hypothesis, task, summary, source, open-question)", memType)
	}

	// Fetch existing node to validate kind.
	n, err := store.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("doc.PromoteNode: get node %q: %w", nodeID, err)
	}

	if n.Kind != db.NodeKindContent {
		return fmt.Errorf("doc.PromoteNode: node %q has kind=%q; only kind='content' nodes can be promoted (not already memory or document)", nodeID, n.Kind)
	}

	// Perform the update in a transaction for safety.
	tx, err := store.Begin()
	if err != nil {
		return fmt.Errorf("doc.PromoteNode: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(
		`UPDATE nodes SET kind = ?, type = ?, updated_at = ? WHERE id = ? AND kind = ?`,
		db.NodeKindMemory, memType, now, nodeID, db.NodeKindContent,
	)
	if err != nil {
		return fmt.Errorf("doc.PromoteNode: update node: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("doc.PromoteNode: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("doc.PromoteNode: no rows updated for node %q (already promoted or not found)", nodeID)
	}

	return tx.Commit()
}

// InlineNode inserts an existing kind='memory' node into docID at toPos by
// creating a CONTAINS edge. The memory node's kind is NOT changed.
//
// This is a validation wrapper over InsertMemoryNode. The only extra check
// is that the referenced memNodeID must have kind='memory'.
//
// Preconditions:
//   - docID must exist and be a valid document node.
//   - memNodeID must exist and have kind='memory'.
//   - toPos is 1-indexed; clamped to [1, len(siblings)+1].
//
// Returns an error if the node does not exist, is not kind='memory', or if the
// CONTAINS edge cannot be created.
func InlineNode(docID, memNodeID string, toPos int, store db.Store) error {
	// Validate the memory node exists and has kind='memory'.
	n, err := store.GetNode(memNodeID)
	if err != nil {
		return fmt.Errorf("doc.InlineNode: get node %q: %w", memNodeID, err)
	}

	if n.Kind != db.NodeKindMemory {
		return fmt.Errorf("doc.InlineNode: node %q has kind=%q; InlineNode requires kind='memory' (use 'ctx doc promote' to promote content nodes first)", memNodeID, n.Kind)
	}

	// Validate the document exists and is kind='document'.
	docNode, err := store.GetNode(docID)
	if err != nil {
		return fmt.Errorf("doc.InlineNode: get document %q: %w", docID, err)
	}
	if docNode.Kind != db.NodeKindDocument {
		return fmt.Errorf("doc.InlineNode: target %q has kind=%q, want kind='document'", docID, docNode.Kind)
	}

	// Delegate to InsertMemoryNode which handles position computation and edge creation.
	if err := InsertMemoryNode(docID, memNodeID, toPos, store); err != nil {
		return fmt.Errorf("doc.InlineNode: %w", err)
	}

	return nil
}
