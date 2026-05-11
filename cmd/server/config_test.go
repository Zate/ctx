package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthConfigRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &authConfig{
		Token:        "test-token",
		RefreshToken: "test-refresh",
		DeviceID:     "device-123",
		ServerURL:    "http://localhost:8377",
		UpdatedAt:    "2025-01-01T00:00:00Z",
	}

	err := saveAuthConfig(cfg)
	require.NoError(t, err)

	// Verify file was created
	path := filepath.Join(tmpDir, ".ctx", "auth.json")
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load it back
	loaded, err := loadAuthConfig()
	require.NoError(t, err)
	assert.Equal(t, cfg.Token, loaded.Token)
	assert.Equal(t, cfg.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, cfg.DeviceID, loaded.DeviceID)
	assert.Equal(t, cfg.ServerURL, loaded.ServerURL)
}

func TestAuthConfigLoad_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := loadAuthConfig()
	assert.Error(t, err)
}

func TestRemoteConfigRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := remoteConfig{
		URL:       "http://localhost:8377",
		UpdatedAt: "2025-01-01T00:00:00Z",
	}

	path := filepath.Join(tmpDir, ".ctx", "remote.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	data, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))

	loaded, err := loadRemoteConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8377", loaded.URL)
}

func TestRemoteConfigLoad_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := loadRemoteConfig()
	assert.Error(t, err)
}

func TestDetectProjectTag(t *testing.T) {
	// This test runs in the actual git repo, so it should detect "ctx"
	tag := detectProjectTag()
	assert.NotEmpty(t, tag)
	assert.NotEqual(t, "unknown", tag)
}

func TestOrNA(t *testing.T) {
	assert.Equal(t, "never", orNA(""))
	assert.Equal(t, "2025-01-01", orNA("2025-01-01"))
}
