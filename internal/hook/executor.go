package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	agentpkg "github.com/zate/ctx/internal/agent"
	"github.com/zate/ctx/internal/db"
)

// ExecuteCommands processes parsed ctx commands against the database.
func ExecuteCommands(d db.Store, commands []CtxCommand) error {
	for _, cmd := range commands {
		if err := executeCommand(d, cmd); err != nil {
			fmt.Fprintf(os.Stderr, "ctx warning: failed to execute %s command: %v\n", cmd.Type, err)
		}
	}
	return nil
}

// ExecuteCommandsWithErrors processes parsed ctx commands and returns errors.
func ExecuteCommandsWithErrors(d db.Store, commands []CtxCommand) []error {
	var errs []error
	for _, cmd := range commands {
		if err := executeCommand(d, cmd); err != nil {
			errs = append(errs, fmt.Errorf("%s command failed: %w", cmd.Type, err))
		}
	}
	return errs
}

func executeCommand(d db.Store, cmd CtxCommand) error {
	switch cmd.Type {
	case "remember":
		return executeRemember(d, cmd)
	case "recall":
		return executeRecall(d, cmd)
	case "summarize":
		return executeSummarize(d, cmd)
	case "link":
		return executeLink(d, cmd)
	case "status":
		return executeStatus(d)
	case "task":
		return executeTask(d, cmd)
	case "expand":
		return executeExpand(d, cmd)
	case "supersede":
		return executeSupersede(d, cmd)
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

func executeRemember(d db.Store, cmd CtxCommand) error {
	nodeType := cmd.Attrs["type"]
	if nodeType == "" {
		return fmt.Errorf("remember: type attribute is required")
	}
	content := strings.TrimSpace(cmd.Content)
	if content == "" {
		return fmt.Errorf("remember: content is required")
	}
	// remember always creates memory-kind nodes; refuse any explicit non-memory kind.
	if k, ok := cmd.Attrs["kind"]; ok && k != "" && k != db.NodeKindMemory {
		return fmt.Errorf("remember: cannot create non-memory nodes (got kind=%q); use ctx doc for documents", k)
	}

	var tags []string
	if tagStr, ok := cmd.Attrs["tags"]; ok && tagStr != "" {
		tags = strings.Split(tagStr, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
	}

	// Auto-add current task tag if working tier
	currentTask, err := d.GetPending("current_task")
	if err == nil && currentTask != "" {
		hasWorking := false
		for _, t := range tags {
			if t == "tier:working" {
				hasWorking = true
				break
			}
		}
		if hasWorking {
			tags = append(tags, "task:"+currentTask)
		}
	}

	// Auto-add agent tag from current session
	currentAgent, agentErr := d.GetPending("current_agent")
	if agentErr == nil && currentAgent != "" {
		hasAgentTag := false
		for _, t := range tags {
			if strings.HasPrefix(t, "agent:") {
				hasAgentTag = true
				break
			}
		}
		if !hasAgentTag {
			tags = append(tags, "agent:"+currentAgent)
		}
	}

	// Auto-add project tag from current session
	currentProject, projErr := d.GetPending("current_project")
	if projErr == nil && currentProject != "" {
		hasProjectTag := false
		for _, t := range tags {
			if strings.HasPrefix(t, "project:") {
				hasProjectTag = true
				break
			}
		}
		if !hasProjectTag {
			tags = append(tags, "project:"+currentProject)
		}
	}

	// Check for existing node with same type and content to avoid duplicates
	existing, err := d.FindByTypeAndContent(nodeType, content)
	if err != nil {
		return fmt.Errorf("remember: failed to check for duplicates: %w", err)
	}
	if existing != nil {
		// Node already exists — merge any new tags
		for _, tag := range tags {
			_ = d.AddTag(existing.ID, tag)
		}
		return nil
	}

	_, err = d.CreateNode(db.CreateNodeInput{
		Type:    nodeType,
		Content: content,
		Tags:    tags,
	})
	return err
}

func executeRecall(d db.Store, cmd CtxCommand) error {
	queryStr := cmd.Attrs["query"]
	if queryStr == "" {
		return fmt.Errorf("recall: query attribute is required")
	}

	// Import query package dynamically to avoid circular deps
	// Instead, we'll use the db directly with a simple approach
	// Store query for later execution by prompt-submit hook
	return d.SetPending("recall_query", queryStr)
}

func executeSummarize(d db.Store, cmd CtxCommand) error {
	nodesStr := cmd.Attrs["nodes"]
	if nodesStr == "" {
		return fmt.Errorf("summarize: nodes attribute is required")
	}
	content := strings.TrimSpace(cmd.Content)
	if content == "" {
		return fmt.Errorf("summarize: content is required")
	}

	nodeIDs := strings.Split(nodesStr, ",")
	for i := range nodeIDs {
		nodeIDs[i] = strings.TrimSpace(nodeIDs[i])
	}

	// Resolve short ID prefixes
	for i, id := range nodeIDs {
		resolved, err := d.ResolveID(id)
		if err != nil {
			return fmt.Errorf("summarize: failed to resolve node ID %q: %w", id, err)
		}
		nodeIDs[i] = resolved
	}

	archive := cmd.Attrs["archive"] == "true"

	summary, err := d.CreateNode(db.CreateNodeInput{
		Type:    "summary",
		Content: content,
	})
	if err != nil {
		return err
	}

	for _, sourceID := range nodeIDs {
		if _, err := d.CreateEdge(summary.ID, sourceID, "DERIVED_FROM"); err != nil {
			return fmt.Errorf("summarize: failed to create edge: %w", err)
		}
		if archive {
			_ = d.RemoveTag(sourceID, "tier:working")
			_ = d.RemoveTag(sourceID, "tier:reference")
			_ = d.RemoveTag(sourceID, "tier:pinned")
			_ = d.AddTag(sourceID, "tier:off-context")
		}
	}

	return nil
}

func executeLink(d db.Store, cmd CtxCommand) error {
	fromID := cmd.Attrs["from"]
	toID := cmd.Attrs["to"]
	edgeType := cmd.Attrs["type"]
	if fromID == "" || toID == "" {
		return fmt.Errorf("link: from and to attributes are required")
	}
	if edgeType == "" {
		edgeType = "RELATES_TO"
	}

	// Resolve short ID prefixes
	resolvedFrom, err := d.ResolveID(fromID)
	if err != nil {
		return fmt.Errorf("link: failed to resolve from ID %q: %w", fromID, err)
	}
	resolvedTo, err := d.ResolveID(toID)
	if err != nil {
		return fmt.Errorf("link: failed to resolve to ID %q: %w", toID, err)
	}

	_, err = d.CreateEdge(resolvedFrom, resolvedTo, edgeType)
	return err
}

func executeStatus(d db.Store) error {
	// Determine agent scope
	currentAgent, _ := d.GetPending("current_agent")

	// Build agent filter SQL fragment
	agentFilter := agentpkg.FilterSQL(currentAgent)

	var totalNodes, totalTokens, edgeCount, tagCount int
	_ = d.QueryRow("SELECT COUNT(*) FROM nodes n WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + agentFilter).Scan(&totalNodes)
	_ = d.QueryRow("SELECT COALESCE(SUM(n.token_estimate), 0) FROM nodes n WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + agentFilter).Scan(&totalTokens)
	_ = d.QueryRow("SELECT COUNT(*) FROM edges").Scan(&edgeCount)
	_ = d.QueryRow("SELECT COUNT(DISTINCT tag) FROM tags").Scan(&tagCount)

	status := fmt.Sprintf("Nodes: %d (%d tokens), Edges: %d, Tags: %d unique", totalNodes, totalTokens, edgeCount, tagCount)

	// Add type breakdown
	rows, err := d.Query("SELECT n.type, COUNT(*) FROM nodes n WHERE n.kind = 'memory' AND n.superseded_by IS NULL" + agentFilter + " GROUP BY n.type ORDER BY n.type")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int
			_ = rows.Scan(&t, &c)
			status += fmt.Sprintf("\n  %s: %d", t, c)
		}
	}

	return d.SetPending("status_output", status)
}

func executeTask(d db.Store, cmd CtxCommand) error {
	name := cmd.Attrs["name"]
	action := cmd.Attrs["action"]

	if name == "" || action == "" {
		return fmt.Errorf("task: name and action attributes are required")
	}

	switch action {
	case "start":
		return d.SetPending("current_task", name)

	case "end":
		// Archive working nodes for this task
		rows, err := d.Query(`SELECT DISTINCT t1.node_id FROM tags t1
			JOIN tags t2 ON t1.node_id = t2.node_id
			WHERE t1.tag = ? AND t2.tag = 'tier:working'`, "task:"+name)
		if err != nil {
			return err
		}
		defer rows.Close()

		var nodeIDs []string
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			nodeIDs = append(nodeIDs, id)
		}

		for _, id := range nodeIDs {
			// Check if it's a decision (keep in reference)
			node, err := d.GetNode(id)
			if err != nil {
				continue
			}
			if node.Type == "decision" {
				// Promote to reference
				_ = d.RemoveTag(id, "tier:working")
				_ = d.AddTag(id, "tier:reference")
			} else {
				// Archive
				_ = d.RemoveTag(id, "tier:working")
				_ = d.AddTag(id, "tier:off-context")
			}
		}

		_ = d.DeletePending("current_task")
		return nil

	default:
		return fmt.Errorf("task: unknown action %s", action)
	}
}

func executeExpand(d db.Store, cmd CtxCommand) error {
	nodeID := cmd.Attrs["node"]
	if nodeID == "" {
		return fmt.Errorf("expand: node attribute is required")
	}

	// Resolve short ID prefix
	resolvedID, err := d.ResolveID(nodeID)
	if err != nil {
		return fmt.Errorf("expand: failed to resolve node ID %q: %w", nodeID, err)
	}
	nodeID = resolvedID

	// Get DERIVED_FROM edges
	edges, err := d.GetEdgesFrom(nodeID)
	if err != nil {
		return err
	}

	var sourceIDs []string
	for _, e := range edges {
		if e.Type == "DERIVED_FROM" {
			sourceIDs = append(sourceIDs, e.ToID)
		}
	}

	if len(sourceIDs) == 0 {
		return nil
	}

	data, _ := json.Marshal(sourceIDs)
	return d.SetPending("expand_nodes", string(data))
}

func executeSupersede(d db.Store, cmd CtxCommand) error {
	oldID := cmd.Attrs["old"]
	newID := cmd.Attrs["new"]
	if oldID == "" || newID == "" {
		return fmt.Errorf("supersede: old and new attributes are required")
	}

	// Resolve short ID prefixes
	resolvedOld, err := d.ResolveID(oldID)
	if err != nil {
		return fmt.Errorf("supersede: failed to resolve old ID %q: %w", oldID, err)
	}
	resolvedNew, err := d.ResolveID(newID)
	if err != nil {
		return fmt.Errorf("supersede: failed to resolve new ID %q: %w", newID, err)
	}
	oldID = resolvedOld
	newID = resolvedNew

	// Mark old as superseded
	_, err = d.Exec("UPDATE nodes SET superseded_by = ? WHERE id = ?", newID, oldID)
	if err != nil {
		return err
	}

	// Create SUPERSEDES edge
	_, err = d.CreateEdge(newID, oldID, "SUPERSEDES")
	return err
}
