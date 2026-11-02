package connections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

var awsSectionHeader = regexp.MustCompile(`^\s*\[([^\]]+)\]`)

func validAWSProfile(p string) bool {
	return p != "" && len(p) <= 128 && !strings.ContainsAny(p, "\x00\r\n[]") && !strings.HasPrefix(p, "-")
}

// Read only named profile sections. Unknown sections (sso-session, services)
// and comments remain byte-for-byte intact when deleting a profile.
func awsSections(raw []byte, config bool) map[string]map[string]string {
	result := map[string]map[string]string{}
	var current map[string]string
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if header := awsSectionHeader.FindStringSubmatch(s); header != nil {
			name := strings.TrimSpace(header[1])
			current = nil
			if config && name != "default" {
				if !strings.HasPrefix(name, "profile ") {
					continue
				}
				name = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
			}
			if !validAWSProfile(name) {
				continue
			}
			if result[name] == nil {
				result[name] = map[string]string{}
			}
			current = result[name]
		} else if current != nil && !strings.HasPrefix(s, "#") && !strings.HasPrefix(s, ";") {
			key, value, ok := strings.Cut(s, "=")
			if ok {
				current[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
			}
		}
	}
	return result
}
func awsProfileFiles() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".aws", "config"), filepath.Join(home, ".aws", "credentials"), nil
}
func readOptional(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return raw, err
}
func (m *Manager) awsProfileRecord(profile string) *record {
	for _, r := range m.records {
		if r.Account.Integration == "aws" && r.CLI != nil && r.CLI.Profile == profile {
			return r
		}
	}
	return nil
}
func awsProfileID(profile string) string {
	sum := sha256.Sum256([]byte(profile))
	return "aws-cli-" + hex.EncodeToString(sum[:12])
}
func awsProfileKind(fields map[string]string) string {
	switch {
	case fields["login_session"] != "":
		return "aws login"
	case fields["sso_session"] != "" || fields["sso_start_url"] != "":
		return "SSO"
	case fields["credential_process"] != "":
		return "credential_process"
	case fields["role_arn"] != "":
		return "assume role"
	case fields["aws_access_key_id"] != "":
		return "static keys"
	default:
		return "configuration"
	}
}
func (m *Manager) syncAWSProfiles(ctx context.Context, force bool) error {
	config, creds, err := m.cloud.profileFiles()
	if err != nil {
		return err
	}
	a, err := readOptional(config)
	if err != nil {
		return errors.New("AWS config could not be read")
	}
	b, err := readOptional(creds)
	if err != nil {
		return errors.New("AWS credentials could not be read")
	}
	fields := awsSections(a, true)
	for name, keys := range awsSections(b, false) {
		if fields[name] == nil {
			fields[name] = map[string]string{}
		}
		for k, v := range keys {
			fields[name][k] = v
		}
	}
	// Removed profiles disappear even while cached identity checks are fresh.
	changed := false
	for id, r := range m.records {
		if r.Account.Integration == "aws" && r.CLI != nil && fields[r.CLI.Profile] == nil {
			delete(m.records, id)
			delete(m.cloud.checked, r.CLI.Profile)
			changed = true
		}
	}
	type check struct {
		r   *record
		out cloudOutput
		err error
	}
	checks := []*check{}
	names := []string{}
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := m.awsProfileRecord(name)
		if r == nil {
			r = &record{CLI: &cloudAccount{Profile: name}, Account: Account{ID: awsProfileID(name), Integration: "aws", Name: name, Profile: name, AuthMethod: "cli", Status: "authorization_required", ProfileStatus: "unchecked", Region: firstNonEmpty(fields[name]["region"], awsDefaultRegion), Tools: awsTools(), History: []Event{}}, Grants: map[string]string{}}
			m.setGroup(r, "read", true)
			m.records[r.Account.ID] = r
			changed = true
		}
		r.Account.ProfileKind = awsProfileKind(fields[name])
		r.Account.Profile = name
		if !force && time.Since(m.cloud.checked[name]) < time.Minute {
			continue
		}
		checks = append(checks, &check{r: r})
	}
	// Bounded parallel identity checks prevent one expired profile from blocking
	// every card. The cache lasts only a minute and does not cache the profile list.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, c := range checks {
		wg.Add(1)
		go func(c *check) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				c.err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			probe, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			c.out, c.err = m.cloudCommand(probe, "aws", c.r.CLI.Profile, []string{"sts", "get-caller-identity", "--output", "json", "--region", c.r.Account.Region})
		}(c)
	}
	wg.Wait()
	for _, c := range checks {
		r := c.r
		m.cloud.checked[r.CLI.Profile] = time.Now()
		changed = true
		var id awsIdentity
		if c.err == nil && c.out.ExitCode == 0 && json.Unmarshal([]byte(c.out.Stdout), &id) == nil && id.Account != "" && id.Arn != "" {
			now := time.Now().UTC()
			r.Account.Identity = id.Arn
			r.Account.Status = "connected"
			r.Account.ProfileStatus = "valid"
			r.Account.VerifiedAt = &now
			previous := r.Account.AWS
			r.Account.AWS = &AWSDetails{AccountID: id.Account, CheckedAt: now}
			r.Account.AWS.Who, r.Account.AWS.Session = parseAWSPrincipal(id.Arn)
			if previous != nil && previous.AccountID == id.Account {
				r.Account.AWS.AccountName = previous.AccountName
			}
			// Aliases are optional. A denial never invalidates the identity.
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			aliases, e := m.cloudCommand(probe, "aws", r.CLI.Profile, []string{"iam", "list-account-aliases", "--output", "json", "--region", r.Account.Region})
			cancel()
			if e == nil && aliases.ExitCode == 0 {
				var v struct {
					Aliases []string `json:"AccountAliases"`
				}
				if json.Unmarshal([]byte(aliases.Stdout), &v) == nil && len(v.Aliases) > 0 {
					r.Account.AWS.AccountName = v.Aliases[0]
				}
			}
		} else {
			r.Account.Status = "authorization_required"
			r.Account.ProfileStatus = "invalid"
			if awsExpired(c.out.Stderr) {
				r.Account.ProfileStatus = "expired"
			}
			if c.err != nil {
				r.Account.ProfileStatus = "unavailable"
			}
		}
	}
	if changed {
		return m.save()
	}
	return nil
}
func awsExpired(s string) bool {
	s = strings.ToLower(s)
	for _, v := range []string{"expired", "reauthenticate", "aws login", "sso login", "invalidgrant"} {
		if strings.Contains(s, v) {
			return true
		}
	}
	return false
}
func removeAWSSection(raw []byte, name string, config bool) []byte {
	var out bytes.Buffer
	skip := false
	for _, line := range strings.SplitAfter(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if header := awsSectionHeader.FindStringSubmatch(s); header != nil {
			section := strings.TrimSpace(header[1])
			target := name
			if config && name != "default" {
				target = "profile " + name
			}
			skip = section == target
		}
		if !skip {
			out.WriteString(line)
		}
	}
	return out.Bytes()
}
func (m *Manager) deleteCloudProfile(ctx context.Context, q Request) (Response, error) {
	r := m.records[q.ID]
	if r != nil && r.CLI != nil && r.Account.Integration == "gcp" {
		return m.deleteGCPProfile(ctx, q)
	}
	if r == nil || r.CLI == nil || r.Account.Integration != "aws" {
		return Response{}, errors.New("Refresh the profiles first")
	}
	name := r.CLI.Profile
	if q.ConfirmProfile != name {
		return Response{}, errors.New("Confirm the exact profile name before deleting")
	}
	// A pending login must not recreate a profile after confirmed deletion.
	for _, flow := range m.connectFlows {
		if flow.Integration == "aws" && flow.profile == name && flow.Status == "waiting" && flow.cli != nil {
			flow.cli.cancel()
			select {
			case <-flow.cli.done:
			case <-ctx.Done():
				return Response{}, ctx.Err()
			}
			flow.Status = "cancelled"
			flow.AuthorizationURL = ""
		}
	}
	config, creds, err := m.cloud.profileFiles()
	if err != nil {
		return Response{}, err
	}
	paths := []string{config, creds}
	before := make([][]byte, 2)
	after := make([][]byte, 2)
	backup := filepath.Join(m.cloud.dir, "backups", time.Now().UTC().Format("20060102T150405.000000000"))
	if err = os.MkdirAll(backup, 0700); err != nil {
		return Response{}, err
	}
	for i, path := range paths {
		before[i], err = readOptional(path)
		if err != nil {
			return Response{}, err
		}
		after[i] = removeAWSSection(before[i], name, i == 0)
		if err = atomicfile.Write(filepath.Join(backup, filepath.Base(path)), before[i], 0600); err != nil {
			return Response{}, errors.New("Profile backup failed; nothing was deleted")
		}
	}
	sections := awsSections(before[0], true)
	login := sections[name]["login_session"]
	shared := false
	for profile, fields := range sections {
		if profile != name && login != "" && fields["login_session"] == login {
			shared = true
		}
	}
	// aws logout removes the session cache, shared by profiles with the same
	// login_session. Preserve a cache another profile still uses.
	if login != "" && !shared {
		out, e := m.cloudCommand(ctx, "aws", name, []string{"logout"})
		if e != nil || out.ExitCode != 0 {
			return Response{}, errors.New("AWS logout failed; the profile and its backup were kept")
		}
	}
	for i, path := range paths {
		current, e := readOptional(path)
		if e != nil || !bytes.Equal(current, before[i]) {
			return Response{}, errors.New("AWS configuration changed; refresh before deleting")
		}
	}
	for i, path := range paths {
		if bytes.Equal(before[i], after[i]) {
			continue
		}
		if err = atomicfile.Write(path, after[i], 0600); err != nil {
			for j := 0; j < i; j++ {
				_ = atomicfile.Write(paths[j], before[j], 0600)
			}
			return Response{}, errors.New("Profile removal failed; backup retained")
		}
	}
	delete(m.records, r.Account.ID)
	delete(m.cloud.checked, name)
	if err = m.save(); err != nil {
		return Response{}, err
	}
	return Response{}, nil
}
