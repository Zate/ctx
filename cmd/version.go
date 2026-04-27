package cmd

import systemcmd "github.com/zate/ctx/cmd/system"

// SetVersionInfo forwards build-time version info to the system subpackage
// where the `version` command lives. main.go still calls cmd.SetVersionInfo
// so the cmd/ public surface is unchanged.
func SetVersionInfo(v, c, d string) {
	systemcmd.SetVersionInfo(v, c, d)
}
