package graph

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var unlinkType string

var unlinkCmd = &cobra.Command{
	Use:   "unlink <from-id> <to-id>",
	Short: "Unlink two nodes",
	Args:  cobra.ExactArgs(2),
	RunE:  runUnlink,
}

func init() {
	unlinkCmd.Flags().StringVar(&unlinkType, "type", "", "Edge type (optional, removes all if not specified)")
	register(unlinkCmd)
}

func runUnlink(cmd *cobra.Command, args []string) error {
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

	if err := d.DeleteEdge(fromID, toID, unlinkType); err != nil {
		return err
	}

	fmt.Printf("Unlinked: %s → %s\n", fromID[:8], toID[:8])
	return nil
}
