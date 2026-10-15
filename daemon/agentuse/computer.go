package agentuse

import (
	"errors"
	"time"
)

// Computer states, from the user's side.
const (
	ComputerReady       = "ready"
	ComputerOff         = "off"
	ComputerUnsupported = "unsupported"
	ComputerScreenOff   = "screen_off"
	ComputerPermission  = "permission"
	ComputerLocked      = "locked"
	ComputerUnavailable = "unavailable"
)

// FixOpenSettings opens this computer's settings page for Permission.
const FixOpenSettings = "open_settings"

// ComputerStatus is whether agents can see and use this computer's screen.
type ComputerStatus struct {
	State   string `json:"state"`
	Summary string `json:"summary"`
	Fix     string `json:"fix,omitempty"`
	// Permission is the missing operating-system permission Fix opens.
	Permission string `json:"permission,omitempty"`
	// Active is the agent using the screen now.
	Active *ActiveAgent `json:"active,omitempty"`
}

type ActiveAgent struct {
	Worker string    `json:"worker"`
	Name   string    `json:"name,omitempty"`
	Since  time.Time `json:"since"`
}

// ComputerHooks connect the screen, which the daemon owns.
type ComputerHooks struct {
	Status       func() ComputerStatus
	OpenSettings func(permission string) error
	Stop         func()
}

// SetComputer lets agents use the screen. Without it (the mewla command, a
// computer without Screen) status reports no computer row.
func (s *Service) SetComputer(hooks ComputerHooks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.computer = &hooks
}

// Enabled reads the switch alone, for checks on every agent action.
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	return !s.settings.Off
}

// ComputerReady says whether to give a new agent the screen. A locked screen
// still counts: it is usable again once unlocked, and the tool says why not
// until then.
func (s *Service) ComputerReady() bool {
	if !s.Enabled() {
		return false
	}
	s.mu.Lock()
	hooks := s.computer
	s.mu.Unlock()
	if hooks == nil {
		return false
	}
	state := hooks.Status().State
	return state == ComputerReady || state == ComputerLocked
}

// StopAgent ends the agent using the screen, from the app.
func (s *Service) StopAgent() (Status, error) {
	s.mu.Lock()
	hooks := s.computer
	s.mu.Unlock()
	if hooks == nil {
		return Status{}, errors.New("no agent can use this computer's screen")
	}
	hooks.Stop()
	return s.Status(), nil
}

// withComputer adds the computer row to a status; it is never cached, so the
// app sees an agent start or stop at once.
func (s *Service) withComputer(status Status) Status {
	s.mu.Lock()
	hooks := s.computer
	s.mu.Unlock()
	if hooks == nil {
		return status
	}
	computer := hooks.Status()
	if !status.Enabled {
		computer = ComputerStatus{State: ComputerOff, Summary: "Agents can't use this computer. Turn it on to let them."}
	}
	status.Computer = &computer
	return status
}
