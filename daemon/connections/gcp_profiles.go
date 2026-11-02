package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		if r.Account.Integration == "gcp" && r.CLI != nil && r.CLI.Profile == profile {
			return r
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
	accounts := map[string]bool{}
	used := map[string]bool{}
	seen := map[string]bool{}
	for _, c := range creds {
		accounts[c.Account] = true
	}
	for _, c := range configs {
		used[c.Properties["core"]["account"]] = true
	}
	// Credentials not attached to a named configuration remain visible/manageable.
	for account := range accounts {
		if !used[account] && validGCPAccount(account) {
			configs = append(configs, gcpConfiguration{Name: "account:" + account, Properties: map[string]map[string]string{"core": {"account": account}}})
		}
	}
	for _, c := range configs {
		if !(gcloudCLI{}).validProfile(c.Name) {
			continue
		}
		seen[c.Name] = true
		r := m.gcpProfileRecord(c.Name)
		if r == nil {
			sum := sha256.Sum256([]byte(c.Name))
			r = &record{CLI: &cloudAccount{Profile: c.Name}, Account: Account{ID: "gcp-cli-" + hex.EncodeToString(sum[:12]), Integration: "gcp", Name: c.Name, Profile: c.Name, AuthMethod: "cli", Tools: gcpTools(), History: []Event{}}, Grants: map[string]string{}}
			m.setGroup(r, "read", true)
			m.records[r.Account.ID] = r
		}
		account := c.Properties["core"]["account"]
		changedIdentity := r.Account.Identity != account
		if changedIdentity {
			r.Account.Enabled = false
			m.setGroup(r, "write", false)
		}
		r.Account.Identity = account
		r.Account.Project = c.Properties["core"]["project"]
		r.Account.Active = c.Active
		r.Account.ProfileKind = "gcloud configuration"
		if strings.HasPrefix(c.Name, "account:") {
			r.Account.Name = account
			r.Account.ProfileKind = "gcloud account"
		}
		key := "gcp:" + c.Name
		if !force && !changedIdentity && time.Since(m.cloud.checked[key]) < time.Minute {
			continue
		}
		r.Account.Status = "authorization_required"
		r.Account.ProfileStatus = "signed out"
		if accounts[account] {
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			// Verify refreshability without exposing or storing the resulting token.
			out, e := m.cloudCommand(probe, "gcp", c.Name, []string{"auth", "print-access-token", "--account", account})
			cancel()
			if e == nil && out.ExitCode == 0 && strings.TrimSpace(out.Stdout) != "" {
				now := time.Now().UTC()
				r.Account.Status = "connected"
				r.Account.ProfileStatus = "valid"
				r.Account.VerifiedAt = &now
			} else {
				r.Account.ProfileStatus = "expired"
			}
		}
		m.cloud.checked[key] = time.Now()
	}
	for id, r := range m.records {
		if r.Account.Integration == "gcp" && r.CLI != nil && !seen[r.CLI.Profile] {
			delete(m.records, id)
			delete(m.cloud.checked, "gcp:"+r.CLI.Profile)
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
	name := r.CLI.Profile
	if q.ConfirmProfile != name {
		return Response{}, errors.New("Confirm the exact configuration name before deleting")
	}
	for _, f := range m.connectFlows {
		if f.Integration == "gcp" && f.profile == name && f.Status == "waiting" && f.cli != nil {
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
	configs, creds, err := m.gcpLocalProfiles(ctx)
	if err != nil {
		return Response{}, err
	}
	account := ""
	found := false
	shared := false
	if strings.HasPrefix(name, "account:") {
		account = strings.TrimPrefix(name, "account:")
		for _, c := range creds {
			if c.Account == account {
				found = true
			}
		}
	} else {
		for _, c := range configs {
			if c.Name == name {
				found = true
				account = c.Properties["core"]["account"]
				if c.Active {
					return Response{}, errors.New("This is gcloud's active configuration. Activate another configuration in gcloud before deleting it.")
				}
			}
		}
	}
	if !found {
		return Response{}, errors.New("Configuration changed; refresh before deleting")
	}
	for _, c := range configs {
		if c.Name != name && account != "" && c.Properties["core"]["account"] == account {
			shared = true
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
	files := []string{"credentials.db", "access_tokens.db", "active_config"}
	if !strings.HasPrefix(name, "account:") {
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
	credentialed := false
	for _, c := range creds {
		if c.Account == account {
			credentialed = true
		}
	}
	if credentialed && !shared {
		out, e := m.cloudCommand(ctx, "gcp", "", []string{"auth", "revoke", account})
		if e != nil || out.ExitCode != 0 {
			return Response{}, errors.New("Google sign-out failed; configuration and backup were kept")
		}
	}
	if !strings.HasPrefix(name, "account:") {
		out, e := m.cloudCommand(ctx, "gcp", "", []string{"config", "configurations", "delete", name})
		if e != nil || out.ExitCode != 0 {
			return Response{}, errors.New("Configuration removal failed; refresh to check sign-in. Backup retained.")
		}
	}
	delete(m.records, r.Account.ID)
	delete(m.cloud.checked, "gcp:list")
	delete(m.cloud.checked, "gcp:"+name)
	if err = m.save(); err != nil {
		return Response{}, err
	}
	return Response{}, nil
}
