// Package node hosts core node CRUD subcommands: add, show, list, delete,
// update, search, query.
package node

import "github.com/spf13/cobra"

var commands []*cobra.Command

func register(c *cobra.Command) {
	commands = append(commands, c)
}

// Register attaches all node commands to the root command.
func Register(root *cobra.Command) {
	for _, c := range commands {
		root.AddCommand(c)
	}
}
