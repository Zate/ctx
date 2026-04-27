package doc

// ComposeDoc loads a document node and all its CONTAINS edges, sorts the
// content nodes by position, and concatenates their bodies in depth-first
// (position) order to reconstruct the original source bytes.

import (
	"database/sql"
	"fmt"

	"github.com/zate/ctx/internal/db"
)

// ComposeDoc reassembles the document with the given docID from the store.
// It reads all CONTAINS edges for the document ordered by position and
// concatenates the content node bodies.
// Returns an error if the document is not found or if any DB operation fails.
func ComposeDoc(docID string, store db.Store) ([]byte, error) {
	// Verify the document node exists.
	node, err := store.GetNode(docID)
	if err != nil {
		return nil, fmt.Errorf("doc.ComposeDoc: get document node: %w", err)
	}
	if node.Kind != db.NodeKindDocument {
		return nil, fmt.Errorf("doc.ComposeDoc: node %q has kind=%q, want %q", docID, node.Kind, db.NodeKindDocument)
	}

	// Load all content nodes for this document via CONTAINS edges, ordered by position.
	rows, err := store.Query(
		`SELECT n.content
		 FROM edges e
		 JOIN nodes n ON n.id = e.to_id
		 WHERE e.from_id = ? AND e.type = 'CONTAINS' AND e.document_id = ?
		 ORDER BY e.position ASC`,
		docID, docID,
	)
	if err != nil {
		return nil, fmt.Errorf("doc.ComposeDoc: query edges: %w", err)
	}
	defer rows.Close()

	var result []byte
	for rows.Next() {
		var content sql.NullString
		if err := rows.Scan(&content); err != nil {
			return nil, fmt.Errorf("doc.ComposeDoc: scan: %w", err)
		}
		if content.Valid {
			result = append(result, []byte(content.String)...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("doc.ComposeDoc: rows: %w", err)
	}

	if result == nil {
		result = []byte{}
	}
	return result, nil
}
