package agentuse

import (
	"errors"
	"time"
)

// iPhone use states, from the user's side.
const (
	IPhoneReady       = "ready"
	IPhoneOff         = "off"
	IPhoneUnsupported = "unsupported"
	// IPhoneBlocked is a Mac that can't let agents use its screen now: the
	// computer's switch, a permission or a lock; Summary says which.
	IPhoneBlocked = "blocked"
)

// IPhoneStatus is iPhone use as the app shows it: the switch, and the Mac's
// iPhone Mirroring behind it.
type IPhoneStatus struct {
	// Enabled is the person's switch (Settings → Labs). It is off until they turn it on.
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
	Summary string `json:"summary"`
	// Fix is the one action that resolves State, when Mewla can do it (open_settings).
	Fix        string `json:"fix,omitempty"`
	Permission string `json:"permission,omitempty"`
	// Running is whether iPhone Mirroring is open on the Mac.
	Running bool `json:"running,omitempty"`
	// Active is the agent using the iPhone now.
	Active *ActiveAgent `json:"active,omitempty"`
	// Picture is a base64 JPEG of the iPhone as the agent last read it, and
	// PictureAt when: sent only when asked for (/iphone?picture=1), to watch
	// the agent without another capture of the iPhone.
	Picture   string     `json:"picture,omitempty"`
	PictureAt *time.Time `json:"picture_at,omitempty"`
}

// IPhoneHooks connect iPhone use, which the daemon owns.
type IPhoneHooks struct {
	// Status is the state with the switch on.
	Status func() IPhoneStatus
	// Stop is the person's Stop: the agent using the iPhone ends and is refused after.
	Stop func()
	// Release ends every agent's use, as the switch going off does.
	Release func()
	// Picture is the latest picture an agent read, for watching it.
	Picture func() ([]byte, time.Time)
	// HandoffDone is the person saying they did what an agent asked; false
	// when no agent was waiting.
	HandoffDone func() bool
}

// SetIPhone lets the status report iPhone use. Without it (the mewla
// command, a computer that isn't a Mac) the status has no iPhone row.
func (s *Service) SetIPhone(hooks IPhoneHooks) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iphone = &hooks
}

// IPhoneEnabled reads the iPhone use switch alone, for checks on every agent action.
func (s *Service) IPhoneEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	return s.settings.IPhone
}

// IPhoneReady says whether to give a new Worker the iPhone tools: the switch
// is on and this computer can drive an iPhone. A Mac that is locked or a
// mirror that isn't open doesn't change that; the tool says so when used.
func (s *Service) IPhoneReady() bool {
	if !s.IPhoneEnabled() {
		return false
	}
	s.mu.Lock()
	hooks := s.iphone
	s.mu.Unlock()
	return hooks != nil && hooks.Status().State != IPhoneUnsupported
}

// SetIPhoneEnabled is the app's switch for iPhone use. Off ends the agent using it.
func (s *Service) SetIPhoneEnabled(enabled bool) (Status, error) {
	s.mu.Lock()
	_ = s.loadLocked()
	next := s.settings
	next.IPhone = enabled
	err := s.saveLocked(next)
	hooks := s.iphone
	s.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	if !enabled && hooks != nil {
		if hooks.Release != nil {
			hooks.Release()
		} else {
			hooks.Stop()
		}
	}
	return s.Refresh(), nil
}

// IPhone is the iPhone's row of the status, or nil without iPhone use here.
func (s *Service) IPhone() *IPhoneStatus {
	s.mu.Lock()
	hooks := s.iphone
	s.mu.Unlock()
	if hooks == nil {
		return nil
	}
	row := hooks.Status()
	row.Enabled = s.IPhoneEnabled()
	if !row.Enabled && row.State != IPhoneUnsupported {
		row = IPhoneStatus{State: IPhoneOff, Summary: "iPhone use is off. Turn it on in Settings → Labs to let agents use your iPhone through iPhone Mirroring on this Mac."}
	}
	return &row
}

// StopIPhone ends the agent using the iPhone, from the app.
func (s *Service) StopIPhone() error {
	s.mu.Lock()
	hooks := s.iphone
	s.mu.Unlock()
	if hooks == nil {
		return errors.New("no agent can use an iPhone from this computer")
	}
	hooks.Stop()
	return nil
}

// IPhoneHandoffDone tells the agent waiting on the person that they are done.
func (s *Service) IPhoneHandoffDone() bool {
	s.mu.Lock()
	hooks := s.iphone
	s.mu.Unlock()
	return hooks != nil && hooks.HandoffDone != nil && hooks.HandoffDone()
}

// withIPhone adds the iPhone row to a status; like the computer's, it is never cached.
func (s *Service) withIPhone(status Status) Status {
	status.IPhone = s.IPhone()
	return status
}

// IPhonePicture is the latest picture of the iPhone an agent read, with when.
func (s *Service) IPhonePicture() ([]byte, time.Time) {
	s.mu.Lock()
	hooks := s.iphone
	s.mu.Unlock()
	if hooks == nil || hooks.Picture == nil {
		return nil, time.Time{}
	}
	return hooks.Picture()
}
