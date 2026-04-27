// Package tag hosts the tag, untag, and tags subcommands.
package tag

import "github.com/spf13/cobra"

var commands []*cobra.Command

func register(c *cobra.Command) {
	commands = append(commands, c)
}

// Register attaches all tag commands to the root command.
func Register(root *cobra.Command) {
	for _, c := range commands {
		root.AddCommand(c)
	}
}
