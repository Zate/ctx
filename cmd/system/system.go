// Package system hosts non-domain "plumbing" subcommands: init, install,
// status, version, accessed.
package system

import "github.com/spf13/cobra"

var commands []*cobra.Command

func register(c *cobra.Command) {
	commands = append(commands, c)
}

// Register attaches all system commands to the root command.
func Register(root *cobra.Command) {
	for _, c := range commands {
		root.AddCommand(c)
	}
}
