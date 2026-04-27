package hook

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
	hookpkg "github.com/zate/ctx/internal/hook"
)

var stopResponse string

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Handle Stop hook",
	RunE:  runStop,
}

func init() {
	stopCmd.Flags().StringVar(&stopResponse, "response", "", "Claude's response text (for testing; otherwise reads transcript)")
}

func runStop(cmd *cobra.Command, args []string) error {
	dbPath := cmd.Root().PersistentFlags().Lookup("db").Value.String()

	d, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctx: failed to open database: %v\n", err)
		fmt.Println("{}")
		return nil
	}
	defer d.Close()

	var response string

	if stopResponse != "" {
		response = stopResponse
	} else {
		// Read stdin for hook input
		transcriptPath, err := readTranscriptPathFromStdin()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctx: failed to read hook input: %v\n", err)
			fmt.Println("{}")
			return nil
		}

		if transcriptPath != "" {
			// Use cursor to only read new content since last prompt-submit
			var cursor int64
			if val, err := d.GetPending("transcript_cursor"); err == nil && val != "" {
				_, _ = fmt.Sscanf(val, "%d", &cursor)
			}

			resp, _, err := readAssistantResponsesFromOffset(transcriptPath, cursor)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ctx: failed to read transcript: %v\n", err)
				fmt.Println("{}")
				return nil
			}
			response = resp
		}
	}

	if response == "" {
		fmt.Println("{}")
		return nil
	}

	// Ensure current_agent is set from global --agent flag if not already stored
	// (stop hook may run without a preceding session-start in some contexts)
	if globalAgent := cmd.Root().PersistentFlags().Lookup("agent"); globalAgent != nil {
		agentVal := globalAgent.Value.String()
		if agentVal != "" {
			existing, err := d.GetPending("current_agent")
			if err != nil || existing == "" {
				_ = d.SetPending("current_agent", agentVal)
			}
		}
	}

	// Parse ctx commands
	commands := hookpkg.ParseCtxCommands(response)
	if len(commands) == 0 {
		fmt.Println("{}")
		return nil
	}

	// Execute commands and track remember successes
	errs := hookpkg.ExecuteCommandsWithErrors(d, commands)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "ctx: %v\n", e)
		}
	}

	// Count successful remember commands for session tracking
	rememberCount := 0
	for _, cmd := range commands {
		if cmd.Type == "remember" {
			rememberCount++
		}
	}
	// Subtract failures (conservative: count remember errs)
	rememberErrCount := 0
	for _, e := range errs {
		if strings.HasPrefix(e.Error(), "remember") {
			rememberErrCount++
		}
	}
	successCount := rememberCount - rememberErrCount

	// Update session store count
	if successCount > 0 {
		existing, err := d.GetPending("session_store_count")
		prev := 0
		if err == nil && existing != "" {
			_, _ = fmt.Sscanf(existing, "%d", &prev)
		}
		_ = d.SetPending("session_store_count", fmt.Sprintf("%d", prev+successCount))
	}

	// Store last_session_stores for next session's awareness
	storeCount, err := d.GetPending("session_store_count")
	if err == nil && storeCount != "" {
		_ = d.SetPending("last_session_stores", storeCount)
	} else {
		_ = d.SetPending("last_session_stores", "0")
	}

	// Auto-sync push (if configured) — gracefully fails
	autoSyncPush(d)

	fmt.Println("{}")
	return nil
}
