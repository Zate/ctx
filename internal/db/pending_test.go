package db_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/testutil"
)

func TestPending_SetAndGet(t *testing.T) {
	d := testutil.SetupTestDB(t)

	err := d.SetPending("recall_result", "some query output")
	require.NoError(t, err)

	val, err := d.GetPending("recall_result")
	require.NoError(t, err)
	assert.Equal(t, "some query output", val)
}

func TestPending_SetOverwrites(t *testing.T) {
	d := testutil.SetupTestDB(t)

	require.NoError(t, d.SetPending("key", "first"))
	require.NoError(t, d.SetPending("key", "second"))

	val, err := d.GetPending("key")
	require.NoError(t, err)
	assert.Equal(t, "second", val)
}

func TestPending_GetNotFound(t *testing.T) {
	d := testutil.SetupTestDB(t)

	_, err := d.GetPending("nonexistent")
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestPending_Delete(t *testing.T) {
	d := testutil.SetupTestDB(t)

	require.NoError(t, d.SetPending("key", "value"))
	require.NoError(t, d.DeletePending("key"))

	_, err := d.GetPending("key")
	assert.True(t, errors.Is(err, db.ErrNotFound))
}

func TestPending_DeleteNonexistent(t *testing.T) {
	d := testutil.SetupTestDB(t)

	// Should not error when deleting a key that doesn't exist
	err := d.DeletePending("nonexistent")
	assert.NoError(t, err)
}

func TestPending_MultipleKeys(t *testing.T) {
	d := testutil.SetupTestDB(t)

	require.NoError(t, d.SetPending("recall_result", "recall data"))
	require.NoError(t, d.SetPending("status_result", "status data"))
	require.NoError(t, d.SetPending("expand_ids", "id1,id2"))

	v1, err := d.GetPending("recall_result")
	require.NoError(t, err)
	assert.Equal(t, "recall data", v1)

	v2, err := d.GetPending("status_result")
	require.NoError(t, err)
	assert.Equal(t, "status data", v2)

	v3, err := d.GetPending("expand_ids")
	require.NoError(t, err)
	assert.Equal(t, "id1,id2", v3)
}
