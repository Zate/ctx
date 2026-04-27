package io

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var importMerge bool

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import graph from JSON (reads stdin)",
	RunE:  runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importMerge, "merge", false, "Skip conflicts instead of failing")
	register(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	var imp exportData
	if err := json.Unmarshal(data, &imp); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	nodesImported := 0
	edgesImported := 0
	tagsImported := 0

	for _, n := range imp.Nodes {
		now := time.Now().UTC().Format(time.RFC3339)
		createdAt := n.CreatedAt.Format(time.RFC3339)
		updatedAt := n.UpdatedAt.Format(time.RFC3339)
		metadata := n.Metadata
		if metadata == "" {
			metadata = "{}"
		}

		var summaryVal interface{}
		if n.Summary != nil {
			summaryVal = *n.Summary
		}

		var supersededVal interface{}
		if n.SupersededBy != nil {
			supersededVal = *n.SupersededBy
		}
		_ = now

		insertSQL := "INSERT INTO nodes (id, type, content, summary, token_estimate, superseded_by, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
		if importMerge {
			insertSQL = "INSERT OR IGNORE INTO nodes (id, type, content, summary, token_estimate, superseded_by, created_at, updated_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
		}
		_, err := d.Exec(insertSQL, n.ID, n.Type, n.Content, summaryVal, n.TokenEstimate, supersededVal, createdAt, updatedAt, metadata)
		if err != nil {
			if !importMerge {
				return fmt.Errorf("failed to import node %s: %w", n.ID, err)
			}
			continue
		}
		nodesImported++
	}

	for _, e := range imp.Edges {
		_, err := d.Exec("INSERT OR IGNORE INTO edges (id, from_id, to_id, type, created_at, metadata) VALUES (?, ?, ?, ?, ?, ?)",
			e.ID, e.FromID, e.ToID, e.Type, e.CreatedAt.Format(time.RFC3339), e.Metadata)
		if err != nil {
			if !importMerge {
				return fmt.Errorf("failed to import edge %s: %w", e.ID, err)
			}
			continue
		}
		edgesImported++
	}

	for _, t := range imp.Tags {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := d.Exec("INSERT OR IGNORE INTO tags (node_id, tag, created_at) VALUES (?, ?, ?)",
			t.NodeID, t.Tag, now)
		if err != nil {
			continue
		}
		tagsImported++
	}

	fmt.Printf("Imported: %d nodes, %d edges, %d tags\n", nodesImported, edgesImported, tagsImported)
	return nil
}
