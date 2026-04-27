package system

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
)

var (
	accessedNode      string
	accessedType      string
	accessedSince     string
	accessedLimit     int
	accessedAllAgents bool
)

var accessedCmd = &cobra.Command{
	Use:   "accessed",
	Short: "Query access history for nodes",
	Long: `Show when and how nodes were accessed. Useful for understanding
which knowledge is actually used vs sitting idle.

Access types:
  hook_inject     - Loaded automatically at session start
  explicit_query  - Retrieved via query, search, list, compose, recall
  get             - Retrieved via show
  graph_walk      - Reached via related, trace

Defaults to the current --agent (or $CTX_AGENT). Use --all-agents to opt out.`,
	RunE: runAccessed,
}

func init() {
	accessedCmd.Flags().StringVar(&accessedNode, "node", "", "Filter by node ID (prefix match)")
	accessedCmd.Flags().StringVar(&accessedType, "type", "", "Filter by access type")
	accessedCmd.Flags().StringVar(&accessedSince, "since", "", "Show entries after this RFC3339 timestamp")
	accessedCmd.Flags().IntVar(&accessedLimit, "limit", 50, "Max entries to return")
	accessedCmd.Flags().BoolVar(&accessedAllAgents, "all-agents", false, "Show entries from all agents (overrides --agent)")
	register(accessedCmd)
}

func runAccessed(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	prefix := accessedNode
	if prefix != "" {
		if resolved, err := d.ResolveID(prefix); err == nil {
			prefix = resolved
		}
	}

	entries, err := d.QueryAccess(db.AccessLogQuery{
		NodeIDPrefix: prefix,
		Agent:        cmdutil.Agent(cmd),
		AllAgents:    accessedAllAgents,
		AccessType:   accessedType,
		Since:        accessedSince,
		Limit:        accessedLimit,
	})
	if err != nil {
		return err
	}

	if cmdutil.Format(cmd) == "json" {
		if entries == nil {
			entries = []*db.AccessEntry{}
		}
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(entries) == 0 {
		fmt.Println("No access entries found.")
		return nil
	}

	for _, e := range entries {
		shortID := e.NodeID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		ctx := ""
		if e.QueryContext != "" {
			ctx = " (" + truncateAccessCtx(e.QueryContext, 40) + ")"
		}
		fmt.Printf("%s  %-15s  %s  %s%s\n",
			e.AccessedAt.Format("2006-01-02 15:04"),
			e.AccessType,
			shortID,
			e.Agent,
			ctx,
		)
	}
	fmt.Printf("\n%d entries shown", len(entries))
	if accessedLimit > 0 && len(entries) >= accessedLimit {
		fmt.Printf(" (limit: %d, use --limit to see more)", accessedLimit)
	}
	fmt.Println()

	return nil
}

func truncateAccessCtx(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
