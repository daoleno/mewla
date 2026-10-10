package watcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// ConfigureResourceFiles installs optional persistent thresholds and the last
// notified state. Neither file contains process environments or credentials.
func (s *ResourceSampler) ConfigureResourceFiles(stateDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configPath = filepath.Join(stateDir, "resource-telemetry.json")
	s.statePath = filepath.Join(stateDir, "run", "resource-pressure.json")
	raw, err := os.ReadFile(s.statePath)
	if err == nil {
		var state resourcePressureState
		if json.Unmarshal(raw, &state) == nil && (state.State == "normal" || state.State == "elevated" || state.State == "critical") {
			s.machine.State = state.State
			s.pending = state.Pending
			if state.LastNotification != nil {
				s.machine.lastNotification = state.LastNotification
			}
		}
	}
}

type resourcePressureState struct {
	Pending          []ResourcePressureEvent `json:"pending,omitempty"`
	State            string                  `json:"state"`
	LastNotification map[string]time.Time    `json:"last_notification"`
}

func (s *ResourceSampler) reloadResourceConfig() error {
	if s.configPath == "" {
		return nil
	}
	info, err := os.Stat(s.configPath)
	if os.IsNotExist(err) {
		if !s.configModified.IsZero() {
			s.machine.Config = ResourceThresholdsFromEnv()
			s.configModified = time.Time{}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.ModTime() == s.configModified {
		return nil
	}
	if info.Size() > 16384 {
		return fmt.Errorf("resource threshold configuration exceeds 16 KiB")
	}
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		return err
	}
	c := ResourceThresholdsFromEnv()
	if err = json.Unmarshal(raw, &c); err != nil {
		return err
	}
	if !c.Valid() {
		return fmt.Errorf("invalid resource thresholds")
	}
	s.machine.Config = c
	s.machine.candidate = ""
	s.configModified = info.ModTime()
	return nil
}
func (s *ResourceSampler) saveResourceState() error {
	if s.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(resourcePressureState{State: s.machine.State, LastNotification: s.machine.lastNotification, Pending: s.pending})
	if err != nil {
		return err
	}
	return atomicfile.Write(s.statePath, raw, 0o600)
}

func (s *ResourceSampler) PendingResourceEvents() []ResourcePressureEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ResourcePressureEvent{}, s.pending...)
}
func (s *ResourceSampler) AcknowledgeResourceEvent(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, event := range s.pending {
		if event.SampledAt.Equal(at) {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			break
		}
	}
	return s.saveResourceState()
}
