package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A cloud CLI is set up, and a cloud called with the credentials found on this
// computer, only once the person has used that cloud through Mewla.

type lazyCLI struct {
	cloudProvider
	name     string
	installs atomic.Int32
	fn       func(context.Context, string) error
}

func (p *lazyCLI) binary() string { return p.name }
func (p *lazyCLI) install(ctx context.Context, dir string) error {
	p.installs.Add(1)
	if p.fn != nil {
		return p.fn(ctx, dir)
	}
	return errors.New("no network in this test")
}
func (p *lazyCLI) installPaths() []string                           { return nil }
func (p *lazyCLI) validateExecutable(context.Context, string) error { return nil }

// installStub puts a working CLI where a managed install leaves it.
func installStub(name string) func(context.Context, string) error {
	return func(_ context.Context, dir string) error {
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "bin", name), []byte("#!/bin/sh\nprintf '%s\\n' 'aws-cli/2.39.3 Python/test'\n"), 0700)
	}
}

type lazyFixture struct {
	m      *Manager
	config string
	aws    *lazyCLI
	gcp    *lazyCLI
	gws    *lazyCLI
	runs   *[]string
	mu     *sync.Mutex
}

func (f lazyFixture) ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, *f.runs...)
}

// lazyManager is a daemon on a fresh state with the three clouds, none of whose
// CLIs exists, and with automatic setup on.
func lazyManager(t *testing.T) lazyFixture {
	t.Helper()
	m, config, _, _ := testCloudManager(t)
	t.Cleanup(m.Close)
	f := lazyFixture{m: m, config: config, runs: &[]string{}, mu: &sync.Mutex{}}
	f.aws = &lazyCLI{cloudProvider: awsCLI{}, name: "mewla-lazy-aws", fn: installStub("mewla-lazy-aws")}
	f.gcp = &lazyCLI{cloudProvider: gcloudCLI{}, name: "mewla-lazy-gcloud"}
	f.gws = &lazyCLI{cloudProvider: gwsCLI{}, name: "mewla-lazy-gws"}
	m.cloud.providers = map[string]cloudProvider{"aws": f.aws, "gcp": f.gcp, "gws": f.gws}
	m.cloud.run = func(_ context.Context, _ string, args, _ []string) (cloudOutput, error) {
		f.mu.Lock()
		*f.runs = append(*f.runs, strings.Join(args, " "))
		f.mu.Unlock()
		if len(args) > 1 && args[0] == "sts" {
			return cloudOutput{Stdout: `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:root","UserId":"root"}`}, nil
		}
		return cloudOutput{Stdout: `{"AccountAliases":["Test account"]}`}, nil
	}
	m.cloud.enableUpdates()
	// Never the real computer's gws sign-in.
	empty := t.TempDir()
	previous := gwsConfigDirs
	gwsConfigDirs = func() []string { return []string{empty} }
	t.Cleanup(func() { gwsConfigDirs = previous })
	return f
}

func (f lazyFixture) list(t *testing.T) Response {
	t.Helper()
	_, err := f.m.Handle(context.Background(), Request{Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	return f.m.cachedList()
}

// settle waits for the refreshes and the installs they started.
func (f lazyFixture) settle(t *testing.T) {
	t.Helper()
	f.m.snapshots.wg.Wait()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		u := f.m.cloud.updates
		u.mu.Lock()
		busy := len(u.active)
		u.mu.Unlock()
		if busy == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("an install did not finish")
		}
	}
}

func TestFreshListSetsUpNoCloud(t *testing.T) {
	f := lazyManager(t)
	// No ~/.aws, no gcloud configuration: nothing for any cloud to start from.
	f.list(t)
	if a, g, w := f.aws.installs.Load(), f.gcp.installs.Load(), f.gws.installs.Load(); a+g+w != 0 {
		t.Fatalf("a fresh list installed CLIs: aws %d, gcp %d, gws %d", a, g, w)
	}
	if ran := f.ran(); len(ran) != 0 {
		t.Fatal("a fresh list ran a cloud CLI", ran)
	}
	if _, err := os.Stat(filepath.Join(f.m.cloud.dir, "aws")); err == nil {
		t.Fatal("a fresh list created the aws CLI directory")
	}
}

func TestListShowsAWSProfilesWithoutCallingAWS(t *testing.T) {
	f := lazyManager(t)
	os.WriteFile(f.config, []byte("[profile one]\nregion=eu-west-1\n[profile two]\nsso_session=s\n[sso-session s]\nsso_start_url=https://x.invalid\n"), 0600)
	response := f.list(t)
	names := []string{}
	for _, a := range response.Accounts {
		if a.Integration == "aws" {
			names = append(names, a.Name)
			if a.Enabled || a.ProfileStatus != "unchecked" {
				t.Fatal("a profile nobody enabled was checked or turned on", a.Name, a.ProfileStatus)
			}
		}
	}
	if len(names) != 2 {
		t.Fatal("profile names from the local files are missing", names)
	}
	if ran := f.ran(); len(ran) != 0 || f.aws.installs.Load() != 0 {
		t.Fatal("listing profile names called AWS or installed its CLI", ran, f.aws.installs.Load())
	}
	// Run again: still nothing, and the cards stay.
	f.m.snapshots.checked = map[string]time.Time{}
	f.list(t)
	if len(f.ran()) != 0 {
		t.Fatal("a second list called AWS")
	}
}

func TestEnabledAWSAccountIsStillChecked(t *testing.T) {
	f := lazyManager(t)
	os.WriteFile(f.config, []byte("[profile used]\nregion=us-east-1\n"), 0600)
	f.list(t)
	enableCloudProfiles(f.m)
	f.m.snapshots.checked = map[string]time.Time{}
	f.m.cloud.checked = map[string]time.Time{}
	response := f.list(t)
	sts := 0
	for _, r := range f.ran() {
		if strings.HasPrefix(r, "sts get-caller-identity") {
			sts++
		}
	}
	if sts == 0 {
		t.Fatal("an account in use was not checked")
	}
	for _, a := range response.Accounts {
		if a.Integration == "aws" && a.ProfileStatus != "valid" {
			t.Fatal("an account in use is not valid", a.ProfileStatus)
		}
	}
}

func TestFirstUseInstallsOnceAndLaterUseIsFree(t *testing.T) {
	f := lazyManager(t)
	os.WriteFile(f.config, []byte("[profile used]\nregion=us-east-1\n"), 0600)
	response := f.list(t)
	var id string
	for _, a := range response.Accounts {
		if a.Integration == "aws" {
			id = a.ID
		}
	}
	if id == "" || f.aws.installs.Load() != 0 {
		t.Fatal("profile card missing or CLI installed before use", id, f.aws.installs.Load())
	}
	// Turning the card on is the first use.
	if _, err := f.m.Handle(context.Background(), Request{Action: "enable", ID: id}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	if f.aws.installs.Load() != 1 {
		t.Fatal("first use did not install the CLI once", f.aws.installs.Load())
	}
	valid := false
	for _, a := range f.m.cachedList().Accounts {
		if a.ID == id {
			valid = a.ProfileStatus == "valid"
		}
	}
	if !valid {
		t.Fatal("the first check ran before the CLI existed")
	}
	// A restart: same state directory, a new runtime, the account still on.
	f.m.cloud.updates.cancel()
	restarted := newCloudRuntime(f.m.cloud.dir)
	restarted.providers, restarted.profileFiles, restarted.run = f.m.cloud.providers, f.m.cloud.profileFiles, f.m.cloud.run
	restarted.enableUpdates()
	f.m.cloud = restarted
	f.m.snapshots.checked = map[string]time.Time{}
	f.list(t)
	if f.aws.installs.Load() != 1 {
		t.Fatal("a restart reinstalled the CLI", f.aws.installs.Load())
	}
	if f.m.cloudInfo["aws"] == nil || !f.m.cloudInfo["aws"].Managed {
		t.Fatal("the account in use does not run on the managed CLI")
	}
}

func TestFailedFirstInstallSaysWhy(t *testing.T) {
	f := lazyManager(t)
	f.aws.fn = func(context.Context, string) error { return errors.New("dial tcp: no route to host") }
	err := f.m.cloud.prepare(context.Background(), "aws")
	if err == nil || !strings.Contains(err.Error(), "no route to host") {
		t.Fatal("the install's real error did not reach the person", err)
	}
	if _, err = f.m.cloudCommand(context.Background(), "aws", "p", []string{"sts", "get-caller-identity"}); err == nil || !strings.Contains(err.Error(), "no route to host") {
		t.Fatal("a command after a failed install lost the reason", err)
	}
}

func TestACloudCommandWithNoAccountOnDoesNotCheckEveryProfile(t *testing.T) {
	f := lazyManager(t)
	os.WriteFile(f.config, []byte("[profile a]\n[profile b]\n"), 0600)
	_, err := f.m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "aws", Profile: "a", CLIArgs: []string{"s3", "ls"}})
	if err == nil || !strings.Contains(err.Error(), "Enable a profile") {
		t.Fatal("expected the enable prompt", err)
	}
	for _, r := range f.ran() {
		if strings.HasPrefix(r, "sts ") {
			t.Fatal("a command checked profiles nobody turned on", r)
		}
	}
}

func TestACommandThatWillBeRefusedDownloadsNothing(t *testing.T) {
	for _, tc := range []struct {
		id   string
		args []string
	}{{"aws", []string{"s3", "ls"}}, {"gcp", []string{"projects", "list"}}, {"gws", []string{"drive", "files", "list"}}} {
		t.Run(tc.id, func(t *testing.T) {
			f := lazyManager(t)
			_, err := f.m.Handle(context.Background(), Request{Action: "cloud_run", Integration: tc.id, CLIArgs: tc.args})
			if err == nil || !strings.Contains(err.Error(), "Enable a profile") {
				t.Fatal("expected the enable prompt", err)
			}
			if n := f.aws.installs.Load() + f.gcp.installs.Load() + f.gws.installs.Load(); n != 0 {
				t.Fatal("a refused command installed a CLI", n)
			}
		})
	}
}

func TestAWriteWithoutAuthorizationDownloadsNothing(t *testing.T) {
	f := lazyManager(t)
	os.WriteFile(f.config, []byte("[profile used]\nregion=us-east-1\n"), 0600)
	f.list(t)
	enableCloudProfiles(f.m)
	f.m.mu.Lock()
	for _, r := range f.m.records {
		if r.Account.Integration == "aws" {
			f.m.setGroup(r, "read", true)
		}
	}
	f.m.mu.Unlock()
	_, err := f.m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "aws", CLIArgs: []string{"ec2", "terminate-instances"}})
	if err == nil || !strings.Contains(err.Error(), "authorized") {
		t.Fatal("expected a refusal for a write without authorization", err)
	}
	if f.aws.installs.Load() != 0 {
		t.Fatal("a refused write installed the CLI")
	}
}

// An installer's progress is long; it must not fail the install.
func TestInstallerStepWithLongOutputSucceedsAndKeepsItsLastLine(t *testing.T) {
	script := filepath.Join(t.TempDir(), "noisy.sh")
	body := "#!/bin/sh\ni=0\nwhile [ $i -lt 3000 ]; do echo 'progress progress progress progress' >&2; i=$((i+1)); done\necho 'LASTLINE' >&2\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runCloudProcess(context.Background(), script, nil, os.Environ()); err == nil {
		t.Fatal("a command's output is still bounded")
	}
	out, err := runCloudInstallProcess(context.Background(), script, nil, os.Environ())
	if err != nil || out.ExitCode != 0 || !strings.HasSuffix(strings.TrimSpace(out.Stderr), "LASTLINE") {
		t.Fatal("an installer step with long output failed", err, out.ExitCode, len(out.Stderr))
	}
	failure := cloudStepFailure("Could not update the staged gcloud SDK", cloudOutput{Stderr: "x\nERROR: disk full\n", ExitCode: 1}, nil)
	if !strings.Contains(failure.Error(), "disk full") {
		t.Fatal("the failure lost the tool's last line", failure)
	}
}

// An account the person already signed in with their own gcloud is listed as an
// off card from what is on the computer: no install, no call to Google.
func TestSignedInGcloudAccountIsListedWithoutInstallOrNetwork(t *testing.T) {
	f := lazyManager(t)
	f.gcp.name = cliStub(t, "gcloud")
	f.m.cloud.run = func(_ context.Context, _ string, args, _ []string) (cloudOutput, error) {
		f.mu.Lock()
		*f.runs = append(*f.runs, strings.Join(args, " "))
		f.mu.Unlock()
		switch {
		case len(args) > 1 && args[0] == "config":
			return cloudOutput{Stdout: `[{"name":"default","is_active":true,"properties":{"core":{"account":"person@example.com","project":"demo"}}}]`}, nil
		case len(args) > 1 && args[0] == "auth" && args[1] == "list":
			return cloudOutput{Stdout: `[{"account":"person@example.com"}]`}, nil
		case len(args) > 1 && args[1] == "print-access-token":
			return cloudOutput{Stdout: "token"}, nil
		}
		return cloudOutput{Stdout: "ok"}, nil
	}
	response := f.list(t)
	var id string
	for _, a := range response.Accounts {
		if a.Integration == "gcp" {
			id = a.ID
			if a.Enabled || a.Identity != "person@example.com" {
				t.Fatal("the signed-in account is not an off card", a.Enabled, a.Identity)
			}
		}
	}
	if id == "" {
		t.Fatal("an account already signed in with gcloud is not listed", f.ran())
	}
	if f.gcp.installs.Load() != 0 {
		t.Fatal("listing the account installed gcloud")
	}
	for _, r := range f.ran() {
		if strings.Contains(r, "print-access-token") {
			t.Fatal("listing the account asked Google for a token", r)
		}
	}
	// Turning it on is the first use: the real check, with no second sign-in.
	if _, err := f.m.Handle(context.Background(), Request{Action: "enable", ID: id}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	valid := false
	for _, a := range f.m.cachedList().Accounts {
		if a.ID == id {
			valid = a.Status == "connected" && a.ProfileStatus == "valid"
		}
	}
	if !valid {
		t.Fatal("the account did not check as valid after being turned on", f.ran())
	}
}

func TestSignedInWorkspaceAccountIsListedWithoutInstallOrCall(t *testing.T) {
	f := lazyManager(t)
	dir := t.TempDir()
	gwsConfigDirs = func() []string { return []string{dir} }
	os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("sealed"), 0600)
	response := f.list(t)
	found := false
	for _, a := range response.Accounts {
		if a.Integration == "gws" {
			found = !a.Enabled && a.ProfileStatus == "unchecked"
		}
	}
	if !found {
		t.Fatal("a Workspace account gws is signed in to is not listed as an off card")
	}
	if f.gws.installs.Load() != 0 || len(f.ran()) != 0 {
		t.Fatal("listing it installed gws or ran a CLI", f.gws.installs.Load(), f.ran())
	}
	// A sign-out made outside Mewla removes the card.
	os.Remove(filepath.Join(dir, "credentials.enc"))
	f.m.snapshots.checked = map[string]time.Time{}
	for _, a := range f.list(t).Accounts {
		if a.Integration == "gws" {
			t.Fatal("the card stayed after gws signed out")
		}
	}
}

// An enabled card whose CLI could not be installed says why, and Check now tries again.
func TestCardSaysWhyItsCLIFailedToInstallAndCheckNowRetries(t *testing.T) {
	f := lazyManager(t)
	f.aws.fn = func(context.Context, string) error { return errors.New("dial tcp: no route to host") }
	os.WriteFile(f.config, []byte("[profile used]\nregion=us-east-1\n"), 0600)
	var id string
	for _, a := range f.list(t).Accounts {
		if a.Integration == "aws" {
			id = a.ID
		}
	}
	if _, err := f.m.Handle(context.Background(), Request{Action: "enable", ID: id}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	card := func() Account {
		for _, a := range f.m.cachedList().Accounts {
			if a.ID == id {
				return a
			}
		}
		t.Fatal("card missing")
		return Account{}
	}
	if a := card(); a.ProfileStatus != "unavailable" || !strings.Contains(a.Unavailable, "no route to host") {
		t.Fatal("the card does not say why its CLI is missing", a.ProfileStatus, a.Unavailable)
	}
	// Check now: the install is tried again, and works this time.
	f.aws.fn = installStub("mewla-lazy-aws")
	if _, err := f.m.Handle(context.Background(), Request{Action: "refresh", ID: id}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	if f.aws.installs.Load() != 2 {
		t.Fatal("Check now did not try the install again", f.aws.installs.Load())
	}
	if a := card(); a.Unavailable != "" || a.ProfileStatus != "valid" {
		t.Fatal("the card still says unavailable after a good install", a.ProfileStatus, a.Unavailable)
	}
}

func TestWorkspaceCardSaysWhyItsCLIFailedToInstall(t *testing.T) {
	f := lazyManager(t)
	dir := t.TempDir()
	gwsConfigDirs = func() []string { return []string{dir} }
	os.WriteFile(filepath.Join(dir, "credentials.enc"), []byte("sealed"), 0600)
	var id string
	for _, a := range f.list(t).Accounts {
		if a.Integration == "gws" {
			id = a.ID
		}
	}
	if _, err := f.m.Handle(context.Background(), Request{Action: "enable", ID: id}); err != nil {
		t.Fatal(err)
	}
	f.settle(t)
	for _, a := range f.m.cachedList().Accounts {
		if a.ID == id && (a.ProfileStatus != "unavailable" || !strings.Contains(a.Unavailable, "no network in this test")) {
			t.Fatal("the Workspace card does not say why its CLI is missing", a.ProfileStatus, a.Unavailable)
		}
	}
}
