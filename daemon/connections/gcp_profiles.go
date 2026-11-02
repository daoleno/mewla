package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

type gcpConfiguration struct {
	Name       string                       `json:"name"`
	Active     bool                         `json:"is_active"`
	Properties map[string]map[string]string `json:"properties"`
}
type gcpCredential struct {
	Account string `json:"account"`
}

func (m *Manager) gcpLocalProfiles(ctx context.Context) ([]gcpConfiguration, []gcpCredential, error) {
	probe, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out, err := m.cloudCommand(probe, "gcp", "", []string{"config", "configurations", "list"})
	if err != nil || out.ExitCode != 0 {
		return nil, nil, errors.New("Could not read gcloud configurations. Check the local CLI installation.")
	}
	var configs []gcpConfiguration
	if json.Unmarshal([]byte(out.Stdout), &configs) != nil {
		return nil, nil, errors.New("Invalid gcloud configurations")
	}
	out, err = m.cloudCommand(probe, "gcp", "", []string{"auth", "list"})
	if err != nil || out.ExitCode != 0 {
		return nil, nil, errors.New("Could not read gcloud accounts")
	}
	var creds []gcpCredential
	if json.Unmarshal([]byte(out.Stdout), &creds) != nil {
		return nil, nil, errors.New("Invalid gcloud accounts")
	}
	return configs, creds, nil
}
func (m *Manager) gcpProfileRecord(profile string) *record {
	for _, r := range m.records {
		if r.Account.Integration != "gcp" || r.CLI == nil || r.CLI.Account == "" {
			continue
		}
		if r.CLI.Profile == profile || r.CLI.Account == profile {
			return r
		}
		for _, name := range r.CLI.Configurations {
			if name == profile {
				return r
			}
		}
	}
	return nil
}
func (m *Manager) syncGCPProfiles(ctx context.Context, force bool) error {
	if _, err := m.cloud.executable("gcp"); err != nil {
		return nil
	}
	if !force && time.Since(m.cloud.checked["gcp:list"]) < 4*time.Second {
		return nil
	}
	configs, creds, err := m.gcpLocalProfiles(ctx)
	if err != nil {
		return err
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].Name < configs[j].Name })
	seen := map[string]bool{}
	// The vendor credential store defines accounts. Empty configurations and
	// configurations whose account was revoked are never connection cards.
	for _, credential := range creds {
		account := credential.Account
		if !validGCPAccount(account) {
			continue
		}
		seen[account] = true
		profile := "account:" + account
		r := m.gcpProfileRecord(profile)
		if r == nil {
			sum := sha256.Sum256([]byte(account))
			r = &record{CLI: &cloudAccount{Profile: profile, Account: account}, Account: Account{ID: "gcp-cli-" + hex.EncodeToString(sum[:12]), Integration: "gcp", Name: account, Identity: account, Profile: profile, AuthMethod: "cli", ProfileKind: "gcloud account", Tools: gcpTools(), History: []Event{}}, Grants: map[string]string{}}
			m.setGroup(r, "read", true)
			m.records[r.Account.ID] = r
		}
		r.CLI.Configuration = ""
		r.CLI.Configurations = nil
		r.Account.Project = ""
		r.Account.Active = false
		var chosen *gcpConfiguration
		for i := range configs {
			c := &configs[i]
			if c.Properties["core"]["account"] != account {
				continue
			}
			r.CLI.Configurations = append(r.CLI.Configurations, c.Name)
			if chosen == nil || c.Active || !chosen.Active && chosen.Properties["core"]["project"] == "" && c.Properties["core"]["project"] != "" {
				chosen = c
			}
		}
		if chosen != nil {
			r.CLI.Configuration = chosen.Name
			r.Account.Project = chosen.Properties["core"]["project"]
			r.Account.Active = chosen.Active
		}
		key := "gcp:" + account
		if !force && time.Since(m.cloud.checked[key]) < time.Minute {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, e := m.cloudCommand(probe, "gcp", profile, []string{"auth", "print-access-token"})
		cancel()
		r.Account.Status = "authorization_required"
		r.Account.ProfileStatus = "expired"
		if e == nil && out.ExitCode == 0 && strings.TrimSpace(out.Stdout) != "" {
			now := time.Now().UTC()
			r.Account.Status = "connected"
			r.Account.ProfileStatus = "valid"
			r.Account.VerifiedAt = &now
		}
		m.cloud.checked[key] = time.Now()
	}
	for id, r := range m.records {
		if r.Account.Integration == "gcp" && r.CLI != nil && (r.CLI.Account == "" || !seen[r.CLI.Account]) {
			delete(m.records, id)
			delete(m.cloud.checked, "gcp:"+r.CLI.Account)
		}
	}
	m.cloud.checked["gcp:list"] = time.Now()
	return m.save()
}
func gcpConfigDir() (string, error) {
	if dir := os.Getenv("CLOUDSDK_CONFIG"); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gcloud"), nil
}
func (m *Manager) deleteGCPProfile(ctx context.Context, q Request) (Response, error) {
	r := m.records[q.ID]
	account := r.CLI.Account
	if account == "" || q.ConfirmProfile != r.CLI.Profile {
		return Response{}, errors.New("Confirm the Google account before deleting")
	}
	configs, creds, err := m.gcpLocalProfiles(ctx)
	if err != nil {
		return Response{}, err
	}
	targets := map[string]bool{}
	signedIn := map[string]bool{}
	active := ""
	for _, c := range creds {
		signedIn[c.Account] = true
	}
	for _, c := range configs {
		if c.Active {
			active = c.Name
		}
		if c.Properties["core"]["account"] == account {
			targets[c.Name] = true
		}
	}
	for _, f := range m.connectFlows {
		if f.Integration == "gcp" && (f.profile == r.CLI.Profile || targets[f.profile] || f.renewID == r.Account.ID) && f.Status == "waiting" && f.cli != nil {
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
	dir, err := gcpConfigDir()
	if err != nil {
		return Response{}, err
	}
	backup := filepath.Join(m.cloud.dir, "backups", "gcp-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if err = os.MkdirAll(backup, 0700); err != nil {
		return Response{}, err
	}
	files := []string{"credentials.db", "credentials.db-wal", "access_tokens.db", "access_tokens.db-wal", "active_config"}
	for name := range targets {
		files = append(files, filepath.Join("configurations", "config_"+name))
	}
	for _, file := range files {
		raw, e := readOptional(filepath.Join(dir, file))
		if e != nil {
			return Response{}, errors.New("Could not back up gcloud; nothing was deleted")
		}
		if e = atomicfile.Write(filepath.Join(backup, filepath.Base(file)), raw, 0600); e != nil {
			return Response{}, e
		}
	}
	run := func(args ...string) bool {
		out, e := m.cloudCommand(ctx, "gcp", "", args)
		return e == nil && out.ExitCode == 0
	}
	// Account deletion includes all configurations for that account. Choose an
	// unaffected configuration, or create an empty one, before vendor deletion.
	if targets[active] {
		fallback := ""
		names := map[string]bool{}
		for _, c := range configs {
			names[c.Name] = true
			if targets[c.Name] {
				continue
			}
			if fallback == "" || signedIn[c.Properties["core"]["account"]] {
				fallback = c.Name
			}
		}
		if fallback == "" {
			fallback = "default"
			for n := 0; names[fallback]; n++ {
				fallback = "mewla-default-" + strconv.Itoa(n+1)
			}
			if !run("config", "configurations", "create", fallback, "--no-activate") {
				return Response{}, errors.New("Could not reset the active Google account. Try again.")
			}
		}
		if !run("config", "configurations", "activate", fallback) {
			return Response{}, errors.New("Could not switch the active Google account. Try again.")
		}
	}
	if signedIn[account] && !run("auth", "revoke", account) {
		if targets[active] {
			_ = run("config", "configurations", "activate", active)
		}
		return Response{}, errors.New("Google sign-out failed. Your account and backup were kept; try again.")
	}
	for name := range targets {
		if !run("config", "configurations", "delete", name) {
			return Response{}, errors.New("Google account signed out, but a local configuration could not be removed. Refresh and try again.")
		}
	}
	delete(m.records, r.Account.ID)
	delete(m.cloud.checked, "gcp:list")
	delete(m.cloud.checked, "gcp:"+account)
	if err = m.save(); err != nil {
		return Response{}, err
	}
	return Response{}, nil
}
