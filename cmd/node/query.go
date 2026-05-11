package node

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/query"
)

var includeSuperseded bool

var queryCmd = &cobra.Command{
	Use:   "query <expression>",
	Short: "Query nodes with structured filters",
	Args:  cobra.ExactArgs(1),
	RunE:  runQuery,
}

func init() {
	queryCmd.Flags().BoolVar(&includeSuperseded, "include-superseded", false, "Include superseded nodes")
	register(queryCmd)
}

func runQuery(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	nodes, err := query.ExecuteQuery(d, args[0], includeSuperseded)
	if err != nil {
		return err
	}

	// Filter by agent partition
	nodes = cmdutil.FilterNodesByAgent(cmd, nodes)

	cmdutil.LogAccessNodes(cmd, d, nodes, "explicit_query", "query:"+args[0])

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFNodes(os.Stdout, nodes, false)
		return nil
	}
	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(nodes, "", "  ")
		fmt.Println(string(data))
	default:
		if len(nodes) == 0 {
			fmt.Println("No nodes found.")
			return nil
		}
		for _, n := range nodes {
			preview := n.Content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			tags := ""
			if len(n.Tags) > 0 {
				tags = " [" + strings.Join(n.Tags, ", ") + "]"
			}
			fmt.Printf("[%s] %s: %s%s\n", n.ID, n.Type, preview, tags)
		}
	}

	return nil
}
