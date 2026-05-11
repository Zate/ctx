package tag

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var untagCmd = &cobra.Command{
	Use:   "untag <id> <tag>",
	Short: "Remove a tag from a node",
	Args:  cobra.ExactArgs(2),
	RunE:  runUntag,
}

func init() {
	register(untagCmd)
}

func runUntag(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	id, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}

	if err := d.RemoveTag(id, args[1]); err != nil {
		return err
	}

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFOk(os.Stdout, "untagged", "id", id, "tag", args[1])
		return nil
	}
	fmt.Printf("Untagged: %s from %s\n", args[1], id)
	return nil
}
