package doc

// XML interchange for document structure.
//
// MarshalScaffold emits a pure-structure <ctx:doc> XML for the document.
// UnmarshalScaffold parses the XML back to a Scaffold struct.
// ApplyScaffold diffs the in-memory scaffold against the live edge graph and
// applies a minimal set of mutations transactionally.
//
// XML shape:
//   <ctx:doc id="DOCID">
//     <ctx:node ref="ULID">
//       <ctx:node ref="ULID"/>
//     </ctx:node>
//   </ctx:doc>
//
// Only CONTAINS edges are represented. No content bodies are embedded.

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/zate/ctx/internal/db"
)

// ScaffoldNode is one node in the scaffold XML tree.
type ScaffoldNode struct {
	Ref      string          `xml:"ref,attr"`
	Children []*ScaffoldNode `xml:"node"`
}

// Scaffold is the in-memory representation of a <ctx:doc> scaffold.
type Scaffold struct {
	DocID    string
	Children []*ScaffoldNode
}

// xmlDoc is the wire format for XML marshal/unmarshal.
type xmlDoc struct {
	XMLName  xml.Name      `xml:"doc"`
	ID       string        `xml:"id,attr"`
	Children []*xmlDocNode `xml:"node"`
}

type xmlDocNode struct {
	Ref      string        `xml:"ref,attr"`
	Children []*xmlDocNode `xml:"node"`
}

// MarshalScaffold loads the document's CONTAINS edge graph and emits the
// canonical <ctx:doc> XML. The output is deterministic: nodes are ordered by
// position (ascending) at each level of the tree.
func MarshalScaffold(docID string, store db.Store) ([]byte, error) {
	// Verify the document node exists.
	node, err := store.GetNode(docID)
	if err != nil {
		return nil, fmt.Errorf("scaffold.Marshal: get document node: %w", err)
	}
	if node.Kind != db.NodeKindDocument {
		return nil, fmt.Errorf("scaffold.Marshal: node %q has kind=%q, want %q", docID, node.Kind, db.NodeKindDocument)
	}

	// Load all CONTAINS edges for this document ordered by position.
	rows, err := store.Query(
		`SELECT to_id, position FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?
		 ORDER BY position ASC`,
		docID, docID,
	)
	if err != nil {
		return nil, fmt.Errorf("scaffold.Marshal: query edges: %w", err)
	}
	defer rows.Close()

	type edgeRow struct {
		toID     string
		position int
	}
	var edges []edgeRow
	for rows.Next() {
		var e edgeRow
		if err := rows.Scan(&e.toID, &e.position); err != nil {
			return nil, fmt.Errorf("scaffold.Marshal: scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scaffold.Marshal: rows: %w", err)
	}

	// Build the XML node list. Persist stores nodes flat (not hierarchically),
	// so each content node maps to one <ctx:node ref="..."/> child of the
	// document root, ordered by position.
	xDoc := &xmlDoc{ID: docID}
	for _, e := range edges {
		xDoc.Children = append(xDoc.Children, &xmlDocNode{Ref: e.toID})
	}

	out, err := xml.MarshalIndent(xDoc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scaffold.Marshal: xml marshal: %w", err)
	}

	// Prepend XML header and add ctx: namespace prefix manually since Go's
	// encoding/xml doesn't support custom namespace prefixes in a clean way.
	raw := string(out)
	raw = strings.ReplaceAll(raw, "<doc ", "<ctx:doc ")
	raw = strings.ReplaceAll(raw, "</doc>", "</ctx:doc>")
	raw = strings.ReplaceAll(raw, "<node ", "<ctx:node ")
	raw = strings.ReplaceAll(raw, "<node/>", "<ctx:node/>")
	raw = strings.ReplaceAll(raw, "</node>", "</ctx:node>")

	result := []byte(xml.Header + raw + "\n")
	return result, nil
}

// UnmarshalScaffold parses a <ctx:doc> XML byte slice back into a Scaffold.
// Returns an error on malformed XML. Does NOT validate node IDs against the
// database — call ApplyScaffold for that (it validates and returns missing IDs).
func UnmarshalScaffold(xmlBytes []byte) (*Scaffold, error) {
	// Strip ctx: prefix so Go's xml package can parse it.
	normalized := string(xmlBytes)
	normalized = strings.ReplaceAll(normalized, "<ctx:doc ", "<doc ")
	normalized = strings.ReplaceAll(normalized, "</ctx:doc>", "</doc>")
	normalized = strings.ReplaceAll(normalized, "<ctx:node ", "<node ")
	normalized = strings.ReplaceAll(normalized, "<ctx:node/>", "<node/>")
	normalized = strings.ReplaceAll(normalized, "</ctx:node>", "</node>")

	var xDoc xmlDoc
	if err := xml.Unmarshal([]byte(normalized), &xDoc); err != nil {
		return nil, fmt.Errorf("scaffold.Unmarshal: xml parse: %w", err)
	}
	if xDoc.ID == "" {
		return nil, fmt.Errorf("scaffold.Unmarshal: missing doc id attribute")
	}

	s := &Scaffold{
		DocID:    xDoc.ID,
		Children: convertXMLNodes(xDoc.Children),
	}
	return s, nil
}

// convertXMLNodes recursively converts xmlDocNode slice to ScaffoldNode slice.
func convertXMLNodes(xNodes []*xmlDocNode) []*ScaffoldNode {
	if len(xNodes) == 0 {
		return nil
	}
	result := make([]*ScaffoldNode, len(xNodes))
	for i, xn := range xNodes {
		result[i] = &ScaffoldNode{
			Ref:      xn.Ref,
			Children: convertXMLNodes(xn.Children),
		}
	}
	return result
}

// scaffoldFlat flattens a Scaffold's children in document order, assigning
// positions i*10 (1-indexed) to match the Persist convention.
func scaffoldFlat(children []*ScaffoldNode) []string {
	var ids []string
	var walk func(nodes []*ScaffoldNode)
	walk = func(nodes []*ScaffoldNode) {
		for _, n := range nodes {
			ids = append(ids, n.Ref)
			walk(n.Children)
		}
	}
	walk(children)
	return ids
}

// ApplyScaffold diffs the scaffold against the current CONTAINS edge graph and
// applies a minimal mutation set transactionally. Pure rearrangement never
// touches content node bodies.
//
// Validation:
//   - All ref IDs in the scaffold must exist in the store as kind='content' nodes.
//   - Any missing IDs cause an error listing all of them.
//   - The scaffold's docID must exist as a kind='document' node.
//
// Mutation strategy:
//   - Compute desired order from scaffold (depth-first, positions i*10).
//   - Compute current order from CONTAINS edges (ordered by position).
//   - DELETE edges whose to_id is not in the new set.
//   - INSERT edges for to_ids not currently present.
//   - UPDATE position for edges that exist but are in the wrong slot.
//   - Only touch edges that actually need changing.
func ApplyScaffold(s *Scaffold, store db.Store) error {
	// 1. Verify document node exists.
	docNode, err := store.GetNode(s.DocID)
	if err != nil {
		return fmt.Errorf("scaffold.Apply: get document node: %w", err)
	}
	if docNode.Kind != db.NodeKindDocument {
		return fmt.Errorf("scaffold.Apply: node %q has kind=%q, want %q", s.DocID, docNode.Kind, db.NodeKindDocument)
	}

	// 2. Get desired flat order.
	desiredIDs := scaffoldFlat(s.Children)

	// 3. Validate all ref IDs exist as content nodes.
	var missing []string
	for _, id := range desiredIDs {
		n, err := store.GetNode(id)
		if err != nil {
			missing = append(missing, id)
			continue
		}
		if n.Kind != db.NodeKindContent {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("scaffold.Apply: unresolved refs: %s", strings.Join(missing, ", "))
	}

	// 4. Load current edges.
	rows, err := store.Query(
		`SELECT id, to_id, position FROM edges
		 WHERE from_id = ? AND type = 'CONTAINS' AND document_id = ?
		 ORDER BY position ASC`,
		s.DocID, s.DocID,
	)
	if err != nil {
		return fmt.Errorf("scaffold.Apply: query current edges: %w", err)
	}

	type currentEdge struct {
		edgeID   string
		toID     string
		position int
	}
	var current []currentEdge
	for rows.Next() {
		var ce currentEdge
		if err := rows.Scan(&ce.edgeID, &ce.toID, &ce.position); err != nil {
			rows.Close()
			return fmt.Errorf("scaffold.Apply: scan edge: %w", err)
		}
		current = append(current, ce)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scaffold.Apply: rows: %w", err)
	}

	// Build lookup: toID -> currentEdge
	currentByToID := make(map[string]currentEdge, len(current))
	for _, ce := range current {
		currentByToID[ce.toID] = ce
	}

	// Build desired set for fast lookup.
	desiredSet := make(map[string]int, len(desiredIDs))
	for i, id := range desiredIDs {
		desiredSet[id] = (i + 1) * 10
	}

	// Begin transaction.
	tx, err := store.Begin()
	if err != nil {
		return fmt.Errorf("scaffold.Apply: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)

	// DELETE edges whose to_id is no longer in the desired set.
	for _, ce := range current {
		if _, ok := desiredSet[ce.toID]; !ok {
			if _, err := tx.Exec(`DELETE FROM edges WHERE id = ?`, ce.edgeID); err != nil {
				return fmt.Errorf("scaffold.Apply: delete edge %s: %w", ce.edgeID, err)
			}
		}
	}

	// UPDATE or INSERT edges in desired order.
	for i, id := range desiredIDs {
		wantPos := (i + 1) * 10
		if ce, exists := currentByToID[id]; exists {
			// Edge exists — only update if position changed.
			if ce.position != wantPos {
				if _, err := tx.Exec(`UPDATE edges SET position = ? WHERE id = ?`, wantPos, ce.edgeID); err != nil {
					return fmt.Errorf("scaffold.Apply: update position for edge %s: %w", ce.edgeID, err)
				}
			}
		} else {
			// Edge doesn't exist — insert it.
			edgeID := db.NewID()
			if _, err := tx.Exec(
				`INSERT INTO edges (id, from_id, to_id, type, created_at, metadata, document_id, position)
				 VALUES (?, ?, ?, 'CONTAINS', ?, '{}', ?, ?)`,
				edgeID, s.DocID, id, now, s.DocID, wantPos,
			); err != nil {
				return fmt.Errorf("scaffold.Apply: insert edge for %s: %w", id, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("scaffold.Apply: commit: %w", err)
	}

	return nil
}

// SearchContent performs a substring search over kind='content' node bodies.
// This is strictly separate from the memory FTS surface (ctx search).
// Returns up to limit results; if limit <= 0 defaults to 50.
func SearchContent(query string, limit int, store db.Store) ([]*db.Node, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := store.Query(
		`SELECT id, type, kind, content, token_estimate, created_at, updated_at, metadata
		 FROM nodes
		 WHERE kind = 'content' AND content LIKE ?
		 LIMIT ?`,
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scaffold.SearchContent: query: %w", err)
	}
	defer rows.Close()

	var nodes []*db.Node
	for rows.Next() {
		n := &db.Node{}
		var createdAt, updatedAt string
		if err := rows.Scan(&n.ID, &n.Type, &n.Kind, &n.Content, &n.TokenEstimate, &createdAt, &updatedAt, &n.Metadata); err != nil {
			return nil, fmt.Errorf("scaffold.SearchContent: scan: %w", err)
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		n.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scaffold.SearchContent: rows: %w", err)
	}

	// Suppress nil; callers can range over an empty slice.
	if nodes == nil {
		nodes = []*db.Node{}
	}
	return nodes, nil
}
