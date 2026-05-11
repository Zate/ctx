package db

import "database/sql"

// Store is the interface for all database operations. Both SQLite (local) and
// PostgreSQL (remote server) backends implement this interface.
type Store interface {
	// Close closes the database connection.
	Close() error

	// --- Node operations ---

	CreateNode(input CreateNodeInput) (*Node, error)
	GetNode(id string) (*Node, error)
	UpdateNode(id string, input UpdateNodeInput) (*Node, error)
	DeleteNode(id string) error
	ListNodes(opts ListOptions) ([]*Node, error)
	// ListMemoryNodes returns only kind='memory' nodes (safe default for all memory-path surfaces).
	ListMemoryNodes(opts ListOptions) ([]*Node, error)
	Search(query string) ([]*Node, error)
	ResolveID(prefix string) (string, error)
	FindByTypeAndContent(nodeType, content string) (*Node, error)

	// --- Edge operations ---

	CreateEdge(fromID, toID, edgeType string) (*Edge, error)
	DeleteEdge(fromID, toID string, edgeType string) error
	GetEdges(nodeID string, direction string) ([]*Edge, error)
	GetEdgesFrom(nodeID string) ([]*Edge, error)
	GetEdgesTo(nodeID string) ([]*Edge, error)

	// --- Tag operations ---

	AddTag(nodeID, tag string) error
	RemoveTag(nodeID, tag string) error
	GetTags(nodeID string) ([]string, error)
	ListAllTags() ([]string, error)
	ListTagsByPrefix(prefix string) ([]string, error)
	GetNodesByTag(tag string) ([]*Node, error)

	// --- Access log operations ---
	// LogAccess and LogAccessBatch silently no-op for non-memory nodes
	// (kind!='memory') and unknown IDs; the kind='memory' guard is enforced
	// at the DB layer so call sites do not need to filter.

	LogAccess(nodeID, accessType, agent, queryContext string) error
	LogAccessBatch(nodeIDs []string, accessType, agent, queryContext string) error
	QueryAccess(opts AccessLogQuery) ([]*AccessEntry, error)

	// --- Pending operations ---

	SetPending(key, value string) error
	GetPending(key string) (string, error)
	DeletePending(key string) error

	// --- Raw SQL access ---
	// These are used by consumers that build dynamic queries (query executor,
	// status commands, import/export, view management). Both SQLite and PostgreSQL
	// backends implement database/sql, so these work for both.
	// TODO: Gradually replace raw SQL usage with high-level methods as we add
	// PostgreSQL support, to handle dialect differences (placeholders, FTS, etc.)

	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
	Begin() (*sql.Tx, error)
}
