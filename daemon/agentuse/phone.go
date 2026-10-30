package agentuse

import "github.com/daoleno/mewla/daemon/phone"

// PhoneStatus is Phone use as the app shows it: the switch, and the phone behind it.
type PhoneStatus struct {
	// Enabled is the person's switch (Settings → Labs). It is off until they turn it on.
	Enabled bool `json:"enabled"`
	phone.Status
}

// SetPhone lets the status report the phone, which the daemon holds the connection to. Without
// it (the mewla command) the status has no phone row.
func (s *Service) SetPhone(status func() phone.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phone = status
}

// PhoneEnabled reads the Phone use switch alone, for checks on every agent action.
func (s *Service) PhoneEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	return s.settings.Phone
}

// PhoneReady says whether to give a new Worker the phone tools: whenever Phone use is on. A
// phone that is offline does not change that; the tool says so when a Worker uses it.
func (s *Service) PhoneReady() bool { return s.PhoneEnabled() }

// SetPhoneEnabled is the app's switch for Phone use.
func (s *Service) SetPhoneEnabled(enabled bool) (Status, error) {
	s.mu.Lock()
	_ = s.loadLocked()
	next := s.settings
	next.Phone = enabled
	err := s.saveLocked(next)
	s.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	return s.Refresh(), nil
}

// Phone is the phone's row of the status, or nil without a phone manager.
func (s *Service) Phone() *PhoneStatus {
	s.mu.Lock()
	status := s.phone
	s.mu.Unlock()
	if status == nil {
		return nil
	}
	row := PhoneStatus{Enabled: s.PhoneEnabled(), Status: status()}
	if row.State == "" {
		row.State = phone.StateOffline
	}
	return &row
}

// withPhone adds the phone row to a status; like the computer's, it is never cached.
func (s *Service) withPhone(status Status) Status {
	status.Phone = s.Phone()
	return status
}
