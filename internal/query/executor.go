package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zate/ctx/internal/db"
)

// ExecuteQuery parses and executes a query against the database.
// By default it scopes to kind='memory' nodes unless the query contains an
// explicit kind:* predicate.
func ExecuteQuery(d db.Store, queryStr string, includeSuperseded bool) ([]*db.Node, error) {
	ast, err := Parse(queryStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query: %w", err)
	}

	if ast == nil {
		return d.ListMemoryNodes(db.ListOptions{IncludeSuperseded: includeSuperseded})
	}

	where, args, joins, err := buildSQL(ast)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	// Apply implicit kind='memory' filter unless the query explicitly uses kind:*
	if !hasKindPredicate(ast) {
		kindFilter := "n.kind = ?"
		args = append([]interface{}{db.NodeKindMemory}, args...)
		if where != "" {
			where = kindFilter + " AND (" + where + ")"
		} else {
			where = kindFilter
		}
	}

	if !includeSuperseded {
		if where != "" {
			where = "(" + where + ") AND n.superseded_by IS NULL"
		} else {
			where = "n.superseded_by IS NULL"
		}
	}

	sqlStr := "SELECT DISTINCT n.id, n.type, n.kind, n.content, n.summary, n.token_estimate, n.superseded_by, n.created_at, n.updated_at, n.metadata FROM nodes n"
	if joins != "" {
		sqlStr += " " + joins
	}
	if where != "" {
		sqlStr += " WHERE " + where
	}
	sqlStr += " ORDER BY n.created_at DESC"

	rows, err := d.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var nodes []*db.Node
	for rows.Next() {
		node := &db.Node{}
		var summary, supersededBy interface{}
		var createdAt, updatedAt string

		err := rows.Scan(&node.ID, &node.Type, &node.Kind, &node.Content, &summary, &node.TokenEstimate,
			&supersededBy, &createdAt, &updatedAt, &node.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}

		if s, ok := summary.(string); ok {
			node.Summary = &s
		}
		if s, ok := supersededBy.(string); ok {
			node.SupersededBy = &s
		}
		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		nodes = append(nodes, node)
	}

	// Load tags for each node
	for _, node := range nodes {
		tags, _ := d.GetTags(node.ID)
		node.Tags = tags
	}

	return nodes, nil
}

// hasKindPredicate returns true if the AST contains any kind:* predicate,
// indicating the caller explicitly wants to override the default kind filter.
func hasKindPredicate(ast *QueryAST) bool {
	if ast == nil {
		return false
	}
	switch ast.Type {
	case "predicate":
		return ast.Key == "kind"
	case "and", "or":
		return hasKindPredicate(ast.Left) || hasKindPredicate(ast.Right)
	case "not":
		return hasKindPredicate(ast.Child)
	}
	return false
}

func buildSQL(ast *QueryAST) (string, []interface{}, string, error) {
	if ast == nil {
		return "", nil, "", nil
	}

	switch ast.Type {
	case "and":
		lWhere, lArgs, lJoins, err := buildSQL(ast.Left)
		if err != nil {
			return "", nil, "", err
		}
		rWhere, rArgs, rJoins, err := buildSQL(ast.Right)
		if err != nil {
			return "", nil, "", err
		}
		joins := mergeJoins(lJoins, rJoins)
		return "(" + lWhere + " AND " + rWhere + ")", append(lArgs, rArgs...), joins, nil

	case "or":
		lWhere, lArgs, lJoins, err := buildSQL(ast.Left)
		if err != nil {
			return "", nil, "", err
		}
		rWhere, rArgs, rJoins, err := buildSQL(ast.Right)
		if err != nil {
			return "", nil, "", err
		}
		joins := mergeJoins(lJoins, rJoins)
		return "(" + lWhere + " OR " + rWhere + ")", append(lArgs, rArgs...), joins, nil

	case "not":
		cWhere, cArgs, cJoins, err := buildSQL(ast.Child)
		if err != nil {
			return "", nil, "", err
		}
		return "NOT (" + cWhere + ")", cArgs, cJoins, nil

	case "predicate":
		return buildPredicate(ast)

	default:
		return "", nil, "", fmt.Errorf("unknown AST type: %s", ast.Type)
	}
}

func buildPredicate(ast *QueryAST) (string, []interface{}, string, error) {
	switch ast.Key {
	case "type":
		return "n.type = ?", []interface{}{ast.Value}, "", nil

	case "tag":
		return "n.id IN (SELECT node_id FROM tags WHERE tag = ?)", []interface{}{ast.Value}, "", nil

	case "created":
		return buildTimeFilter("n.created_at", ast.Operator, ast.Value)

	case "updated":
		return buildTimeFilter("n.updated_at", ast.Operator, ast.Value)

	case "tokens":
		op := ast.Operator
		if op == "" {
			op = "="
		}
		n, err := strconv.Atoi(ast.Value)
		if err != nil {
			return "", nil, "", fmt.Errorf("invalid token count: %s", ast.Value)
		}
		return fmt.Sprintf("n.token_estimate %s ?", op), []interface{}{n}, "", nil

	case "has":
		switch ast.Value {
		case "summary":
			return "n.summary IS NOT NULL", nil, "", nil
		case "edges":
			return "(EXISTS (SELECT 1 FROM edges WHERE from_id = n.id) OR EXISTS (SELECT 1 FROM edges WHERE to_id = n.id))", nil, "", nil
		default:
			return "", nil, "", fmt.Errorf("unknown has value: %s", ast.Value)
		}

	case "from":
		return "n.id IN (SELECT to_id FROM edges WHERE from_id = ?)", []interface{}{ast.Value}, "", nil

	case "to":
		return "n.id IN (SELECT from_id FROM edges WHERE to_id = ?)", []interface{}{ast.Value}, "", nil

	case "kind":
		return "n.kind = ?", []interface{}{ast.Value}, "", nil

	default:
		return "", nil, "", fmt.Errorf("unknown key: %s", ast.Key)
	}
}

func buildTimeFilter(column, op, value string) (string, []interface{}, string, error) {
	if op == "" {
		op = ">"
	}

	// Check if it's an absolute date
	if strings.Contains(value, "-") {
		// Absolute date like 2024-01-01
		t, err := time.Parse("2006-01-02", value)
		if err != nil {
			return "", nil, "", fmt.Errorf("invalid date: %s", value)
		}
		// Flip operator: created:>2024-01-01 means "created after this date"
		return fmt.Sprintf("%s %s ?", column, op), []interface{}{t.UTC().Format(time.RFC3339)}, "", nil
	}

	// Relative duration like 24h, 7d, 1w
	dur, err := parseDuration(value)
	if err != nil {
		return "", nil, "", fmt.Errorf("invalid duration: %s", value)
	}

	threshold := time.Now().Add(-dur).UTC().Format(time.RFC3339)
	// For relative: created:>24h means "created in the last 24 hours", so created_at > threshold
	return fmt.Sprintf("%s %s ?", column, op), []interface{}{threshold}, "", nil
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}

	last := s[len(s)-1]
	numStr := s[:len(s)-1]

	switch last {
	case 'h':
		return time.ParseDuration(numStr + "h")
	case 'd':
		hours, err := time.ParseDuration(numStr + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24, nil
	case 'w':
		hours, err := time.ParseDuration(numStr + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24 * 7, nil
	default:
		return time.ParseDuration(s)
	}
}

func mergeJoins(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a == b {
		return a
	}
	return a + " " + b
}
