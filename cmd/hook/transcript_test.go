package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadAssistantResponsesFromOffset_StringContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []map[string]any{
		{
			"type": "assistant",
			"message": map[string]any{
				"content": "Hello from string content",
			},
		},
	}
	writeTranscript(t, path, lines)

	resp, _, err := readAssistantResponsesFromOffset(path, 0)
	require.NoError(t, err)
	assert.Equal(t, "Hello from string content", resp)
}

func TestReadAssistantResponsesFromOffset_ArrayContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []map[string]any{
		{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "First block of text.",
					},
					map[string]any{
						"type": "text",
						"text": `<ctx:remember type="fact" tags="tier:reference">A test fact.</ctx:remember>`,
					},
				},
			},
		},
	}
	writeTranscript(t, path, lines)

	resp, _, err := readAssistantResponsesFromOffset(path, 0)
	require.NoError(t, err)
	assert.Contains(t, resp, "First block of text.")
	assert.Contains(t, resp, "ctx:remember")
	assert.Contains(t, resp, "A test fact.")
}

func TestReadAssistantResponsesFromOffset_MixedBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []map[string]any{
		{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Some text",
					},
					map[string]any{
						"type": "tool_use",
						"name": "Read",
					},
					map[string]any{
						"type": "text",
						"text": "More text after tool use",
					},
				},
			},
		},
	}
	writeTranscript(t, path, lines)

	resp, _, err := readAssistantResponsesFromOffset(path, 0)
	require.NoError(t, err)
	assert.Contains(t, resp, "Some text")
	assert.Contains(t, resp, "More text after tool use")
	assert.NotContains(t, resp, "tool_use")
}

func TestReadAssistantResponsesFromOffset_SkipsNonAssistant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []map[string]any{
		{
			"type": "user",
			"message": map[string]any{
				"content": "User message",
			},
		},
		{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Assistant reply",
					},
				},
			},
		},
		{
			"type":     "file-history-snapshot",
			"snapshot": map[string]any{},
		},
	}
	writeTranscript(t, path, lines)

	resp, _, err := readAssistantResponsesFromOffset(path, 0)
	require.NoError(t, err)
	assert.Equal(t, "Assistant reply", resp)
}

func TestReadAssistantResponsesFromOffset_Offset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []map[string]any{
		{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "First response"},
				},
			},
		},
		{
			"type": "assistant",
			"message": map[string]any{
				"content": []any{
					map[string]any{"type": "text", "text": "Second response"},
				},
			},
		},
	}
	writeTranscript(t, path, lines)

	// Read first, get offset
	resp1, offset, err := readAssistantResponsesFromOffset(path, 0)
	require.NoError(t, err)
	assert.Contains(t, resp1, "First response")
	assert.Contains(t, resp1, "Second response")

	// Read from offset — should get nothing since we read everything
	resp2, _, err := readAssistantResponsesFromOffset(path, offset)
	require.NoError(t, err)
	assert.Empty(t, resp2)
}

func writeTranscript(t *testing.T, path string, entries []map[string]any) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		require.NoError(t, err)
		_, _ = f.Write(data)
		_, _ = f.Write([]byte("\n"))
	}
}
