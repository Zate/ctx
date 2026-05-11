package hook_test

// Hook executor must keep memory isolated from doc/content kinds:
//   - <ctx:remember> refuses kind=document / kind=content
//   - <ctx:recall> (via stored query) does not match non-memory nodes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/hook"
	"github.com/zate/ctx/testutil"
)

// TestRemember_RefusesNonMemoryKind checks that remember rejects a kind=document attribute.
func TestRemember_RefusesNonMemoryKind(t *testing.T) {
	d := testutil.SetupTestDB(t)

	cmds := []hook.CtxCommand{
		{
			Type:    "remember",
			Attrs:   map[string]string{"type": "fact", "kind": "document", "tags": "tier:pinned"},
			Content: "This should not be stored as a document.",
		},
	}
	errs := hook.ExecuteCommandsWithErrors(d, cmds)
	assert.Len(t, errs, 1, "remember with kind=document should return an error")

	// No node should have been created
	nodes, err := d.ListNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

// TestRemember_RefusesContentKind checks that remember rejects a kind=content attribute.
func TestRemember_RefusesContentKind(t *testing.T) {
	d := testutil.SetupTestDB(t)

	cmds := []hook.CtxCommand{
		{
			Type:    "remember",
			Attrs:   map[string]string{"type": "fact", "kind": "content"},
			Content: "This should not be stored as a content chunk.",
		},
	}
	errs := hook.ExecuteCommandsWithErrors(d, cmds)
	assert.Len(t, errs, 1, "remember with kind=content should return an error")

	nodes, err := d.ListNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, nodes)
}

// TestRemember_AcceptsMemoryKind checks that remember accepts explicit kind=memory.
func TestRemember_AcceptsMemoryKind(t *testing.T) {
	d := testutil.SetupTestDB(t)

	cmds := []hook.CtxCommand{
		{
			Type:    "remember",
			Attrs:   map[string]string{"type": "fact", "kind": "memory", "tags": "tier:pinned"},
			Content: "Explicit memory kind is fine.",
		},
	}
	errs := hook.ExecuteCommandsWithErrors(d, cmds)
	assert.Empty(t, errs, "remember with kind=memory should succeed")

	nodes, err := d.ListMemoryNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
}

// TestRemember_NoKindDefaultsToMemory checks that omitting kind still creates memory nodes.
func TestRemember_NoKindDefaultsToMemory(t *testing.T) {
	d := testutil.SetupTestDB(t)

	cmds := []hook.CtxCommand{
		{
			Type:    "remember",
			Attrs:   map[string]string{"type": "fact", "tags": "tier:working"},
			Content: "Default kind is memory.",
		},
	}
	errs := hook.ExecuteCommandsWithErrors(d, cmds)
	assert.Empty(t, errs)

	nodes, err := d.ListMemoryNodes(db.ListOptions{})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, db.NodeKindMemory, nodes[0].Kind)
}

// TestListMemoryNodes_ExposedOnStore verifies the Store interface method exists.
func TestListMemoryNodes_ExposedOnStore(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.CreateNode(db.CreateNodeInput{Type: "fact", Content: "hello"})
	require.NoError(t, err)

	nodes, err := d.ListMemoryNodes(db.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
}
