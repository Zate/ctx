package graph

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var linkType string

var linkCmd = &cobra.Command{
	Use:   "link <from-id> <to-id>",
	Short: "Link two nodes",
	Args:  cobra.ExactArgs(2),
	RunE:  runLink,
}

func init() {
	linkCmd.Flags().StringVar(&linkType, "type", "RELATES_TO", "Edge type")
	register(linkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	fromID, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}
	toID, err := cmdutil.ResolveArg(d, args[1])
	if err != nil {
		return err
	}

	edge, err := d.CreateEdge(fromID, toID, linkType)
	if err != nil {
		return err
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(edge, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Printf("Linked: %s → %s (%s)\n", fromID[:8], toID[:8], linkType)
	}

	return nil
}
