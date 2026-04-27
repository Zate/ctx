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
	"accessed": {
		Priority: 43,
		Example:  `ctx accessed --type explicit_query --since 2026-04-01 --limit 20`,
		Notes:    "shows access history (hook_inject|explicit_query|get|graph_walk); --node prefix; --all-agents to opt out of agent scope",
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

	// Doc subsystem — opt-in only; agents do NOT use these by default.
	// ctx doc nodes are invisible to memory queries (recall/search/status/session-start).
	// Reach for these ONLY when the user explicitly asks to decompose/edit/compose a markdown document.
	// All doc subcommands are hidden from tier-1 index to prevent accidental adoption.
	// Access via: ctx --agent-help doc <subcommand>
	"doc import": {
		AgentHidden:  true,
		ArgsOverride: "<path>",
		Example:      `ctx doc import README.md`,
		Notes:        "byte-identity verified on import; rolls back on mismatch",
	},
	"doc export": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc export 01ABC123`,
		Notes:        "writes to stdout by default; use -o to write to a file",
	},
	"doc show": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc show 01ABC123`,
	},
	"doc verify": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc verify 01ABC123`,
		Notes:        "exits 0 on sha256 match, 1 on mismatch",
	},
	"doc scaffold": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc scaffold 01ABC123 > scaffold.xml`,
		Notes:        "emits pure-structure XML; no content bodies embedded",
	},
	"doc apply": {
		AgentHidden:  true,
		ArgsOverride: "<xml-file>",
		Example:      `ctx doc apply scaffold.xml`,
		Notes:        "diffs scaffold against live edge graph and applies minimal mutations transactionally",
	},
	"doc search": {
		AgentHidden:  true,
		ArgsOverride: "<query>",
		Example:      `ctx doc search "database migration"`,
		Notes:        "LIKE-based match over content node bodies only; not FTS; separate from ctx search",
	},
	"doc mv": {
		AgentHidden:  true,
		ArgsOverride: "<node-id>",
		Example:      `ctx doc mv 01ABC123 --doc 01DOC456 --pos 3`,
	},
	"doc insert": {
		AgentHidden:  true,
		ArgsOverride: "<node-id>",
		Example:      `ctx doc insert 01ABC123 --doc 01DOC456 --pos 2`,
		Notes:        "node must already exist; use --memory to insert a kind=memory node",
	},
	"doc remove": {
		AgentHidden:  true,
		ArgsOverride: "<node-id>",
		Example:      `ctx doc remove 01ABC123 --doc 01DOC456`,
		Notes:        "drops CONTAINS edge only; content node is preserved",
	},
	"doc fork": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc fork 01DOC456`,
		Notes:        "new document shares content nodes; structures diverge independently after fork",
	},
	"doc split": {
		AgentHidden:  true,
		ArgsOverride: "<node-id>",
		Example:      `ctx doc split 01ABC123 --doc 01DOC456 --at 42`,
		Notes:        "offset must land on a UTF-8 character boundary; sha256(compose) unchanged after split",
	},
	"doc promote": {
		AgentHidden:  true,
		ArgsOverride: "<node-id>",
		Example:      `ctx doc promote 01ABC123 --into-memory --type fact`,
		Notes:        "--into-memory is a required safety gate; valid types: fact, decision, pattern, observation, hypothesis, task, summary, source, open-question",
	},
	"doc inline": {
		AgentHidden:  true,
		ArgsOverride: "<doc-id>",
		Example:      `ctx doc inline 01DOC456 --memory 01MEM789 --pos 1`,
		Notes:        "memory node kind unchanged; body appears in composed output at specified position",
	},

	// Server/auth/sync — not agent-relevant for memory operations
	"serve":              {AgentHidden: true},
	"mcp":                {AgentHidden: true},
	"auth status":        {AgentHidden: true},
	"auth logout":        {AgentHidden: true},
	"device list":        {AgentHidden: true},
	"device revoke":      {AgentHidden: true},
	"remote set":         {AgentHidden: true},
	"remote show":        {AgentHidden: true},
	"remote remove":      {AgentHidden: true},
	"sync pull":          {AgentHidden: true},
	"sync push":          {AgentHidden: true},
	"sync status":        {AgentHidden: true},
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
	"accessed.type": {
		EnumValues: []string{"hook_inject", "explicit_query", "get", "graph_walk"},
	},
	"doc promote.type": {
		EnumValues: []string{"fact", "decision", "pattern", "observation", "hypothesis", "task", "summary", "source", "open-question"},
	},
}
