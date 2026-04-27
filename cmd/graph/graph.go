// Package graph hosts edge and traversal subcommands: link, unlink, edges,
// related, trace. Commands stay at the top level (e.g. `ctx link`).
package graph

import "github.com/spf13/cobra"

var commands []*cobra.Command

func register(c *cobra.Command) {
	commands = append(commands, c)
}

// Register attaches all graph commands to the root command.
func Register(root *cobra.Command) {
	for _, c := range commands {
		root.AddCommand(c)
	}
}
