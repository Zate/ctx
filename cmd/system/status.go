package system

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	agentpkg "github.com/zate/ctx/internal/agent"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show database status",
	RunE:  runStatus,
}

func init() {
	register(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	// Get file size
	info, _ := os.Stat(cmdutil.DBPath(cmd))
	var fileSize int64
	if info != nil {
		fileSize = info.Size()
	}

	// Build cmdutil.Agent(cmd) filter SQL
	af := agentpkg.FilterSQL(cmdutil.Agent(cmd))

	// Count nodes by type
	type typeCount struct {
		Type  string `json:"type"`
		Count int    `json:"count"`
	}
	rows, err := d.Query("SELECT n.type, COUNT(*) FROM nodes n WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + af + " GROUP BY n.type ORDER BY n.type")
	if err != nil {
		return err
	}
	defer rows.Close()

	var typeCounts []typeCount
	totalNodes := 0
	for rows.Next() {
		var tc typeCount
		_ = rows.Scan(&tc.Type, &tc.Count)
		typeCounts = append(typeCounts, tc)
		totalNodes += tc.Count
	}

	// Total tokens
	var totalTokens int
	_ = d.QueryRow("SELECT COALESCE(SUM(n.token_estimate), 0) FROM nodes n WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + af).Scan(&totalTokens)

	// Edge count
	var edgeCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM edges").Scan(&edgeCount)

	// Unique tags (scoped to visible memory nodes)
	var tagCount int
	_ = d.QueryRow("SELECT COUNT(DISTINCT t.tag) FROM tags t JOIN nodes n ON t.node_id = n.id WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + af).Scan(&tagCount)

	// Tier breakdown
	type tierInfo struct {
		Tier   string `json:"tier"`
		Nodes  int    `json:"nodes"`
		Tokens int    `json:"tokens"`
	}
	tierRows, err := d.Query(`SELECT t.tag, COUNT(DISTINCT t.node_id), COALESCE(SUM(n.token_estimate), 0)
		FROM tags t JOIN nodes n ON t.node_id = n.id
		WHERE t.tag LIKE 'tier:%' AND n.kind = 'memory' AND n.superseded_by IS NULL` + af + `
		GROUP BY t.tag ORDER BY t.tag`)
	if err != nil {
		return err
	}
	defer tierRows.Close()

	var tiers []tierInfo
	for tierRows.Next() {
		var ti tierInfo
		_ = tierRows.Scan(&ti.Tier, &ti.Nodes, &ti.Tokens)
		tiers = append(tiers, ti)
	}

	switch cmdutil.Format(cmd) {
	case "json":
		out := map[string]interface{}{
			"database":     cmdutil.DBPath(cmd),
			"agent":        cmdutil.Agent(cmd),
			"file_size":    fileSize,
			"total_nodes":  totalNodes,
			"total_tokens": totalTokens,
			"total_edges":  edgeCount,
			"unique_tags":  tagCount,
			"types":        typeCounts,
			"tiers":        tiers,
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	default:
		if cmdutil.Agent(cmd) != "" {
			fmt.Printf("Agent: %s\n", cmdutil.Agent(cmd))
		}
		fmt.Printf("Database: %s", cmdutil.DBPath(cmd))
		if fileSize > 0 {
			fmt.Printf(" (%.1f KB)", float64(fileSize)/1024)
		}
		fmt.Println()
		fmt.Printf("Nodes: %d (estimated %d tokens)\n", totalNodes, totalTokens)
		for _, tc := range typeCounts {
			fmt.Printf("  %s: %d\n", tc.Type, tc.Count)
		}
		fmt.Printf("Edges: %d\n", edgeCount)
		fmt.Printf("Tags: %d unique\n", tagCount)
		if len(tiers) > 0 {
			fmt.Println("\nTier breakdown:")
			for _, ti := range tiers {
				fmt.Printf("  %s: %d nodes (%d tokens)\n", ti.Tier, ti.Nodes, ti.Tokens)
			}
		}
	}

	return nil
}
