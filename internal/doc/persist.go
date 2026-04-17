package doc

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zate/ctx/internal/db"
)

// Persist writes the DocTree to the store transactionally:
//   - One document node (kind='document') with src_hash in metadata.
//   - One content node (kind='content') per DocNode in the tree (depth-first).
//   - One CONTAINS edge per content node: from_id=docID, to_id=contentID,
//     document_id=docID, position=<strictly-increasing>.
//
// On any error the transaction is rolled back.
// Returns the document node ID on success.
func Persist(tree *DocTree, src []byte, store db.Store) (string, error) {
	h := sha256.Sum256(src)
	hashHex := fmt.Sprintf("%x", h)

	metaBytes, err := json.Marshal(map[string]string{
		"src_hash": hashHex,
	})
	if err != nil {
		return "", fmt.Errorf("doc.Persist: marshal metadata: %w", err)
	}

	tx, err := store.Begin()
	if err != nil {
		return "", fmt.Errorf("doc.Persist: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	docID := db.NewID()

	// Insert document node.
	_, err = tx.Exec(
		`INSERT INTO nodes (id, type, kind, content, token_estimate, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		docID, "fact", db.NodeKindDocument,
		"document:"+docID, // placeholder content (document nodes have no renderable body)
		0, now, now,
		string(metaBytes),
	)
	if err != nil {
		return "", fmt.Errorf("doc.Persist: insert document node: %w", err)
	}

	// Flatten the tree depth-first to get all content nodes in order.
	allNodes := flattenChildren(tree)

	// Assign positions: multiples of 10 to leave room for future inserts.
	for i, n := range allNodes {
		contentID := db.NewID()
		position := (i + 1) * 10

		_, err = tx.Exec(
			`INSERT INTO nodes (id, type, kind, content, token_estimate, created_at, updated_at, metadata)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
			contentID, "fact", db.NodeKindContent,
			string(n.Body),
			estimateTokens(n.Body),
			now, now,
		)
		if err != nil {
			return "", fmt.Errorf("doc.Persist: insert content node %d: %w", i, err)
		}

		edgeID := db.NewID()
		_, err = tx.Exec(
			`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
			 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
			edgeID, docID, contentID, now, docID, position,
		)
		if err != nil {
			return "", fmt.Errorf("doc.Persist: insert edge for content node %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("doc.Persist: commit: %w", err)
	}

	return docID, nil
}

// ComposeFromStore re-assembles the document for docID by reading all CONTAINS
// edges in position order and concatenating content node bodies.
// This is the minimal Phase 2 implementation; Phase 3 will expand it.
func ComposeFromStore(docID string, store db.Store) ([]byte, error) {
	rows, err := store.Query(
		`SELECT n.content
		 FROM edges e
		 JOIN nodes n ON n.id = e.to_id
		 WHERE e.from_id = ? AND e.type = 'CONTAINS' AND e.document_id = ?
		 ORDER BY e.position ASC`,
		docID, docID,
	)
	if err != nil {
		return nil, fmt.Errorf("doc.ComposeFromStore: query edges: %w", err)
	}
	defer rows.Close()

	var result []byte
	for rows.Next() {
		var content sql.NullString
		if err := rows.Scan(&content); err != nil {
			return nil, fmt.Errorf("doc.ComposeFromStore: scan: %w", err)
		}
		if content.Valid {
			result = append(result, []byte(content.String)...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("doc.ComposeFromStore: rows: %w", err)
	}

	return result, nil
}

// estimateTokens provides a rough token count for content bytes.
// Uses a simple word-count heuristic consistent with the project's token package.
func estimateTokens(body []byte) int {
	words := 0
	inWord := false
	for _, b := range body {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			inWord = false
		} else if !inWord {
			words++
			inWord = true
		}
	}
	return words
}
