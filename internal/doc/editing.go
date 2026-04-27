package doc

// Editing primitives: mv, insert, remove, fork, split.
//
// Design:
//   - MvNode, InsertNode, RemoveNode: operate on CONTAINS edges only.
//     Construct a modified Scaffold and call ApplyScaffold where natural;
//     use direct SQL for operations that don't map cleanly to the scaffold diff.
//   - ForkDoc: create a new kind='document' node + copy all CONTAINS edges
//     with the new document_id. Verify compose(forkID) == compose(origID).
//   - SplitNode: the only primitive that creates new content nodes. Splits
//     a content node at a byte offset into two new sibling content nodes.
//     The original node body is left intact (it may be referenced by other
//     docs). UTF-8 safety: reject any offset that lands on a continuation byte.
//
// Invariants preserved:
//   - CONTAINS edge positions are strictly increasing (i*10 gap).
//   - Content node bodies are never mutated (split creates NEW nodes).
//   - Cross-doc moves are rejected.
//   - split at offset=0, offset=len(body), or mid-UTF-8 is rejected.

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/zate/ctx/internal/db"
)

// ---------------------------------------------------------------------------
// MvNode: reparent (reorder) a content node within the same document.
//
// Finds the CONTAINS edge for nodeID in docID, removes it from its current
// position, and re-inserts it at toPos (1-indexed position rank).
// Sibling positions are renumbered i*10 after the move.
//
// Errors:
//   - nodeID not in docID (cross-doc move detected) → "cross-doc"
//   - toPos out of range → error
// ---------------------------------------------------------------------------

func MvNode(docID, nodeID string, toPos int, store db.Store) error {
	// 1. Load current content node IDs in position order.
	ids := loadContainsOrder(docID, store)

	// 2. Check that nodeID is actually in this document.
	found := false
	for _, id := range ids {
		if id == nodeID {
			found = true
			break
		}
	}
	if !found {
		// The node may exist in another document — report cross-doc error.
		return fmt.Errorf("doc.MvNode: cross-doc move rejected: node %q is not in document %q", nodeID, docID)
	}

	// 3. Build new order by removing nodeID and inserting at toPos.
	newOrder := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != nodeID {
			newOrder = append(newOrder, id)
		}
	}

	if toPos < 1 {
		toPos = 1
	}
	if toPos > len(newOrder)+1 {
		toPos = len(newOrder) + 1
	}

	// Insert nodeID at toPos-1 (0-indexed).
	insertIdx := toPos - 1
	newOrder = append(newOrder, "")
	copy(newOrder[insertIdx+1:], newOrder[insertIdx:])
	newOrder[insertIdx] = nodeID

	// 4. Apply the new order directly via position updates.
	// We use direct SQL (not ApplyScaffold) because MvNode deals with nodes
	// already confirmed to be in the doc — no kind validation needed.
	return applyPositions(docID, newOrder, store)
}

// InsertNode inserts a kind='content' (or memory) node into docID at toPos.
// The node must already exist in the store. After insertion, all sibling
// positions are renumbered i*10.
func InsertNode(docID, nodeID string, toPos int, store db.Store) error {
	return insertNodeInternal(docID, nodeID, toPos, false, store)
}

// InsertMemoryNode inserts a kind='memory' node into docID at toPos. The
// node's kind is NOT changed — kind promotion is a separate explicit
// operation (PromoteNode/InlineNode in promotion.go) so the memory node
// remains visible to the memory recall surface unless promotion is
// requested.
func InsertMemoryNode(docID, nodeID string, toPos int, store db.Store) error {
	return insertNodeInternal(docID, nodeID, toPos, true, store)
}

func insertNodeInternal(docID, nodeID string, toPos int, allowMemory bool, store db.Store) error {
	// Validate the node exists and has the right kind.
	n, err := store.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("doc.InsertNode: get node %q: %w", nodeID, err)
	}
	if n.Kind != db.NodeKindContent && n.Kind != db.NodeKindDocument {
		if !allowMemory || n.Kind != db.NodeKindMemory {
			return fmt.Errorf("doc.InsertNode: node %q has kind=%q; expected content or memory", nodeID, n.Kind)
		}
	}

	// Load current order (already in this doc).
	ids := loadContainsOrder(docID, store)

	// Build new order with nodeID inserted at toPos.
	if toPos < 1 {
		toPos = 1
	}
	if toPos > len(ids)+1 {
		toPos = len(ids) + 1
	}

	insertIdx := toPos - 1
	newOrder := make([]string, 0, len(ids)+1)
	newOrder = append(newOrder, ids[:insertIdx]...)
	newOrder = append(newOrder, nodeID)
	newOrder = append(newOrder, ids[insertIdx:]...)

	return applyPositionsWithMemory(docID, newOrder, store)
}

// RemoveNode removes the CONTAINS edge for nodeID in docID.
// The content node itself is NOT deleted (it may be referenced by other docs).
//
// If nodeID has descendant CONTAINS edges (from_id=nodeID in the same doc),
// and recursive=false, an error is returned.
// If recursive=true, all descendant CONTAINS edges are removed too.
func RemoveNode(docID, nodeID string, recursive bool, store db.Store) error {
	// Check for descendant CONTAINS edges from nodeID within this document.
	var descCount int
	row := store.QueryRow(
		`SELECT COUNT(*) FROM edges WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?`,
		nodeID, docID,
	)
	if err := row.Scan(&descCount); err != nil {
		return fmt.Errorf("doc.RemoveNode: count descendants: %w", err)
	}

	if descCount > 0 && !recursive {
		return fmt.Errorf("doc.RemoveNode: node %q has %d descendant edge(s) in document %q; use --recursive to remove", nodeID, descCount, docID)
	}

	tx, err := store.Begin()
	if err != nil {
		return fmt.Errorf("doc.RemoveNode: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if recursive && descCount > 0 {
		// Remove all edges from nodeID (descendant edges) in this doc.
		if _, err := tx.Exec(
			`DELETE FROM edges WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?`,
			nodeID, docID,
		); err != nil {
			return fmt.Errorf("doc.RemoveNode: delete descendant edges: %w", err)
		}
	}

	// Remove the CONTAINS edge pointing TO nodeID within this doc.
	if _, err := tx.Exec(
		`DELETE FROM edges WHERE to_id = ? AND type = 'CONTAINS' AND document_id = ?`,
		nodeID, docID,
	); err != nil {
		return fmt.Errorf("doc.RemoveNode: delete edge for node %q: %w", nodeID, err)
	}

	return tx.Commit()
}

// ForkDoc creates a new kind='document' node and copies all CONTAINS edges
// from docID to new edges with document_id=forkID (same content node IDs,
// same positions). Returns the new document ID.
//
// The fork is an independent copy of the edge structure; mutations to one
// document's CONTAINS edges do not affect the other.
func ForkDoc(docID string, store db.Store) (string, error) {
	// 1. Verify source document.
	origNode, err := store.GetNode(docID)
	if err != nil {
		return "", fmt.Errorf("doc.ForkDoc: get source document: %w", err)
	}
	if origNode.Kind != db.NodeKindDocument {
		return "", fmt.Errorf("doc.ForkDoc: node %q has kind=%q, want %q", docID, origNode.Kind, db.NodeKindDocument)
	}

	// 2. Load original CONTAINS edges.
	rows, err := store.Query(
		`SELECT to_id, position FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?
		 ORDER BY position ASC`,
		docID, docID,
	)
	if err != nil {
		return "", fmt.Errorf("doc.ForkDoc: query edges: %w", err)
	}

	type edgeRow struct {
		toID     string
		position int
	}
	var edges []edgeRow
	for rows.Next() {
		var e edgeRow
		if err := rows.Scan(&e.toID, &e.position); err != nil {
			rows.Close()
			return "", fmt.Errorf("doc.ForkDoc: scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("doc.ForkDoc: rows: %w", err)
	}

	// 3. Begin transaction.
	tx, err := store.Begin()
	if err != nil {
		return "", fmt.Errorf("doc.ForkDoc: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// 4. Create new document node.
	forkID := db.NewID()
	if _, err := tx.Exec(
		`INSERT INTO nodes (id, type, kind, content, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		forkID, "fact", db.NodeKindDocument,
		"document:"+forkID,
		0, now, now,
		origNode.Metadata, // copy src_hash metadata
	); err != nil {
		return "", fmt.Errorf("doc.ForkDoc: insert fork document node: %w", err)
	}

	// 5. Copy CONTAINS edges with new document_id=forkID.
	for _, e := range edges {
		edgeID := db.NewID()
		if _, err := tx.Exec(
			`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
			 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
			edgeID, forkID, e.toID, now, forkID, e.position,
		); err != nil {
			return "", fmt.Errorf("doc.ForkDoc: insert CONTAINS edge for %q: %w", e.toID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("doc.ForkDoc: commit: %w", err)
	}

	return forkID, nil
}

// SplitNode splits the content node nodeID at byte offset splitAt within docID.
//
// The operation:
//   - Validates nodeID belongs to docID and offset is valid.
//   - Creates two new kind='content' nodes: body[:splitAt] and body[splitAt:].
//   - Inserts two new CONTAINS edges at the same position slot as the original
//     (first half at old_pos, second half at old_pos + position_gap/2).
//   - Removes the old CONTAINS edge (the original content node body is preserved).
//
// Rejections:
//   - offset == 0 or offset == len(body): empty half would be produced.
//   - offset lands on a UTF-8 continuation byte.
func SplitNode(docID, nodeID string, splitAt int, store db.Store) error {
	// 1. Verify node is in this document and get its edge position.
	var oldEdgeID string
	var oldPosition int
	row := store.QueryRow(
		`SELECT id, position FROM edges
		 WHERE from_id = ? AND to_id = ? AND type = 'CONTAINS' AND document_id = ?`,
		docID, nodeID, docID,
	)
	if err := row.Scan(&oldEdgeID, &oldPosition); err != nil {
		return fmt.Errorf("doc.SplitNode: node %q not found in document %q: %w", nodeID, docID, err)
	}

	// 2. Get node body.
	n, err := store.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("doc.SplitNode: get node %q: %w", nodeID, err)
	}
	body := []byte(n.Content)

	// 3. Validate offset.
	if splitAt <= 0 {
		return fmt.Errorf("doc.SplitNode: offset %d is invalid (must be > 0)", splitAt)
	}
	if splitAt >= len(body) {
		return fmt.Errorf("doc.SplitNode: offset %d is invalid (must be < len(body)=%d)", splitAt, len(body))
	}

	// 4. UTF-8 safety: reject if splitAt lands on a continuation byte.
	if !utf8.RuneStart(body[splitAt]) {
		return fmt.Errorf("doc.SplitNode: offset %d splits a multi-byte UTF-8 codepoint", splitAt)
	}

	firstHalf := body[:splitAt]
	secondHalf := body[splitAt:]

	// 5. Determine positions for the two new edges.
	// Find the next sibling's position so we can fit two edges between
	// old_position and next_position. If no next sibling, use old_pos + 5.
	var nextPos int
	nextRow := store.QueryRow(
		`SELECT COALESCE(MIN(position), 0) FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ? AND position > ?`,
		docID, docID, oldPosition,
	)
	if err := nextRow.Scan(&nextPos); err != nil {
		nextPos = 0
	}

	var pos1, pos2 int
	if nextPos == 0 {
		// No next sibling — use old_pos and old_pos+5.
		pos1 = oldPosition
		pos2 = oldPosition + 5
	} else {
		gap := nextPos - oldPosition
		if gap >= 2 {
			pos1 = oldPosition
			pos2 = oldPosition + gap/2
		} else {
			// Not enough room in gap — renumber all positions after applying split.
			// Use temporary large values and renumber afterwards.
			pos1 = oldPosition
			pos2 = oldPosition + 1
		}
	}

	// 6. Begin transaction.
	tx, err := store.Begin()
	if err != nil {
		return fmt.Errorf("doc.SplitNode: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// 7. Create two new content nodes.
	node1ID := db.NewID()
	node2ID := db.NewID()

	for _, args := range []struct {
		id   string
		body []byte
	}{
		{node1ID, firstHalf},
		{node2ID, secondHalf},
	} {
		if _, err := tx.Exec(
			`INSERT INTO nodes (id, type, kind, content, token_estimate, created_at, updated_at, metadata)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
			args.id, "fact", db.NodeKindContent,
			string(args.body),
			estimateTokens(args.body),
			now, now,
		); err != nil {
			return fmt.Errorf("doc.SplitNode: insert new content node: %w", err)
		}
	}

	// 8. Delete old CONTAINS edge.
	if _, err := tx.Exec(`DELETE FROM edges WHERE id = ?`, oldEdgeID); err != nil {
		return fmt.Errorf("doc.SplitNode: delete old edge: %w", err)
	}

	// 9. Insert two new CONTAINS edges.
	edge1ID := db.NewID()
	if _, err := tx.Exec(
		`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
		 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
		edge1ID, docID, node1ID, now, docID, pos1,
	); err != nil {
		return fmt.Errorf("doc.SplitNode: insert first half edge: %w", err)
	}

	edge2ID := db.NewID()
	if _, err := tx.Exec(
		`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
		 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
		edge2ID, docID, node2ID, now, docID, pos2,
	); err != nil {
		return fmt.Errorf("doc.SplitNode: insert second half edge: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("doc.SplitNode: commit: %w", err)
	}

	// 10. If positions weren't distinct (pos1 == pos2 or conflict), renumber.
	if pos1 >= pos2 {
		return renumberPositions(docID, store)
	}

	return nil
}

// CreateContentNode creates a new kind='content' node with the given body
// and returns its ID. Used by tests and callers that need to manufacture
// a standalone content chunk for insertion.
func CreateContentNode(body string, store db.Store) (string, error) {
	if body == "" {
		return "", fmt.Errorf("doc.CreateContentNode: body cannot be empty")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	nodeID := db.NewID()
	if _, err := store.Exec(
		`INSERT INTO nodes (id, type, kind, content, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
		nodeID, "fact", db.NodeKindContent,
		body,
		estimateTokens([]byte(body)),
		now, now,
	); err != nil {
		return "", fmt.Errorf("doc.CreateContentNode: insert: %w", err)
	}
	return nodeID, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// loadContainsOrder returns content node IDs for docID in position order.
// Used internally; no error return — returns nil on failure (callers check length).
func loadContainsOrder(docID string, store db.Store) []string {
	rows, err := store.Query(
		`SELECT to_id FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?
		 ORDER BY position ASC`,
		docID, docID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil
		}
		ids = append(ids, id)
	}
	return ids
}

// applyPositions updates CONTAINS edge positions for docID to match newOrder.
// Assigns positions (i+1)*10. This is a minimal, direct SQL update — it does
// NOT validate node kinds (callers do that). Works for any mix of content/memory.
func applyPositions(docID string, newOrder []string, store db.Store) error {
	return applyPositionsWithMemory(docID, newOrder, store)
}

// applyPositionsWithMemory writes CONTAINS edge positions for docID = newOrder.
// Inserts edges that don't exist; updates positions for edges that do.
func applyPositionsWithMemory(docID string, newOrder []string, store db.Store) error {
	// Load current edges (edge_id, to_id) for this doc.
	rows, err := store.Query(
		`SELECT id, to_id, position FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?`,
		docID, docID,
	)
	if err != nil {
		return fmt.Errorf("doc.applyPositions: query: %w", err)
	}

	type edgeInfo struct {
		edgeID   string
		position int
	}
	currentByToID := make(map[string]edgeInfo)
	for rows.Next() {
		var eID, toID string
		var pos int
		if err := rows.Scan(&eID, &toID, &pos); err != nil {
			rows.Close()
			return fmt.Errorf("doc.applyPositions: scan: %w", err)
		}
		currentByToID[toID] = edgeInfo{edgeID: eID, position: pos}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("doc.applyPositions: rows: %w", err)
	}

	// Build desired set.
	desiredSet := make(map[string]int, len(newOrder))
	for i, id := range newOrder {
		desiredSet[id] = (i + 1) * 10
	}

	tx, err := store.Begin()
	if err != nil {
		return fmt.Errorf("doc.applyPositions: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// Delete edges no longer in desired set.
	for toID, info := range currentByToID {
		if _, ok := desiredSet[toID]; !ok {
			if _, err := tx.Exec(`DELETE FROM edges WHERE id = ?`, info.edgeID); err != nil {
				return fmt.Errorf("doc.applyPositions: delete edge: %w", err)
			}
		}
	}

	// Update or insert.
	for i, id := range newOrder {
		wantPos := (i + 1) * 10
		if info, exists := currentByToID[id]; exists {
			if info.position != wantPos {
				if _, err := tx.Exec(`UPDATE edges SET position = ? WHERE id = ?`, wantPos, info.edgeID); err != nil {
					return fmt.Errorf("doc.applyPositions: update position: %w", err)
				}
			}
		} else {
			edgeID := db.NewID()
			if _, err := tx.Exec(
				`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
				 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
				edgeID, docID, id, now, docID, wantPos,
			); err != nil {
				return fmt.Errorf("doc.applyPositions: insert edge for %q: %w", id, err)
			}
		}
	}

	return tx.Commit()
}

// renumberPositions renumbers all CONTAINS edges for docID with positions i*10.
// Called after split when position slots are tight.
func renumberPositions(docID string, store db.Store) error {
	ids := loadContainsOrder(docID, store)
	return applyPositionsWithMemory(docID, ids, store)
}
