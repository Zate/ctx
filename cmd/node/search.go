package node

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var searchLimit int

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().IntVar(&searchLimit, "limit", 0, "Limit results (0 = no limit)")
	register(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	nodes, err := d.Search(args[0])
	if err != nil {
		return err
	}

	// Filter by agent partition
	nodes = cmdutil.FilterNodesByAgent(cmd, nodes)

	// Apply limit after filtering
	more := false
	if searchLimit > 0 && len(nodes) > searchLimit {
		nodes = nodes[:searchLimit]
		more = true
	}

	cmdutil.LogAccessNodes(cmd, d, nodes, "explicit_query", "search:"+args[0])

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFNodes(os.Stdout, nodes, more)
		if more {
			fmt.Fprintf(os.Stdout, "next ctx search %s --limit %d\n", args[0], searchLimit)
		}
		return nil
	}
	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(nodes, "", "  ")
		fmt.Println(string(data))
	default:
		if len(nodes) == 0 {
			fmt.Println("No results found.")
			return nil
		}
		for _, n := range nodes {
			preview := n.Content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			fmt.Printf("[%s] %s: %s\n", n.ID, n.Type, preview)
		}
	}

	return nil
}
