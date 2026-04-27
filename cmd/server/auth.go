package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

type authConfig struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	DeviceID     string `json:"device_id"`
	ServerURL    string `json:"server_url"`
	UpdatedAt    string `json:"updated_at"`
}

var authDeviceName string

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with a remote ctx server using device flow",
	RunE:  runAuth,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored authentication credentials",
	RunE:  runAuthLogout,
}

func init() {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "ctx-device"
	}
	authCmd.Flags().StringVar(&authDeviceName, "device-name", hostname, "Name for this device")
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
	register(authCmd)
}

func runAuth(cmd *cobra.Command, args []string) error {
	remoteCfg, err := loadRemoteConfig()
	if err != nil {
		return fmt.Errorf("no remote configured. Run 'ctx remote set <url>' first")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1: Initiate device flow
	body, _ := json.Marshal(map[string]string{"device_name": authDeviceName})
	resp, err := client.Post(remoteCfg.URL+"/api/auth/device", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to initiate device flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s", string(respBody))
	}

	var initResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("\nTo authorize this device, visit:\n  %s\n\n", initResp.VerificationURI)
	fmt.Printf("Enter code: %s\n\n", initResp.UserCode)
	fmt.Println("Waiting for authorization...")

	// Try to open browser
	openBrowser(initResp.VerificationURI + "?user_code=" + initResp.UserCode)

	// Step 2: Poll for token
	interval := time.Duration(initResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(initResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		tokenBody, _ := json.Marshal(map[string]string{"device_code": initResp.DeviceCode})
		tokenResp, err := client.Post(remoteCfg.URL+"/api/auth/token", "application/json", bytes.NewReader(tokenBody))
		if err != nil {
			continue
		}

		respBytes, _ := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()

		if tokenResp.StatusCode == http.StatusAccepted {
			// Still pending
			continue
		}

		if tokenResp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("authorization denied")
		}

		if tokenResp.StatusCode == http.StatusOK {
			var tokenData struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				DeviceID     string `json:"device_id"`
			}
			if err := json.Unmarshal(respBytes, &tokenData); err != nil {
				return fmt.Errorf("failed to parse token response: %w", err)
			}

			cfg := authConfig{
				Token:        tokenData.AccessToken,
				RefreshToken: tokenData.RefreshToken,
				DeviceID:     tokenData.DeviceID,
				ServerURL:    remoteCfg.URL,
				UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
			}

			if err := saveAuthConfig(&cfg); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}

			fmt.Printf("\nAuthorized! Device ID: %s\n", tokenData.DeviceID)
			return nil
		}

		return fmt.Errorf("unexpected response: %s", string(respBytes))
	}

	return fmt.Errorf("authorization timed out")
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadAuthConfig()
	if err != nil {
		fmt.Println("Not authenticated. Run 'ctx auth' to authenticate.")
		return nil
	}
	fmt.Printf("Server:    %s\n", cfg.ServerURL)
	fmt.Printf("Device ID: %s\n", cfg.DeviceID)
	fmt.Printf("Updated:   %s\n", cfg.UpdatedAt)
	return nil
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	path, err := authConfigPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Credentials removed.")
	return nil
}

func authConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ctx", "auth.json"), nil
}

func loadAuthConfig() (*authConfig, error) {
	path, err := authConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg authConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveAuthConfig(cfg *authConfig) error {
	path, err := authConfigPath()
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
	return os.WriteFile(path, data, 0600)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
