package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
	hookpkg "github.com/zate/ctx/internal/hook"
	"github.com/zate/ctx/internal/query"
)

var promptSubmitCmd = &cobra.Command{
	Use:   "prompt-submit",
	Short: "Handle UserPromptSubmit hook",
	RunE:  runPromptSubmit,
}

func runPromptSubmit(cmd *cobra.Command, args []string) error {
	dbPath := cmd.Root().PersistentFlags().Lookup("db").Value.String()

	d, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctx: failed to open database: %v\n", err)
		fmt.Println("{}")
		return nil
	}
	defer d.Close()

	// Parse ctx commands from transcript (incremental via cursor)
	transcriptPath, _ := readTranscriptPathFromStdin()
	if transcriptPath != "" {
		var cursor int64
		if val, err := d.GetPending("transcript_cursor"); err == nil && val != "" {
			_, _ = fmt.Sscanf(val, "%d", &cursor)
		}

		response, newOffset, err := readAssistantResponsesFromOffset(transcriptPath, cursor)
		if err == nil && response != "" {
			commands := hookpkg.ParseCtxCommands(response)
			if len(commands) > 0 {
				errs := hookpkg.ExecuteCommandsWithErrors(d, commands)
				for _, e := range errs {
					fmt.Fprintf(os.Stderr, "ctx: %v\n", e)
				}

				// Count successful remembers
				rememberCount := 0
				for _, cmd := range commands {
					if cmd.Type == "remember" {
						rememberCount++
					}
				}
				rememberErrCount := 0
				for _, e := range errs {
					if strings.HasPrefix(e.Error(), "remember") {
						rememberErrCount++
					}
				}
				if successCount := rememberCount - rememberErrCount; successCount > 0 {
					existing, err := d.GetPending("session_store_count")
					prev := 0
					if err == nil && existing != "" {
						_, _ = fmt.Sscanf(existing, "%d", &prev)
					}
					_ = d.SetPending("session_store_count", fmt.Sprintf("%d", prev+successCount))
				}
			}
		}
		if err == nil {
			_ = d.SetPending("transcript_cursor", fmt.Sprintf("%d", newOffset))
		}
	}

	// Resolve agent for filtering
	currentAgent, _ := d.GetPending("current_agent")

	var contextParts []string

	// Check for recall query
	recallQuery, err := d.GetPending("recall_query")
	if err == nil && recallQuery != "" {
		nodes, err := query.ExecuteQuery(d, recallQuery, false)
		if err == nil {
			// Filter by agent partition
			nodes = filterNodesByAgent(nodes, currentAgent)

			if len(nodes) > 0 {
				ids := make([]string, len(nodes))
				for i, n := range nodes {
					ids[i] = n.ID
				}
				_ = d.LogAccessBatch(ids, "explicit_query", currentAgent, "recall:"+recallQuery)
			}

			var b strings.Builder
			fmt.Fprintf(&b, "## Recall Results\n\nQuery: `%s`\n\n", recallQuery)
			if len(nodes) == 0 {
				b.WriteString("No matching nodes found.\n")
			} else {
				fmt.Fprintf(&b, "Found %d nodes:\n\n", len(nodes))
				for _, n := range nodes {
					fmt.Fprintf(&b, "- [%s:%s] %s\n", n.Type, n.ID, n.Content)
					if len(n.Tags) > 0 {
						fmt.Fprintf(&b, "  - Tags: %s\n", strings.Join(n.Tags, ", "))
					}
				}
			}
			b.WriteString("\n---\n")
			contextParts = append(contextParts, b.String())
		}
		_ = d.DeletePending("recall_query")
	}

	// Check for recall_results (pre-computed)
	recallResults, err := d.GetPending("recall_results")
	if err == nil && recallResults != "" {
		contextParts = append(contextParts, recallResults)
		_ = d.DeletePending("recall_results")
	}

	// Check for status output
	statusOutput, err := d.GetPending("status_output")
	if err == nil && statusOutput != "" {
		contextParts = append(contextParts, "## Memory Status\n\n"+statusOutput+"\n\n---\n")
		_ = d.DeletePending("status_output")
	}

	// Check for expand nodes
	expandJSON, err := d.GetPending("expand_nodes")
	if err == nil && expandJSON != "" {
		var expandIDs []string
		_ = json.Unmarshal([]byte(expandJSON), &expandIDs)

		if len(expandIDs) > 0 {
			var b strings.Builder
			b.WriteString("## Expanded Nodes\n\n")
			for _, id := range expandIDs {
				node, err := d.GetNode(id)
				if err != nil {
					continue
				}
				fmt.Fprintf(&b, "- [%s:%s] %s\n", node.Type, node.ID, node.Content)
				if len(node.Tags) > 0 {
					fmt.Fprintf(&b, "  - Tags: %s\n", strings.Join(node.Tags, ", "))
				}
			}
			b.WriteString("\n---\n")
			contextParts = append(contextParts, b.String())
		}
		_ = d.DeletePending("expand_nodes")
	}

	// Increment session turn count
	turnCount := 0
	if val, err := d.GetPending("session_turn_count"); err == nil && val != "" {
		turnCount, _ = strconv.Atoi(val)
	}
	turnCount++
	_ = d.SetPending("session_turn_count", strconv.Itoa(turnCount))

	// Nudge if 4+ turns with no stores this session
	if turnCount >= 4 {
		storeCount := 0
		if val, err := d.GetPending("session_store_count"); err == nil && val != "" {
			storeCount, _ = strconv.Atoi(val)
		}
		if storeCount == 0 {
			contextParts = append(contextParts, "<!-- ctx: No knowledge stored this session. Consider:\n- Constraints/preferences → type=fact, tier:pinned\n- Foundational decisions → type=decision, tier:pinned\n- Active conventions → type=pattern, tier:pinned\n- Task-scoped work → tier:working\n- Durable but not critical → tier:reference (accessed via recall) -->")
		}
	}

	if len(contextParts) == 0 {
		fmt.Println("{}")
		return nil
	}

	additionalContext := strings.Join(contextParts, "\n")

	output := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": additionalContext,
		},
	}

	data, _ := json.Marshal(output)
	fmt.Println(string(data))
	return nil
}
