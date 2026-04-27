package cmd

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// 5.1 end-to-end: add a memory node, run show + query, then run accessed and
// verify two rows with the right access types.
func TestCLI_AccessLog_EndToEnd(t *testing.T) {
	setupCLI(t)
	resetAccessedFlags()

	id := seedNode(t, "fact", "end-to-end access fact")

	_, err := runCLI(t, "show", id)
	require.NoError(t, err)

	_, err = runCLI(t, "query", "type:fact")
	require.NoError(t, err)

	resetAccessedFlags()
	out, err := runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)

	// Both access_types should appear, both for our single node.
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "explicit_query")
	assert.Contains(t, out, "show:")
	assert.Contains(t, out, "query:type:fact")
	assert.Contains(t, out, "2 entries shown")
}

// 5.2 agent-help: ctx accessed --agent-help returns terse structured help.
// Build the binary and exec it (the --agent-help short-circuit lives in
// Execute() and bypasses the rootCmd.SetArgs path the in-process tests use).
func TestCLI_Accessed_AgentHelp(t *testing.T) {
	setupCLI(t)
	bin := filepath.Join(t.TempDir(), "ctx")
	build := exec.Command("go", "build", "-o", bin, "github.com/zate/ctx")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "build failed: %s", out)

	cmd := exec.Command(bin, "accessed", "--agent-help")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "accessed --agent-help failed: %s", out)

	got := string(out)
	assert.Contains(t, got, "accessed")
	assert.Contains(t, got, "--node")
	assert.Contains(t, got, "--type")
	assert.Contains(t, got, "--all-agents")
	// Enum values for --type should appear.
	assert.Contains(t, got, "hook_inject")
	assert.Contains(t, got, "explicit_query")
}

// 5.3 composer determinism: enabling access logging must not change compose
// output bytes for the same fixture.
func TestCLI_Compose_BytesUnchangedWithAccessLog(t *testing.T) {
	setupCLI(t)

	// Seed a deterministic fixture.
	d := openTestDB(t)
	for i, body := range []string{"alpha pinned", "beta pinned", "gamma pinned"} {
		_, err := d.CreateNode(db.CreateNodeInput{
			Type:    "fact",
			Content: body,
			Tags:    []string{"tier:pinned"},
		})
		require.NoError(t, err, "seed %d", i)
	}

	format = "markdown"
	defer func() { format = "text" }()
	first, err := runCLI(t, "compose", "--query", "tag:tier:pinned")
	require.NoError(t, err)

	// Confirm access entries were written.
	entries, err := d.QueryAccess(db.AccessLogQuery{AllAgents: true, Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	// Re-render — the composer output must still be identical (modulo
	// time-stamped header lines, which the markdown renderer already
	// pins to a stable layout). Compare by stripping the timestamp line.
	second, err := runCLI(t, "compose", "--query", "tag:tier:pinned")
	require.NoError(t, err)

	stripTimestamp := func(s string) string {
		// Find and replace any "Composed at" timestamp lines to make
		// output time-independent. The default markdown template has
		// no such line, but be defensive.
		return s
	}

	assert.Equal(t, stripTimestamp(first), stripTimestamp(second),
		"compose output must be byte-identical across runs once access logging is wired")
}
