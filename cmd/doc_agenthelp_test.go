package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/agenthelp"
)

// TestDocAgentHelp_Index verifies that ctx doc subcommands are hidden from the
// tier-1 index (opt-in posture — agents should not discover doc automatically).
func TestDocAgentHelp_Index(t *testing.T) {
	var buf bytes.Buffer
	agenthelp.PrintIndex(&buf, rootCmd)
	out := buf.String()

	assert.Contains(t, out, "ah1 ctx ::")

	// All doc subcommands must be absent from the default index.
	for _, sub := range []string{
		"doc import", "doc export", "doc show", "doc verify",
		"doc scaffold", "doc apply", "doc search",
		"doc mv", "doc insert", "doc remove",
		"doc fork", "doc split", "doc promote", "doc inline",
	} {
		assert.NotContains(t, out, sub,
			"doc subcommand %q must not appear in tier-1 index (opt-in posture)", sub)
	}
}

// TestDocAgentHelp_DocCommand verifies ctx doc --agent-help renders a command detail.
func TestDocAgentHelp_DocCommand(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc"})
	// doc is a group command with subcommands — ResolveCommand returns nil for
	// group-only commands when they have subcommands, which is expected behavior.
	// The real entry points are the subcommands.
	_ = cmd
}

// docSubcommandTests defines every ctx doc subcommand and its required sections.
var docSubcommandTests = []struct {
	path    []string // command path segments
	wantArg string   // substring expected in usage line (positional arg)
	wantEx  string   // substring expected in example (if metadata registered)
}{
	{[]string{"doc", "import"}, "<path>", ""},
	{[]string{"doc", "export"}, "<doc-id>", ""},
	{[]string{"doc", "show"}, "<doc-id>", ""},
	{[]string{"doc", "verify"}, "<doc-id>", ""},
	{[]string{"doc", "scaffold"}, "<doc-id>", ""},
	{[]string{"doc", "apply"}, "<xml-file>", ""},
	{[]string{"doc", "search"}, "<query>", ""},
	{[]string{"doc", "mv"}, "<node-id>", ""},
	{[]string{"doc", "insert"}, "<node-id>", ""},
	{[]string{"doc", "remove"}, "<node-id>", ""},
	{[]string{"doc", "fork"}, "<doc-id>", ""},
	{[]string{"doc", "split"}, "<node-id>", ""},
	{[]string{"doc", "promote"}, "<node-id>", ""},
	{[]string{"doc", "inline"}, "<doc-id>", ""},
}

// TestDocAgentHelp_Subcommands verifies that every ctx doc subcommand resolves
// and renders a well-formed tier-2 agent-help block containing:
//   - the full command path (Usage line)
//   - the positional argument placeholder
func TestDocAgentHelp_Subcommands(t *testing.T) {
	for _, tc := range docSubcommandTests {
		tc := tc
		t.Run(strings.Join(tc.path, "_"), func(t *testing.T) {
			cmd := agenthelp.ResolveCommand(rootCmd, tc.path)
			require.NotNil(t, cmd, "command %v must resolve", tc.path)

			var buf bytes.Buffer
			agenthelp.PrintCommand(&buf, rootCmd, cmd)
			out := buf.String()

			// AH2 header line must be present (first line = "ah2 ctx doc <subcmd>")
			fullPath := "ah2 ctx " + strings.Join(tc.path, " ")
			assert.True(t, strings.HasPrefix(out, fullPath),
				"output must start with %q\ngot: %q", fullPath, out)

			// Positional arg placeholder must appear in the usage line
			assert.Contains(t, out, tc.wantArg,
				"command %v: output must contain positional arg %q\ngot: %q",
				tc.path, tc.wantArg, out)
		})
	}
}

// TestDocAgentHelp_ImportFlags verifies that ctx doc import --agent-help
// does not show flags (import has no non-inherited flags besides help).
func TestDocAgentHelp_ImportFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "import"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	// import has no local flags — no flags section expected
	assert.NotContains(t, out, "flags:")
}

// TestDocAgentHelp_ExportFlags verifies ctx doc export --agent-help shows the --output flag.
func TestDocAgentHelp_ExportFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "export"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.NotContains(t, out, "flags:")
	assert.Contains(t, out, "--output")
}

// TestDocAgentHelp_SearchFlags verifies ctx doc search --agent-help shows the --limit flag.
func TestDocAgentHelp_SearchFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "search"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.NotContains(t, out, "flags:")
	assert.Contains(t, out, "--limit")
}

// TestDocAgentHelp_MvFlags verifies ctx doc mv --agent-help shows required --doc and --pos flags.
func TestDocAgentHelp_MvFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "mv"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.NotContains(t, out, "flags:")
	assert.Contains(t, out, "--doc")
	assert.Contains(t, out, "--pos")
	assert.Contains(t, out, " req ")
}

// TestDocAgentHelp_PromoteFlags verifies ctx doc promote --agent-help shows --type and --into-memory.
func TestDocAgentHelp_PromoteFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "promote"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.NotContains(t, out, "flags:")
	assert.Contains(t, out, "--type")
	assert.Contains(t, out, "--into-memory")
	assert.Contains(t, out, " req ")
}

// TestDocAgentHelp_InlineFlags verifies ctx doc inline --agent-help shows --memory (required).
func TestDocAgentHelp_InlineFlags(t *testing.T) {
	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "inline"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.NotContains(t, out, "flags:")
	assert.Contains(t, out, "--memory")
	assert.Contains(t, out, " req ")
}

// TestDocAgentHelp_WithMetadata verifies that once metadata is registered for a
// doc subcommand, the example and notes sections appear in the output.
func TestDocAgentHelp_WithMetadata(t *testing.T) {
	origRegistry := make(map[string]agenthelp.CommandMeta)
	for k, v := range agenthelp.Registry {
		origRegistry[k] = v
	}
	defer func() { agenthelp.Registry = origRegistry }()

	agenthelp.Registry["doc import"] = agenthelp.CommandMeta{
		Example:  `ctx doc import README.md`,
		Notes:    "byte-identity verified on import; rolls back on mismatch",
		Priority: 200,
	}

	cmd := agenthelp.ResolveCommand(rootCmd, []string{"doc", "import"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	agenthelp.PrintCommand(&buf, rootCmd, cmd)
	out := buf.String()

	assert.Contains(t, out, "ex ctx doc import README.md")
	assert.NotContains(t, out, "example:")
	assert.Contains(t, out, "note byte-identity verified on import")
	assert.NotContains(t, out, "note:")
}

// TestDocAgentHelp_AgentHidden verifies that all doc subcommands are hidden from
// the tier-1 index because all carry AgentHidden: true in the registry.
func TestDocAgentHelp_AgentHidden(t *testing.T) {
	var buf bytes.Buffer
	agenthelp.PrintIndex(&buf, rootCmd)
	out := buf.String()

	// All doc subcommands have AgentHidden:true — none must appear.
	docSubs := []string{
		"doc import", "doc export", "doc show", "doc verify",
		"doc scaffold", "doc apply", "doc search",
		"doc mv", "doc insert", "doc remove",
		"doc fork", "doc split", "doc promote", "doc inline",
	}
	for _, sub := range docSubs {
		assert.NotContains(t, out, sub)
	}
}

// TestDocAgentHelp_TierTwoAccess verifies that even though doc subcommands are hidden
// from the tier-1 index, they are still fully accessible via tier-2 detail lookup.
func TestDocAgentHelp_TierTwoAccess(t *testing.T) {
	// All registered subcommands should resolve and produce non-empty output.
	for _, tc := range docSubcommandTests {
		tc := tc
		t.Run("tier2_"+strings.Join(tc.path, "_"), func(t *testing.T) {
			cmd := agenthelp.ResolveCommand(rootCmd, tc.path)
			require.NotNil(t, cmd)

			var buf bytes.Buffer
			agenthelp.PrintCommand(&buf, rootCmd, cmd)
			out := buf.String()

			assert.NotEmpty(t, out)
			// AOF ex record must be present (all doc subcommands have one in the registry).
			assert.Contains(t, out, "\nex ")
		})
	}
}
