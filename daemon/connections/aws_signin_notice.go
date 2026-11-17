package connections

import (
	"time"
)

// awsSignInWarnAhead is how long before an AWS sign-in's end Chat warns.
const awsSignInWarnAhead = 24 * time.Hour

// awsSignInCheckEvery is how often the daemon checks AWS sign-ins on its own,
// so a sign-in that ends while nobody has Extensions open still reaches Chat.
const awsSignInCheckEvery = 30 * time.Minute

// noteAWSSignIns tells Chat, once each, about an enabled AWS profile whose
// sign-in ends within a day or has ended. It runs with the manager's lock held
// and reports whether it changed a record.
func (m *Manager) noteAWSSignIns(now time.Time) bool {
	changed := false
	for _, r := range m.records {
		if r.Account.Integration != "aws" || r.CLI == nil || !r.Account.Enabled {
			continue
		}
		n, key := awsSignInNotice(r, now)
		if key == "" || key == r.CLI.Notified {
			continue
		}
		r.CLI.Notified = key
		changed = true
		if m.signInLost != nil {
			m.signInLost(n)
		}
	}
	return changed
}

// awsSignInNotice is the Chat line for a profile's sign-in and the key that
// keeps it to once per sign-in; key is "" when there is nothing to say.
func awsSignInNotice(r *record, now time.Time) (SignInLost, string) {
	a := r.Account
	name := a.Profile
	if name == "" {
		name = a.Name
	}
	n := SignInLost{AccountID: a.ID, At: now.UTC()}
	switch a.ProfileStatus {
	case "expired", "invalid":
		// A profile that never worked here has nothing to lose.
		if a.VerifiedAt == nil {
			return n, ""
		}
		n.Title = "AWS sign-in ended"
		if a.ProfileKind == "aws login" {
			n.Body = "“" + name + "” signs in with aws login, which AWS ends after at most 12 hours and cannot extend. Tap Sign in for another 12 hours. For a sign-in that lasts weeks, add the account from your organization's IAM Identity Center in Extensions › AWS."
		} else {
			n.Body = "AWS ended the sign-in for “" + name + "”, so agents can't use it. Tap Sign in and approve the code on any device."
		}
		return n, "ended:" + a.VerifiedAt.UTC().Format(time.RFC3339)
	case "valid":
		// Only a sign-in whose real end is known (Identity Center, its start
		// and the organization's length) is warned about. One that lasts under
		// two days (aws login's 12 h, a default 8 h session) would be warned
		// about all the time; its end is said once it happens.
		if a.ProfileSignInBy == nil || a.ProfileSignInLimit != "session" || r.CLI.SessionLength < int64(2*awsSignInWarnAhead/time.Second) {
			return n, ""
		}
		left := a.ProfileSignInBy.Sub(now)
		if left <= 0 || left > awsSignInWarnAhead {
			return n, ""
		}
		n.Title = "AWS sign-in ends soon"
		n.Body = "“" + name + "” stays signed in until " + a.ProfileSignInBy.In(time.Local).Format("Mon 15:04") + ". Tap Sign in and approve the code on any device, so agents keep access."
		return n, "soon:" + a.ProfileSignInBy.UTC().Format(time.RFC3339)
	}
	return n, ""
}

// watchAWSSignIns refreshes AWS profiles in the background while any is
// enabled; the refresh tells Chat about sign-ins that end.
func (m *Manager) watchAWSSignIns() {
	s := m.snapshots
	if s == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(awsSignInCheckEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
			}
			m.mu.Lock()
			enabled := false
			for _, r := range m.records {
				if r.Account.Integration == "aws" && r.CLI != nil && r.Account.Enabled {
					enabled = true
					break
				}
			}
			m.mu.Unlock()
			if enabled {
				m.refreshCloudProfiles()
			}
		}
	}()
}
