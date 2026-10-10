// Package enrollment implements the unauthenticated half of trusted-device
// onboarding. The decision half remains behind the existing signed device
// authorization boundary in server.
package enrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	DefaultTTL = 5 * time.Minute
	MaxPending = 32
)

type Status string

const (
	Pending  Status = "pending"
	Approved Status = "approved"
	Denied   Status = "denied"
	Expired  Status = "expired"
)

type Request struct {
	ID              string     `json:"id"`
	DeviceID        string     `json:"device_id"`
	DeviceName      string     `json:"device_name"`
	Platform        string     `json:"platform"`
	Origin          string     `json:"origin"`
	DevicePublicKey string     `json:"device_public_key"`
	Number          string     `json:"number"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	Status          Status     `json:"status"`
	DecisionAt      *time.Time `json:"decision_at,omitempty"`
	ApprovedBy      string     `json:"approved_by,omitempty"`
	SecretHash      string     `json:"secret_hash,omitempty"`
}

type stored struct {
	Requests []Request `json:"requests"`
}

type Manager struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

func New(stateDir string) (*Manager, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("enrollment state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	return &Manager{path: filepath.Join(stateDir, "pending-enrollments.json"), now: time.Now}, nil
}

func (m *Manager) Create(deviceID, deviceName, platform, origin, publicKey string) (Request, string, error) {
	deviceID, deviceName, platform, origin = strings.TrimSpace(deviceID), strings.TrimSpace(deviceName), strings.TrimSpace(platform), strings.TrimSpace(origin)
	publicKey = strings.ToLower(strings.TrimSpace(publicKey))
	if deviceID == "" || len(deviceID) > 128 || len(deviceName) > 128 || len(platform) > 64 || len(origin) > 512 {
		return Request{}, "", errors.New("invalid enrollment request")
	}
	if key, err := hex.DecodeString(publicKey); err != nil || len(key) != ed25519.PublicKeySize {
		return Request{}, "", errors.New("invalid device public key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, err := m.loadLocked()
	if err != nil {
		return Request{}, "", err
	}
	now := m.now().UTC()
	requests, _ = prune(requests, now)
	active := 0
	for _, item := range requests {
		if item.Status == Pending {
			active++
		}
	}
	if active >= MaxPending {
		return Request{}, "", errors.New("too many pending enrollment requests")
	}
	for _, item := range requests {
		if item.Status == Pending && item.DeviceID == deviceID {
			return Request{}, "", errors.New("enrollment already pending")
		}
	}
	idBytes := make([]byte, 16)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return Request{}, "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return Request{}, "", err
	}
	numberBytes := make([]byte, 2)
	if _, err := rand.Read(numberBytes); err != nil {
		return Request{}, "", err
	}
	number := fmt.Sprintf("%03d", int(uint16(numberBytes[0])|uint16(numberBytes[1])<<8)%1000)
	request := Request{ID: hex.EncodeToString(idBytes), DeviceID: deviceID, DeviceName: deviceName, Platform: platform, Origin: origin, DevicePublicKey: publicKey, Number: number, CreatedAt: now, ExpiresAt: now.Add(DefaultTTL), Status: Pending, SecretHash: hash(secretBytes)}
	requests = append(requests, request)
	if err := m.saveLocked(requests); err != nil {
		return Request{}, "", err
	}
	public := request
	public.SecretHash = ""
	return public, hex.EncodeToString(secretBytes), nil
}

// Pending returns only actionable requests, oldest first, and persists expiry
// and retention cleanup without exposing the requesters' status capabilities.
func (m *Manager) Pending() ([]Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, err := m.loadLocked()
	if err != nil {
		return nil, err
	}
	pruned, changed := prune(requests, m.now().UTC())
	if changed {
		if err := m.saveLocked(pruned); err != nil {
			return nil, err
		}
	}
	pending := make([]Request, 0, len(pruned))
	for _, request := range pruned {
		if request.Status != Pending {
			continue
		}
		request.SecretHash = ""
		pending = append(pending, request)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt.Before(pending[j].CreatedAt) })
	return pending, nil
}

func (m *Manager) Status(id, secret string) (Request, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, err := m.loadLocked()
	if err != nil {
		return Request{}, false
	}
	now := m.now().UTC()
	requests, _ = prune(requests, now)
	for _, request := range requests {
		if request.ID == strings.TrimSpace(id) && equalHash(request.SecretHash, secret) {
			if request.Status == Pending && now.After(request.ExpiresAt) {
				request.Status = Expired
			}
			request.SecretHash = ""
			return request, true
		}
	}
	return Request{}, false
}

// Decide atomically records an approver's choice. The caller performs device
// enrollment through the auth manager, the sole trusted-device store.
func (m *Manager) Decide(id, number, approver string, approve bool) (Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	requests, err := m.loadLocked()
	if err != nil {
		return Request{}, err
	}
	now := m.now().UTC()
	for i := range requests {
		if requests[i].ID != strings.TrimSpace(id) {
			continue
		}
		if requests[i].Status != Pending {
			return Request{}, errors.New("enrollment is no longer pending")
		}
		if !now.Before(requests[i].ExpiresAt) {
			requests[i].Status = Expired
			_ = m.saveLocked(requests)
			return Request{}, errors.New("enrollment is no longer pending")
		}
		if strings.TrimSpace(number) != requests[i].Number {
			return Request{}, errors.New("verification number does not match")
		}
		requests[i].ApprovedBy = strings.TrimSpace(approver)
		requests[i].DecisionAt = &now
		if approve {
			requests[i].Status = Approved
		} else {
			requests[i].Status = Denied
		}
		if err := m.saveLocked(requests); err != nil {
			return Request{}, err
		}
		requests[i].SecretHash = ""
		return requests[i], nil
	}
	return Request{}, errors.New("enrollment request not found")
}

func prune(requests []Request, now time.Time) ([]Request, bool) {
	next := requests[:0]
	changed := false
	for _, item := range requests {
		if item.Status == Pending && !now.Before(item.ExpiresAt) {
			item.Status = Expired
			changed = true
		}
		switch item.Status {
		case Approved, Denied, Expired:
			terminalAt := item.ExpiresAt
			if item.DecisionAt != nil {
				terminalAt = *item.DecisionAt
			}
			if !now.Before(terminalAt.Add(time.Hour)) {
				changed = true
				continue
			}
		}
		next = append(next, item)
	}
	return next, changed
}

func hash(secret []byte) string { sum := sha256.Sum256(secret); return hex.EncodeToString(sum[:]) }
func equalHash(stored, secret string) bool {
	raw, err := hex.DecodeString(strings.TrimSpace(secret))
	return err == nil && hash(raw) == stored
}

func (m *Manager) loadLocked() ([]Request, error) {
	value, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Request{}, nil
	}
	if err != nil {
		return nil, err
	}
	var data stored
	if err := json.Unmarshal(value, &data); err != nil {
		return nil, err
	}
	return data.Requests, nil
}
func (m *Manager) saveLocked(requests []Request) error {
	data, err := json.MarshalIndent(stored{Requests: requests}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(m.path, append(data, '\n'), 0o600)
}
