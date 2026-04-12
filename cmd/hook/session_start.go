package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/view"
)

var (
	sessionStartProject    string
	sessionStartAgent      string
	sessionStartPrimerFile string
	sessionStartFailClosed bool
)

var sessionStartCmd = &cobra.Command{
	Use:   "session-start",
	Short: "Handle SessionStart hook",
	RunE:  runSessionStart,
}

func init() {
	sessionStartCmd.Flags().StringVar(&sessionStartProject, "project", "", "Current project name for scoped context loading")
	sessionStartCmd.Flags().StringVar(&sessionStartAgent, "agent", "", "Agent identity for scoped memory (overrides global --agent)")
	sessionStartCmd.Flags().StringVar(&sessionStartPrimerFile, "primer-file", "", "Path to a markdown file with usage instructions to inject (replaces the built-in primer)")
	sessionStartCmd.Flags().BoolVar(&sessionStartFailClosed, "fail-closed", false, "If --project is empty, load zero nodes instead of every pinned node globally")
}

func runSessionStart(cmd *cobra.Command, args []string) error {
	dbPath := cmd.Root().PersistentFlags().Lookup("db").Value.String()

	d, err := db.Open(dbPath)
	if err != nil {
		// Fail gracefully - return empty output
		fmt.Fprintf(os.Stderr, "ctx: failed to open database: %v\n", err)
		fmt.Println("{}")
		return nil
	}
	defer d.Close()

	// Auto-sync pull (if configured) — gracefully fails
	autoSyncPull(d)

	// Read last_session_stores before resetting
	lastStores := -1
	if val, err := d.GetPending("last_session_stores"); err == nil && val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			lastStores = n
		}
	}

	// Reset session counters for new session
	_ = d.SetPending("session_turn_count", "0")
	_ = d.SetPending("session_store_count", "0")
	_ = d.DeletePending("transcript_cursor")

	// Resolve agent: local flag > global flag > env
	effectiveAgent := sessionStartAgent
	if effectiveAgent == "" {
		if globalAgent := cmd.Root().PersistentFlags().Lookup("agent"); globalAgent != nil {
			effectiveAgent = globalAgent.Value.String()
		}
	}

	// Store current agent for auto-tagging in remember commands
	if effectiveAgent != "" {
		_ = d.SetPending("current_agent", effectiveAgent)
	} else {
		_ = d.DeletePending("current_agent")
	}

	// Store current project for auto-tagging in remember commands
	if sessionStartProject != "" {
		_ = d.SetPending("current_project", sessionStartProject)
	} else {
		_ = d.DeletePending("current_project")
	}

	// Fail closed: when project detection failed upstream, emit an empty
	// context with a warning primer instead of leaking every pinned node
	// across projects. State has already been reset above.
	if sessionStartFailClosed && sessionStartProject == "" {
		result := &view.ComposeResult{
			RenderedAt:        time.Now().UTC(),
			LastSessionStores: lastStores,
			Primer: "ctx: **project not detected** — context load skipped (fail-closed).\n" +
				"Set `CTX_PROJECT` or run Claude from inside a git repo to enable scoped memory.\n",
		}
		context := view.RenderMarkdown(result)
		output := map[string]interface{}{
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":     "SessionStart",
				"additionalContext": context,
			},
		}
		data, _ := json.Marshal(output)
		fmt.Println(string(data))
		return nil
	}

	// Get default view query
	var queryStr string
	var budget int
	err = d.QueryRow("SELECT query, budget FROM views WHERE name = 'default'").Scan(&queryStr, &budget)
	if err != nil {
		queryStr = "tag:tier:pinned OR tag:tier:working"
		budget = 50000
	}

	// Check for expand_nodes pending
	expandJSON, err := d.GetPending("expand_nodes")
	var expandIDs []string
	if err == nil && expandJSON != "" {
		_ = json.Unmarshal([]byte(expandJSON), &expandIDs)
		_ = d.DeletePending("expand_nodes")
	}

	result, err := view.Compose(d, view.ComposeOptions{
		Query:                 queryStr,
		Budget:                budget,
		Project:               sessionStartProject,
		Agent:                 effectiveAgent,
		IncludeReferenceStats: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctx: failed to compose context: %v\n", err)
		fmt.Println("{}")
		return nil
	}

	// Add expanded nodes if any
	if len(expandIDs) > 0 {
		for _, id := range expandIDs {
			node, err := d.GetNode(id)
			if err != nil {
				continue
			}
			// Check if already included
			found := false
			for _, n := range result.Nodes {
				if n.ID == id {
					found = true
					break
				}
			}
			if !found {
				result.Nodes = append(result.Nodes, node)
				result.TotalTokens += node.TokenEstimate
				result.NodeCount++
			}
		}
	}

	result.LastSessionStores = lastStores

	// Load custom primer if specified, otherwise use built-in
	if sessionStartPrimerFile != "" {
		data, err := os.ReadFile(sessionStartPrimerFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctx: failed to read primer file %s: %v\n", sessionStartPrimerFile, err)
		} else {
			result.Primer = string(data)
		}
	}

	context := view.RenderMarkdown(result)

	output := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "SessionStart",
			"additionalContext": context,
		},
	}

	data, _ := json.Marshal(output)
	fmt.Println(string(data))
	return nil
}
