package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	ctxsync "github.com/zate/ctx/internal/sync"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local database with remote server",
}

var syncStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync status — compare local and remote versions",
	RunE:  runSyncStatus,
}

var syncPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push local changes to the remote server",
	RunE:  runSyncPush,
}

var syncPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull remote changes to the local database",
	RunE:  runSyncPull,
}

var syncRegisterRepoCmd = &cobra.Command{
	Use:   "register-repo",
	Short: "Register current git repo with the remote server for project mapping",
	RunE:  runSyncRegisterRepo,
}

func init() {
	syncCmd.AddCommand(syncStatusCmd)
	syncCmd.AddCommand(syncPushCmd)
	syncCmd.AddCommand(syncPullCmd)
	syncCmd.AddCommand(syncRegisterRepoCmd)
	register(syncCmd)
}

func runSyncStatus(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	state, err := ctxsync.LoadSyncState(auth.ServerURL)
	if err != nil {
		return err
	}

	// Count local changes since last push
	changes, _, err := ctxsync.GetLocalChanges(store, state.LastPushVersion)
	if err != nil {
		return err
	}

	// Get server status
	resp, err := authedRequest("GET", auth.ServerURL+"/api/status", nil, auth.Token)
	if err != nil {
		return fmt.Errorf("cannot reach server: %w", err)
	}
	defer resp.Body.Close()

	var serverStatus map[string]any
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &serverStatus)

	fmt.Printf("Sync status:\n")
	fmt.Printf("  Server:           %s\n", auth.ServerURL)
	fmt.Printf("  Last push:        %s\n", orNA(state.LastPushAt))
	fmt.Printf("  Last pull:        %s\n", orNA(state.LastPullAt))
	fmt.Printf("  Local changes:    %d node(s) pending push\n", len(changes))
	fmt.Printf("  Server nodes:     %v\n", serverStatus["total_nodes"])
	return nil
}

func runSyncPush(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	state, err := ctxsync.LoadSyncState(auth.ServerURL)
	if err != nil {
		return err
	}

	changes, maxVersion, err := ctxsync.GetLocalChanges(store, state.LastPushVersion)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		fmt.Println("Nothing to push.")
		return nil
	}

	pushReq := ctxsync.PushRequest{
		DeviceID:    auth.DeviceID,
		SyncVersion: state.LastPushVersion,
		Changes:     changes,
	}

	body, _ := json.Marshal(pushReq)
	resp, err := authedRequest("POST", auth.ServerURL+"/api/sync/push", json.RawMessage(body), auth.Token)
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	var pushResp ctxsync.PushResponse
	if err := json.Unmarshal(respBody, &pushResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	state.LastPushVersion = maxVersion
	state.LastPushAt = time.Now().UTC().Format(time.RFC3339)
	if err := ctxsync.SaveSyncState(state); err != nil {
		return fmt.Errorf("failed to save sync state: %w", err)
	}

	fmt.Printf("Pushed %d node(s). Conflicts: %d. Server version: %d\n",
		pushResp.Accepted, pushResp.Conflicts, pushResp.SyncVersion)
	return nil
}

func runSyncPull(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	store, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	state, err := ctxsync.LoadSyncState(auth.ServerURL)
	if err != nil {
		return err
	}

	pullReq := ctxsync.PullRequest{
		DeviceID:    auth.DeviceID,
		SyncVersion: state.LastPullVersion,
	}

	body, _ := json.Marshal(pullReq)
	resp, err := authedRequest("POST", auth.ServerURL+"/api/sync/pull", json.RawMessage(body), auth.Token)
	if err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	var pullResp ctxsync.PullResponse
	if err := json.Unmarshal(respBody, &pullResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(pullResp.Changes) == 0 {
		fmt.Println("Already up to date.")
		return nil
	}

	applied, conflicts, err := ctxsync.ApplyRemoteChanges(store, pullResp.Changes)
	if err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	state.LastPullVersion = pullResp.SyncVersion
	state.LastPullAt = time.Now().UTC().Format(time.RFC3339)
	if err := ctxsync.SaveSyncState(state); err != nil {
		return fmt.Errorf("failed to save sync state: %w", err)
	}

	fmt.Printf("Pulled %d change(s). Applied: %d. Conflicts: %d (kept local).\n",
		len(pullResp.Changes), applied, conflicts)
	return nil
}

func runSyncRegisterRepo(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	normalizedURL, err := getNormalizedGitRemote()
	if err != nil {
		return fmt.Errorf("failed to detect git remote: %w", err)
	}

	projectTag := detectProjectTag()

	reqBody := map[string]string{
		"normalized_url": normalizedURL,
		"project_tag":    projectTag,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := authedRequest("POST", auth.ServerURL+"/api/repo-mappings", json.RawMessage(body), auth.Token)
	if err != nil {
		return fmt.Errorf("failed to register repo: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(respBody))
	}

	fmt.Printf("Registered repo: %s → project:%s\n", normalizedURL, projectTag)
	return nil
}

// authedRequest is defined in device.go — this is the helper used by sync commands too.
// It sends an HTTP request with Bearer token authentication.

func orNA(s string) string {
	if s == "" {
		return "never"
	}
	return s
}

func getNormalizedGitRemote() (string, error) {
	// Try to read git remote URL
	cmd := newGitCmd("remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository or no origin remote")
	}

	url := string(bytes.TrimSpace(out))
	return ctxsync.NormalizeGitURL(url), nil
}

func detectProjectTag() string {
	// Use the current directory name as the project tag
	cmd := newGitCmd("rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	path := string(bytes.TrimSpace(out))
	// Get the last component of the path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func newGitCmd(args ...string) *exec.Cmd {
	return exec.Command("git", args...)
}
