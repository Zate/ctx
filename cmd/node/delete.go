package node

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	agentpkg "github.com/zate/ctx/internal/agent"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a node",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func init() {
	register(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	id, err := cmdutil.ResolveArg(d, args[0])
	if err != nil {
		return err
	}

	// Agent guard: only allow deleting nodes visible to the current agent
	node, err := d.GetNode(id)
	if err != nil {
		return err
	}
	if !agentpkg.ShouldInclude(node, cmdutil.Agent(cmd)) {
		return fmt.Errorf("node %s is not accessible to the current agent scope", id[:8])
	}

	if err := d.DeleteNode(id); err != nil {
		return err
	}

	if cmdutil.AgentOut(cmd) {
		cmdutil.AOFOk(os.Stdout, "deleted", "id", id)
		return nil
	}
	fmt.Printf("Deleted: %s\n", id)
	return nil
}
