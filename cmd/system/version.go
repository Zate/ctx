package system

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// SetVersionInfo is called from main to inject build-time values.
func SetVersionInfo(v, c, d string) {
	version = v
	commit = c
	date = d
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print ctx version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ctx %s (commit %s, built %s)\n", version, commit, date)
	},
}

func init() {
	register(versionCmd)
}
