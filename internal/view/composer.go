package view

import (
	"fmt"
	"sort"
	"strings"
	"time"

	agentpkg "github.com/zate/ctx/internal/agent"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/query"
)

type ComposeOptions struct {
	Query                 string
	IDs                   []string // If set, compose exactly these nodes (bypasses query)
	SeedID                string   // If set, start from this node and traverse edges
	Depth                 int      // Traversal depth for seed mode (default 1)
	Budget                int
	Project               string // If set, filter out nodes scoped to other projects
	Agent                 string // If set, filter to agent-scoped + global nodes
	IncludeReferenceStats bool   // If true, count available tier:reference nodes
	IncludeEdges          bool   // If true, fetch and include edges between composed nodes
}

type ComposeResult struct {
	Nodes             []*db.Node
	Edges             []*db.Edge // Edges between composed nodes (if IncludeEdges)
	TotalTokens       int
	NodeCount         int
	RenderedAt        time.Time
	LastSessionStores int            // -1 means unknown/not set
	ReferenceCount    int            // Number of available tier:reference nodes
	ReferenceByType   map[string]int // Breakdown by node type
	Primer            string         // Custom primer text (replaces built-in if set)
}

func Compose(d db.Store, opts ComposeOptions) (*ComposeResult, error) {
	var nodes []*db.Node
	var err error
	explicitIDs := false // true when user explicitly requested specific nodes

	if len(opts.IDs) > 0 {
		explicitIDs = true
		// Fetch specific nodes by ID (supports short prefixes)
		for _, id := range opts.IDs {
			resolved, resolveErr := d.ResolveID(id)
			if resolveErr != nil {
				return nil, fmt.Errorf("failed to resolve node ID %q: %w", id, resolveErr)
			}
			node, getErr := d.GetNode(resolved)
			if getErr != nil {
				return nil, fmt.Errorf("failed to get node %q: %w", resolved, getErr)
			}
			nodes = append(nodes, node)
		}
	} else if opts.SeedID != "" {
		// Traverse graph from seed node
		resolved, resolveErr := d.ResolveID(opts.SeedID)
		if resolveErr != nil {
			return nil, fmt.Errorf("failed to resolve seed ID %q: %w", opts.SeedID, resolveErr)
		}
		depth := opts.Depth
		if depth <= 0 {
			depth = 1
		}
		collected := traverseGraph(d, resolved, depth)
		for _, id := range collected {
			node, getErr := d.GetNode(id)
			if getErr != nil {
				continue
			}
			// Exclude non-memory nodes from seed traversal
			if node.Kind != "" && node.Kind != db.NodeKindMemory {
				continue
			}
			nodes = append(nodes, node)
		}
		// Enable edges automatically for seed traversal
		opts.IncludeEdges = true
	} else if opts.Query != "" {
		nodes, err = query.ExecuteQuery(d, opts.Query, false)
	} else {
		nodes, err = d.ListNodes(db.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Sort by priority: pinned > reference > working > other
	// Within same tier, sort by ULID (stable creation order) for KV cache consistency.
	// Using ID sort instead of CreatedAt ensures the same node set always produces
	// the same token sequence, enabling prefix cache hits across sessions.
	sort.SliceStable(nodes, func(i, j int) bool {
		pi := tierPriority(nodes[i].Tags)
		pj := tierPriority(nodes[j].Tags)
		if pi != pj {
			return pi < pj
		}
		return nodes[i].ID < nodes[j].ID
	})

	// Skip project/agent filtering when user explicitly requested specific nodes
	if !explicitIDs {
		// Filter by project scope
		// When project is empty, include global nodes AND project-scoped nodes
		// (don't exclude project-scoped nodes just because no --project was given)
		var filtered []*db.Node
		for _, n := range nodes {
			if shouldIncludeForProject(n, opts.Project) {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered

		// Filter by agent partition
		nodes = agentpkg.FilterNodes(nodes, opts.Agent)
	}

	// Apply budget
	result := &ComposeResult{
		RenderedAt:        time.Now().UTC(),
		LastSessionStores: -1,
	}

	if opts.Budget <= 0 {
		return result, nil
	}

	for _, n := range nodes {
		if result.TotalTokens+n.TokenEstimate > opts.Budget {
			continue
		}
		result.Nodes = append(result.Nodes, n)
		result.TotalTokens += n.TokenEstimate
		result.NodeCount++
	}

	// Fetch edges between composed nodes if requested
	if opts.IncludeEdges && len(result.Nodes) > 0 {
		nodeSet := make(map[string]bool, len(result.Nodes))
		for _, n := range result.Nodes {
			nodeSet[n.ID] = true
		}
		for _, n := range result.Nodes {
			edges, edgeErr := d.GetEdgesFrom(n.ID)
			if edgeErr != nil {
				continue
			}
			for _, e := range edges {
				if nodeSet[e.ToID] {
					result.Edges = append(result.Edges, e)
				}
			}
		}
	}

	// Count available reference nodes if requested
	if opts.IncludeReferenceStats {
		refNodes, err := query.ExecuteQuery(d, "tag:tier:reference", false)
		if err == nil {
			// Apply same project filtering (always filter)
			var filteredRef []*db.Node
			for _, n := range refNodes {
				if shouldIncludeForProject(n, opts.Project) {
					filteredRef = append(filteredRef, n)
				}
			}
			refNodes = agentpkg.FilterNodes(filteredRef, opts.Agent)
			result.ReferenceCount = len(refNodes)
			result.ReferenceByType = make(map[string]int)
			for _, n := range refNodes {
				result.ReferenceByType[n.Type]++
			}
		}
	}

	return result, nil
}

// shouldIncludeForProject returns true if a node should be included given the current project.
// A node is project-scoped if it has any tag matching "project:*" (excluding "project:global").
// If project-scoped, it only loads if one of its project tags matches the current project.
// Nodes with no project tags or tagged "project:global" load everywhere.
// When currentProject is empty, all nodes are included (no project filtering).
func shouldIncludeForProject(node *db.Node, currentProject string) bool {
	// No project filter specified — include everything
	if currentProject == "" {
		return true
	}
	hasProjectTag := false
	matchesCurrent := false
	for _, tag := range node.Tags {
		if tag == "project:global" {
			return true
		}
		if strings.HasPrefix(tag, "project:") {
			hasProjectTag = true
			project := strings.TrimPrefix(tag, "project:")
			if strings.EqualFold(project, currentProject) {
				matchesCurrent = true
			}
		}
	}
	if !hasProjectTag {
		return true
	}
	return matchesCurrent
}

func tierPriority(tags []string) int {
	for _, t := range tags {
		switch t {
		case "tier:pinned":
			return 0
		case "tier:reference":
			return 1
		case "tier:working":
			return 2
		}
	}
	return 3
}

func RenderMarkdown(result *ComposeResult) string {
	var b strings.Builder

	// Omit rendered-at timestamp for KV cache stability — the same node set
	// should always produce the same token sequence across sessions.
	header := fmt.Sprintf("<!-- ctx: %d nodes, %d tokens",
		result.NodeCount, result.TotalTokens)
	if result.LastSessionStores > 0 {
		header += fmt.Sprintf(" | last session: %d nodes stored", result.LastSessionStores)
	} else if result.LastSessionStores == 0 {
		header += " | last session: no new knowledge stored"
	}
	header += " -->\n\n"
	b.WriteString(header)

	// Usage primer — custom or built-in
	if result.Primer != "" {
		b.WriteString(result.Primer)
		if !strings.HasSuffix(result.Primer, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("You have persistent memory via `ctx`. Use the `ctx` CLI (via Bash) to store and query knowledge.\n\n")
		b.WriteString("**Store knowledge when:**\n")
		b.WriteString("- You make or learn a **decision** -- `ctx add --type decision --tag tier:pinned \"...\"`\n")
		b.WriteString("- You discover a **preference** or convention -- `ctx add --type fact --tag tier:pinned \"...\"`\n")
		b.WriteString("- You see a recurring **pattern** -- `ctx add --type pattern --tag tier:pinned \"...\"`\n")
		b.WriteString("- Debugging reveals a **root cause** -- `ctx add --type observation --tag tier:working \"...\"`\n")
		b.WriteString("- An idea worth revisiting -- `ctx add --type hypothesis --tag tier:working \"...\"`\n")
		b.WriteString("- Durable but not critical knowledge -- use `--tag tier:reference`\n\n")
		b.WriteString("**Key question:** Every session? -- `tier:pinned`. Someday? -- `tier:reference`. This task? -- `tier:working`.\n\n")
		b.WriteString("**Query:** `ctx query 'type:decision AND tag:project:X'` | **Status:** `ctx status`\n")
		b.WriteString("Always include a `tier:` tag and `project:` tag. Invoke the `ctx` skill for full reference.\n\n")
	}

	// Show reference availability if stats are present
	if result.ReferenceCount > 0 {
		fmt.Fprintf(&b, "**Reference available:** %d nodes not auto-loaded (use `ctx query` to access)", result.ReferenceCount)
		if len(result.ReferenceByType) > 0 {
			var parts []string
			// Sort types for consistent output
			typeNames := make([]string, 0, len(result.ReferenceByType))
			for t := range result.ReferenceByType {
				typeNames = append(typeNames, t)
			}
			sort.Strings(typeNames)
			for _, t := range typeNames {
				parts = append(parts, fmt.Sprintf("%d %ss", result.ReferenceByType[t], t))
			}
			fmt.Fprintf(&b, " — %s", strings.Join(parts, ", "))
		}
		b.WriteString("\n\n")
	}

	// Group by tier then type
	groups := map[string][]*db.Node{
		"pinned":    {},
		"reference": {},
		"working":   {},
		"other":     {},
	}

	for _, n := range result.Nodes {
		tier := "other"
		for _, t := range n.Tags {
			switch t {
			case "tier:pinned":
				tier = "pinned"
			case "tier:reference":
				tier = "reference"
			case "tier:working":
				tier = "working"
			}
		}
		groups[tier] = append(groups[tier], n)
	}

	renderGroup := func(title string, nodes []*db.Node) {
		if len(nodes) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n", title)

		// Sub-group by type
		byType := map[string][]*db.Node{}
		typeOrder := []string{}
		for _, n := range nodes {
			if _, exists := byType[n.Type]; !exists {
				typeOrder = append(typeOrder, n.Type)
			}
			byType[n.Type] = append(byType[n.Type], n)
		}

		for _, t := range typeOrder {
			if len(typeOrder) > 1 {
				fmt.Fprintf(&b, "### %s\n\n", titleCase(t))
			}
			for _, n := range byType[t] {
				content := n.Content
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				fmt.Fprintf(&b, "- [%s:%s] %s\n", n.Type, n.ID, content)
				if len(n.Tags) > 0 {
					fmt.Fprintf(&b, "  - Tags: %s\n", strings.Join(n.Tags, ", "))
				}
			}
			b.WriteString("\n")
		}
	}

	renderGroup("Pinned", groups["pinned"])
	renderGroup("Reference", groups["reference"])
	renderGroup("Working Context", groups["working"])
	renderGroup("Other", groups["other"])

	// Render relationships between composed nodes
	if len(result.Edges) > 0 {
		// Build ID-to-short-label map
		nodeLabels := make(map[string]string, len(result.Nodes))
		for _, n := range result.Nodes {
			nodeLabels[n.ID] = fmt.Sprintf("[%s:%s]", n.Type, n.ID)
		}

		b.WriteString("## Relationships\n\n")
		for _, e := range result.Edges {
			fromLabel := nodeLabels[e.FromID]
			toLabel := nodeLabels[e.ToID]
			if fromLabel == "" {
				fromLabel = e.FromID
			}
			if toLabel == "" {
				toLabel = e.ToID
			}
			fmt.Fprintf(&b, "- %s —%s→ %s\n", fromLabel, e.Type, toLabel)
		}
		b.WriteString("\n")
	}

	b.WriteString("<!-- ctx:end -->\n")
	return b.String()
}

func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// traverseGraph does a BFS from a seed node, following edges up to maxDepth.
// Returns a list of unique node IDs in traversal order.
func traverseGraph(d db.Store, seedID string, maxDepth int) []string {
	visited := map[string]bool{seedID: true}
	order := []string{seedID}
	frontier := []string{seedID}

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []string
		for _, nodeID := range frontier {
			// Follow outgoing edges
			edges, err := d.GetEdges(nodeID, "")
			if err != nil {
				continue
			}
			for _, e := range edges {
				neighbor := e.ToID
				if e.ToID == nodeID {
					neighbor = e.FromID
				}
				if !visited[neighbor] {
					visited[neighbor] = true
					order = append(order, neighbor)
					nextFrontier = append(nextFrontier, neighbor)
				}
			}
		}
		frontier = nextFrontier
	}

	return order
}

func RenderText(result *ComposeResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Context: %d nodes, %d tokens\n\n", result.NodeCount, result.TotalTokens)
	for _, n := range result.Nodes {
		preview := n.Content
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		tags := ""
		if len(n.Tags) > 0 {
			tags = " [" + strings.Join(n.Tags, ", ") + "]"
		}
		fmt.Fprintf(&b, "[%s] %s: %s%s\n", n.ID, n.Type, preview, tags)
	}
	return b.String()
}
