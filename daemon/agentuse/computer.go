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
	// Agents are all the agents using the screen, longest first.
	Agents []ActiveAgent `json:"agents,omitempty"`
	// DisplayKept says the display is held on for agents, so the Mac does not
	// dim and lock between their steps.
	DisplayKept bool `json:"display_kept,omitempty"`
	// Permissions are each operating-system grant the screen needs and where
	// it stands, read afresh on every status (a Mac's Screen Recording and
	// Accessibility).
	Permissions []ComputerGrant `json:"permissions,omitempty"`
}

// ComputerGrant is one grant and its state: granted, off (in the list,
// switched off), missing (not in the list) or restart (given, in effect once
// Mewla starts again).
type ComputerGrant struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
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
	// Follow watches a grant the person is giving at this computer now (the
	// Mac app's permission panel): once given, Mewla applies it by itself.
	Follow func(permission string) error
	// Restart starts Mewla again so a given grant takes effect.
	Restart func() error
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

// FollowPermission is the app's "the person is giving this grant now".
func (s *Service) FollowPermission(permission string) (Status, error) {
	s.mu.Lock()
	hooks := s.computer
	s.mu.Unlock()
	if hooks == nil || hooks.Follow == nil {
		return Status{}, errors.New("this computer has no such permission to give")
	}
	if err := hooks.Follow(permission); err != nil {
		return Status{}, err
	}
	return s.Status(), nil
}

// ErrNothingToRestart: no grant waits for Mewla to start again, so the app's
// restart is refused (it is only ever offered for one that does).
var ErrNothingToRestart = errors.New("no permission waits for Mewla to restart")

// RestartForPermission starts Mewla again so a given grant takes effect; only
// while one does wait for it (state "restart").
func (s *Service) RestartForPermission() (Status, error) {
	s.mu.Lock()
	hooks := s.computer
	s.mu.Unlock()
	if hooks == nil || hooks.Restart == nil {
		return Status{}, errors.New("Mewla can't restart itself here")
	}
	waiting := false
	for _, grant := range hooks.Status().Permissions {
		waiting = waiting || grant.State == "restart"
	}
	if !waiting {
		return Status{}, ErrNothingToRestart
	}
	status := s.Status()
	return status, hooks.Restart()
}

// withLive adds the rows that are never cached: the computer's, the phone's and the iPhone's.
func (s *Service) withLive(status Status) Status {
	return s.withIPhone(s.withPhone(s.withComputer(status)))
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
		// The grants stand as they are with the switch off: the Mac app still shows them.
		computer = ComputerStatus{State: ComputerOff, Summary: "Agents can't use this computer. Turn it on to let them.", Permissions: computer.Permissions}
	}
	status.Computer = &computer
	return status
}

// AgentDisplayHooks put the browser's agent windows on the agent display (off
// the person's screen) when the computer has one. Hold makes it and returns
// where it is, in global points; Release lets go of one hold. Opened hears that
// a Worker's window is open there and where; Closed that it closed. Keys are
// the bridge's connection ids.
type AgentDisplayHooks struct {
	Hold    func(key string) (WindowBounds, error)
	Release func(key string)
	Opened  func(key, worker string, cell WindowBounds)
	Closed  func(key string)
}

// SetAgentDisplay lets the browser's agent windows open on the agent display.
func (s *Service) SetAgentDisplay(h AgentDisplayHooks) {
	b := s.Bridge()
	b.Display = func(key, worker string) (WindowBounds, func(), error) {
		bounds, err := h.Hold("browser:" + key)
		if err != nil {
			return WindowBounds{}, nil, err
		}
		return bounds, func() {
			if h.Closed != nil {
				h.Closed(key)
			}
			h.Release("browser:" + key)
		}, nil
	}
	b.Placed = h.Opened
}

// SetWorkerNames lets the tab group of a Worker's browser tabs carry its name.
func (s *Service) SetWorkerNames(name func(worker string) string) {
	s.Bridge().WorkerName = name
}
