package graph

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var relatedDepth int

var relatedCmd = &cobra.Command{
	Use:   "related <id>",
	Short: "Find related nodes",
	Args:  cobra.ExactArgs(1),
	RunE:  runRelated,
}

func init() {
	relatedCmd.Flags().IntVar(&relatedDepth, "depth", 1, "Traversal depth")
	register(relatedCmd)
}

func runRelated(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	id, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}

	visited := map[string]bool{id: true}
	type relatedNode struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Edge    string `json:"edge_type"`
	}
	var results []relatedNode

	current := []string{id}
	for depth := 0; depth < relatedDepth; depth++ {
		var next []string
		for _, id := range current {
			edges, _ := d.GetEdges(id, "both")
			for _, e := range edges {
				targetID := e.ToID
				if targetID == id {
					targetID = e.FromID
				}
				if visited[targetID] {
					continue
				}
				visited[targetID] = true
				next = append(next, targetID)

				node, err := d.GetNode(targetID)
				if err != nil {
					continue
				}
				results = append(results, relatedNode{
					ID:      node.ID,
					Type:    node.Type,
					Content: node.Content,
					Edge:    e.Type,
				})
			}
		}
		current = next
	}

	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		_ = d.LogAccessBatch(ids, "graph_walk", cmdutil.Agent(cmd), "related:"+args[0])
	}

	if cmdutil.AgentOut(cmd) {
		fmt.Fprintf(os.Stdout, "ok related count=%d\n", len(results))
		if len(results) > 0 {
			fmt.Fprintln(os.Stdout, "@ id type edge_type summary")
			for _, r := range results {
				body := r.Content
				if len(body) > 120 {
					body = body[:120] + "…"
				}
				fmt.Fprintf(os.Stdout, "- %s %s %s %s\n", r.ID, r.Type, r.Edge, cmdutil.AOFQuote(body))
			}
		}
		return nil
	}
	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
	default:
		if len(results) == 0 {
			fmt.Println("No related nodes found.")
			return nil
		}
		for _, r := range results {
			preview := r.Content
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			fmt.Printf("[%s] %s (%s): %s\n", r.ID, r.Type, r.Edge, preview)
		}
	}

	return nil
}

