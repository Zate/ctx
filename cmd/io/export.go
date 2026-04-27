package io

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
)

var exportQuery string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export graph to JSON",
	RunE:  runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportQuery, "query", "", "Filter by query")
	register(exportCmd)
}

type exportData struct {
	Nodes []*db.Node `json:"nodes"`
	Edges []*db.Edge `json:"edges"`
	Tags  []tagEntry `json:"tags"`
}

type tagEntry struct {
	NodeID string `json:"node_id"`
	Tag    string `json:"tag"`
}

func runExport(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	var nodes []*db.Node
	if exportQuery != "" {
		nodes, err = query.ExecuteQuery(d, exportQuery, true)
	} else {
		nodes, err = d.ListNodes(db.ListOptions{IncludeSuperseded: true})
	}
	if err != nil {
		return err
	}

	// Filter by agent partition
	nodes = cmdutil.FilterNodesByAgent(cmd, nodes)

	// Get all edges for exported nodes
	nodeIDs := map[string]bool{}
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}

	var allEdges []*db.Edge
	for _, n := range nodes {
		edges, _ := d.GetEdgesFrom(n.ID)
		for _, e := range edges {
			if nodeIDs[e.ToID] {
				allEdges = append(allEdges, e)
			}
		}
	}

	// Get all tags
	var tags []tagEntry
	for _, n := range nodes {
		for _, t := range n.Tags {
			tags = append(tags, tagEntry{NodeID: n.ID, Tag: t})
		}
	}

	data, _ := json.MarshalIndent(exportData{
		Nodes: nodes,
		Edges: allEdges,
		Tags:  tags,
	}, "", "  ")
	fmt.Println(string(data))

	return nil
}
