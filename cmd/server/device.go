package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var deviceCmd = &cobra.Command{
	Use:   "device",
	Short: "Manage registered devices on the remote server",
}

var deviceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered devices",
	RunE:  runDeviceList,
}

var deviceRevokeCmd = &cobra.Command{
	Use:   "revoke <device-id>",
	Short: "Revoke a device's access",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeviceRevoke,
}

func init() {
	deviceCmd.AddCommand(deviceListCmd)
	deviceCmd.AddCommand(deviceRevokeCmd)
	register(deviceCmd)
}

func runDeviceList(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	resp, err := authedRequest("GET", auth.ServerURL+"/api/devices", nil, auth.Token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	var devices []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		LastSeen  string `json:"last_seen"`
		LastIP    string `json:"last_ip"`
		Revoked   bool   `json:"revoked"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(body, &devices); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(devices) == 0 {
		fmt.Println("No devices registered.")
		return nil
	}

	for _, d := range devices {
		status := "active"
		if d.Revoked {
			status = "REVOKED"
		}
		current := ""
		if d.ID == auth.DeviceID {
			current = " (this device)"
		}
		fmt.Printf("%-26s  %-20s  %-8s  %s%s\n", d.ID, d.Name, status, d.LastSeen, current)
	}
	return nil
}

func runDeviceRevoke(cmd *cobra.Command, args []string) error {
	auth, err := loadAuthConfig()
	if err != nil {
		return fmt.Errorf("not authenticated. Run 'ctx auth' first")
	}

	deviceID := args[0]
	resp, err := authedRequest("POST", auth.ServerURL+"/api/devices/"+deviceID+"/revoke", nil, auth.Token)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	fmt.Printf("Device %s revoked.\n", deviceID)
	return nil
}

func authedRequest(method, url string, body any, token string) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}
