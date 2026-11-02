package connections

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGCPCLIClassification(t *testing.T) {
	p := gcloudCLI{}
	for _, args := range [][]string{{"projects", "list"}, {"compute", "instances", "list", "--project", "example"}, {"storage", "buckets", "describe", "gs://bucket", "--format=json"}} {
		read, err := p.classify(args)
		if err != nil || !read {
			t.Fatalf("read %v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"projects", "delete", "list"}, {"iam", "roles", "list"}, {"billing", "accounts", "list"}, {"sql", "instances", "list"}, {"secrets", "versions", "access", "latest"}, {"run", "services", "describe", "secret-env"}, {"projects", "list", "--unknown=value"}, {"projects", "list", "--impersonate-service-account=x"}, {"compute", "ssh", "host"}, {"auth", "print-access-token"}, {"projects", "list", "--flags-file=/etc/passwd"}, {"projects", "list", "--conf=other"}, {"projects", "list", "--account=other"}} {
		read, err := p.classify(args)
		if err == nil && read {
			t.Fatalf("unsafe read: %v", args)
		}
	}
	args := p.commandArgs("work", []string{"projects", "list"})
	if strings.Join(args, " ") != "projects list --configuration work --format=json --quiet" {
		t.Fatal(args)
	}
	t.Setenv("CLOUDSDK_CORE_ACCOUNT", "other")
	t.Setenv("CLOUDSDK_API_ENDPOINT_OVERRIDES_COMPUTE", "https://evil.test")
	t.Setenv("CLOUDSDK_CONFIG", "/canonical/config")
	env := strings.Join(p.environment(), "\n")
	if strings.Contains(env, "other") || strings.Contains(env, "evil.test") || !strings.Contains(env, "CLOUDSDK_CONFIG=/canonical/config") {
		t.Fatal("credential routing overridden")
	}
}
func TestGCPRemoteLogin(t *testing.T) {
	p := gcloudCLI{}
	address := "https://accounts.google.com/o/oauth2/auth?client_id=vendor&response_type=code&code_challenge=test"
	if p.loginURL("Open:\n"+address+"\nCode:") != address || p.loginURL("https://accounts.google.com.evil.test/o/oauth2/auth?client_id=x&response_type=code") != "" {
		t.Fatal("URL parsing")
	}
	if !p.validateCode(address, "4/valid_authorization-code") || p.validateCode(address, "x\nsecond-line") {
		t.Fatal("code validation")
	}
	args := strings.Join(p.loginArgs("test"), " ")
	if !strings.Contains(args, "--no-launch-browser --force") || strings.Contains(args, "--no-browser") {
		t.Fatal(args)
	}
}

type testGcloudCLI struct {
	gcloudCLI
	path string
}

func (p testGcloudCLI) binary() string { return p.path }
func testGCPManager(t *testing.T) (*Manager, *[]gcpConfiguration, *[]gcpCredential, *[]string) {
	t.Helper()
	m, _, _, _ := testCloudManager(t)
	t.Setenv("CLOUDSDK_CONFIG", t.TempDir())
	configs := []gcpConfiguration{{Name: "keep", Active: true, Properties: map[string]map[string]string{"core": {"account": "same@example.com", "project": "project-a"}}}, {Name: "throwaway", Properties: map[string]map[string]string{"core": {"account": "same@example.com", "project": "project-b"}}}}
	creds := []gcpCredential{{Account: "same@example.com"}, {Account: "unused@example.com"}}
	calls := []string{}
	m.cloud.providers["gcp"] = testGcloudCLI{path: "/bin/true"}
	m.cloud.run = func(_ context.Context, _ string, args, env []string) (cloudOutput, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		var value any = map[string]any{"ok": true}
		switch {
		case strings.HasPrefix(call, "config configurations list"):
			value = configs
		case strings.HasPrefix(call, "auth list"):
			value = creds
		case strings.HasPrefix(call, "auth print-access-token"):
			return cloudOutput{Stdout: "sensitive-token-never-persist"}, nil
		case strings.HasPrefix(call, "config configurations delete"):
			for i, c := range configs {
				if c.Name == args[3] {
					configs = append(configs[:i], configs[i+1:]...)
					break
				}
			}
		}
		raw, _ := json.Marshal(value)
		return cloudOutput{Stdout: string(raw)}, nil
	}
	return m, &configs, &creds, &calls
}
func TestGCPProfilesAreLiveAndGrantsFollowIdentity(t *testing.T) {
	m, configs, _, _ := testGCPManager(t)
	mustHandle(t, m, Request{Action: "list"})
	if len(m.records) != 3 {
		t.Fatal("missing configs or unbound credentials")
	}
	r := m.gcpProfileRecord("throwaway")
	id := r.Account.ID
	if r.Account.Enabled || r.Account.Project != "project-b" || r.Account.ProfileStatus != "valid" {
		t.Fatal(r.Account)
	}
	r.Account.Enabled = true
	m.setGroup(r, "write", true)
	(*configs)[1].Properties["core"]["account"] = "changed@example.com"
	if err := m.syncGCPProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if r.Account.ID != id || r.Account.Enabled || r.Account.Status == "connected" {
		t.Fatal("identity change retained grants")
	}
	*configs = (*configs)[:1]
	if err := m.syncGCPProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if m.gcpProfileRecord("throwaway") != nil {
		t.Fatal("deleted configuration retained")
	}
	raw, _ := json.Marshal(m.records)
	if strings.Contains(string(raw), "sensitive-token") {
		t.Fatal("token persisted")
	}
}
func TestGCPDeleteProtectsSharedAccounts(t *testing.T) {
	m, _, _, calls := testGCPManager(t)
	mustHandle(t, m, Request{Action: "list"})
	r := m.gcpProfileRecord("throwaway")
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_delete", ID: r.Account.ID, ConfirmProfile: "keep"}); err == nil {
		t.Fatal("missing exact confirmation")
	}
	mustHandle(t, m, Request{Action: "cloud_delete", ID: r.Account.ID, ConfirmProfile: "throwaway"})
	for _, call := range *calls {
		if strings.HasPrefix(call, "auth revoke") {
			t.Fatal("revoked a shared credential")
		}
	}
	if m.gcpProfileRecord("keep") == nil {
		t.Fatal("other config removed")
	}
	backups, _ := filepath.Glob(filepath.Join(m.cloud.dir, "backups", "gcp-*", "config_throwaway"))
	if len(backups) != 1 {
		t.Fatal("backup absent")
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup mode")
	}
	orphan := m.gcpProfileRecord("account:unused@example.com")
	mustHandle(t, m, Request{Action: "cloud_delete", ID: orphan.Account.ID, ConfirmProfile: orphan.CLI.Profile})
	found := false
	for _, call := range *calls {
		if strings.HasPrefix(call, "auth revoke unused@example.com ") {
			found = true
		}
	}
	if !found {
		t.Fatal("exact orphan account not revoked")
	}
}
func TestGCPRunBothAuthorizationGates(t *testing.T) {
	m, _, _, calls := testGCPManager(t)
	mustHandle(t, m, Request{Action: "list"})
	r := m.gcpProfileRecord("throwaway")
	r.Account.Enabled = true
	q := Request{Action: "cloud_run", Integration: "gcp", Profile: "throwaway", CLIArgs: []string{"projects", "list"}}
	mustHandle(t, m, q)
	q.CLIArgs = []string{"projects", "delete", "example"}
	n := len(*calls)
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("writes disabled")
	}
	m.setGroup(r, "write", true)
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("missing task authorization")
	}
	if len(*calls) != n {
		t.Fatal("denied command reached CLI")
	}
	q.Authorize = true
	mustHandle(t, m, q)
	if len(*calls) != n+1 {
		t.Fatal("authorized command did not run once")
	}
}
func TestGCPCopyCodeFlowOnSharedCore(t *testing.T) {
	m, _, _, _ := testGCPManager(t)
	script := filepath.Join(t.TempDir(), "gcloud")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' 'https://accounts.google.com/o/oauth2/auth?client_id=vendor&response_type=code'\nread code\n"), 0700)
	m.cloud.providers["gcp"] = testGcloudCLI{path: script}
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "gcp", Profile: "throwaway"}}).Flow
	if !f.CodeRequired || f.AddressRequired || f.WebReturn {
		t.Fatal("wrong remote flow")
	}
	mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: "4/valid_authorization-code"})
	select {
	case <-m.connectFlows[f.ID].cli.done:
	case <-time.After(time.Second):
		t.Fatal("login blocked")
	}
	result := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
	if result.Account == nil || !result.Account.Enabled || result.Account.Profile != "throwaway" {
		t.Fatal("login not adopted")
	}
}
func TestRetiredGCPOAuthRemoved(t *testing.T) {
	m, _, _, _ := testGCPManager(t)
	m.records["old-gcp"] = &record{Account: Account{ID: "old-gcp", Integration: "gcp"}, OAuth: &oauthAccount{}}
	m.vault.Set("integration:old-gcp", "retired")
	m.vault.Set("oauth-client:gcp", "retired")
	if err := m.removeOldAWS(); err != nil {
		t.Fatal(err)
	}
	if m.records["old-gcp"] != nil {
		t.Fatal("old MCP card survived")
	}
	for _, key := range []string{"integration:old-gcp", "oauth-client:gcp"} {
		if _, ok, _ := m.vault.Get(key); ok {
			t.Fatal("old OAuth secret survived")
		}
	}
}
