// Package doc provides markdown decomposition and persistence for ctx doc.
//
// Decompose splits a markdown source into a tree of DocNodes at heading boundaries.
// Each node owns its source bytes from its own start up to the next sibling's start,
// preserving all whitespace byte-for-byte. Concatenating the tree in depth-first order
// yields the original source.
//
// Persist writes the tree to the database as a document node + content nodes +
// CONTAINS edges (document-scoped, positioned). After Persist, Compose re-assembles
// the source and the caller can assert byte-identity.
package doc

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// DocNode is a node in the decomposed document tree.
// Body holds the raw source bytes this node owns (heading line + body up to
// next sibling's first byte). Children are nested headings.
// The root DocNode has an empty Body and holds all top-level children.
type DocNode struct {
	Body     []byte
	Children []*DocNode
}

// DocTree is the root of the decomposed document.
type DocTree = DocNode

// Decompose parses src as GFM markdown and returns the document tree.
// Splitting is coarse: at heading boundaries only. Each heading "owns" all
// bytes from its first byte (the '#') up to but not including the next
// sibling (or parent's next sibling) heading's first byte.
// The root node's Body is always empty; its Children hold top-level sections
// (and any preamble before the first heading as a leaf child with no children).
func Decompose(src []byte) (*DocTree, error) {
	if len(src) == 0 {
		return &DocTree{}, nil
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)
	reader := text.NewReader(src)
	parser := md.Parser()
	doc := parser.Parse(reader)

	// Collect heading positions in document order.
	// We use the actual line start (including the '#' characters), not the
	// goldmark content start (which is after the leading hashes and space).
	type headingInfo struct {
		offset int
		level  int
	}
	var headings []headingInfo

	// The walk callback never returns an error, so ast.Walk cannot fail here.
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if n.Type() == ast.TypeInline {
			return ast.WalkSkipChildren, nil
		}
		if h, ok := n.(*ast.Heading); ok {
			lines := h.Lines()
			if lines.Len() > 0 {
				contentStart := lines.At(0).Start
				lineStart := findLineStart(src, contentStart)
				headings = append(headings, headingInfo{
					offset: lineStart,
					level:  h.Level,
				})
			}
		}
		return ast.WalkContinue, nil
	})

	root := &DocTree{}

	// Build sections: each heading's byte range is [start, next_heading_start).
	type section struct {
		start int
		end   int
		level int
	}

	sections := make([]section, len(headings))
	for i, h := range headings {
		end := len(src)
		if i+1 < len(headings) {
			end = headings[i+1].offset
		}
		sections[i] = section{start: h.offset, end: end, level: h.level}
	}

	// Preamble: content before the first heading.
	preambleEnd := 0
	if len(headings) > 0 {
		preambleEnd = headings[0].offset
	} else {
		preambleEnd = len(src)
	}

	if preambleEnd > 0 {
		preambleNode := &DocNode{Body: src[0:preambleEnd]}
		root.Children = append(root.Children, preambleNode)
	}

	// Build the tree using a parent stack.
	// Stack entries: (node, heading_level). Root is level 0.
	type stackEntry struct {
		node  *DocNode
		level int
	}
	stack := []stackEntry{{node: root, level: 0}}

	for _, sec := range sections {
		node := &DocNode{Body: src[sec.start:sec.end]}

		// Pop stack until the top's level is strictly less than current level.
		for len(stack) > 1 && stack[len(stack)-1].level >= sec.level {
			stack = stack[:len(stack)-1]
		}

		parent := stack[len(stack)-1].node
		parent.Children = append(parent.Children, node)
		stack = append(stack, stackEntry{node: node, level: sec.level})
	}

	return root, nil
}

// findLineStart returns the offset of the start of the line containing 'offset'.
// For a heading at byte position 'offset', this returns the offset of the first
// '#' in the heading line.
func findLineStart(src []byte, offset int) int {
	for i := offset - 1; i >= 0; i-- {
		if src[i] == '\n' {
			return i + 1
		}
	}
	return 0
}

// flattenChildren returns all DocNodes in the tree in depth-first order,
// excluding the root itself.
func flattenChildren(root *DocTree) []*DocNode {
	var result []*DocNode
	var walk func(n *DocNode)
	walk = func(n *DocNode) {
		for _, child := range n.Children {
			result = append(result, child)
			walk(child)
		}
	}
	walk(root)
	return result
}

// Compose reassembles the document by concatenating all node bodies in
// depth-first order. It operates on an in-memory DocTree; ComposeDoc in
// composer.go performs the equivalent reconstruction from the store.
func Compose(root *DocTree) []byte {
	var buf bytes.Buffer
	var walk func(n *DocNode)
	walk = func(n *DocNode) {
		buf.Write(n.Body)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return buf.Bytes()
}
