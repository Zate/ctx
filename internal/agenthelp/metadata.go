package agenthelp

// CommandMeta holds agent-help metadata for a command.
type CommandMeta struct {
	// Example is a single common invocation shown in tier 2.
	Example string
	// Notes is an optional one-liner providing critical context (e.g. syntax hints).
	Notes string
	// Priority controls tier 1 sort order (lower = higher in list). 0 means default (100).
	Priority int
	// AgentHidden hides the command from the tier 1 index (still accessible via tier 2).
	AgentHidden bool
	// ArgsOverride replaces the Cobra Use-derived args in both tier 1 and tier 2 signatures.
	ArgsOverride string
}

// FlagMeta holds agent-help metadata for a specific flag.
type FlagMeta struct {
	// EnumValues overrides the type display with enum(a|b|c).
	EnumValues []string
}

// Registry holds all agent-help metadata, keyed by command path (e.g. "add", "hook session-start").
var Registry = map[string]CommandMeta{
	// Core CRUD — what agents use 90% of the time
	"add": {
		Priority:     10,
		ArgsOverride: "<text>",
		Example:      `ctx add --type fact --tag tier:pinned --tag project:myapp "postgres 15 is the production database"`,
	},
	"show": {
		Priority: 11,
		Example:  `ctx show 01ABC123`,
	},
	"query": {
		Priority: 12,
		Example:  `ctx query 'type:fact AND tag:tier:pinned'`,
		Notes:    "syntax: type:T, tag:K:V, created:>DATE, tokens:<N; operators: AND, OR, NOT, parens",
	},
	"list": {
		Priority: 13,
		Example:  `ctx list --type decision --since 7d`,
	},
	"search": {
		Priority: 14,
		Example:  `ctx search "database migration"`,
	},
	"delete": {
		Priority: 15,
		Example:  `ctx delete 01ABC123`,
	},
	"update": {
		Priority: 16,
		Example:  `ctx update 01ABC123 --content "updated content"`,
	},

	// Tagging
	"tag": {
		Priority: 20,
		Example:  `ctx tag 01ABC123 tier:pinned project:myapp`,
	},
	"untag": {
		Priority: 21,
		Example:  `ctx untag 01ABC123 tier:working`,
	},
	"tags": {
		Priority: 22,
		Example:  `ctx tags --prefix tier:`,
	},

	// Graph
	"link": {
		Priority: 30,
		Example:  `ctx link 01ABC123 01DEF456 --type DERIVED_FROM`,
	},
	"unlink": {
		Priority: 31,
		Example:  `ctx unlink 01ABC123 01DEF456`,
	},
	"edges": {
		Priority: 32,
		Example:  `ctx edges 01ABC123 --direction out`,
	},
	"related": {
		Priority: 33,
		Example:  `ctx related 01ABC123 --depth 2`,
	},
	"trace": {
		Priority: 34,
		Example:  `ctx trace 01ABC123`,
	},

	// Composition
	"compose": {
		Priority: 40,
		Example:  `ctx compose --query 'tag:tier:pinned' --budget 10000`,
		Notes:    "--query uses same syntax as the query command; --ids for explicit node selection",
	},
	"summarize": {
		Priority: 41,
		Example:  `ctx summarize 01ABC 01DEF --content "summary of both nodes" --archive-sources`,
	},
	"status": {
		Priority: 42,
		Example:  `ctx status`,
	},

	// Views
	"view create": {
		Priority: 50,
		Example:  `ctx view create myview --query 'type:decision AND tag:project:api' --budget 20000`,
	},
	"view list": {
		Priority: 51,
		Example:  `ctx view list`,
	},
	"view render": {
		Priority: 52,
		Example:  `ctx view render myview`,
	},
	"view delete": {
		Priority: 53,
	},

	// Hooks — agents don't call these directly, but they should know they exist
	"hook session-start": {
		Priority: 60,
		Example:  `ctx hook session-start --project myapp --agent claude`,
	},
	"hook prompt-submit": {
		Priority: 61,
		Notes:    "reads transcript path from stdin (called by Claude Code hooks, not directly)",
	},
	"hook stop": {
		Priority: 62,
		Example:  `ctx hook stop --response "the agent response text"`,
	},

	// Import/Export
	"export": {
		Priority: 70,
		Example:  `ctx export --query 'tag:project:myapp'`,
	},
	"import": {
		Priority: 71,
		Example:  `cat export.json | ctx import --merge`,
		Notes:    "reads JSON from stdin",
	},
	"ingest": {
		Priority: 72,
		Example:  `ctx ingest notes.md --tag tier:reference`,
	},

	// Infrastructure — hidden from tier 1, agents don't call these
	"init": {
		Priority: 80,
		Example:  `ctx init`,
	},
	"version": {
		Priority: 81,
		Example:  `ctx version`,
	},

	// Server/auth/sync — not agent-relevant for memory operations
	"serve":            {AgentHidden: true},
	"mcp":              {AgentHidden: true},
	"auth status":      {AgentHidden: true},
	"auth logout":      {AgentHidden: true},
	"device list":      {AgentHidden: true},
	"device revoke":    {AgentHidden: true},
	"remote set":       {AgentHidden: true},
	"remote show":      {AgentHidden: true},
	"remote remove":    {AgentHidden: true},
	"sync pull":        {AgentHidden: true},
	"sync push":        {AgentHidden: true},
	"sync status":      {AgentHidden: true},
	"sync register-repo": {AgentHidden: true},
}

// FlagRegistry holds enum/type overrides keyed by "command.flagname".
var FlagRegistry = map[string]FlagMeta{
	"add.type": {
		EnumValues: []string{"fact", "decision", "pattern", "observation", "hypothesis", "task", "summary", "source", "open-question"},
	},
	"update.type": {
		EnumValues: []string{"fact", "decision", "pattern", "observation", "hypothesis", "task", "summary", "source", "open-question"},
	},
	"list.type": {
		EnumValues: []string{"fact", "decision", "pattern", "observation", "hypothesis", "task", "summary", "source", "open-question"},
	},
	"link.type": {
		EnumValues: []string{"DERIVED_FROM", "DEPENDS_ON", "SUPERSEDES", "RELATES_TO", "CHILD_OF"},
	},
	"edges.direction": {
		EnumValues: []string{"in", "out", "both"},
	},
	"compose.template": {
		EnumValues: []string{"default", "document"},
	},
}
