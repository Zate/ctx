package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Full-text search",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()

	nodes, err := d.Search(args[0])
	if err != nil {
		return err
	}

	// Filter by agent partition
	nodes = filterNodesByAgent(nodes)

	logAccessNodes(d, nodes, "explicit_query", "search:"+args[0])

	switch format {
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
