package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/zate/ctx/internal/token"
)

// PostgresStore is the PostgreSQL implementation of the Store interface.
// Used by the remote server for hosted/shared access.
type PostgresStore struct {
	db *sql.DB
}

// compile-time check that PostgresStore implements Store.
var _ Store = (*PostgresStore)(nil)

// OpenPostgres opens a PostgreSQL database connection and runs migrations.
// connStr is a PostgreSQL connection string (e.g. "postgres://user:pass@host:5432/dbname?sslmode=require").
func OpenPostgres(connStr string) (*PostgresStore, error) {
	sqlDB, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	d := &PostgresStore{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to migrate postgres: %w", err)
	}

	return d, nil
}

func (d *PostgresStore) Close() error {
	return d.db.Close()
}

// --- Raw SQL access ---

func (d *PostgresStore) Exec(query string, args ...interface{}) (sql.Result, error) {
	return d.db.Exec(query, args...)
}

func (d *PostgresStore) QueryRow(query string, args ...interface{}) *sql.Row {
	return d.db.QueryRow(query, args...)
}

func (d *PostgresStore) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return d.db.Query(query, args...)
}

func (d *PostgresStore) Begin() (*sql.Tx, error) {
	return d.db.Begin()
}

// --- Node operations ---

func (d *PostgresStore) CreateNode(input CreateNodeInput) (*Node, error) {
	if !validNodeTypes[input.Type] {
		return nil, fmt.Errorf("invalid node type: %s", input.Type)
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("content cannot be empty")
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

	_, err = tx.Exec(`INSERT INTO nodes (id, type, content, summary, token_estimate, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, input.Type, input.Content, summary, tokenEst, nowStr, nowStr, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}

	for _, tag := range input.Tags {
		_, err = tx.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
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
		Content:       input.Content,
		Summary:       input.Summary,
		TokenEstimate: tokenEst,
		CreatedAt:     now,
		UpdatedAt:     now,
		Metadata:      metadata,
		Tags:          input.Tags,
	}, nil
}

func (d *PostgresStore) FindByTypeAndContent(nodeType, content string) (*Node, error) {
	var id string
	err := d.db.QueryRow(
		`SELECT id FROM nodes WHERE type = $1 AND content = $2 AND superseded_by IS NULL LIMIT 1`,
		nodeType, content).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find node: %w", err)
	}
	return d.GetNode(id)
}

func (d *PostgresStore) ResolveID(prefix string) (string, error) {
	if len(prefix) == 26 {
		var id string
		err := d.db.QueryRow("SELECT id FROM nodes WHERE id = $1", prefix).Scan(&id)
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

	rows, err := d.db.Query("SELECT id FROM nodes WHERE id LIKE $1 LIMIT 2", prefix+"%")
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

func (d *PostgresStore) GetNode(id string) (*Node, error) {
	node := &Node{}
	var summary, supersededBy sql.NullString
	var createdAt, updatedAt string

	err := d.db.QueryRow(`SELECT id, type, content, summary, token_estimate, superseded_by, created_at, updated_at, metadata
		FROM nodes WHERE id = $1`, id).Scan(
		&node.ID, &node.Type, &node.Content, &summary, &node.TokenEstimate,
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

func (d *PostgresStore) UpdateNode(id string, input UpdateNodeInput) (*Node, error) {
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

	_, err = d.db.Exec(`UPDATE nodes SET type=$1, content=$2, summary=$3, token_estimate=$4, updated_at=$5, metadata=$6
		WHERE id=$7`, nodeType, content, summaryVal, tokenEst, nowStr, metadata, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}

	return d.GetNode(id)
}

func (d *PostgresStore) DeleteNode(id string) error {
	result, err := d.db.Exec("DELETE FROM nodes WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *PostgresStore) ListNodes(opts ListOptions) ([]*Node, error) {
	query := `SELECT n.id, n.type, n.content, n.summary, n.token_estimate, n.superseded_by, n.created_at, n.updated_at, n.metadata
		FROM nodes n`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if !opts.IncludeSuperseded {
		conditions = append(conditions, "n.superseded_by IS NULL")
	}
	if opts.Type != "" {
		conditions = append(conditions, fmt.Sprintf("n.type = $%d", argIdx))
		args = append(args, opts.Type)
		argIdx++
	}
	// Merge single Tag into Tags for backwards compatibility
	tags := opts.Tags
	if opts.Tag != "" {
		tags = append(tags, opts.Tag)
	}
	for i, tag := range tags {
		alias := fmt.Sprintf("t%d", i)
		query += fmt.Sprintf(" JOIN tags %s ON n.id = %s.node_id", alias, alias)
		conditions = append(conditions, fmt.Sprintf("%s.tag = $%d", alias, argIdx))
		args = append(args, tag)
		argIdx++
	}
	if opts.Since != nil {
		conditions = append(conditions, fmt.Sprintf("n.created_at >= $%d", argIdx))
		args = append(args, opts.Since.UTC().Format(time.RFC3339))
		argIdx++
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

		err := rows.Scan(&node.ID, &node.Type, &node.Content, &summary, &node.TokenEstimate,
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

// ListMemoryNodes returns only kind='memory' nodes.
// NOTE: The Postgres schema does not yet have the kind column (v5 migration is SQLite-only).
// This stub filters in-memory until a corresponding Postgres migration is added.
func (d *PostgresStore) ListMemoryNodes(opts ListOptions) ([]*Node, error) {
	all, err := d.ListNodes(opts)
	if err != nil {
		return nil, err
	}
	var out []*Node
	for _, n := range all {
		if n.Kind == "" || n.Kind == NodeKindMemory {
			out = append(out, n)
		}
	}
	return out, nil
}

func (d *PostgresStore) Search(queryStr string) ([]*Node, error) {
	// PostgreSQL uses tsvector/tsquery for full-text search instead of FTS5
	rows, err := d.db.Query(`SELECT n.id, n.type, n.content, n.summary, n.token_estimate, n.superseded_by, n.created_at, n.updated_at, n.metadata
		FROM nodes n
		WHERE n.search_vector @@ plainto_tsquery('english', $1)
		ORDER BY ts_rank(n.search_vector, plainto_tsquery('english', $1)) DESC`, queryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		node := &Node{}
		var summary, supersededBy sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(&node.ID, &node.Type, &node.Content, &summary, &node.TokenEstimate,
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

// --- Edge operations ---

func (d *PostgresStore) CreateEdge(fromID, toID, edgeType string) (*Edge, error) {
	if !validEdgeTypes[edgeType] {
		return nil, fmt.Errorf("invalid edge type: %s", edgeType)
	}

	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = $1", fromID).Scan(&count)
	if err != nil || count == 0 {
		return nil, fmt.Errorf("from node %s not found", fromID)
	}
	err = d.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE id = $1", toID).Scan(&count)
	if err != nil || count == 0 {
		return nil, fmt.Errorf("to node %s not found", toID)
	}

	id := NewID()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	_, err = d.db.Exec(`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, '{}') ON CONFLICT DO NOTHING`, id, fromID, toID, edgeType, nowStr)
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

func (d *PostgresStore) DeleteEdge(fromID, toID string, edgeType string) error {
	query := "DELETE FROM edges WHERE from_id = $1 AND to_id = $2"
	args := []interface{}{fromID, toID}
	if edgeType != "" {
		query += " AND type = $3"
		args = append(args, edgeType)
	}
	_, err := d.db.Exec(query, args...)
	return err
}

func (d *PostgresStore) GetEdges(nodeID string, direction string) ([]*Edge, error) {
	var query string
	var args []interface{}
	switch direction {
	case "out":
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE from_id = $1"
		args = []interface{}{nodeID}
	case "in":
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE to_id = $1"
		args = []interface{}{nodeID}
	default:
		query = "SELECT id, from_id, to_id, type, created_at, metadata FROM edges WHERE from_id = $1 OR to_id = $2"
		args = []interface{}{nodeID, nodeID}
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get edges: %w", err)
	}
	defer rows.Close()

	return scanEdges(rows)
}

func (d *PostgresStore) GetEdgesFrom(nodeID string) ([]*Edge, error) {
	return d.GetEdges(nodeID, "out")
}

func (d *PostgresStore) GetEdgesTo(nodeID string) ([]*Edge, error) {
	return d.GetEdges(nodeID, "in")
}

// --- Tag operations ---

func (d *PostgresStore) AddTag(nodeID, tag string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`INSERT INTO tags (node_id, tag, created_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		nodeID, tag, now)
	if err != nil {
		return fmt.Errorf("failed to add tag: %w", err)
	}
	return nil
}

func (d *PostgresStore) RemoveTag(nodeID, tag string) error {
	_, err := d.db.Exec("DELETE FROM tags WHERE node_id = $1 AND tag = $2", nodeID, tag)
	if err != nil {
		return fmt.Errorf("failed to remove tag: %w", err)
	}
	return nil
}

func (d *PostgresStore) GetTags(nodeID string) ([]string, error) {
	rows, err := d.db.Query("SELECT tag FROM tags WHERE node_id = $1 ORDER BY tag", nodeID)
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

func (d *PostgresStore) ListAllTags() ([]string, error) {
	rows, err := d.db.Query("SELECT DISTINCT tag FROM tags ORDER BY tag")
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

func (d *PostgresStore) ListTagsByPrefix(prefix string) ([]string, error) {
	rows, err := d.db.Query("SELECT DISTINCT tag FROM tags WHERE tag LIKE $1 ORDER BY tag", prefix+"%")
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

func (d *PostgresStore) GetNodesByTag(tag string) ([]*Node, error) {
	return d.ListNodes(ListOptions{Tag: tag})
}

// --- Pending operations ---

func (d *PostgresStore) SetPending(key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`INSERT INTO pending (key, value, created_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = $2, created_at = $3`,
		key, value, now)
	if err != nil {
		return fmt.Errorf("failed to set pending %s: %w", key, err)
	}
	return nil
}

func (d *PostgresStore) GetPending(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM pending WHERE key = $1", key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get pending %s: %w", key, err)
	}
	return value, nil
}

func (d *PostgresStore) DeletePending(key string) error {
	_, err := d.db.Exec("DELETE FROM pending WHERE key = $1", key)
	return err
}

// --- Migrations ---

var postgresMigrations = []struct {
	version int
	sql     string
}{
	{1, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			summary TEXT,
			token_estimate INTEGER NOT NULL,
			superseded_by TEXT REFERENCES nodes(id),
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			search_vector tsvector GENERATED ALWAYS AS (to_tsvector('english', content)) STORED
		);

		CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(type);
		CREATE INDEX IF NOT EXISTS idx_nodes_created ON nodes(created_at);
		CREATE INDEX IF NOT EXISTS idx_nodes_superseded ON nodes(superseded_by);
		CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes USING GIN(search_vector);

		CREATE TABLE IF NOT EXISTS edges (
			id TEXT PRIMARY KEY,
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			type TEXT NOT NULL,
			created_at TEXT NOT NULL,
			metadata TEXT DEFAULT '{}',
			FOREIGN KEY (from_id) REFERENCES nodes(id) ON DELETE CASCADE,
			FOREIGN KEY (to_id) REFERENCES nodes(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
		CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
		CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_edges_unique ON edges(from_id, to_id, type);

		CREATE TABLE IF NOT EXISTS tags (
			node_id TEXT NOT NULL,
			tag TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (node_id, tag),
			FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);

		CREATE TABLE IF NOT EXISTS views (
			name TEXT PRIMARY KEY,
			query TEXT NOT NULL,
			budget INTEGER DEFAULT 50000,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS pending (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
	`},
	{2, `
		-- Server-only tables for remote mode

		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			refresh_token_hash TEXT,
			last_seen TIMESTAMPTZ,
			last_ip TEXT,
			revoked BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);
		CREATE INDEX IF NOT EXISTS idx_devices_token ON devices(token_hash);

		CREATE TABLE IF NOT EXISTS repo_mappings (
			id TEXT PRIMARY KEY,
			normalized_url TEXT UNIQUE NOT NULL,
			project_tag TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS sync_log (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id),
			direction TEXT NOT NULL,
			nodes_affected INTEGER NOT NULL DEFAULT 0,
			sync_version BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_sync_log_device ON sync_log(device_id);
		CREATE INDEX IF NOT EXISTS idx_sync_log_version ON sync_log(sync_version);

		-- Add sync tracking columns to nodes
		ALTER TABLE nodes ADD COLUMN IF NOT EXISTS sync_version BIGINT DEFAULT 0;
		ALTER TABLE nodes ADD COLUMN IF NOT EXISTS origin_device TEXT;
	`},
}

func (d *PostgresStore) migrate() error {
	// Ensure schema_version table exists
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	var currentVersion int
	err = d.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		currentVersion = 0
	}

	for _, m := range postgresMigrations {
		if m.version > currentVersion {
			tx, err := d.db.Begin()
			if err != nil {
				return fmt.Errorf("failed to begin transaction for migration %d: %w", m.version, err)
			}

			if _, err := tx.Exec(m.sql); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migration %d failed: %w", m.version, err)
			}

			if _, err := tx.Exec("INSERT INTO schema_version (version, applied_at) VALUES ($1, $2)",
				m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("failed to set schema version %d: %w", m.version, err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit migration %d: %w", m.version, err)
			}
		}
	}

	// Create default view if not exists
	_, err = d.db.Exec(`INSERT INTO views (name, query, budget, created_at, updated_at)
		VALUES ('default', 'tag:tier:pinned OR tag:tier:working', 50000, $1, $2)
		ON CONFLICT DO NOTHING`,
		time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("failed to create default view: %w", err)
	}

	return nil
}
