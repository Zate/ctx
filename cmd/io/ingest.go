package io

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
)

var ingestTags []string

var ingestCmd = &cobra.Command{
	Use:   "ingest <file>",
	Short: "Ingest a file as a source node",
	Args:  cobra.ExactArgs(1),
	RunE:  runIngest,
}

func init() {
	ingestCmd.Flags().StringArrayVar(&ingestTags, "tag", nil, "Tags (repeatable)")
	register(ingestCmd)
}

func runIngest(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	content, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	filename := filepath.Base(args[0])
	metadata, _ := json.Marshal(map[string]string{
		"source_file": args[0],
		"filename":    filename,
	})

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:     "source",
		Content:  string(content),
		Metadata: string(metadata),
		Tags:     ingestTags,
	})
	if err != nil {
		return err
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Printf("Ingested: %s → %s (%d tokens)\n", filename, node.ID, node.TokenEstimate)
	}

	return nil
}
