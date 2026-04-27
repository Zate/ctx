package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type remoteConfig struct {
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage remote server connection",
}

var remoteSetCmd = &cobra.Command{
	Use:   "set <url>",
	Short: "Set the remote server URL",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemoteSet,
}

var remoteShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current remote server URL",
	RunE:  runRemoteShow,
}

var remoteRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove the remote server configuration",
	RunE:  runRemoteRemove,
}

func init() {
	remoteCmd.AddCommand(remoteSetCmd)
	remoteCmd.AddCommand(remoteShowCmd)
	remoteCmd.AddCommand(remoteRemoveCmd)
	register(remoteCmd)
}

func runRemoteSet(cmd *cobra.Command, args []string) error {
	url := strings.TrimRight(args[0], "/")

	// Validate connectivity
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url + "/health")
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w", url, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server at %s returned status %d", url, resp.StatusCode)
	}

	cfg := remoteConfig{
		URL:       url,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	path, err := remoteConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	fmt.Printf("Remote set to %s\n", url)
	return nil
}

func runRemoteShow(cmd *cobra.Command, args []string) error {
	cfg, err := loadRemoteConfig()
	if err != nil {
		fmt.Println("No remote configured. Use 'ctx remote set <url>' to configure.")
		return nil
	}
	fmt.Printf("URL: %s\nSet: %s\n", cfg.URL, cfg.UpdatedAt)
	return nil
}

func runRemoteRemove(cmd *cobra.Command, args []string) error {
	path, err := remoteConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Remote configuration removed.")
	return nil
}

func remoteConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ctx", "remote.json"), nil
}

func loadRemoteConfig() (*remoteConfig, error) {
	path, err := remoteConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg remoteConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
