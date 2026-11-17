package connections

import (
	"time"
)

// SignInLost is a plugin account whose service refused to renew its sign-in.
// Chat shows it once, with the one thing that fixes it.
type SignInLost struct {
	AccountID string
	At        time.Time
	Title     string
	Body      string
}

// SetSignInLost receives each account whose refresh the service refused, once
// per change to authorization_required. It runs with the manager's lock held
// and must not call back into the manager.
func (m *Manager) SetSignInLost(fn func(SignInLost)) {
	m.mu.Lock()
	m.signInLost = fn
	m.mu.Unlock()
}

func (m *Manager) noteSignInLost(r *record, code string) {
	if m.signInLost == nil {
		return
	}
	m.signInLost(signInLostNotice(r, code, time.Now().UTC()))
}

func signInLostNotice(r *record, code string, at time.Time) SignInLost {
	service := r.Account.Integration
	if e := lookup(service); e != nil {
		service = e.Name
	}
	account := r.Account.Name
	if account == "" {
		account = service
	}
	n := SignInLost{AccountID: r.Account.ID, At: at, Title: service + " sign-in ended", Body: service + " stopped accepting “" + account + "”. Reconnect it in Extensions."}
	if clientRefused(code) {
		// Signing in again works only until the next renewal: the service
		// refuses Mewla's client, not the person's sign-in.
		n.Title = service + " sign-in can't renew"
		// The fix is Mewla's to ship, never a setting in the service: say
		// so, so nobody goes looking for one.
		n.Body = service + " refused to renew “" + account + "” for Mewla's app (" + code + "). This is Mewla's to fix, not a setting in " + service + ": update Mewla, then Reconnect it in Extensions if it still says so."
	}
	return n
}
