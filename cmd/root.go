package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	graphcmd "github.com/zate/ctx/cmd/graph"
	"github.com/zate/ctx/cmd/hook"
	iocmd "github.com/zate/ctx/cmd/io"
	mcpcmd "github.com/zate/ctx/cmd/mcp"
	servercmd "github.com/zate/ctx/cmd/server"
	tagcmd "github.com/zate/ctx/cmd/tag"
	agentpkg "github.com/zate/ctx/internal/agent"
	"github.com/zate/ctx/internal/agenthelp"
	"github.com/zate/ctx/internal/db"
)

var (
	dbPath    string
	format    string
	backend   string
	agent     string
	agentHelp bool
)

var rootCmd = &cobra.Command{
	Use:   "ctx",
	Short: "Persistent context management for Claude",
	Long:  "A CLI tool for managing persistent, structured memory across conversations.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	home, _ := os.UserHomeDir()
	defaultDB := filepath.Join(home, ".ctx", "store.db")
	if envDB := os.Getenv("CTX_DB"); envDB != "" {
		defaultDB = envDB
	}
	defaultBackend := "sqlite"
	if envBackend := os.Getenv("CTX_BACKEND"); envBackend != "" {
		defaultBackend = envBackend
	}
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", defaultDB, "Database path (file path for sqlite, connection string for postgres)")
	rootCmd.PersistentFlags().StringVar(&format, "format", "text", "Output format: text, json, markdown")
	rootCmd.PersistentFlags().StringVar(&backend, "backend", defaultBackend, "Database backend: sqlite, postgres")
	defaultAgent := os.Getenv("CTX_AGENT")
	rootCmd.PersistentFlags().StringVar(&agent, "agent", defaultAgent, "Agent identity for memory partitioning (filters to agent-scoped + global nodes)")
	rootCmd.PersistentFlags().BoolVar(&agentHelp, "agent-help", false, "Token-optimized help for LLM agents")
	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() + "\nLLM agent? Use --agent-help for token-optimized usage.\n")
	rootCmd.AddCommand(hook.HookCmd)
	mcpcmd.Register(rootCmd)
	servercmd.Register(rootCmd)
	graphcmd.Register(rootCmd)
	tagcmd.Register(rootCmd)
	iocmd.Register(rootCmd)
}

func Execute() error {
	if handleAgentHelp() {
		return nil
	}
	return rootCmd.Execute()
}

// handleAgentHelp checks os.Args for --agent-help and handles it before Cobra
// dispatches (avoiding arg-validation errors on commands that require positional args).
func handleAgentHelp() bool {
	args := os.Args[1:]
	found := false
	var cmdArgs []string
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--agent-help" {
			found = true
		} else if skip, consumesNext := isGlobalFlag(a); skip {
			skipNext = consumesNext
		} else {
			cmdArgs = append(cmdArgs, a)
		}
	}
	if !found {
		return false
	}

	if len(cmdArgs) == 0 {
		agenthelp.PrintIndex(os.Stdout, rootCmd)
	} else {
		cmd := agenthelp.ResolveCommand(rootCmd, cmdArgs)
		if cmd == nil {
			agenthelp.FormatError(os.Stderr, rootCmd, cmdArgs[0])
			os.Exit(1)
		}
		agenthelp.PrintCommand(os.Stdout, rootCmd, cmd)
	}
	return true
}

// isGlobalFlag returns whether arg is a global flag to strip, and whether it consumes the next arg.
func isGlobalFlag(arg string) (skip bool, consumesNext bool) {
	for _, f := range []string{"--db", "--format", "--backend", "--agent"} {
		if strings.HasPrefix(arg, f+"=") {
			return true, false
		}
		if arg == f {
			return true, true
		}
	}
	return false, false
}

func openDB() (db.Store, error) {
	switch backend {
	case "postgres", "postgresql":
		d, err := db.OpenPostgres(dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open postgres database: %w", err)
		}
		return d, nil
	case "sqlite", "":
		d, err := db.Open(dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open database: %w", err)
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unknown backend %q: use 'sqlite' or 'postgres'", backend)
	}
}

// resolveArg resolves a node ID prefix to a full ID using the database.
func resolveArg(d db.Store, prefix string) (string, error) {
	resolved, err := d.ResolveID(prefix)
	if err != nil {
		return "", fmt.Errorf("cannot resolve ID %q: %w", prefix, err)
	}
	return resolved, nil
}

// agentTag returns the agent tag string for the current agent, or empty if no agent is set.
func agentTag() string {
	return agentpkg.Tag(agent)
}

// filterNodesByAgent filters a slice of nodes to only include those visible to the current agent.
func filterNodesByAgent(nodes []*db.Node) []*db.Node {
	return agentpkg.FilterNodes(nodes, agent)
}

// logAccessNodes records access for the given nodes using LogAccessBatch.
// Errors are swallowed; the DB-layer kind='memory' guard handles non-memory IDs.
func logAccessNodes(d db.Store, nodes []*db.Node, accessType, queryContext string) {
	if len(nodes) == 0 {
		return
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	_ = d.LogAccessBatch(ids, accessType, agent, queryContext)
}
