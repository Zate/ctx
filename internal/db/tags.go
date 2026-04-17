package db

import (
	"fmt"
	"time"
)

func (d *SQLiteStore) AddTag(nodeID, tag string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`INSERT OR IGNORE INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
		nodeID, tag, now)
	if err != nil {
		return fmt.Errorf("failed to add tag: %w", err)
	}
	return nil
}

func (d *SQLiteStore) RemoveTag(nodeID, tag string) error {
	_, err := d.db.Exec("DELETE FROM tags WHERE node_id = ? AND tag = ?", nodeID, tag)
	if err != nil {
		return fmt.Errorf("failed to remove tag: %w", err)
	}
	return nil
}

func (d *SQLiteStore) GetTags(nodeID string) ([]string, error) {
	rows, err := d.db.Query("SELECT tag FROM tags WHERE node_id = ? ORDER BY tag", nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (d *SQLiteStore) ListAllTags() ([]string, error) {
	// Only return tags belonging to kind='memory' nodes.
	rows, err := d.db.Query(`SELECT DISTINCT t.tag FROM tags t
		JOIN nodes n ON t.node_id = n.id
		WHERE n.kind = 'memory'
		ORDER BY t.tag`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (d *SQLiteStore) ListTagsByPrefix(prefix string) ([]string, error) {
	// Only return tags belonging to kind='memory' nodes.
	rows, err := d.db.Query(`SELECT DISTINCT t.tag FROM tags t
		JOIN nodes n ON t.node_id = n.id
		WHERE n.kind = 'memory' AND t.tag LIKE ?
		ORDER BY t.tag`, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("failed to list tags by prefix: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (d *SQLiteStore) GetNodesByTag(tag string) ([]*Node, error) {
	return d.ListMemoryNodes(ListOptions{Tag: tag})
}
