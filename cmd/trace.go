package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var traceReverse bool

var traceCmd = &cobra.Command{
	Use:   "trace <id>",
	Short: "Trace provenance of a node",
	Args:  cobra.ExactArgs(1),
	RunE:  runTrace,
}

func init() {
	traceCmd.Flags().BoolVar(&traceReverse, "reverse", false, "Trace what depends on this node")
	rootCmd.AddCommand(traceCmd)
}

func runTrace(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()

	id, err := resolveArg(d, args[0])
	if err != nil {
		return err
	}

	visited := map[string]bool{}
	type traceNode struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Depth   int    `json:"depth"`
	}
	var results []traceNode

	var walk func(id string, depth int) error
	walk = func(id string, depth int) error {
		if visited[id] {
			return nil
		}
		visited[id] = true

		node, err := d.GetNode(id)
		if err != nil {
			return nil
		}
		results = append(results, traceNode{
			ID:      node.ID,
			Type:    node.Type,
			Content: node.Content,
			Depth:   depth,
		})

		var edges []*struct{ FromID, ToID string }
		if traceReverse {
			edgeList, _ := d.GetEdgesTo(id)
			for _, e := range edgeList {
				if e.Type == "DERIVED_FROM" || e.Type == "DEPENDS_ON" {
					edges = append(edges, &struct{ FromID, ToID string }{e.FromID, e.ToID})
				}
			}
			for _, e := range edges {
				_ = walk(e.FromID, depth+1)
			}
		} else {
			edgeList, _ := d.GetEdgesFrom(id)
			for _, e := range edgeList {
				if e.Type == "DERIVED_FROM" || e.Type == "DEPENDS_ON" {
					edges = append(edges, &struct{ FromID, ToID string }{e.FromID, e.ToID})
				}
			}
			for _, e := range edges {
				_ = walk(e.ToID, depth+1)
			}
		}
		return nil
	}

	if err := walk(id, 0); err != nil {
		return err
	}

	if len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		_ = d.LogAccessBatch(ids, "graph_walk", agent, "trace:"+args[0])
	}

	switch format {
	case "json":
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
	default:
		for _, r := range results {
			indent := ""
			for i := 0; i < r.Depth; i++ {
				indent += "  "
			}
			preview := r.Content
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			fmt.Printf("%s[%s] %s: %s\n", indent, r.ID, r.Type, preview)
		}
	}

	return nil
}
