package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	installMCP bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ctx (DEPRECATED: use ctx plugin for Claude Code)",
	Long:  "DEPRECATED: 'ctx install' now delegates to 'ctx init' for database setup only. Use the ctx plugin for Claude Code instead.",
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&installMCP, "mcp", false, "Output MCP server configuration for Claude Desktop")
	register(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	if installMCP {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine home directory: %w", err)
		}
		return printMCPConfig(home)
	}

	fmt.Fprintln(os.Stderr, "DEPRECATED: 'ctx install' is deprecated. Use the ctx plugin for Claude Code instead.")
	fmt.Fprintln(os.Stderr, "Running 'ctx init' (database setup only)...")
	fmt.Fprintln(os.Stderr, "")

	return runInit(cmd, args)
}

// printMCPConfig outputs Claude Desktop MCP configuration for the ctx server.
func printMCPConfig(home string) error {
	// Find the ctx binary path
	ctxPath, err := findCtxBinary()
	if err != nil {
		return err
	}

	dbPathStr := filepath.Join(home, ".ctx", "store.db")

	config := map[string]any{
		"mcpServers": map[string]any{
			"ctx": map[string]any{
				"command": ctxPath,
				"args":    []string{"mcp"},
				"env": map[string]string{
					"CTX_DB": dbPathStr,
				},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	fmt.Println("Add this to your Claude Desktop configuration:")
	fmt.Println()
	fmt.Println(string(data))
	fmt.Println()
	fmt.Println("Configuration file locations:")
	fmt.Println("  macOS:   ~/Library/Application Support/Claude/claude_desktop_config.json")
	fmt.Println("  Linux:   ~/.config/Claude/claude_desktop_config.json")
	fmt.Println("  Windows: %APPDATA%/Claude/claude_desktop_config.json")

	return nil
}

// findCtxBinary returns the path to the ctx binary, preferring PATH lookup.
func findCtxBinary() (string, error) {
	name := "ctx"
	if runtime.GOOS == "windows" {
		name = "ctx.exe"
	}
	// Check if ctx is in PATH
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Fall back to current executable
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine ctx binary path: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}
