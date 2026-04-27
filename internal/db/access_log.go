package db

import (
	"fmt"
	"time"
)

// AccessEntry is a single access log record returned by QueryAccess.
type AccessEntry struct {
	ID           int64     `json:"id"`
	NodeID       string    `json:"node_id"`
	AccessedAt   time.Time `json:"accessed_at"`
	Agent        string    `json:"agent"`
	AccessType   string    `json:"access_type"`
	QueryContext string    `json:"query_context,omitempty"`
}

// AccessLogQuery filters access log entries.
//
// Empty fields mean "no filter". Limit defaults to 100 when zero or negative.
// AllAgents=true overrides Agent (call site should still set Agent for clarity).
type AccessLogQuery struct {
	NodeIDPrefix string // prefix match on node_id (empty = all)
	Agent        string // exact match on agent (empty = all when AllAgents)
	AllAgents    bool   // ignore Agent filter
	AccessType   string // hook_inject | explicit_query | get | graph_walk
	Since        string // RFC3339; entries with accessed_at >= Since
	Limit        int
}

// Access type constants — keep in sync with the wired call sites in Phase 3.
const (
	AccessTypeHookInject    = "hook_inject"
	AccessTypeExplicitQuery = "explicit_query"
	AccessTypeGet           = "get"
	AccessTypeGraphWalk     = "graph_walk"
)

// LogAccess records a single retrieval of a node.
//
// Isolation: the insert is gated by a kind='memory' subquery, so doc/content
// nodes (or unknown IDs) are silently skipped — no row is written and no
// error is returned. Callers do not need to check kind themselves.
func (d *SQLiteStore) LogAccess(nodeID, accessType, agent, queryContext string) error {
	_, err := d.db.Exec(
		`INSERT INTO access_log (node_id, accessed_at, agent, access_type, query_context)
		 SELECT ?, ?, ?, ?, ?
		 WHERE EXISTS (SELECT 1 FROM nodes WHERE id = ? AND kind = 'memory')`,
		nodeID,
		time.Now().UTC().Format(time.RFC3339),
		agent,
		accessType,
		queryContext,
		nodeID,
	)
	return err
}

// LogAccessBatch records multiple access events in a single transaction.
//
// Isolation: each insert uses the same kind='memory' guard as LogAccess;
// non-memory or unknown IDs are silently skipped without aborting the batch.
func (d *SQLiteStore) LogAccessBatch(nodeIDs []string, accessType, agent, queryContext string) error {
	if len(nodeIDs) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO access_log (node_id, accessed_at, agent, access_type, query_context)
		 SELECT ?, ?, ?, ?, ?
		 WHERE EXISTS (SELECT 1 FROM nodes WHERE id = ? AND kind = 'memory')`,
	)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range nodeIDs {
		if _, err := stmt.Exec(id, now, agent, accessType, queryContext, id); err != nil {
			return fmt.Errorf("exec for %s: %w", id, err)
		}
	}

	return tx.Commit()
}

// QueryAccess returns access log entries matching opts, newest first.
func (d *SQLiteStore) QueryAccess(opts AccessLogQuery) ([]*AccessEntry, error) {
	q := `SELECT id, node_id, accessed_at, agent, access_type, query_context FROM access_log WHERE 1=1`
	var args []interface{}

	if opts.NodeIDPrefix != "" {
		q += ` AND node_id LIKE ?`
		args = append(args, opts.NodeIDPrefix+"%")
	}
	if !opts.AllAgents && opts.Agent != "" {
		q += ` AND agent = ?`
		args = append(args, opts.Agent)
	}
	if opts.AccessType != "" {
		q += ` AND access_type = ?`
		args = append(args, opts.AccessType)
	}
	if opts.Since != "" {
		q += ` AND accessed_at >= ?`
		args = append(args, opts.Since)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	q += ` ORDER BY accessed_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query access log: %w", err)
	}
	defer rows.Close()

	var entries []*AccessEntry
	for rows.Next() {
		e := &AccessEntry{}
		var accessedAt, qctx string
		if err := rows.Scan(&e.ID, &e.NodeID, &accessedAt, &e.Agent, &e.AccessType, &qctx); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		e.AccessedAt, _ = time.Parse(time.RFC3339, accessedAt)
		e.QueryContext = qctx
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
