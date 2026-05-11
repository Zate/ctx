package view

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
)

var (
	summarizeContent string
	archiveSources   bool
)

var summarizeCmd = &cobra.Command{
	Use:   "summarize <id>...",
	Short: "Create a summary from nodes",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runSummarize,
}

func init() {
	summarizeCmd.Flags().StringVar(&summarizeContent, "content", "", "Summary content (required)")
	_ = summarizeCmd.MarkFlagRequired("content")
	summarizeCmd.Flags().BoolVar(&archiveSources, "archive-sources", false, "Tag sources as tier:off-context")
	register(summarizeCmd)
}

func runSummarize(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	summary, err := d.CreateNode(db.CreateNodeInput{
		Type:    "summary",
		Content: summarizeContent,
	})
	if err != nil {
		return err
	}

	for _, sourceID := range args {
		_, err := d.CreateEdge(summary.ID, sourceID, "DERIVED_FROM")
		if err != nil {
			return fmt.Errorf("failed to create edge to %s: %w", sourceID, err)
		}

		if archiveSources {
			_ = d.RemoveTag(sourceID, "tier:working")
			_ = d.RemoveTag(sourceID, "tier:reference")
			_ = d.RemoveTag(sourceID, "tier:pinned")
			_ = d.AddTag(sourceID, "tier:off-context")
		}
	}

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFNode(os.Stdout, summary, "created")
		if archiveSources {
			fmt.Fprintf(os.Stdout, "archived %d\n", len(args))
		}
		return nil
	}
	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Printf("Created summary: %s\n", summary.ID)
		if archiveSources {
			fmt.Printf("Archived %d source(s)\n", len(args))
		}
	}

	return nil
}
