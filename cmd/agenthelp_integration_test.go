//go:build integration

package cmd

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The --agent-help short-circuit lives in Execute() and bypasses the
// rootCmd.SetArgs path the in-process tests use, so this test must build
// and exec the real binary.
func TestCLI_Accessed_AgentHelp(t *testing.T) {
	setupCLI(t)
	bin := filepath.Join(t.TempDir(), "ctx")
	build := exec.Command("go", "build", "-o", bin, "github.com/zate/ctx")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "build failed: %s", out)

	cmd := exec.Command(bin, "accessed", "--agent-help")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "accessed --agent-help failed: %s", out)

	got := string(out)
	assert.Contains(t, got, "accessed")
	assert.Contains(t, got, "--node")
	assert.Contains(t, got, "--type")
	assert.Contains(t, got, "--all-agents")
	assert.Contains(t, got, "hook_inject")
	assert.Contains(t, got, "explicit_query")
}
