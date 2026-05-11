package node

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
)

var (
	updateContent string
	updateType    string
	updateMeta    string
)

var updateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a node",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateContent, "content", "", "New content")
	updateCmd.Flags().StringVar(&updateType, "type", "", "New type")
	updateCmd.Flags().StringVar(&updateMeta, "meta", "", "New metadata JSON")
	register(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	input := db.UpdateNodeInput{}
	if updateContent != "" {
		input.Content = &updateContent
	}
	if updateType != "" {
		input.Type = &updateType
	}
	if updateMeta != "" {
		input.Metadata = &updateMeta
	}

	id, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}

	node, err := d.UpdateNode(id, input)
	if err != nil {
		return err
	}

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFNode(os.Stdout, node, "updated")
		return nil
	}
	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Printf("Updated: %s\n", node.ID)
	}

	return nil
}
