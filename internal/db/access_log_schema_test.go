package db_test

// Phase 1 tests — schema migration v6 adds the access_log table on both backends.

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// expected indexes per Nyx parity
var expectedAccessLogIndexes = []string{
	"idx_access_log_agent",
	"idx_access_log_node",
	"idx_access_log_time",
	"idx_access_log_type",
}

// 1.1 — Fresh SQLite DB lands on schema_version=6 with access_log + 4 indexes.
func TestSchemaV6_SQLite_Fresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema_v6.db")
	d, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	var version int
	require.NoError(t,
		d.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version))
	assert.Equal(t, 6, version, "schema_version should be 6 after fresh open")

	// Table exists.
	var tbl string
	err = d.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='access_log'`,
	).Scan(&tbl)
	require.NoError(t, err)
	assert.Equal(t, "access_log", tbl)

	// All 4 indexes exist.
	rows, err := d.Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='access_log' ORDER BY name`,
	)
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		// sqlite auto-creates internal indexes for AUTOINCREMENT; filter to ours.
		if len(name) >= 4 && name[:4] == "idx_" {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	assert.Equal(t, expectedAccessLogIndexes, got)
}

// 1.3 — Re-running migrate() on a v6 DB is a no-op.
func TestSchemaV6_SQLite_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	d1, err := db.Open(path)
	require.NoError(t, err)
	d1.Close()

	d2, err := db.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { d2.Close() })

	var version int
	require.NoError(t,
		d2.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version))
	assert.Equal(t, 6, version)

	// Each migration recorded exactly once.
	var rowCount int
	require.NoError(t,
		d2.QueryRow("SELECT COUNT(*) FROM schema_version WHERE version = 6").Scan(&rowCount))
	assert.Equal(t, 1, rowCount, "v6 should be recorded exactly once after re-open")
}

// 1.4 — FK cascade: deleting a node removes its access_log rows.
func TestSchemaV6_SQLite_FKCascade(t *testing.T) {
	d := openTestDB(t)

	mem, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "cascade test node"})
	require.NoError(t, err)

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(
		`INSERT INTO access_log (node_id, accessed_at, agent, access_type, query_context)
		 VALUES (?, ?, ?, ?, ?)`,
		mem.ID, now, "testagent", "get", "show:test",
	)
	require.NoError(t, err)

	var before int
	require.NoError(t,
		d.QueryRow("SELECT COUNT(*) FROM access_log WHERE node_id = ?", mem.ID).Scan(&before))
	require.Equal(t, 1, before)

	require.NoError(t, d.DeleteNode(mem.ID))

	var after int
	require.NoError(t,
		d.QueryRow("SELECT COUNT(*) FROM access_log WHERE node_id = ?", mem.ID).Scan(&after))
	assert.Equal(t, 0, after, "deleting the node should cascade-delete its access_log rows")
}

// 1.2 — Same v6 assertions for Postgres, gated on CTX_TEST_POSTGRES_DSN.
func TestSchemaV6_Postgres(t *testing.T) {
	dsn := os.Getenv("CTX_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CTX_TEST_POSTGRES_DSN not set")
	}

	d, err := db.OpenPostgres(dsn)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	var version int
	require.NoError(t,
		d.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version))
	assert.GreaterOrEqual(t, version, 6, "schema_version should be at least 6")

	var exists bool
	require.NoError(t, d.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'access_log')`,
	).Scan(&exists))
	assert.True(t, exists, "access_log table should exist")

	rows, err := d.Query(
		`SELECT indexname FROM pg_indexes WHERE tablename = 'access_log' AND indexname LIKE 'idx_%' ORDER BY indexname`,
	)
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		got = append(got, name)
	}
	assert.Equal(t, expectedAccessLogIndexes, got)
}
