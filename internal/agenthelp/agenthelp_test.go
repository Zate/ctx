package agenthelp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noop(_ *cobra.Command, _ []string) error { return nil }

func buildTestTree() *cobra.Command {
	root := &cobra.Command{
		Use:   "tool",
		Short: "a test tool",
	}

	add := &cobra.Command{
		Use:   "add <text>",
		Short: "add a thing",
		RunE:  noop,
	}
	add.Flags().String("type", "", "node type")
	_ = add.MarkFlagRequired("type")
	add.Flags().StringArray("tag", nil, "tags (repeatable)")
	add.Flags().Bool("stdin", false, "read from stdin")

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "show a thing",
		RunE:  noop,
	}
	show.Flags().Bool("with-edges", false, "include edges")

	list := &cobra.Command{
		Use:   "list",
		Short: "list things",
		RunE:  noop,
	}
	list.Flags().String("type", "", "filter by type")
	list.Flags().Int("limit", 0, "limit results")

	// Group command
	hook := &cobra.Command{
		Use:   "hook",
		Short: "hook subcommands",
	}
	sessionStart := &cobra.Command{
		Use:   "session-start",
		Short: "start session",
		RunE:  noop,
	}
	sessionStart.Flags().String("project", "", "project name")
	hook.AddCommand(sessionStart)

	// Hidden command (should be excluded)
	hidden := &cobra.Command{
		Use:    "secret",
		Short:  "hidden command",
		Hidden: true,
		RunE:   noop,
	}

	// Deprecated command
	old := &cobra.Command{
		Use:   "old-cmd",
		Short: "DEPRECATED: use new-cmd",
		RunE:  noop,
	}

	// Completion command (should be excluded)
	completion := &cobra.Command{
		Use:   "completion",
		Short: "generate completions",
		RunE:  noop,
	}

	root.AddCommand(add, show, list, hook, hidden, old, completion)
	return root
}

func TestPrintIndex(t *testing.T) {
	root := buildTestTree()
	var buf bytes.Buffer
	PrintIndex(&buf, root)
	out := buf.String()

	// Header — AOF ah1 record
	assert.True(t, strings.HasPrefix(out, "ah1 tool :: a test tool\n"))

	// Core commands present — AOF cmd records
	assert.Contains(t, out, "cmd add <text> :: add a thing")
	assert.Contains(t, out, "cmd show <id> :: show a thing")
	assert.Contains(t, out, "cmd list :: list things")

	// Group command flattened — AOF cmd record
	assert.Contains(t, out, "cmd hook session-start :: start session")

	// Discovery breadcrumb
	assert.Contains(t, out, "more tool <cmd> --agent-help")

	// Excluded
	assert.NotContains(t, out, "secret")
	assert.NotContains(t, out, "old-cmd")
	assert.NotContains(t, out, "completion")
}

func TestPrintIndex_PrioritySorting(t *testing.T) {
	// Register metadata to test priority sorting
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"show": {Priority: 1},
		"add":  {Priority: 2},
		"list": {Priority: 3},
	}

	root := buildTestTree()
	var buf bytes.Buffer
	PrintIndex(&buf, root)
	out := buf.String()

	// show should appear before add, add before list
	showIdx := strings.Index(out, "show")
	addIdx := strings.Index(out, "add")
	listIdx := strings.Index(out, "list")
	assert.Less(t, showIdx, addIdx, "show should appear before add")
	assert.Less(t, addIdx, listIdx, "add should appear before list")
}

func TestPrintIndex_AgentHidden(t *testing.T) {
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"list": {AgentHidden: true},
	}

	root := buildTestTree()
	var buf bytes.Buffer
	PrintIndex(&buf, root)
	out := buf.String()

	assert.NotContains(t, out, "list")
	assert.Contains(t, out, "add")
}

func TestPrintCommand(t *testing.T) {
	root := buildTestTree()

	t.Run("simple command with flags", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{"add"})
		require.NotNil(t, cmd)

		// Clear registries so test uses plain types
		origReg := Registry
		origFlag := FlagRegistry
		Registry = map[string]CommandMeta{}
		FlagRegistry = map[string]FlagMeta{}
		defer func() { Registry = origReg; FlagRegistry = origFlag }()

		var buf bytes.Buffer
		PrintCommand(&buf, root, cmd)
		out := buf.String()

		// AOF ah2 + use + flag records
		assert.Contains(t, out, "ah2 tool add\n")
		assert.Contains(t, out, "use tool add")
		assert.Contains(t, out, "flag --type:string req :: node type")
		assert.Contains(t, out, "flag --tag:string repeat :: tags (repeatable)")
		assert.Contains(t, out, "flag --stdin:bool opt :: read from stdin")
	})

	t.Run("nested command", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{"hook", "session-start"})
		require.NotNil(t, cmd)

		var buf bytes.Buffer
		PrintCommand(&buf, root, cmd)
		out := buf.String()

		assert.Contains(t, out, "ah2 tool hook session-start\n")
		assert.Contains(t, out, "use tool hook session-start")
		assert.Contains(t, out, "flag --project:string opt :: project name")
	})
}

func TestPrintCommand_WithExample(t *testing.T) {
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"add": {Example: `tool add --type fact "hello world"`},
	}

	root := buildTestTree()
	cmd := ResolveCommand(root, []string{"add"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	PrintCommand(&buf, root, cmd)
	out := buf.String()

	assert.Contains(t, out, `ex tool add --type fact "hello world"`)
	assert.NotContains(t, out, "example:\n")
}

func TestPrintCommand_WithNotes(t *testing.T) {
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"add": {
			Example: `tool add --type fact "hello"`,
			Notes:   "content is required unless --stdin is used",
		},
	}

	root := buildTestTree()
	cmd := ResolveCommand(root, []string{"add"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	PrintCommand(&buf, root, cmd)
	out := buf.String()

	assert.Contains(t, out, "note content is required unless --stdin is used\n")
	assert.Contains(t, out, "ex tool add --type fact \"hello\"\n")
	assert.NotContains(t, out, "example:\n")
}

func TestPrintIndex_ArgsOverride(t *testing.T) {
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"add": {ArgsOverride: "<text>"},
	}

	root := buildTestTree()
	var buf bytes.Buffer
	PrintIndex(&buf, root)
	out := buf.String()

	// AOF cmd record should use ArgsOverride <text>
	assert.Contains(t, out, "cmd add <text> :: add a thing")
	assert.NotContains(t, out, "add [content]")
}

func TestPrintCommand_WithArgsOverride(t *testing.T) {
	origRegistry := make(map[string]CommandMeta)
	for k, v := range Registry {
		origRegistry[k] = v
	}
	defer func() {
		Registry = origRegistry
	}()

	Registry = map[string]CommandMeta{
		"add": {ArgsOverride: "<text>"},
	}

	root := buildTestTree()
	cmd := ResolveCommand(root, []string{"add"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	PrintCommand(&buf, root, cmd)
	out := buf.String()

	// Should use <text> instead of the cobra Use-derived <text>
	assert.Contains(t, out, "tool add <text> [flags]")
}

func TestPrintCommand_WithEnumFlag(t *testing.T) {
	origFlagReg := make(map[string]FlagMeta)
	for k, v := range FlagRegistry {
		origFlagReg[k] = v
	}
	defer func() {
		FlagRegistry = origFlagReg
	}()

	FlagRegistry = map[string]FlagMeta{
		"add.type": {EnumValues: []string{"fact", "decision", "pattern"}},
	}

	root := buildTestTree()
	cmd := ResolveCommand(root, []string{"add"})
	require.NotNil(t, cmd)

	var buf bytes.Buffer
	PrintCommand(&buf, root, cmd)
	out := buf.String()

	assert.Contains(t, out, "--type:enum(fact|decision|pattern)")
}

func TestResolveCommand(t *testing.T) {
	root := buildTestTree()

	t.Run("top-level command", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{"add"})
		require.NotNil(t, cmd)
		assert.Equal(t, "add", cmd.Name())
	})

	t.Run("nested command", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{"hook", "session-start"})
		require.NotNil(t, cmd)
		assert.Equal(t, "session-start", cmd.Name())
	})

	t.Run("unknown command", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{"nonexistent"})
		assert.Nil(t, cmd)
	})

	t.Run("empty args", func(t *testing.T) {
		cmd := ResolveCommand(root, []string{})
		assert.Nil(t, cmd)
	})
}

func TestFormatError(t *testing.T) {
	root := buildTestTree()

	t.Run("close match", func(t *testing.T) {
		var buf bytes.Buffer
		FormatError(&buf, root, "ad")
		out := buf.String()
		assert.Contains(t, out, "err unknown_cmd cmd=ad")
		assert.Contains(t, out, `did you mean "add"`)
		assert.Contains(t, out, "next tool --agent-help add")
	})

	t.Run("no close match", func(t *testing.T) {
		var buf bytes.Buffer
		FormatError(&buf, root, "zzzzzzz")
		out := buf.String()
		assert.Contains(t, out, "err unknown_cmd cmd=zzzzzzz")
		assert.Contains(t, out, "run tool --agent-help for command list")
		assert.Contains(t, out, "next tool --agent-help")
	})
}

func TestLevenshtein(t *testing.T) {
	assert.Equal(t, 0, levenshtein("abc", "abc"))
	assert.Equal(t, 1, levenshtein("ad", "add"))
	assert.Equal(t, 1, levenshtein("abc", "ab"))
	assert.Equal(t, 3, levenshtein("abc", "xyz"))
	assert.Equal(t, 3, levenshtein("", "abc"))
	assert.Equal(t, 3, levenshtein("abc", ""))
}

func TestCollectFlagsDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("name", "default-val", "a name")
	cmd.Flags().Int("count", 0, "a count")
	cmd.Flags().Bool("verbose", false, "be verbose")

	flags := collectFlags(cmd, "test")

	var nameFlag *flagEntry
	for i := range flags {
		if flags[i].name == "name" {
			nameFlag = &flags[i]
		}
	}
	require.NotNil(t, nameFlag)
	assert.Contains(t, nameFlag.defaultHint, "[default=default-val]")

	for _, f := range flags {
		if f.name == "count" || f.name == "verbose" {
			assert.NotContains(t, f.defaultHint, "[default=")
		}
	}
}
