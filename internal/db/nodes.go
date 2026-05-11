package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zate/ctx/internal/token"
)

var validNodeTypes = map[string]bool{
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

type Node struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Kind          string    `json:"kind,omitempty"` // "memory" (default), "document", "content"
	Content       string    `json:"content"`
	Summary       *string   `json:"summary,omitempty"`
	TokenEstimate int       `json:"token_estimate"`
	SupersededBy  *string   `json:"superseded_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Metadata      string    `json:"metadata"`
	Tags          []string  `json:"tags,omitempty"`
}

// NodeKindMemory is the default kind for regular memory nodes.
const NodeKindMemory = "memory"

// NodeKindDocument is the kind for document container nodes.
const NodeKindDocument = "document"

// NodeKindContent is the kind for document chunk nodes.
const NodeKindContent = "content"

type CreateNodeInput struct {
	Type     string
	Kind     string // defaults to "memory" if empty
	Content  string
	Summary  *string
	Metadata string
	Tags     []string
}

type UpdateNodeInput struct {
	Content  *string
	Type     *string
	Summary  *string
	Metadata *string
}

type ListOptions struct {
	Type              string
	Tag               string   // Deprecated: use Tags for multi-tag filtering
	Tags              []string // Filter by multiple tags (AND logic)
	Since             *time.Time
	Limit             int
	IncludeSuperseded bool
}

func NewID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

func (d *SQLiteStore) CreateNode(input CreateNodeInput) (*Node, error) {
	if !validNodeTypes[input.Type] {
		return nil, fmt.Errorf("invalid node type: %s", input.Type)
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}

	kind := input.Kind
	if kind == "" {
		kind = NodeKindMemory
	}

	id := NewID()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	tokenEst := token.Estimate(input.Content)
	metadata := input.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	tx, err := d.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var summary sql.NullString
	if input.Summary != nil {
		summary = sql.NullString{String: *input.Summary, Valid: true}
	}

	_, err = tx.Exec(`INSERT INTO nodes (id, type, kind, content, summary, token_estimate, created_at, updated_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Type, kind, input.Content, summary, tokenEst, nowStr, nowStr, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}

	for _, tag := range input.Tags {
		_, err = tx.Exec(`INSERT OR IGNORE INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)`,
			id, tag, nowStr)
		if err != nil {
			return nil, fmt.Errorf("failed to add tag %s: %w", tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return &Node{
		ID:            id,
		Type:          input.Type,
		Kind:          kind,
		Content:       input.Content,
		Summary:       input.Summary,
		TokenEstimate: tokenEst,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata:      metadata,
		Tags:          input.Tags,
	}, nil
}

// FindByTypeAndContent returns an existing active (non-superseded) node with
// matching type and content, or nil if none exists.
func (d *SQLiteStore) FindByTypeAndContent(nodeType, content string) (*Node, error) {
	var id string
	err := d.db.QueryRow(
		`SELECT id FROM nodes WHERE type = ? AND content = ? AND superseded_by IS NULL LIMIT 1`,
		nodeType, content).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find node: %w", err)
	}
	return d.GetNode(id)
}

// ResolveID resolves a node ID prefix to a full ID.
// If the input is already a full ULID (26 chars), it's returned as-is after validation.
// For shorter prefixes, it finds the unique matching node.
// Returns ErrNotFound if no match, or an error if multiple nodes match the prefix.
func (d *SQLiteStore) ResolveID(prefix string) (string, error) {
	if len(prefix) == 26 {
		var id string
		err := d.db.QueryRow("SELECT id FROM nodes WHERE id = ?", prefix).Scan(&id)
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		if err != nil {
			return "", fmt.Errorf("failed to resolve ID: %w", err)
		}
		return id, nil
	}
	if len(prefix) == 0 {
		return "", fmt.Errorf("empty ID prefix")
	}

	rows, err := d.db.Query("SELECT id FROM nodes WHERE id LIKE ? LIMIT 2", prefix+"%")
	if err != nil {
		return "", fmt.Errorf("failed to resolve ID prefix: %w", err)
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("failed to scan ID: %w", err)
		}
		matches = append(matches, id)
	}

	switch len(matches) {
	case 0:
		return "", ErrNotFound
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous ID prefix %q: matches %s and %s", prefix, matches[0], matches[1])
	}
}

func (d *SQLiteStore) GetNode(id string) (*Node, error) {
	node := &Node{}
	var summary, supersededBy sql.NullString
	var createdAt, updatedAt string

	err := d.db.QueryRow(`SELECT id, type, kind, content, summary, token_estimate, superseded_by, created_at, updated_at, metadata
		FROM nodes WHERE id = ?`, id).Scan(
		&node.ID, &node.Type, &node.Kind, &node.Content, &summary, &node.TokenEstimate,
		&supersededBy, &createdAt, &updatedAt, &node.Metadata)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	if summary.Valid {
		node.Summary = &summary.String
	}
	if supersededBy.Valid {
		node.SupersededBy = &supersededBy.String
	}
	node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	tags, err := d.GetTags(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}
	node.Tags = tags

	return node, nil
}

func (d *SQLiteStore) UpdateNode(id string, input UpdateNodeInput) (*Node, error) {
	// Check node exists
	existing, err := d.GetNode(id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	content := existing.Content
	nodeType := existing.Type
	metadata := existing.Metadata
	summary := existing.Summary

	if input.Content != nil {
		content = *input.Content
	}
	if input.Type != nil {
		if !validNodeTypes[*input.Type] {
			return nil, fmt.Errorf("invalid node type: %s", *input.Type)
		}
		nodeType = *input.Type
	}
	if input.Metadata != nil {
		metadata = *input.Metadata
	}
	if input.Summary != nil {
		summary = input.Summary
	}

	tokenEst := token.Estimate(content)

	var summaryVal sql.NullString
	if summary != nil {
		summaryVal = sql.NullString{String: *summary, Valid: true}
	}

	_, err = d.db.Exec(`UPDATE nodes SET type=?, content=?, summary=?, token_estimate=?, updated_at=?, metadata=?
		WHERE id=?`, nodeType, content, summaryVal, tokenEst, nowStr, metadata, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}

	return d.GetNode(id)
}

func (d *SQLiteStore) DeleteNode(id string) error {
	result, err := d.db.Exec("DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *SQLiteStore) listNodesWithKindFilter(opts ListOptions, kindFilter string) ([]*Node, error) {
	query := `SELECT n.id, n.type, n.kind, n.content, n.summary, n.token_estimate, n.superseded_by, n.created_at, n.updated_at, n.metadata
		FROM nodes n`
	var conditions []string
	var args []interface{}

	if !opts.IncludeSuperseded {
		conditions = append(conditions, "n.superseded_by IS NULL")
	}
	if kindFilter != "" {
		conditions = append(conditions, "n.kind = ?")
		args = append(args, kindFilter)
	}
	if opts.Type != "" {
		conditions = append(conditions, "n.type = ?")
		args = append(args, opts.Type)
	}
	// Merge single Tag into Tags for backwards compatibility
	tagList := opts.Tags
	if opts.Tag != "" {
		tagList = append(tagList, opts.Tag)
	}
	for i, tag := range tagList {
		alias := fmt.Sprintf("t%d", i)
		query += fmt.Sprintf(" JOIN tags %s ON n.id = %s.node_id", alias, alias)
		conditions = append(conditions, fmt.Sprintf("%s.tag = ?", alias))
		args = append(args, tag)
	}
	if opts.Since != nil {
		conditions = append(conditions, "n.created_at >= ?")
		args = append(args, opts.Since.UTC().Format(time.RFC3339))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY n.created_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		node := &Node{}
		var summary, supersededBy sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(&node.ID, &node.Type, &node.Kind, &node.Content, &summary, &node.TokenEstimate,
			&supersededBy, &createdAt, &updatedAt, &node.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}

		if summary.Valid {
			node.Summary = &summary.String
		}
		if supersededBy.Valid {
			node.SupersededBy = &supersededBy.String
		}
		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		tags, _ := d.GetTags(node.ID)
		node.Tags = tags
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// ListNodes returns all nodes (no kind filter). For memory-path surfaces,
// prefer ListMemoryNodes which implicitly scopes to kind='memory'.
func (d *SQLiteStore) ListNodes(opts ListOptions) ([]*Node, error) {
	return d.listNodesWithKindFilter(opts, "")
}

// ListMemoryNodes returns only kind='memory' nodes (the safe default for all
// memory-path surfaces: list, status, tags, query, view).
func (d *SQLiteStore) ListMemoryNodes(opts ListOptions) ([]*Node, error) {
	return d.listNodesWithKindFilter(opts, NodeKindMemory)
}

func (d *SQLiteStore) Search(query string) ([]*Node, error) {
	// FTS only indexes kind='memory' rows (enforced by triggers), so no extra filter needed.
	rows, err := d.db.Query(`SELECT n.id, n.type, n.kind, n.content, n.summary, n.token_estimate, n.superseded_by, n.created_at, n.updated_at, n.metadata
		FROM nodes n
		JOIN nodes_fts f ON n.rowid = f.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY rank`, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		node := &Node{}
		var summary, supersededBy sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(&node.ID, &node.Type, &node.Kind, &node.Content, &summary, &node.TokenEstimate,
			&supersededBy, &createdAt, &updatedAt, &node.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}

		if summary.Valid {
			node.Summary = &summary.String
		}
		if supersededBy.Valid {
			node.SupersededBy = &supersededBy.String
		}
		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		tags, _ := d.GetTags(node.ID)
		node.Tags = tags
		nodes = append(nodes, node)
	}

	return nodes, nil
}
