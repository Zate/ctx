package doc

import "github.com/spf13/cobra"

// Register attaches the doc command to the root command.
func Register(root *cobra.Command) {
	root.AddCommand(docCmd)
}
