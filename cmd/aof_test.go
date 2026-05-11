package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
)

// executeAOF runs a command with --agent-out and returns stdout.
// It redirects os.Stdout via a pipe to capture AOF output written directly.
func executeAOF(t *testing.T, args ...string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	agentOut = true
	rootCmd.SetArgs(args)
	execErr := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	agentOut = false

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	require.NoError(t, execErr)
	resetCommandFlags()
	return buf.String()
}

// TestAOF_Add verifies that ctx add --agent-out emits ok node AOF.
func TestAOF_Add(t *testing.T) {
	setupCLI(t)
	out := executeAOF(t, "add", "test memory node", "--type", "fact")
	assert.Contains(t, out, "ok node status=created")
	assert.Contains(t, out, "id ")
	assert.Contains(t, out, "type fact")
	assert.Contains(t, out, "summary ")
}

// TestAOF_List verifies that ctx list --agent-out emits ok nodes AOF.
func TestAOF_List(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "listed node"})
	require.NoError(t, err)

	out := executeAOF(t, "list")
	assert.Contains(t, out, "ok nodes count=")
	assert.Contains(t, out, "@ id type tags summary")
	assert.Contains(t, out, "- ")
}

// TestAOF_List_Empty verifies that ctx list --agent-out with no nodes emits ok nodes count=0.
func TestAOF_List_Empty(t *testing.T) {
	setupCLI(t)
	out := executeAOF(t, "list")
	assert.Contains(t, out, "ok nodes count=0")
	assert.NotContains(t, out, "@ id type")
}

// TestAOF_Show verifies that ctx show --agent-out emits ok node AOF.
func TestAOF_Show(t *testing.T) {
	setupCLI(t)
	// Use db directly to create a node so we have the ID without parsing output
	d := openTestDB(t)
	node, err := d.CreateNode(db.CreateNodeInput{Type: "decision", Content: "show me this node"})
	require.NoError(t, err)

	out := executeAOF(t, "show", node.ID)
	assert.Contains(t, out, "ok node status=ok")
	assert.Contains(t, out, "id "+node.ID)
	assert.Contains(t, out, "type decision")
}

// TestAOF_Search verifies that ctx search --agent-out emits ok nodes AOF.
func TestAOF_Search(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "searchable content here"})
	require.NoError(t, err)

	out := executeAOF(t, "search", "searchable")
	assert.Contains(t, out, "ok nodes count=")
	assert.Contains(t, out, "@ id type tags summary")
}

// TestAOF_Query verifies that ctx query --agent-out emits ok nodes AOF.
func TestAOF_Query(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "queryable fact node"})
	require.NoError(t, err)

	out := executeAOF(t, "query", "type:fact")
	assert.Contains(t, out, "ok nodes count=")
	assert.Contains(t, out, "@ id type tags summary")
}

// TestAOF_Delete verifies that ctx delete --agent-out emits ok deleted AOF.
func TestAOF_Delete(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	node, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "to be deleted"})
	require.NoError(t, err)

	out := executeAOF(t, "delete", node.ID)
	assert.Contains(t, out, "ok deleted")
	assert.Contains(t, out, "id="+node.ID)
}

// TestAOF_Tag verifies that ctx tag --agent-out emits ok tagged AOF.
func TestAOF_Tag(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	node, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "tag me"})
	require.NoError(t, err)

	out := executeAOF(t, "tag", node.ID, "tier:working")
	assert.Contains(t, out, "ok tagged")
	assert.Contains(t, out, "id="+node.ID)
	assert.Contains(t, out, "tags=tier:working")
}

// TestAOF_Link verifies that ctx link --agent-out emits ok edge AOF.
func TestAOF_Link(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	n1, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "node one"})
	require.NoError(t, err)
	n2, err := d.CreateNode(db.CreateNodeInput{Type: "decision", Content: "node two"})
	require.NoError(t, err)

	out := executeAOF(t, "link", n1.ID, n2.ID, "--type", "RELATES_TO")
	assert.Contains(t, out, "ok edge")
	assert.Contains(t, out, "from="+n1.ID)
	assert.Contains(t, out, "to="+n2.ID)
	assert.Contains(t, out, "type=RELATES_TO")
}

// TestAOF_Status verifies that ctx status --agent-out emits ok status AOF.
func TestAOF_Status(t *testing.T) {
	setupCLI(t)
	out := executeAOF(t, "status")
	assert.Contains(t, out, "ok status")
	assert.Contains(t, out, "nodes=")
	assert.Contains(t, out, "tokens=")
}

// TestAOF_Update verifies that ctx update --agent-out emits ok node updated AOF.
func TestAOF_Update(t *testing.T) {
	setupCLI(t)
	d := openTestDB(t)
	node, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "original content"})
	require.NoError(t, err)

	out := executeAOF(t, "update", node.ID, "--content", "updated content")
	assert.Contains(t, out, "ok node status=updated")
	assert.Contains(t, out, "id "+node.ID)
}
