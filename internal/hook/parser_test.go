package hook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCtxCommands(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []CtxCommand
	}{
		{
			name: "simple remember",
			input: `Here's my analysis.

<ctx:remember type="fact" tags="project:x,tier:reference">
The API uses OAuth 2.0.
</ctx:remember>

Let me know if you need more.`,
			want: []CtxCommand{
				{
					Type: "remember",
					Attrs: map[string]string{
						"type": "fact",
						"tags": "project:x,tier:reference",
					},
					Content: "The API uses OAuth 2.0.",
				},
			},
		},
		{
			name: "remember with multiline content",
			input: `<ctx:remember type="decision" tags="tier:reference">
We decided to use PostgreSQL.

Rationale:
- Better concurrency
- Native JSON support
- Team familiarity
</ctx:remember>`,
			want: []CtxCommand{
				{
					Type: "remember",
					Attrs: map[string]string{
						"type": "decision",
						"tags": "tier:reference",
					},
					Content: "We decided to use PostgreSQL.\n\nRationale:\n- Better concurrency\n- Native JSON support\n- Team familiarity",
				},
			},
		},
		{
			name: "self-closing recall",
			input: `Let me check what we discussed.
<ctx:recall query="type:fact AND tag:auth"/>`,
			want: []CtxCommand{
				{
					Type: "recall",
					Attrs: map[string]string{
						"query": "type:fact AND tag:auth",
					},
				},
			},
		},
		{
			name:  "self-closing with spaces",
			input: `<ctx:recall query="type:fact" />`,
			want: []CtxCommand{
				{
					Type:  "recall",
					Attrs: map[string]string{"query": "type:fact"},
				},
			},
		},
		{
			name: "status command",
			input: `Let me check my memory state.
<ctx:status/>`,
			want: []CtxCommand{
				{Type: "status"},
			},
		},
		{
			name: "multiple commands",
			input: `<ctx:remember type="fact">Fact one</ctx:remember>
Some text in between.
<ctx:remember type="decision">Decision one</ctx:remember>
<ctx:link from="abc" to="def" type="DEPENDS_ON"/>`,
			want: []CtxCommand{
				{Type: "remember", Attrs: map[string]string{"type": "fact"}, Content: "Fact one"},
				{Type: "remember", Attrs: map[string]string{"type": "decision"}, Content: "Decision one"},
				{Type: "link", Attrs: map[string]string{"from": "abc", "to": "def", "type": "DEPENDS_ON"}},
			},
		},
		{
			name: "summarize with archive",
			input: `<ctx:summarize nodes="01HQ1234,01HQ5678" archive="true">
The auth system uses OIDC with custom claims.
</ctx:summarize>`,
			want: []CtxCommand{
				{
					Type: "summarize",
					Attrs: map[string]string{
						"nodes":   "01HQ1234,01HQ5678",
						"archive": "true",
					},
					Content: "The auth system uses OIDC with custom claims.",
				},
			},
		},
		{
			name:  "task start",
			input: `<ctx:task name="implement-auth" action="start"/>`,
			want: []CtxCommand{
				{
					Type: "task",
					Attrs: map[string]string{
						"name":   "implement-auth",
						"action": "start",
					},
				},
			},
		},
		{
			name:  "expand command",
			input: `<ctx:expand node="01HQ1234"/>`,
			want: []CtxCommand{
				{
					Type:  "expand",
					Attrs: map[string]string{"node": "01HQ1234"},
				},
			},
		},
		{
			name:  "supersede command",
			input: `<ctx:supersede old="01HQ1234" new="01HQ5678"/>`,
			want: []CtxCommand{
				{
					Type: "supersede",
					Attrs: map[string]string{
						"old": "01HQ1234",
						"new": "01HQ5678",
					},
				},
			},
		},
		{
			name:  "ignore commands in code blocks",
			input: "Here's an example:\n```xml\n<ctx:remember type=\"fact\">ignored</ctx:remember>\n```",
			want:  []CtxCommand{},
		},
		{
			name:  "ignore commands in inline code",
			input: "Use `<ctx:remember type=\"fact\">` to store facts.",
			want:  []CtxCommand{},
		},
		{
			name:  "no commands",
			input: "This is just regular text with no ctx commands.",
			want:  []CtxCommand{},
		},
		{
			name:  "malformed - unclosed tag",
			input: `<ctx:remember type="fact">content without closing`,
			want:  []CtxCommand{},
		},
		{
			name:  "quotes in content",
			input: `<ctx:remember type="fact">User said "hello world"</ctx:remember>`,
			want: []CtxCommand{
				{
					Type:    "remember",
					Attrs:   map[string]string{"type": "fact"},
					Content: `User said "hello world"`,
				},
			},
		},
		{
			name:  "unicode content",
			input: `<ctx:remember type="fact">User speaks 日本語 and uses emoji 🚀</ctx:remember>`,
			want: []CtxCommand{
				{
					Type:    "remember",
					Attrs:   map[string]string{"type": "fact"},
					Content: "User speaks 日本語 and uses emoji 🚀",
				},
			},
		},
		{
			name:  "real command after code block example",
			input: "Here's an example:\n```xml\n<ctx:remember type=\"fact\">ignored</ctx:remember>\n```\n\n<ctx:remember type=\"fact\">This is real</ctx:remember>",
			want: []CtxCommand{
				{
					Type:    "remember",
					Attrs:   map[string]string{"type": "fact"},
					Content: "This is real",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCtxCommands(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
