package graph

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var edgesDirection string

var edgesCmd = &cobra.Command{
	Use:   "edges <id>",
	Short: "Show connections for a node",
	Args:  cobra.ExactArgs(1),
	RunE:  runEdges,
}

func init() {
	edgesCmd.Flags().StringVar(&edgesDirection, "direction", "both", "Direction: in, out, both")
	register(edgesCmd)
}

func runEdges(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	id, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}

	edges, err := d.GetEdges(id, edgesDirection)
	if err != nil {
		return err
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(edges, "", "  ")
		fmt.Println(string(data))
	default:
		if len(edges) == 0 {
			fmt.Println("No edges found.")
			return nil
		}
		for _, e := range edges {
			if e.FromID == id {
				fmt.Printf("→ %s (%s)\n", e.ToID[:8], e.Type)
			} else {
				fmt.Printf("← %s (%s)\n", e.FromID[:8], e.Type)
			}
		}
	}

	return nil
}
