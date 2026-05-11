package agenthelp

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// PrintIndex renders the AH1 compact command index for --agent-help (no subcommand).
// Output uses AOF record types: ah1, cmd, more. Target: <300 tokens.
func PrintIndex(w io.Writer, root *cobra.Command) {
	fmt.Fprintf(w, "ah1 %s :: %s\n", root.Name(), root.Short)

	cmds := collectCommands(root, "")
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].priority < cmds[j].priority
	})
	for _, c := range cmds {
		fmt.Fprintf(w, "cmd %s :: %s\n", c.usage, c.short)
	}
	fmt.Fprintf(w, "more %s <cmd> --agent-help\n", root.Name())
}

// PrintCommand renders the AH2 command detail for --agent-help <subcommand>.
// Output uses AOF record types: ah2, use, arg, flag, ex, note. Target: <150 tokens.
func PrintCommand(w io.Writer, root *cobra.Command, cmd *cobra.Command) {
	path := commandPath(cmd, root)
	pathKey := commandKey(cmd, root)

	fmt.Fprintf(w, "ah2 %s\n", path)

	// use line — canonical invocation
	args := inferArgs(cmd, pathKey)
	use := strings.TrimSpace(path + " " + args)
	fmt.Fprintf(w, "use %s\n", use)

	// flags
	flags := collectFlags(cmd, pathKey)
	for _, f := range flags {
		req := "opt"
		if strings.Contains(f.constraint, "[required]") {
			req = "req"
		} else if strings.Contains(f.typeName, "[]") || f.repeat {
			req = "repeat"
		}
		fmt.Fprintf(w, "flag --%s:%s %s :: %s%s\n", f.name, f.typeName, req, f.purpose, f.defaultHint)
	}

	if meta, ok := Registry[pathKey]; ok {
		if meta.Notes != "" {
			fmt.Fprintf(w, "note %s\n", meta.Notes)
		}
		if meta.Example != "" {
			fmt.Fprintf(w, "ex %s\n", meta.Example)
		}
	}
}

// FormatError produces an AE1-style error hint for an unknown command.
// Output uses AOF record types: err, hint, next.
func FormatError(w io.Writer, root *cobra.Command, badCmd string) {
	fmt.Fprintf(w, "err unknown_cmd cmd=%s\n", badCmd)

	// Try fuzzy match against available commands
	if match := closestCommand(root, badCmd); match != "" {
		fmt.Fprintf(w, "hint did you mean %q?\n", match)
		fmt.Fprintf(w, "next %s --agent-help %s\n", root.Name(), match)
	} else {
		fmt.Fprintf(w, "hint run %s --agent-help for command list\n", root.Name())
		fmt.Fprintf(w, "next %s --agent-help\n", root.Name())
	}
}

type commandEntry struct {
	usage    string
	short    string
	priority int
}

const defaultPriority = 100

// collectCommands walks the command tree and produces flat "group cmd <args>" entries.
func collectCommands(parent *cobra.Command, prefix string) []commandEntry {
	var entries []commandEntry

	for _, cmd := range parent.Commands() {
		if cmd.Hidden || !cmd.IsAvailableCommand() {
			continue
		}
		// Skip noise commands
		if cmd.Name() == "completion" || cmd.Name() == "help" {
			continue
		}
		// Skip deprecated commands
		if cmd.Deprecated != "" || strings.Contains(cmd.Short, "DEPRECATED") {
			continue
		}

		name := cmd.Name()
		if prefix != "" {
			name = prefix + " " + name
		}

		// Check if hidden from agent index via metadata
		if meta, ok := Registry[name]; ok && meta.AgentHidden {
			continue
		}

		subs := cmd.Commands()
		hasVisibleSubs := false
		for _, s := range subs {
			if !s.Hidden && s.IsAvailableCommand() {
				hasVisibleSubs = true
				break
			}
		}

		if hasVisibleSubs {
			entries = append(entries, collectCommands(cmd, name)...)
		} else {
			// Use ArgsOverride from registry if available
			args := inlineArgs(cmd)
			pri := defaultPriority
			if meta, ok := Registry[name]; ok {
				if meta.Priority > 0 {
					pri = meta.Priority
				}
				if meta.ArgsOverride != "" {
					args = meta.ArgsOverride
				}
			}
			usage := name
			if args != "" {
				usage += " " + args
			}
			entries = append(entries, commandEntry{
				usage:    usage,
				short:    cmd.Short,
				priority: pri,
			})
		}
	}

	return entries
}

// inlineArgs extracts inline arg hints from cmd.Use (everything after the command name).
func inlineArgs(cmd *cobra.Command) string {
	_, after, found := strings.Cut(cmd.Use, " ")
	if !found {
		return ""
	}
	return after
}

// inferArgs builds the full arg + [flags] signature for tier 2.
func inferArgs(cmd *cobra.Command, pathKey string) string {
	// Use override if registered
	args := ""
	if meta, ok := Registry[pathKey]; ok && meta.ArgsOverride != "" {
		args = meta.ArgsOverride
	} else {
		args = inlineArgs(cmd)
	}

	hasFlags := false
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" && !f.Hidden {
			hasFlags = true
		}
	})
	parts := []string{}
	if args != "" {
		parts = append(parts, args)
	}
	if hasFlags {
		parts = append(parts, "[flags]")
	}
	return strings.Join(parts, " ")
}

type flagEntry struct {
	name        string
	typeName    string
	purpose     string
	constraint  string // kept for required detection
	repeat      bool
	defaultHint string
}

// collectFlags gathers non-hidden, non-inherited flags for a command.
func collectFlags(cmd *cobra.Command, pathKey string) []flagEntry {
	var flags []flagEntry
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}

		// Check for enum override in flag registry
		flagKey := pathKey + "." + f.Name
		typeName := mapType(f.Value.Type())
		repeat := false
		if t := f.Value.Type(); t == "stringArray" || t == "stringSlice" {
			repeat = true
		}
		if fm, ok := FlagRegistry[flagKey]; ok && len(fm.EnumValues) > 0 {
			typeName = "enum(" + strings.Join(fm.EnumValues, "|") + ")"
		}

		purpose := f.Usage
		constraint := ""

		// Check for required annotation
		if ann, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(ann) > 0 && ann[0] == "true" {
			constraint = " [required]"
		}

		// Non-obvious defaults
		defaultHint := ""
		def := f.DefValue
		if def != "" && def != "false" && def != "0" && def != "[]" {
			if typeName != "bool" {
				defaultHint = fmt.Sprintf(" [default=%s]", def)
			}
		}

		flags = append(flags, flagEntry{
			name:        f.Name,
			typeName:    typeName,
			purpose:     purpose,
			constraint:  constraint,
			repeat:      repeat,
			defaultHint: defaultHint,
		})
	})
	return flags
}

func mapType(cobraType string) string {
	switch cobraType {
	case "string":
		return "string"
	case "int", "int32", "int64":
		return "int"
	case "bool":
		return "bool"
	case "stringArray", "stringSlice":
		return "string"
	default:
		return cobraType
	}
}

// commandPath returns the full command path relative to root (e.g. "ctx hook session-start").
func commandPath(cmd *cobra.Command, root *cobra.Command) string {
	parts := []string{}
	for c := cmd; c != nil && c != root; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return root.Name() + " " + strings.Join(parts, " ")
}

// commandKey returns the registry key for a command (e.g. "hook session-start").
func commandKey(cmd *cobra.Command, root *cobra.Command) string {
	parts := []string{}
	for c := cmd; c != nil && c != root; c = c.Parent() {
		parts = append([]string{c.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}

// ResolveCommand finds a command by its path segments (e.g. ["hook", "session-start"]).
func ResolveCommand(root *cobra.Command, args []string) *cobra.Command {
	cmd := root
	for _, a := range args {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == a {
				cmd = c
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if cmd == root {
		return nil
	}
	return cmd
}

// closestCommand finds the closest matching command name using Levenshtein distance.
func closestCommand(root *cobra.Command, input string) string {
	best := ""
	bestDist := 4 // threshold: must be within 3 edits
	for _, cmd := range root.Commands() {
		if cmd.Hidden || !cmd.IsAvailableCommand() {
			continue
		}
		d := levenshtein(input, cmd.Name())
		if d < bestDist {
			bestDist = d
			best = cmd.Name()
		}
	}
	return best
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
