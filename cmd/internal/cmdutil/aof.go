package cmdutil

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
)

// AgentOut returns true if --agent-out was set on the root command.
func AgentOut(cmd *cobra.Command) bool {
	f := cmd.Root().PersistentFlags().Lookup("agent-out")
	if f == nil {
		return false
	}
	return f.Value.String() == "true"
}

// AOFQuote wraps a value in quotes if it contains spaces or is empty.
func AOFQuote(s string) string {
	if s == "" {
		return "_"
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// AOFNode writes a single node as AOF ok/id/type/summary/tags lines.
func AOFNode(w io.Writer, n *db.Node, status string) {
	fmt.Fprintf(w, "ok node status=%s\n", status)
	fmt.Fprintf(w, "id %s\n", n.ID)
	fmt.Fprintf(w, "type %s\n", n.Type)
	content := n.Content
	if len(content) > 200 {
		content = content[:200] + "…"
	}
	fmt.Fprintf(w, "summary %s\n", AOFQuote(content))
	if len(n.Tags) > 0 {
		fmt.Fprintf(w, "tags %s\n", strings.Join(n.Tags, "|"))
	}
}

// AOFNodes writes a list of nodes as AOF schema + rows.
func AOFNodes(w io.Writer, nodes []*db.Node, more bool) {
	fmt.Fprintf(w, "ok nodes count=%d more=%d\n", len(nodes), boolInt(more))
	if len(nodes) == 0 {
		return
	}
	fmt.Fprintln(w, "@ id type tags summary")
	for _, n := range nodes {
		tags := "_"
		if len(n.Tags) > 0 {
			tags = strings.Join(n.Tags, "|")
		}
		content := n.Content
		if len(content) > 120 {
			content = content[:120] + "…"
		}
		fmt.Fprintf(w, "- %s %s %s %s\n", n.ID, n.Type, tags, AOFQuote(content))
	}
}

// AOFOk writes a simple ok line with key=value pairs.
func AOFOk(w io.Writer, kind string, pairs ...string) {
	fmt.Fprintf(w, "ok %s", kind)
	for i := 0; i+1 < len(pairs); i += 2 {
		fmt.Fprintf(w, " %s=%s", pairs[i], AOFQuote(pairs[i+1]))
	}
	fmt.Fprintln(w)
}

// AOFErr writes an AOF error line with a hint.
func AOFErr(w io.Writer, code, hint string) {
	fmt.Fprintf(w, "err %s\n", code)
	if hint != "" {
		fmt.Fprintf(w, "hint %s\n", hint)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
