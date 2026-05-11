//go:build integration

package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// hookHarness simulates the full Claude Code hook lifecycle against an isolated DB.
// It shells out to the ctx binary so that stdin/stdout work exactly as they do
// in production (the hooks read os.Stdin directly, not cobra's InOrStdin).
type hookHarness struct {
	t       *testing.T
	dbPath  string
	binPath string
	tmpDir  string
}

func newHookHarness(t *testing.T) *hookHarness {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Initialize DB
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	d.Close()

	// Build the binary once (use the one at /tmp/ctx-test-bin or build it)
	binPath := filepath.Join(dir, "ctx")
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = findRepoRoot(t)
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		// Fallback: try to use system binary
		if path, lookErr := exec.LookPath("ctx"); lookErr == nil {
			binPath = path
		} else {
			t.Fatalf("failed to build ctx binary: %v\n%s", err, out)
		}
	}

	return &hookHarness{
		t:       t,
		dbPath:  dbPath,
		binPath: binPath,
		tmpDir:  dir,
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from current dir to find go.mod
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

func (h *hookHarness) openDB() db.Store {
	h.t.Helper()
	d, err := db.Open(h.dbPath)
	require.NoError(h.t, err)
	return d
}

// writeTranscriptFile writes JSONL transcript entries to a temp file.
func (h *hookHarness) writeTranscriptFile(entries []map[string]any) string {
	h.t.Helper()
	path := filepath.Join(h.tmpDir, "transcript.jsonl")
	f, err := os.Create(path)
	require.NoError(h.t, err)
	defer f.Close()
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		require.NoError(h.t, err)
		_, _ = f.Write(data)
		_, _ = f.Write([]byte("\n"))
	}
	return path
}

// appendTranscriptEntries appends new entries to an existing transcript file.
func (h *hookHarness) appendTranscriptEntries(path string, entries []map[string]any) {
	h.t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	require.NoError(h.t, err)
	defer f.Close()
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		require.NoError(h.t, err)
		_, _ = f.Write(data)
		_, _ = f.Write([]byte("\n"))
	}
}

// run executes the ctx binary with the given args and optional stdin.
func (h *hookHarness) run(args []string, stdin string) (string, string) {
	h.t.Helper()
	cmd := exec.Command(h.binPath, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		h.t.Logf("cmd %v failed: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

// runSessionStart calls the session-start hook command.
func (h *hookHarness) runSessionStart(project, agent string) string {
	h.t.Helper()
	args := []string{"hook", "session-start", "--db", h.dbPath}
	if project != "" {
		args = append(args, "--project="+project)
	}
	if agent != "" {
		args = append(args, "--agent="+agent)
	}
	out, _ := h.run(args, "")
	return out
}

// runSessionStartFailClosed calls session-start with --fail-closed.
func (h *hookHarness) runSessionStartFailClosed(project string) string {
	h.t.Helper()
	args := []string{"hook", "session-start", "--db", h.dbPath, "--fail-closed"}
	if project != "" {
		args = append(args, "--project="+project)
	}
	out, _ := h.run(args, "")
	return out
}

// runPromptSubmit calls the prompt-submit hook with transcript path on stdin.
func (h *hookHarness) runPromptSubmit(transcriptPath, agent string) string {
	h.t.Helper()
	stdinJSON := fmt.Sprintf(`{"transcript_path":"%s"}`, transcriptPath)
	args := []string{"hook", "prompt-submit", "--db", h.dbPath}
	if agent != "" {
		args = append(args, "--agent="+agent)
	}
	out, _ := h.run(args, stdinJSON)
	return out
}

// runStop calls the stop hook with transcript path on stdin.
func (h *hookHarness) runStop(transcriptPath, agent string) string {
	h.t.Helper()
	stdinJSON := fmt.Sprintf(`{"transcript_path":"%s"}`, transcriptPath)
	args := []string{"hook", "stop", "--db", h.dbPath}
	if agent != "" {
		args = append(args, "--agent="+agent)
	}
	out, _ := h.run(args, stdinJSON)
	return out
}

// runStopNoStdin calls the stop hook with empty stdin (simulates the Nyx bug).
func (h *hookHarness) runStopNoStdin(agent string) (string, string) {
	h.t.Helper()
	args := []string{"hook", "stop", "--db", h.dbPath}
	if agent != "" {
		args = append(args, "--agent="+agent)
	}
	return h.run(args, "")
}

// runStopWithResponse calls the stop hook with --response flag.
func (h *hookHarness) runStopWithResponse(response, agent string) string {
	h.t.Helper()
	args := []string{"hook", "stop", "--db", h.dbPath, "--response", response}
	if agent != "" {
		args = append(args, "--agent="+agent)
	}
	out, _ := h.run(args, "")
	return out
}

// listNodes returns all nodes in the test DB.
func (h *hookHarness) listNodes() []*db.Node {
	h.t.Helper()
	d := h.openDB()
	defer d.Close()
	nodes, err := d.ListNodes(db.ListOptions{})
	require.NoError(h.t, err)
	return nodes
}

// nodeCount returns the number of nodes in the test DB.
func (h *hookHarness) nodeCount() int {
	h.t.Helper()
	return len(h.listNodes())
}

// getPending reads a pending value from the test DB.
func (h *hookHarness) getPending(key string) string {
	h.t.Helper()
	d := h.openDB()
	defer d.Close()
	val, _ := d.GetPending(key)
	return val
}

// getNodeTags returns all tags for a node.
func (h *hookHarness) getNodeTags(nodeID string) []string {
	h.t.Helper()
	d := h.openDB()
	defer d.Close()
	tags, err := d.GetTags(nodeID)
	require.NoError(h.t, err)
	return tags
}

// assistantEntry creates a transcript entry for an assistant message with text blocks.
func assistantEntry(texts ...string) map[string]any {
	var blocks []any
	for _, t := range texts {
		blocks = append(blocks, map[string]any{"type": "text", "text": t})
	}
	return map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"content": blocks,
		},
	}
}

// userEntry creates a transcript entry for a user message.
func userEntry(text string) map[string]any {
	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"content": text,
		},
	}
}

// =============================================================================
// Integration Tests: Full Hook Lifecycle
// =============================================================================

func TestIntegration_FullSessionLifecycle(t *testing.T) {
	h := newHookHarness(t)

	// 1. Session start
	h.runSessionStart("testproject", "")

	assert.Equal(t, "testproject", h.getPending("current_project"))
	assert.Equal(t, "0", h.getPending("session_turn_count"))
	assert.Equal(t, "0", h.getPending("session_store_count"))
	assert.Equal(t, "", h.getPending("transcript_cursor"))

	// 2. First turn: agent responds with a ctx:remember command
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(
			"Let me store this fact.\n\n" +
				`<ctx:remember type="fact" tags="tier:pinned">Always run tests.</ctx:remember>`,
		),
	})

	// 3. Prompt-submit processes the response
	h.runPromptSubmit(transcript, "")

	assert.Equal(t, 1, h.nodeCount(), "should have created 1 node")
	assert.Equal(t, "1", h.getPending("session_store_count"))
	assert.Equal(t, "1", h.getPending("session_turn_count"))

	nodes := h.listNodes()
	require.Len(t, nodes, 1)
	assert.Equal(t, "fact", nodes[0].Type)
	assert.Equal(t, "Always run tests.", nodes[0].Content)

	tags := h.getNodeTags(nodes[0].ID)
	assert.Contains(t, tags, "tier:pinned")
	assert.Contains(t, tags, "project:testproject")

	// 4. Second turn: another response with more commands
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("Continue"),
		assistantEntry(
			`<ctx:remember type="decision" tags="tier:reference">Use SQLite for storage.</ctx:remember>`,
		),
	})
	h.runPromptSubmit(transcript, "")

	assert.Equal(t, 2, h.nodeCount(), "should have 2 nodes now")
	assert.Equal(t, "2", h.getPending("session_store_count"))
	assert.Equal(t, "2", h.getPending("session_turn_count"))

	// 5. Final turn: last response with commands (only stop hook sees this)
	h.appendTranscriptEntries(transcript, []map[string]any{
		assistantEntry(
			`<ctx:remember type="observation" tags="tier:working">The final insight.</ctx:remember>`,
		),
	})

	// 6. Stop hook picks up the last response
	h.runStop(transcript, "")

	assert.Equal(t, 3, h.nodeCount(), "stop hook should have stored the 3rd node")
}

func TestIntegration_StopHookNoStdin_LosesNodes(t *testing.T) {
	// This test reproduces the Nyx stop hook bug where stdin is consumed
	// before ctx hook stop is called, causing it to get no transcript path.
	h := newHookHarness(t)

	h.runSessionStart("book", "nyx")

	// Agent responds with ctx:remember in the LAST message (no prompt-submit after)
	_ = h.writeTranscriptFile([]map[string]any{
		userEntry("Tell me about the project"),
		assistantEntry(
			`<ctx:remember type="decision" tags="tier:pinned,project:Book">Important decision from final response.</ctx:remember>`,
		),
	})

	// Stop hook called with no stdin (the bug)
	_, stderr := h.runStopNoStdin("nyx")
	assert.Contains(t, stderr, "failed to read hook input",
		"stop hook should report stdin failure")

	// The node is LOST — this is the bug
	assert.Equal(t, 0, h.nodeCount(),
		"BUG CONFIRMED: stop hook with no stdin loses the final response's ctx commands")
}

func TestIntegration_StopHookWithStdin_Works(t *testing.T) {
	// Contrast: when stop hook gets proper stdin, it works
	h := newHookHarness(t)

	h.runSessionStart("book", "nyx")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Tell me about the project"),
		assistantEntry(
			`<ctx:remember type="decision" tags="tier:pinned,project:Book">Important decision from final response.</ctx:remember>`,
		),
	})

	// Stop hook called WITH stdin (correct behavior)
	h.runStop(transcript, "nyx")

	assert.Equal(t, 1, h.nodeCount(),
		"stop hook with proper stdin should store the node")
}

// =============================================================================
// Integration Tests: Agent Partitioning Visibility
// =============================================================================

func TestIntegration_AgentPartitioning_HidesNodesWithoutFlag(t *testing.T) {
	// Reproduces the visibility issue: nodes stored with agent:nyx are invisible
	// when queried without --agent
	h := newHookHarness(t)

	// Session with --agent=nyx stores nodes with agent:nyx tag
	h.runSessionStart("book", "nyx")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Store something"),
		assistantEntry(
			`<ctx:remember type="fact" tags="tier:pinned,project:Book">Agent-scoped fact.</ctx:remember>`,
		),
	})

	h.runPromptSubmit(transcript, "nyx")

	// Node exists in DB
	assert.Equal(t, 1, h.nodeCount())
	nodes := h.listNodes()
	tags := h.getNodeTags(nodes[0].ID)
	assert.Contains(t, tags, "agent:nyx", "should have agent:nyx tag")

	// ctx list WITHOUT --agent should hide agent-scoped nodes
	out, _ := h.run([]string{"list", "--db", h.dbPath}, "")
	assert.NotContains(t, out, "Agent-scoped fact",
		"Without --agent, agent-scoped nodes should be hidden from list")

	// ctx list WITH --agent=nyx should show them
	out2, _ := h.run([]string{"list", "--db", h.dbPath, "--agent=nyx"}, "")
	assert.Contains(t, out2, "Agent-scoped fact",
		"With --agent=nyx, the node should be visible")
}

func TestIntegration_AgentPartitioning_GlobalNodesAlwaysVisible(t *testing.T) {
	h := newHookHarness(t)

	// Session WITHOUT agent — nodes are global
	h.runSessionStart("book", "")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Store something"),
		assistantEntry(
			`<ctx:remember type="fact" tags="tier:pinned,project:book">Global fact.</ctx:remember>`,
		),
	})
	h.runPromptSubmit(transcript, "")

	nodes := h.listNodes()
	require.Len(t, nodes, 1)
	tags := h.getNodeTags(nodes[0].ID)
	for _, tag := range tags {
		assert.False(t, strings.HasPrefix(tag, "agent:"),
			"global node should NOT have agent tag, got: %s", tag)
	}

	// Visible without agent
	out, _ := h.run([]string{"list", "--db", h.dbPath}, "")
	assert.Contains(t, out, "Global fact", "global nodes visible without --agent")

	// Also visible with an agent
	out2, _ := h.run([]string{"list", "--db", h.dbPath, "--agent=nyx"}, "")
	assert.Contains(t, out2, "Global fact", "global nodes visible with --agent too")
}

// =============================================================================
// Integration Tests: Cursor-based Incremental Processing
// =============================================================================

func TestIntegration_CursorTracking_IncrementalProcessing(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	// Turn 1
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">Fact one.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 1, h.nodeCount())

	// Turn 2: append to same transcript
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("More"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">Fact two.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 2, h.nodeCount(), "should have 2 nodes, not re-process turn 1")

	// Turn 3
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("Even more"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">Fact three.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 3, h.nodeCount(), "should have 3 nodes total")

	// Stop should add nothing new (cursor past all entries)
	h.runStop(transcript, "")
	assert.Equal(t, 3, h.nodeCount(), "stop should not duplicate")
}

func TestIntegration_StopHookCatchesFinalResponse(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	// Turn 1: processed by prompt-submit
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">From turn 1.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 1, h.nodeCount())

	// Turn 2
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("Continue"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">From turn 2.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 2, h.nodeCount())

	// Final response: no prompt-submit after this
	h.appendTranscriptEntries(transcript, []map[string]any{
		assistantEntry(`<ctx:remember type="decision" tags="tier:pinned">Final decision.</ctx:remember>`),
	})

	// Stop hook catches it
	h.runStop(transcript, "")
	assert.Equal(t, 3, h.nodeCount(), "stop hook must catch the final response")

	// Verify the final node
	nodes := h.listNodes()
	found := false
	for _, n := range nodes {
		if n.Content == "Final decision." {
			found = true
			assert.Equal(t, "decision", n.Type)
		}
	}
	assert.True(t, found, "final decision node should exist")
}

// =============================================================================
// Integration Tests: Commands Inside Code Blocks
// =============================================================================

func TestIntegration_CommandsInCodeBlocks_Ignored(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Show me how to use ctx"),
		assistantEntry(
			"Here's how to store a fact:\n\n" +
				"```xml\n" +
				`<ctx:remember type="fact" tags="tier:pinned">This is inside a code block.</ctx:remember>` + "\n" +
				"```\n\n" +
				"And here's a real one:\n\n" +
				`<ctx:remember type="fact" tags="tier:pinned">This is real.</ctx:remember>`,
		),
	})

	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 1, h.nodeCount(), "only the real command outside code block should be stored")
	nodes := h.listNodes()
	assert.Equal(t, "This is real.", nodes[0].Content)
}

// =============================================================================
// Integration Tests: Multiple Commands in Single Response
// =============================================================================

func TestIntegration_MultipleCommandsInOneResponse(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Store several things"),
		assistantEntry(
			`<ctx:remember type="fact" tags="tier:pinned">Fact A.</ctx:remember>` + "\n" +
				`<ctx:remember type="decision" tags="tier:reference">Decision B.</ctx:remember>` + "\n" +
				`<ctx:remember type="observation" tags="tier:working">Observation C.</ctx:remember>`,
		),
	})

	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 3, h.nodeCount())
	assert.Equal(t, "3", h.getPending("session_store_count"))
}

// =============================================================================
// Integration Tests: Session Reset
// =============================================================================

func TestIntegration_SessionStartResetsState(t *testing.T) {
	h := newHookHarness(t)

	// First session
	h.runSessionStart("project1", "agent1")
	assert.Equal(t, "project1", h.getPending("current_project"))
	assert.Equal(t, "agent1", h.getPending("current_agent"))

	// Simulate some activity
	d := h.openDB()
	_ = d.SetPending("session_store_count", "5")
	_ = d.SetPending("transcript_cursor", "99999")
	_ = d.SetPending("session_turn_count", "10")
	d.Close()

	// New session — should reset
	h.runSessionStart("project2", "agent2")
	assert.Equal(t, "project2", h.getPending("current_project"))
	assert.Equal(t, "agent2", h.getPending("current_agent"))
	assert.Equal(t, "0", h.getPending("session_turn_count"))
	assert.Equal(t, "0", h.getPending("session_store_count"))
	assert.Equal(t, "", h.getPending("transcript_cursor"), "cursor should be reset")
}

// =============================================================================
// Integration Tests: Deduplication Across Turns
// =============================================================================

func TestIntegration_DeduplicationAcrossTurns(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	// Turn 1: store a fact
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned">Same fact.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 1, h.nodeCount())

	// Turn 2: same fact again (should dedup, not create new)
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("Again"),
		assistantEntry(`<ctx:remember type="fact" tags="tier:pinned,project:extra">Same fact.</ctx:remember>`),
	})
	h.runPromptSubmit(transcript, "")
	assert.Equal(t, 1, h.nodeCount(), "should still be 1 node (deduped)")

	// But the extra tag should be merged
	nodes := h.listNodes()
	tags := h.getNodeTags(nodes[0].ID)
	assert.Contains(t, tags, "project:extra", "new tag should be merged")
}

// =============================================================================
// Integration Tests: Real Transcript Format
// =============================================================================

func TestIntegration_RealTranscriptFormat(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("book", "nyx")

	transcript := h.writeTranscriptFile([]map[string]any{
		// System entries (should be ignored)
		{"type": "system", "content": "System instructions..."},
		{"type": "file-history-snapshot", "snapshot": map[string]any{}},

		userEntry("Tell me about the project"),

		assistantEntry(
			"Here's what I found.\n\n" +
				`<ctx:remember type="decision" tags="tier:pinned,project:Book">Creative direction established.</ctx:remember>` + "\n\n" +
				`<ctx:remember type="fact" tags="tier:reference,project:Book">Research docs completed.</ctx:remember>`,
		),

		{"type": "system", "content": "More system stuff"},
		{"type": "file-history-snapshot", "snapshot": map[string]any{}},

		userEntry("Continue"),

		assistantEntry(
			"Here's more.\n\n" +
				`<ctx:remember type="observation" tags="tier:working,project:Book">Session was productive.</ctx:remember>`,
		),

		// Trailing system entries
		{"type": "last-prompt", "content": "..."},
		{"type": "agent-setting", "content": "..."},
	})

	h.runPromptSubmit(transcript, "nyx")
	assert.Equal(t, 3, h.nodeCount(), "should parse 3 commands from real-format transcript")

	// Verify agent auto-tagging
	nodes := h.listNodes()
	for _, n := range nodes {
		tags := h.getNodeTags(n.ID)
		assert.Contains(t, tags, "agent:nyx",
			fmt.Sprintf("node %q should have agent:nyx tag", n.Content))
	}
}

// =============================================================================
// Integration Test: Nyx Session-End Bug Full Reproduction
// =============================================================================

func TestIntegration_NyxStopBug_StdinConsumed(t *testing.T) {
	// Full reproduction of the Nyx session-end.sh bug:
	// 1. Session starts with --agent=nyx
	// 2. Mid-session commands are stored via prompt-submit (works)
	// 3. Final response has ctx:remember commands
	// 4. Stop hook is called without stdin (Nyx consumed it)
	// 5. Commands from final response are LOST

	h := newHookHarness(t)

	// Step 1: Session start
	h.runSessionStart("book", "nyx")

	// Step 2: Mid-session turn (prompt-submit processes this)
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Work on the book"),
		assistantEntry(
			`<ctx:remember type="decision" tags="tier:pinned,project:Book">Mid-session decision.</ctx:remember>`,
		),
	})
	h.runPromptSubmit(transcript, "nyx")
	assert.Equal(t, 1, h.nodeCount(), "mid-session node stored via prompt-submit")

	// Step 3: Final response (after last user prompt, no more prompt-submit)
	h.appendTranscriptEntries(transcript, []map[string]any{
		userEntry("Wrap up"),
		assistantEntry(
			`<ctx:remember type="decision" tags="tier:pinned,project:Book">Final session summary.</ctx:remember>` + "\n" +
				`<ctx:remember type="fact" tags="tier:pinned,project:Book">Key research finding.</ctx:remember>`,
		),
	})
	// No prompt-submit here — session is ending

	// Step 4: Stop hook called WITHOUT stdin (the Nyx bug)
	_, stderr := h.runStopNoStdin("nyx")
	assert.Contains(t, stderr, "failed to read hook input")

	// Step 5: Verify the loss — only mid-session node survives
	assert.Equal(t, 1, h.nodeCount(),
		"BUG: only mid-session node survives; final response nodes lost due to no stdin")

	// Now demonstrate that with proper stdin, all 3 would be stored
	h.runStop(transcript, "nyx")
	assert.Equal(t, 3, h.nodeCount(),
		"With proper stdin, stop hook stores all 3 nodes")
}

// =============================================================================
// Integration Test: --response flag as workaround
// =============================================================================

func TestIntegration_StopResponseFlag_Workaround(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("test", "")

	response := `Here's what I learned.

<ctx:remember type="fact" tags="tier:pinned">Important fact from response flag.</ctx:remember>
<ctx:remember type="decision" tags="tier:reference">Key decision via response flag.</ctx:remember>`

	h.runStopWithResponse(response, "")

	assert.Equal(t, 2, h.nodeCount(), "--response flag should work as workaround")
}

// =============================================================================
// Integration Test: Auto-tagging project and agent
// =============================================================================

func TestIntegration_AutoTagging_ProjectAndAgent(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("myproject", "myagent")

	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(
			`<ctx:remember type="fact" tags="tier:pinned">A simple fact.</ctx:remember>`,
		),
	})
	h.runPromptSubmit(transcript, "myagent")

	nodes := h.listNodes()
	require.Len(t, nodes, 1)
	tags := h.getNodeTags(nodes[0].ID)
	assert.Contains(t, tags, "project:myproject", "should auto-add project tag")
	assert.Contains(t, tags, "agent:myagent", "should auto-add agent tag")
	assert.Contains(t, tags, "tier:pinned", "explicit tag preserved")
}

func TestIntegration_AutoTagging_NoOverrideExplicit(t *testing.T) {
	h := newHookHarness(t)

	h.runSessionStart("myproject", "myagent")

	// Explicitly set a different project tag
	transcript := h.writeTranscriptFile([]map[string]any{
		userEntry("Hello"),
		assistantEntry(
			`<ctx:remember type="fact" tags="tier:pinned,project:other">Explicit project.</ctx:remember>`,
		),
	})
	h.runPromptSubmit(transcript, "myagent")

	nodes := h.listNodes()
	require.Len(t, nodes, 1)
	tags := h.getNodeTags(nodes[0].ID)
	assert.Contains(t, tags, "project:other", "explicit project should be kept")
	assert.NotContains(t, tags, "project:myproject", "auto-project should NOT override explicit")
}

// extractAdditionalContext pulls hookSpecificOutput.additionalContext out of
// the JSON emitted by session-start. Returns empty string if missing.
func extractAdditionalContext(t *testing.T, out string) string {
	t.Helper()
	var parsed struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	return parsed.HookSpecificOutput.AdditionalContext
}

// seedPinnedNode inserts a pinned node tagged for the given project.
func (h *hookHarness) seedPinnedNode(project, content string) {
	h.t.Helper()
	d := h.openDB()
	defer d.Close()
	_, err := d.CreateNode(db.CreateNodeInput{
		Type:    "fact",
		Content: content,
		Tags:    []string{"tier:pinned", "project:" + project},
	})
	require.NoError(h.t, err)
}

// TestIntegration_SessionStart_FailClosed verifies that --fail-closed with no
// --project loads zero nodes instead of leaking every pinned node globally.
// This guards against the bug where a plugin hook silently passed --project=""
// and the composer returned the full pinned set across every project.
func TestIntegration_SessionStart_FailClosed(t *testing.T) {
	h := newHookHarness(t)

	h.seedPinnedNode("book", "Novel plotting note.")
	h.seedPinnedNode("cc-plugins", "Plugin marketplace convention.")
	h.seedPinnedNode("ctx", "ctx implementation detail.")

	// Sanity check: without fail-closed and no project, legacy behavior
	// would leak all three nodes into context.
	openOut := h.runSessionStart("", "")
	openCtx := extractAdditionalContext(t, openOut)
	assert.Contains(t, openCtx, "Novel plotting note.",
		"without --fail-closed, ctx still returns all pinned nodes globally")

	// With --fail-closed and no project, zero nodes should appear in context.
	closedOut := h.runSessionStartFailClosed("")
	closedCtx := extractAdditionalContext(t, closedOut)
	assert.NotContains(t, closedCtx, "Novel plotting note.",
		"fail-closed must not leak project:book nodes")
	assert.NotContains(t, closedCtx, "Plugin marketplace convention.",
		"fail-closed must not leak project:cc-plugins nodes")
	assert.NotContains(t, closedCtx, "ctx implementation detail.",
		"fail-closed must not leak project:ctx nodes")
	assert.Contains(t, closedCtx, "0 nodes",
		"fail-closed header should report zero nodes composed")
	assert.Contains(t, closedCtx, "project not detected",
		"fail-closed should surface a warning primer to the agent")

	// With --fail-closed AND an explicit project, normal scoped behavior
	// must still work — this is the happy path when git detection succeeds.
	scopedOut := h.runSessionStartFailClosed("book")
	scopedCtx := extractAdditionalContext(t, scopedOut)
	assert.Contains(t, scopedCtx, "Novel plotting note.",
		"fail-closed with --project=book should still load book nodes")
	assert.NotContains(t, scopedCtx, "Plugin marketplace convention.",
		"fail-closed with --project=book should still exclude other projects")
}
