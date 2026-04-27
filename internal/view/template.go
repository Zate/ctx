package view

import (
	"fmt"
	"strings"

	"github.com/zate/ctx/internal/db"
)

// RenderTemplate renders a ComposeResult using a named template.
// Currently only "default" is supported. Returns the rendered markdown.
func RenderTemplate(result *ComposeResult, templateName string) string {
	switch templateName {
	case "document":
		return renderDocumentTemplate(result)
	default:
		return renderDefaultTemplate(result)
	}
}

// renderDefaultTemplate renders a clean document from composed nodes.
// Unlike RenderMarkdown (designed for session-start context injection),
// this template is meant for standalone document output.
func renderDefaultTemplate(result *ComposeResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Composed Document\n\n")
	fmt.Fprintf(&b, "> %d nodes, %d tokens\n\n", result.NodeCount, result.TotalTokens)

	for i, n := range result.Nodes {
		if i > 0 {
			b.WriteString("---\n\n")
		}
		shortID := n.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}

		fmt.Fprintf(&b, "### %s `%s`\n\n", titleCase(n.Type), shortID)

		if n.Summary != nil && *n.Summary != "" {
			fmt.Fprintf(&b, "*%s*\n\n", *n.Summary)
		}

		b.WriteString(n.Content)
		b.WriteString("\n\n")

		if len(n.Tags) > 0 {
			fmt.Fprintf(&b, "**Tags:** %s\n\n", strings.Join(n.Tags, ", "))
		}
	}

	// Render relationships
	if len(result.Edges) > 0 {
		b.WriteString("---\n\n")
		b.WriteString("## Relationships\n\n")

		nodeLabels := buildNodeLabels(result.Nodes)
		for _, e := range result.Edges {
			from := nodeLabels[e.FromID]
			to := nodeLabels[e.ToID]
			fmt.Fprintf(&b, "- %s → **%s** → %s\n", from, formatEdgeType(e.Type), to)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderDocumentTemplate renders a more formal document structure,
// grouping nodes by type with full content.
func renderDocumentTemplate(result *ComposeResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Knowledge Document\n\n")
	fmt.Fprintf(&b, "_Generated from %d nodes (%d tokens)_\n\n", result.NodeCount, result.TotalTokens)

	// Group by type
	byType := make(map[string][]*db.Node)
	typeOrder := []string{}
	for _, n := range result.Nodes {
		if _, exists := byType[n.Type]; !exists {
			typeOrder = append(typeOrder, n.Type)
		}
		byType[n.Type] = append(byType[n.Type], n)
	}

	for _, t := range typeOrder {
		nodes := byType[t]
		fmt.Fprintf(&b, "## %s (%d)\n\n", titleCase(t)+"s", len(nodes))

		for _, n := range nodes {
			shortID := n.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			fmt.Fprintf(&b, "### %s\n\n", shortID)
			b.WriteString(n.Content)
			b.WriteString("\n\n")
		}
	}

	// Relationships
	if len(result.Edges) > 0 {
		b.WriteString("## Relationships\n\n")
		nodeLabels := buildNodeLabels(result.Nodes)
		for _, e := range result.Edges {
			from := nodeLabels[e.FromID]
			to := nodeLabels[e.ToID]
			fmt.Fprintf(&b, "- %s → **%s** → %s\n", from, formatEdgeType(e.Type), to)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func buildNodeLabels(nodes []*db.Node) map[string]string {
	labels := make(map[string]string, len(nodes))
	for _, n := range nodes {
		labels[n.ID] = fmt.Sprintf("%s:%s", n.Type, n.ID)
	}
	return labels
}

func formatEdgeType(t string) string {
	return strings.ReplaceAll(strings.ToLower(t), "_", " ")
}
