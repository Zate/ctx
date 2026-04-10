package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// These tests answer one question per command: does the thing the CLI
// claims to do actually happen? They execute through Cobra (exactly the
// path a user hits) and verify the resulting state or output directly.

// setupCLI gives each test a fresh temp DB and resets CLI global state
// so prior test runs don't leak flag values.
func setupCLI(t *testing.T) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "test.db")
	format = "text"
	agent = ""
	backend = "sqlite"

	// Reset flag-backed package vars so cobra re-parses cleanly.
	listType = ""
	listTags = nil
	listSince = ""
	listLimit = 0
	addType = ""
	addTags = nil
	addMeta = nil
	addStdin = false
	updateContent = ""
	updateType = ""
	updateMeta = ""
	linkType = "RELATES_TO"
	unlinkType = ""
	showWithEdges = false
	includeSuperseded = false
	composeQuery = ""
	composeBudget = 50000
	composeIDs = ""
	composeEdges = false
	composeTemplate = ""
	composeSeed = ""
	composeDepth = 1
	composeProject = ""

	// Run migrations once so the DB is ready.
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	d.Close()
}

// runCLI executes rootCmd with the given args and captures stdout.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs(args)
	execErr := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), execErr
}

// openTestDB opens the test DB directly for state assertions.
func openTestDB(t *testing.T) db.Store {
	t.Helper()
	d, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

// seedNode inserts a node directly (bypassing the CLI) for tests that
// need an existing node to act on.
func seedNode(t *testing.T, nodeType, content string, tags ...string) string {
	t.Helper()
	d := openTestDB(t)
	node, err := d.CreateNode(db.CreateNodeInput{
		Type:    nodeType,
		Content: content,
		Tags:    tags,
	})
	require.NoError(t, err)
	return node.ID
}

// ---- add: does ctx add create a node? ----

func TestCLI_Add_CreatesNode(t *testing.T) {
	setupCLI(t)

	_, err := runCLI(t, "add", "--type", "fact", "hello world")
	require.NoError(t, err)

	d := openTestDB(t)
	nodes, err := d.ListNodes(db.ListOptions{Type: "fact"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "hello world", nodes[0].Content)
	assert.Equal(t, "fact", nodes[0].Type)
}

func TestCLI_Add_WithMultipleTags(t *testing.T) {
	setupCLI(t)

	_, err := runCLI(t, "add", "--type", "decision",
		"--tag", "tier:pinned",
		"--tag", "project:Book",
		"we chose SQLite")
	require.NoError(t, err)

	d := openTestDB(t)
	nodes, err := d.ListNodes(db.ListOptions{Type: "decision"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	tags, err := d.GetTags(nodes[0].ID)
	require.NoError(t, err)
	assert.Contains(t, tags, "tier:pinned")
	assert.Contains(t, tags, "project:Book")
}

// ---- show: does ctx show print the node's content? ----

func TestCLI_Show_DisplaysContent(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "persistent knowledge for the win")

	out, err := runCLI(t, "show", id)
	require.NoError(t, err)
	assert.Contains(t, out, "persistent knowledge for the win")
	assert.Contains(t, out, id)
	assert.Contains(t, out, "fact")
}

// ---- list: does ctx list show existing nodes? ----

func TestCLI_List_ShowsNodes(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "first fact")
	seedNode(t, "fact", "second fact")

	out, err := runCLI(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "first fact")
	assert.Contains(t, out, "second fact")
}

// This is the regression test for the --tag bug we just fixed.
// The claim: "list --tag A --tag B" should AND-filter by both tags.
func TestCLI_List_MultipleTagsAreANDed(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "book pinned", "project:Book", "tier:pinned")
	seedNode(t, "fact", "book reference", "project:Book", "tier:reference")
	seedNode(t, "fact", "other pinned", "project:other", "tier:pinned")

	out, err := runCLI(t, "list",
		"--tag", "project:Book",
		"--tag", "tier:pinned")
	require.NoError(t, err)

	assert.Contains(t, out, "book pinned")
	assert.NotContains(t, out, "book reference", "different tier — should be filtered out")
	assert.NotContains(t, out, "other pinned", "different project — should be filtered out")
}

// ---- delete: does ctx delete remove the node? ----

func TestCLI_Delete_RemovesNode(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "doomed fact")

	_, err := runCLI(t, "delete", id)
	require.NoError(t, err)

	d := openTestDB(t)
	_, err = d.GetNode(id)
	assert.True(t, errors.Is(err, db.ErrNotFound), "node should no longer exist after delete")
}

// ---- update: does ctx update --content actually change the content? ----

func TestCLI_Update_ChangesContent(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "original content")

	_, err := runCLI(t, "update", id, "--content", "updated content")
	require.NoError(t, err)

	d := openTestDB(t)
	node, err := d.GetNode(id)
	require.NoError(t, err)
	assert.Equal(t, "updated content", node.Content)
}

// ---- tag: does ctx tag add the tag? ----

func TestCLI_Tag_AddsTag(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "untagged fact")

	_, err := runCLI(t, "tag", id, "tier:pinned")
	require.NoError(t, err)

	d := openTestDB(t)
	tags, err := d.GetTags(id)
	require.NoError(t, err)
	assert.Contains(t, tags, "tier:pinned")
}

func TestCLI_Tag_AddsMultipleTags(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "needs tags")

	_, err := runCLI(t, "tag", id, "tier:pinned", "project:ctx")
	require.NoError(t, err)

	d := openTestDB(t)
	tags, err := d.GetTags(id)
	require.NoError(t, err)
	assert.Contains(t, tags, "tier:pinned")
	assert.Contains(t, tags, "project:ctx")
}

// ---- untag: does ctx untag remove the tag? ----

func TestCLI_Untag_RemovesTag(t *testing.T) {
	setupCLI(t)
	id := seedNode(t, "fact", "tagged fact", "tier:pinned", "project:ctx")

	_, err := runCLI(t, "untag", id, "tier:pinned")
	require.NoError(t, err)

	d := openTestDB(t)
	tags, err := d.GetTags(id)
	require.NoError(t, err)
	assert.NotContains(t, tags, "tier:pinned", "tag should be gone")
	assert.Contains(t, tags, "project:ctx", "other tags should remain")
}

// ---- link: does ctx link create an edge? ----

func TestCLI_Link_CreatesEdge(t *testing.T) {
	setupCLI(t)
	fromID := seedNode(t, "fact", "from node")
	toID := seedNode(t, "fact", "to node")

	_, err := runCLI(t, "link", fromID, toID)
	require.NoError(t, err)

	d := openTestDB(t)
	edges, err := d.GetEdgesFrom(fromID)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, toID, edges[0].ToID)
	assert.Equal(t, "RELATES_TO", edges[0].Type)
}

func TestCLI_Link_RespectsTypeFlag(t *testing.T) {
	setupCLI(t)
	fromID := seedNode(t, "decision", "new decision")
	toID := seedNode(t, "decision", "old decision")

	_, err := runCLI(t, "link", fromID, toID, "--type", "SUPERSEDES")
	require.NoError(t, err)

	d := openTestDB(t)
	edges, err := d.GetEdgesFrom(fromID)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "SUPERSEDES", edges[0].Type)
}

// ---- unlink: does ctx unlink remove the edge? ----

func TestCLI_Unlink_RemovesEdge(t *testing.T) {
	setupCLI(t)
	fromID := seedNode(t, "fact", "from node")
	toID := seedNode(t, "fact", "to node")

	d := openTestDB(t)
	_, err := d.CreateEdge(fromID, toID, "RELATES_TO")
	require.NoError(t, err)

	_, err = runCLI(t, "unlink", fromID, toID)
	require.NoError(t, err)

	edges, err := d.GetEdgesFrom(fromID)
	require.NoError(t, err)
	assert.Empty(t, edges, "edge should be gone after unlink")
}

// ---- search: does ctx search find nodes by full-text content? ----

func TestCLI_Search_FindsByContent(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "Postgres uses MVCC for transaction isolation")
	seedNode(t, "fact", "SQLite uses WAL for crash safety")

	out, err := runCLI(t, "search", "Postgres")
	require.NoError(t, err)
	assert.Contains(t, out, "Postgres uses MVCC")
	assert.NotContains(t, out, "SQLite uses WAL")
}

// ---- query: does ctx query return matching nodes by structured filter? ----

func TestCLI_Query_ByType(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "a fact")
	seedNode(t, "decision", "a decision")

	out, err := runCLI(t, "query", "type:fact")
	require.NoError(t, err)
	assert.Contains(t, out, "a fact")
	assert.NotContains(t, out, "a decision")
}

func TestCLI_Query_ByTagConjunction(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "match both", "tier:pinned", "project:Book")
	seedNode(t, "fact", "wrong project", "tier:pinned", "project:other")
	seedNode(t, "fact", "wrong tier", "tier:reference", "project:Book")

	out, err := runCLI(t, "query", "tag:tier:pinned AND tag:project:Book")
	require.NoError(t, err)
	assert.Contains(t, out, "match both")
	assert.NotContains(t, out, "wrong project")
	assert.NotContains(t, out, "wrong tier")
}

// ---- compose: does ctx compose render a context blob containing the nodes? ----

func TestCLI_Compose_RendersPinnedNodes(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "pinned fact content", "tier:pinned")
	seedNode(t, "decision", "pinned decision content", "tier:pinned")

	out, err := runCLI(t, "compose", "--query", "tag:tier:pinned")
	require.NoError(t, err)
	assert.Contains(t, out, "pinned fact content")
	assert.Contains(t, out, "pinned decision content")
	assert.Contains(t, out, "Context:")
}

func TestCLI_Compose_Markdown(t *testing.T) {
	setupCLI(t)
	seedNode(t, "fact", "pinned fact content", "tier:pinned")

	format = "markdown"
	out, err := runCLI(t, "compose", "--query", "tag:tier:pinned")
	require.NoError(t, err)
	assert.Contains(t, out, "pinned fact content")
	assert.Contains(t, out, "## Pinned")
	assert.Contains(t, out, "<!-- ctx:end -->")
}

// ---- end-to-end: does the full add → list → delete cycle work through the CLI? ----

func TestCLI_AddListDeleteCycle(t *testing.T) {
	setupCLI(t)

	// Add
	_, err := runCLI(t, "add", "--type", "fact", "lifecycle test content")
	require.NoError(t, err)

	// List finds it
	out, err := runCLI(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "lifecycle test content")

	// Grab the ID from the DB
	d := openTestDB(t)
	nodes, err := d.ListNodes(db.ListOptions{Type: "fact"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	id := nodes[0].ID

	// Delete
	_, err = runCLI(t, "delete", id)
	require.NoError(t, err)

	// List no longer finds it
	out, err = runCLI(t, "list")
	require.NoError(t, err)
	assert.NotContains(t, out, "lifecycle test content")
}
