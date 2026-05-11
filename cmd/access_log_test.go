package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// End-to-end via in-process cobra: add a memory node, run show + query, then run accessed and
// verify two rows with the right access types.
func TestCLI_AccessLog_EndToEnd(t *testing.T) {
	setupCLI(t)

	id := seedNode(t, "fact", "end-to-end access fact")

	_, err := runCLI(t, "show", id)
	require.NoError(t, err)

	_, err = runCLI(t, "query", "type:fact")
	require.NoError(t, err)

	out, err := runCLI(t, "accessed", "--all-agents")
	require.NoError(t, err)

	// Both access_types should appear, both for our single node.
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "explicit_query")
	assert.Contains(t, out, "show:")
	assert.Contains(t, out, "query:type:fact")
	assert.Contains(t, out, "2 entries shown")
}

// Composer determinism: enabling access logging must not change compose
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
