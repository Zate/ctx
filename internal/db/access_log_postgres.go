package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LogAccess (Postgres). Mirrors the SQLite kind='memory' isolation guard.
func (d *PostgresStore) LogAccess(nodeID, accessType, agent, queryContext string) error {
	_, err := d.db.Exec(
		`INSERT INTO access_log (node_id, accessed_at, agent, access_type, query_context)
		 SELECT $1, $2, $3, $4, $5
		 WHERE EXISTS (SELECT 1 FROM nodes WHERE id = $6 AND kind = 'memory')`,
		nodeID,
		time.Now().UTC().Format(time.RFC3339),
		agent,
		accessType,
		queryContext,
		nodeID,
	)
	return err
}

// LogAccessBatch (Postgres). Single transaction; per-row kind='memory' guard.
func (d *PostgresStore) LogAccessBatch(nodeIDs []string, accessType, agent, queryContext string) error {
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
		 SELECT $1, $2, $3, $4, $5
		 WHERE EXISTS (SELECT 1 FROM nodes WHERE id = $6 AND kind = 'memory')`,
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

// QueryAccess (Postgres).
func (d *PostgresStore) QueryAccess(opts AccessLogQuery) ([]*AccessEntry, error) {
	var sb strings.Builder
	sb.WriteString(`SELECT id, node_id, accessed_at, agent, access_type, query_context FROM access_log WHERE 1=1`)
	var args []interface{}
	n := 0
	next := func() string {
		n++
		return "$" + strconv.Itoa(n)
	}

	if opts.NodeIDPrefix != "" {
		sb.WriteString(` AND node_id LIKE `)
		sb.WriteString(next())
		args = append(args, opts.NodeIDPrefix+"%")
	}
	if !opts.AllAgents && opts.Agent != "" {
		sb.WriteString(` AND agent = `)
		sb.WriteString(next())
		args = append(args, opts.Agent)
	}
	if opts.AccessType != "" {
		sb.WriteString(` AND access_type = `)
		sb.WriteString(next())
		args = append(args, opts.AccessType)
	}
	if opts.Since != "" {
		sb.WriteString(` AND accessed_at >= `)
		sb.WriteString(next())
		args = append(args, opts.Since)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	sb.WriteString(` ORDER BY accessed_at DESC, id DESC LIMIT `)
	sb.WriteString(next())
	args = append(args, limit)

	rows, err := d.db.Query(sb.String(), args...)
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
