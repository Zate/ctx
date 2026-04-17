package db

import (
	"database/sql"
	"fmt"
	"time"
)

var validEdgeTypes = map[string]bool{
	"DERIVED_FROM": true,
	"DEPENDS_ON":   true,
	"SUPERSEDES":   true,
	"RELATES_TO":   true,
	"CHILD_OF":     true,
	"CONTAINS":     true, // document-scoped edge: document→content nodes
}

type Edge struct {
	ID        string    `json:"id"`
	FromID    string    `json:"from_id"`
	ToID      string    `json:"to_id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  string    `json:"metadata"`
}

func (d *SQLiteStore) CreateEdge(fromID, toID, edgeType string) (*Edge, error) {
	if !validEdgeTypes[edgeType] {
		return nil, fmt.Errorf("invalid edge type: %s", edgeType)
	}

	// Check that both nodes exist
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = ?", fromID).Scan(&count)
	if err != nil || count == 0 {
		return nil, fmt.Errorf("from node %s not found", fromID)
	}
	err = d.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = ?", toID).Scan(&count)
	if err != nil || count == 0 {
		return nil, fmt.Errorf("to node %s not found", toID)
	}

	id := NewID()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	_, err = d.db.Exec(`INSERT OR IGNORE INTO edges (id, from_id, to_id, type, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, '{}')`, id, fromID, toID, edgeType, nowStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create edge: %w", err)
	}

	return &Edge{
		ID:        id,
		FromID:    fromID,
		ToID:      toID,
		Type:      edgeType,
		CreatedAt: now,
		Metadata:  "{}",
	}, nil
}

func (d *SQLiteStore) DeleteEdge(fromID, toID string, edgeType string) error {
	query := "DELETE FROM edges WHERE from_id = ? AND to_id = ?"
	args := []interface{}{fromID, toID}
	if edgeType != "" {
		query += " AND type = ?"
		args = append(args, edgeType)
	}
	_, err := d.db.Exec(query, args...)
	return err
}

func (d *SQLiteStore) GetEdges(nodeID string, direction string) ([]*Edge, error) {
	var query string
	switch direction {
	case "out":
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE from_id = ?"
	case "in":
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE to_id = ?"
	default: // "both" or ""
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE from_id = ? OR to_id = ?"
	}

	var args []interface{}
	if direction == "out" || direction == "in" {
		args = []interface{}{nodeID}
	} else {
		args = []interface{}{nodeID, nodeID}
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get edges: %w", err)
	}
	defer rows.Close()

	return scanEdges(rows)
}

func (d *SQLiteStore) GetEdgesFrom(nodeID string) ([]*Edge, error) {
	return d.GetEdges(nodeID, "out")
}

func (d *SQLiteStore) GetEdgesTo(nodeID string) ([]*Edge, error) {
	return d.GetEdges(nodeID, "in")
}

func scanEdges(rows *sql.Rows) ([]*Edge, error) {
	var edges []*Edge
	for rows.Next() {
		e := &Edge{}
		var createdAt string
		err := rows.Scan(&e.ID, &e.FromID, &e.ToID, &e.Type, &createdAt, &e.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan edge: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		edges = append(edges, e)
	}
	return edges, nil
}
