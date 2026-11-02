package connections

import (
	"bytes"
	"context"
	"errors"
	"github.com/daoleno/mewla/daemon/atomicfile"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var awsNewProfile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func renameAWSSections(raw []byte, old, name string, config bool) []byte {
	lines := strings.SplitAfter(string(raw), "\n")
	for i, line := range lines {
		match := awsSectionHeader.FindStringSubmatchIndex(line)
		if match != nil {
			target := old
			replacement := name
			if config {
				if old != "default" {
					target = "profile " + old
				}
				if name != "default" {
					replacement = "profile " + name
				}
			}
			if strings.TrimSpace(line[match[2]:match[3]]) == target {
				lines[i] = line[:match[2]] + replacement + line[match[3]:]
			}
		} else if config {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && strings.TrimSpace(key) == "source_profile" && strings.TrimSpace(value) == old {
				at := strings.Index(line, "=")
				ending := ""
				if strings.HasSuffix(line, "\n") {
					ending = "\n"
				}
				lines[i] = line[:at+1] + " " + name + ending
			}
		}
	}
	return []byte(strings.Join(lines, ""))
}
func (m *Manager) renameAWSProfile(ctx context.Context, q Request) (Response, error) {
	if m.cloud == nil {
		return Response{}, errors.New("Cloud CLI is unavailable")
	}
	r := m.records[q.ID]
	if r == nil || r.CLI == nil || r.Account.Integration != "aws" {
		return Response{}, errors.New("Refresh the profiles first")
	}
	old, name := r.CLI.Profile, strings.TrimSpace(q.Profile)
	if !awsNewProfile.MatchString(name) {
		return Response{}, errors.New("Use 1–128 letters, numbers, dots, underscores or hyphens; start with a letter or number.")
	}
	if old == name {
		a := m.projection(r)
		return Response{Account: &a}, nil
	}
	for _, f := range m.connectFlows {
		if f.Integration == "aws" && f.profile == old && f.Status == "waiting" {
			return Response{}, errors.New("Finish or cancel this profile's sign-in before renaming")
		}
	}
	config, creds, err := m.cloud.profileFiles()
	if err != nil {
		return Response{}, err
	}
	paths := []string{config, creds}
	before := make([][]byte, 2)
	after := make([][]byte, 2)
	exists := false
	for i, path := range paths {
		before[i], err = readOptional(path)
		if err != nil {
			return Response{}, err
		}
		sections := awsSections(before[i], i == 0)
		if sections[name] != nil {
			return Response{}, errors.New("That profile name is already in use")
		}
		exists = exists || sections[old] != nil
		after[i] = renameAWSSections(before[i], old, name, i == 0)
	}
	if !exists {
		return Response{}, errors.New("This profile changed in the CLI. Refresh and try again")
	}
	backup := filepath.Join(m.cloud.dir, "backups", "rename-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if err = os.MkdirAll(backup, 0700); err != nil {
		return Response{}, err
	}
	for i, path := range paths {
		if err = atomicfile.Write(filepath.Join(backup, filepath.Base(path)), before[i], 0600); err != nil {
			return Response{}, errors.New("Profile backup failed; nothing was renamed")
		}
	}
	for i, path := range paths {
		current, e := readOptional(path)
		if e != nil || !bytes.Equal(current, before[i]) {
			return Response{}, errors.New("AWS configuration changed; refresh before renaming")
		}
	}
	rollback := func() {
		for i, path := range paths {
			if !bytes.Equal(before[i], after[i]) {
				_ = atomicfile.Write(path, before[i], 0600)
			}
		}
	}
	for i, path := range paths {
		if bytes.Equal(before[i], after[i]) {
			continue
		}
		if err = atomicfile.Write(path, after[i], 0600); err != nil {
			rollback()
			return Response{}, errors.New("Profile rename failed; backup retained")
		}
	}
	oldID := r.Account.ID
	delete(m.records, oldID)
	r.CLI.Profile = name
	r.Account.Profile = name
	r.Account.Name = name
	r.Account.ID = awsProfileID(name)
	m.records[r.Account.ID] = r
	if err = m.save(); err != nil {
		delete(m.records, r.Account.ID)
		r.CLI.Profile = old
		r.Account.Profile = old
		r.Account.Name = old
		r.Account.ID = oldID
		m.records[oldID] = r
		rollback()
		return Response{}, err
	}
	delete(m.cloud.checked, old)
	for _, f := range m.connectFlows {
		if f.Integration == "aws" && f.profile == old {
			f.profile = name
			if f.AccountID == oldID {
				f.AccountID = r.Account.ID
			}
		}
	}
	a := m.projection(r)
	return Response{Account: &a}, nil
}
