package system

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the ctx database",
	Long:  "Creates the ~/.ctx/ directory and initializes the database. This is the minimal setup needed to use ctx.",
	RunE:  runInit,
}

func init() {
	register(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	ctxDir := filepath.Join(home, ".ctx")
	if err := os.MkdirAll(ctxDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", ctxDir, err)
	}

	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	d.Close()

	dbPathStr := filepath.Join(ctxDir, "store.db")
	fmt.Printf("Database ready: %s\n", dbPathStr)
	return nil
}
