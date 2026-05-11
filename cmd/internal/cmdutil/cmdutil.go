// Package cmdutil provides shared helpers for cobra subcommand packages
// (cmd/server, cmd/mcp, etc.) so they can access persistent flags and the
// database without importing the parent cmd package.
package cmdutil

import (
	"fmt"

	"github.com/spf13/cobra"
	agentpkg "github.com/zate/ctx/internal/agent"
	"github.com/zate/ctx/internal/db"
)

// flagValue reads a persistent flag from the root command. Returns "" if missing.
func flagValue(cmd *cobra.Command, name string) string {
	if cmd == nil {
		return ""
	}
	f := cmd.Root().PersistentFlags().Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// DBPath returns the --db flag value.
func DBPath(cmd *cobra.Command) string { return flagValue(cmd, "db") }

// Backend returns the --backend flag value.
func Backend(cmd *cobra.Command) string { return flagValue(cmd, "backend") }

// Agent returns the --agent flag value.
func Agent(cmd *cobra.Command) string { return flagValue(cmd, "agent") }

// Format returns the --format flag value.
func Format(cmd *cobra.Command) string { return flagValue(cmd, "format") }

// OpenDB opens the database using the persistent --db and --backend flags.
func OpenDB(cmd *cobra.Command) (db.Store, error) {
	return OpenDBWith(DBPath(cmd), Backend(cmd))
}

// OpenDBWith opens the database with explicit path and backend.
func OpenDBWith(path, backend string) (db.Store, error) {
	switch backend {
	case "postgres", "postgresql":
		d, err := db.OpenPostgres(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open postgres database: %w", err)
		}
		return d, nil
	case "sqlite", "":
		d, err := db.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unknown backend %q: use 'sqlite' or 'postgres'", backend)
	}
}

// ResolveArg resolves a node ID prefix to a full ID.
func ResolveArg(d db.Store, prefix string) (string, error) {
	resolved, err := d.ResolveID(prefix)
	if err != nil {
		return "", fmt.Errorf("cannot resolve ID %q: %w", prefix, err)
	}
	return resolved, nil
}

// AgentTag returns the agent tag for the current --agent value, or empty.
func AgentTag(cmd *cobra.Command) string {
	return agentpkg.Tag(Agent(cmd))
}

// FilterNodesByAgent filters nodes to those visible to the current agent.
func FilterNodesByAgent(cmd *cobra.Command, nodes []*db.Node) []*db.Node {
	return agentpkg.FilterNodes(nodes, Agent(cmd))
}

// LogAccessNodes records access for the given nodes via LogAccessBatch.
// Errors are swallowed; the DB-layer kind='memory' guard handles non-memory IDs.
func LogAccessNodes(cmd *cobra.Command, d db.Store, nodes []*db.Node, accessType, queryContext string) {
	if len(nodes) == 0 {
		return
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	_ = d.LogAccessBatch(ids, accessType, Agent(cmd), queryContext)
}
