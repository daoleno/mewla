package connections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// gwsStatus is `gws auth status`, the CLI's own view of its configuration.
type gwsStatus struct {
	AuthMethod         string `json:"auth_method"`
	ClientConfig       string `json:"client_config"`
	ClientConfigExists bool   `json:"client_config_exists"`
	ClientConfigError  string `json:"client_config_error"`
	ProjectID          string `json:"project_id"`
	TokenValid         *bool  `json:"token_valid"`
	User               string `json:"user"`
}

func (m *Manager) gwsAuthStatus(ctx context.Context) (gwsStatus, error) {
	var st gwsStatus
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := m.cloudCommand(probe, "gws", gwsProfile, []string{"auth", "status"})
	if err != nil || out.ExitCode != 0 {
		return st, errors.New("Could not read the Google Workspace CLI status. Check the local CLI installation.")
	}
	raw := out.Stdout
	if i := strings.Index(raw, "{"); i >= 0 {
		raw = raw[i:]
	}
	if json.Unmarshal([]byte(raw), &st) != nil {
		return st, errors.New("Invalid Google Workspace CLI status")
	}
	return st, nil
}

func (m *Manager) gwsProfileRecord(profile string) *record {
	for _, r := range m.records {
		if r.Account.Integration != "gws" || r.CLI == nil {
			continue
		}
		if profile == "" || profile == gwsProfile || r.CLI.Profile == profile || r.CLI.Account == profile {
			return r
		}
	}
	return nil
}

// syncGWSProfiles mirrors gws's single signed-in account. The CLI's config
// directory defines it: a sign-out outside Mewla removes the card.
func (m *Manager) syncGWSProfiles(ctx context.Context, force bool) error {
	if m.cloudLocal {
		return m.syncGWSLocal()
	}
	if _, err := m.cloud.executable("gws"); err != nil {
		return nil
	}
	if !force && time.Since(m.cloud.checked["gws:list"]) < 20*time.Second {
		return nil
	}
	st, err := m.gwsAuthStatus(ctx)
	if err != nil {
		return err
	}
	existing := m.gwsProfileRecord(gwsProfile)
	if st.AuthMethod != "oauth2" {
		if existing != nil {
			delete(m.records, existing.Account.ID)
		}
		m.cloud.checked["gws:list"] = time.Now()
		return m.save()
	}
	r := existing
	if r == nil {
		r = &record{CLI: &cloudAccount{Profile: gwsProfile}, Account: Account{ID: "gws-cli-default", Integration: "gws", Profile: gwsProfile, AuthMethod: "cli", ProfileKind: "Google account", Tools: gwsTools(), History: []Event{}}, Grants: map[string]string{}}
		m.setGroup(r, "read", true)
		m.records[r.Account.ID] = r
	}
	// An expired token no longer names its account; keep the one we knew.
	if st.User != "" {
		r.CLI.Account = st.User
	}
	name := r.CLI.Account
	if name == "" {
		name = "Google account"
	}
	r.Account.Name, r.Account.Identity = name, name
	r.Account.Project = st.ProjectID
	r.Account.Active = true
	if st.TokenValid != nil && !*st.TokenValid {
		r.Account.Status, r.Account.ProfileStatus = "authorization_required", "expired"
	} else {
		now := time.Now().UTC()
		r.Account.Status, r.Account.ProfileStatus, r.Account.VerifiedAt = "connected", "valid", &now
	}
	if m.cloudDiscovered != nil {
		m.cloudDiscovered()
	}
	m.cloud.checked["gws:list"] = time.Now()
	return m.save()
}

// gwsConfigDirs are where gws keeps its config on this computer.
var gwsConfigDirs = func() []string {
	dirs := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".config", "gws"))
	}
	if base, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, "gws"))
	}
	return dirs
}

// gwsSignedInLocally says whether gws's own config directory holds a sign-in.
// It only looks at which files exist: no CLI runs and nothing is read.
func gwsSignedInLocally() bool {
	for _, dir := range gwsConfigDirs() {
		for _, name := range []string{"credentials.enc", "credentials.json"} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}

// syncGWSLocal lists the Google account gws is already signed in to, as a card
// that is off and unchecked. It needs no gws copy and makes no call to Google;
// turning the card on is the first use, which asks gws who it is.
func (m *Manager) syncGWSLocal() error {
	existing := m.gwsProfileRecord(gwsProfile)
	if !gwsSignedInLocally() {
		if existing != nil && !existing.Account.Enabled {
			delete(m.records, existing.Account.ID)
		}
		return nil
	}
	if existing == nil {
		r := &record{CLI: &cloudAccount{Profile: gwsProfile}, Account: Account{ID: "gws-cli-default", Integration: "gws", Name: "Google account", Identity: "Google account", Profile: gwsProfile, AuthMethod: "cli", ProfileKind: "Google account", Status: "authorization_required", ProfileStatus: "unchecked", Active: true, Tools: gwsTools(), History: []Event{}}, Grants: map[string]string{}}
		m.setGroup(r, "read", true)
		m.records[r.Account.ID] = r
	}
	if m.cloudDiscovered != nil {
		m.cloudDiscovered()
	}
	return m.save()
}

// Delete signs out of gws. The OAuth client in its config stays, so connecting
// again is one step.
func (m *Manager) deleteGWSProfile(ctx context.Context, q Request) (Response, error) {
	r := m.records[q.ID]
	if r == nil || r.CLI == nil || r.Account.Integration != "gws" || q.ConfirmProfile != r.CLI.Profile && (r.CLI.Account == "" || q.ConfirmProfile != r.CLI.Account) {
		return Response{}, errors.New("Confirm the Google account before deleting")
	}
	for _, f := range m.connectFlows {
		if f.Integration == "gws" && f.Status == "waiting" && f.cli != nil {
			f.cli.cancel()
			select {
			case <-f.cli.done:
			case <-ctx.Done():
				return Response{}, ctx.Err()
			}
			f.Status = "cancelled"
			f.AuthorizationURL = ""
		}
	}
	out, err := m.cloudCommand(ctx, "gws", gwsProfile, []string{"auth", "logout"})
	if err != nil || out.ExitCode != 0 {
		return Response{}, errors.New("Google sign-out failed. Your account was kept; try again.")
	}
	delete(m.records, r.Account.ID)
	delete(m.cloud.checked, "gws:list")
	if err = m.save(); err != nil {
		return Response{}, err
	}
	return Response{}, nil
}
