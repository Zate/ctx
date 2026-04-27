package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// DeviceFlowState tracks an in-progress device authorization flow.
type DeviceFlowState struct {
	DeviceCode string
	UserCode   string
	ExpiresAt  time.Time
	Approved   bool
	Denied     bool
	DeviceName string

	// Set after approval
	Token        string
	RefreshToken string
	DeviceID     string
}

// DeviceFlowStore manages in-flight device authorization flows in memory.
// Flows are short-lived (expires in FlowTTL) so persistence isn't needed.
type DeviceFlowStore struct {
	mu    sync.Mutex
	flows map[string]*DeviceFlowState // keyed by device_code
	byUC  map[string]string           // user_code -> device_code
}

const (
	FlowTTL       = 10 * time.Minute
	TokenExpiry   = 30 * 24 * time.Hour // 30 days
	RefreshExpiry = 90 * 24 * time.Hour // 90 days
)

// NewDeviceFlowStore creates a new in-memory flow store.
func NewDeviceFlowStore() *DeviceFlowStore {
	return &DeviceFlowStore{
		flows: make(map[string]*DeviceFlowState),
		byUC:  make(map[string]string),
	}
}

// Initiate creates a new device authorization flow.
func (s *DeviceFlowStore) Initiate(deviceName string) *DeviceFlowState {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := &DeviceFlowState{
		DeviceCode: generateToken(32),
		UserCode:   generateUserCode(),
		ExpiresAt:  time.Now().Add(FlowTTL),
		DeviceName: deviceName,
	}

	s.flows[state.DeviceCode] = state
	s.byUC[state.UserCode] = state.DeviceCode

	return state
}

// GetByDeviceCode retrieves a flow by device code.
func (s *DeviceFlowStore) GetByDeviceCode(deviceCode string) *DeviceFlowState {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.flows[deviceCode]
	if state == nil || time.Now().After(state.ExpiresAt) {
		return nil
	}
	return state
}

// GetByUserCode retrieves a flow by user code.
func (s *DeviceFlowStore) GetByUserCode(userCode string) *DeviceFlowState {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceCode, ok := s.byUC[userCode]
	if !ok {
		return nil
	}
	state := s.flows[deviceCode]
	if state == nil || time.Now().After(state.ExpiresAt) {
		return nil
	}
	return state
}

// Approve approves a device flow, setting the token and refresh token.
func (s *DeviceFlowStore) Approve(userCode, deviceID, token, refreshToken string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceCode, ok := s.byUC[userCode]
	if !ok {
		return false
	}
	state := s.flows[deviceCode]
	if state == nil || time.Now().After(state.ExpiresAt) {
		return false
	}

	state.Approved = true
	state.DeviceID = deviceID
	state.Token = token
	state.RefreshToken = refreshToken
	return true
}

// Deny denies a device flow.
func (s *DeviceFlowStore) Deny(userCode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	deviceCode, ok := s.byUC[userCode]
	if !ok {
		return false
	}
	state := s.flows[deviceCode]
	if state == nil {
		return false
	}

	state.Denied = true
	return true
}

// Cleanup removes expired flows.
func (s *DeviceFlowStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for dc, state := range s.flows {
		if now.After(state.ExpiresAt) || state.Approved || state.Denied {
			delete(s.byUC, state.UserCode)
			delete(s.flows, dc)
		}
	}
}

// HashToken creates a SHA-256 hash of a token for storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// generateToken generates a cryptographically random hex token.
func generateToken(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateToken generates a new random token (exported for use by server).
func GenerateToken() string {
	return generateToken(32)
}

// GenerateRefreshToken generates a new random refresh token.
func GenerateRefreshToken() string {
	return generateToken(48)
}

// generateUserCode generates a short, human-readable code like "ABCD-1234".
func generateUserCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	letters := "ABCDEFGHJKLMNPQRSTUVWXYZ" // no I, O to avoid confusion
	digits := "23456789"                  // no 0, 1 to avoid confusion

	return fmt.Sprintf("%c%c%c%c-%c%c%c%c",
		letters[b[0]%byte(len(letters))],
		letters[b[1]%byte(len(letters))],
		letters[b[2]%byte(len(letters))],
		letters[b[3]%byte(len(letters))],
		digits[b[0]>>4%byte(len(digits))],
		digits[b[1]>>4%byte(len(digits))],
		digits[b[2]>>4%byte(len(digits))],
		digits[b[3]>>4%byte(len(digits))],
	)
}
