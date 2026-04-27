// Package server hosts the remote-server related subcommands:
// serve, auth, device, sync, remote. All commands stay at the top level
// of the CLI (e.g. `ctx serve`); this package only organizes the source files.
package server

import "github.com/spf13/cobra"

// commands collects the top-level cobra commands defined in this package.
// Each file's init() appends to this slice via register(); Register() then
// attaches them all to the root command.
var commands []*cobra.Command

func register(c *cobra.Command) {
	commands = append(commands, c)
}

// Register attaches all server-cluster commands to the root command.
func Register(root *cobra.Command) {
	for _, c := range commands {
		root.AddCommand(c)
	}
}
