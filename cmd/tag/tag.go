package tag

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var tagCmd = &cobra.Command{
	Use:   "tag <id> <tag>...",
	Short: "Add tags to a node",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runTag,
}

func init() {
	register(tagCmd)
}

func runTag(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	nodeID, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}
	for _, tag := range args[1:] {
		if err := d.AddTag(nodeID, tag); err != nil {
			return fmt.Errorf("failed to add tag %s: %w", tag, err)
		}
	}

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFOk(os.Stdout, "tagged", "id", nodeID, "tags", strings.Join(args[1:], "|"))
		return nil
	}
	fmt.Printf("Tagged: %s with %s\n", nodeID[:8], strings.Join(args[1:], ", "))
	return nil
}
