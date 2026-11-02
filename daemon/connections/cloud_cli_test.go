package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAWSCLIPolicy(t *testing.T) {
	for _, args := range [][]string{{"ec2", "describe-instances"}, {"s3", "ls"}, {"sts", "get-caller-identity"}} {
		read, err := awsCLI{}.classify(args)
		if err != nil || !read {
			t.Fatalf("read refused: %v", args)
		}
	}
	for _, args := range [][]string{{"ec2", "terminate-instances"}, {"sts", "get-session-token"}, {"secretsmanager", "get-secret-value"}, {"ssm", "get-parameters"}, {"s3api", "get-object"}, {"lambda", "get-function"}, {"madeup", "get-secret"}} {
		read, err := awsCLI{}.classify(args)
		if err != nil || read {
			t.Fatalf("unsafe classification: %v", args)
		}
	}
	for _, args := range [][]string{{"configure", "export-credentials"}, {"ec2", "describe-instances", "--endpoint-url=https://evil.test"}, {"ec2", "describe-instances", "--endp", "https://evil.test"}, {"ec2", "describe-instances", "--pro", "other"}, {"ec2", "describe-instances", "--cli-input-json", "file:///etc/passwd"}, {"ec2;echo", "describe-instances"}, {"login", "--remote"}} {
		if _, err := (awsCLI{}).classify(args); err == nil {
			t.Fatalf("override accepted: %v", args)
		}
	}
}
func TestAWSRemoteCodeBinding(t *testing.T) {
	p := awsCLI{}
	address := "https://us-east-1.signin.aws.amazon.com/v1/authorize?client_id=arn:aws:signin:::devtools/cross-device&state=expected"
	if p.loginURL("Open:\n"+address+"\nPaste code:") != address {
		t.Fatal("CLI URL not parsed")
	}
	for _, query := range []string{"code=x&state=wrong", "code=x&state=expected&state=other", "code=x&code=y&state=expected"} {
		if p.validateCode(address, base64.StdEncoding.EncodeToString([]byte(query))) {
			t.Fatal("foreign/ambiguous code accepted")
		}
	}
	if !p.validateCode(address, base64.StdEncoding.EncodeToString([]byte("code=x&state=expected"))) {
		t.Fatal("own code refused")
	}
	if strings.Contains(strings.Join(p.loginArgs("test"), " "), "redirect") || !strings.Contains(strings.Join(p.loginArgs("test"), " "), "--remote") {
		t.Fatal("login is not always remote")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "do-not-inherit")
	t.Setenv("AWS_ENDPOINT_URL", "https://evil.test")
	for _, env := range p.environment() {
		if strings.Contains(env, "do-not-inherit") || strings.Contains(env, "evil.test") {
			t.Fatal("inherited credential override")
		}
	}
}
func testCloudManager(t *testing.T) (*Manager, string, string, *[]string) {
	t.Helper()
	m := testManager(t, func(*http.Request) (*http.Response, error) { t.Fatal("MCP/HTTP should not run"); return nil, nil })
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	creds := filepath.Join(dir, "credentials")
	m.cloud = newCloudRuntime(filepath.Join(dir, "cli"))
	m.cloud.profileFiles = func() (string, string, error) { return config, creds, nil }
	calls := []string{}
	var mu sync.Mutex
	m.cloud.providers["aws"] = testAWSCLI{path: "/bin/true"}
	m.cloud.run = func(_ context.Context, _ string, args, env []string) (cloudOutput, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 1 && args[0] == "sts" {
			return cloudOutput{Stdout: `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:root","UserId":"root"}`}, nil
		}
		return cloudOutput{Stdout: `{"AccountAliases":["Test account"]}`}, nil
	}
	return m, config, creds, &calls
}

type testAWSCLI struct {
	awsCLI
	path string
}

func (p testAWSCLI) binary() string { return p.path }
func TestCloudProfileListAndDeleteAreScoped(t *testing.T) {
	m, config, creds, calls := testCloudManager(t)
	original := "# keep comment\n[profile keep]\nregion = eu-west-1\n[profile throwaway]\nlogin_session = test-session\n[services private]\nfoo = bar\n"
	if err := os.WriteFile(config, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(creds, []byte("[keep]\naws_access_key_id = retained\n[throwaway]\naws_access_key_id = deleted\n"), 0600)
	first := mustHandle(t, m, Request{Action: "list"})
	if len(first.Accounts) != 2 {
		t.Fatal("profiles not listed")
	}
	r := m.awsProfileRecord("throwaway")
	id := r.Account.ID
	if r.Account.Enabled || r.Account.ProfileStatus != "valid" || r.Account.ProfileKind != "aws login" {
		t.Fatal("profile not verified/disabled by default")
	}
	count := len(*calls)
	mustHandle(t, m, Request{Action: "list"})
	if len(*calls) != count {
		t.Fatal("identity cache not used")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_delete", ID: id, ConfirmProfile: "keep"}); err == nil {
		t.Fatal("delete did not require matching confirmation")
	}
	mustHandle(t, m, Request{Action: "cloud_delete", ID: id, ConfirmProfile: "throwaway"})
	after, _ := os.ReadFile(config)
	if string(after) != "# keep comment\n[profile keep]\nregion = eu-west-1\n[services private]\nfoo = bar\n" {
		t.Fatalf("other config altered: %s", after)
	}
	after, _ = os.ReadFile(creds)
	if string(after) != "[keep]\naws_access_key_id = retained\n" {
		t.Fatal("other credentials altered")
	}
	backups, _ := filepath.Glob(filepath.Join(m.cloud.dir, "backups", "*", "config"))
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != original {
		t.Fatal("backup changed")
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup permissions")
	}
	// A terminal deletion removes the card and its grants on the next list.
	os.WriteFile(config, []byte("[services private]\nfoo=bar\n"), 0600)
	os.WriteFile(creds, nil, 0600)
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 0 {
		t.Fatal("CLI deletion not reflected")
	}
}
func TestCloudRunUsesProfileAndBothGates(t *testing.T) {
	m, config, _, calls := testCloudManager(t)
	os.WriteFile(config, []byte("[profile test]\nregion=us-east-1\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	r := m.awsProfileRecord("test")
	r.Account.Enabled = true
	q := Request{Action: "cloud_run", Integration: "aws", Profile: "test", CLIArgs: []string{"ec2", "describe-instances"}}
	if result := mustHandle(t, m, q); result.CLIOutput == nil {
		t.Fatal("CLI text not returned")
	}
	if !strings.Contains((*calls)[len(*calls)-1], "--profile test") {
		t.Fatal("profile not bound")
	}
	q.CLIArgs = []string{"ec2", "terminate-instances", "--instance-ids", "i-test"}
	count := len(*calls)
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("ungranted execute allowed")
	}
	m.setGroup(r, "write", true)
	if _, err := m.Handle(context.Background(), q); err == nil {
		t.Fatal("task authorization missing")
	}
	if len(*calls) != count {
		t.Fatal("denied execute reached CLI")
	}
	q.Authorize = true
	mustHandle(t, m, q)
	if len(*calls) != count+1 {
		t.Fatal("authorized command not executed once")
	}
}
func TestCloudLoginCancellationAndNoReplay(t *testing.T) {
	// A disposable script behaves like an interactive vendor CLI. Production
	// still invokes the installed vendor binary directly, never command text.
	m, _, _, _ := testCloudManager(t)
	script := filepath.Join(t.TempDir(), "aws")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' 'https://us-east-1.signin.aws.amazon.com/v1/authorize?client_id=arn:aws:signin:::devtools/cross-device&state=expected'\nread code\n"), 0700)
	m.cloud.providers["aws"] = testAWSCLI{path: script}
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws", Profile: "throwaway"}}).Flow
	if !f.CodeRequired || f.AddressRequired || f.WebReturn {
		t.Fatal("wrong login UI")
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Callback: "bad"}); err == nil {
		t.Fatal("bad code accepted")
	}
	login := m.connectFlows[f.ID].cli
	mustHandle(t, m, Request{Action: "connect_cancel", FlowID: f.ID})
	select {
	case <-login.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel leaked child")
	}
	result := mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: base64.StdEncoding.EncodeToString([]byte("code=x&state=expected"))})
	if result.Flow.Status != "cancelled" {
		t.Fatal("cancelled login resumed")
	}
}
func TestRetiredAWSOAuthRemoved(t *testing.T) {
	m, _, _, _ := testCloudManager(t)
	m.records["old"] = &record{Account: Account{ID: "old", Integration: "aws"}, OAuth: &oauthAccount{}}
	m.vault.Set("integration:old", "secret")
	m.vault.Set("oauth-registration:aws", "retired")
	if err := m.removeOldAWS(); err != nil {
		t.Fatal(err)
	}
	if len(m.records) != 0 {
		t.Fatal("old card survived")
	}
	if _, ok, _ := m.vault.Get("integration:old"); ok {
		t.Fatal("old grant survived")
	}
}
func TestCloudOutputNeverBecomesHistory(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[profile test]\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	r := m.awsProfileRecord("test")
	r.Account.Enabled = true
	m.cloud.run = func(context.Context, string, []string, []string) (cloudOutput, error) {
		return cloudOutput{Stdout: "secret-diagnostic-value"}, nil
	}
	mustHandle(t, m, Request{Action: "cloud_run", Integration: "aws", CLIArgs: []string{"s3", "ls"}})
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "secret-diagnostic-value") {
		t.Fatal("output persisted")
	}
}

func TestCloudCompletedLoginReusesProfileAndCodeOnce(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[profile test]\nregion=us-east-1\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	id := m.awsProfileRecord("test").Account.ID
	script := filepath.Join(t.TempDir(), "aws")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' 'https://us-east-1.signin.aws.amazon.com/v1/authorize?client_id=arn:aws:signin:::devtools/cross-device&state=expected'\nread code\n"), 0700)
	m.cloud.providers["aws"] = testAWSCLI{path: script}
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws", AccountID: id}}).Flow
	code := base64.StdEncoding.EncodeToString([]byte("code=test&state=expected"))
	mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: code})
	<-m.connectFlows[f.ID].cli.done
	result := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
	if result.Account == nil || result.Account.ID != id || !result.Account.Enabled || len(m.records) != 1 {
		t.Fatal("login duplicated or failed to adopt profile")
	}
	replay := mustHandle(t, m, Request{Action: "connect_finish", FlowID: f.ID, Callback: code})
	if replay.Account.ID != id || len(m.records) != 1 {
		t.Fatal("replay changed account")
	}
}
func TestDeleteKeepsSharedLoginAndOtherSections(t *testing.T) {
	m, config, creds, calls := testCloudManager(t)
	os.WriteFile(config, []byte("[profile throwaway]\nlogin_session=shared\n[profile keep] # comment\nlogin_session=shared\n"), 0600)
	os.WriteFile(creds, nil, 0600)
	mustHandle(t, m, Request{Action: "list"})
	r := m.awsProfileRecord("throwaway")
	mustHandle(t, m, Request{Action: "cloud_delete", ID: r.Account.ID, ConfirmProfile: "throwaway"})
	for _, call := range *calls {
		if strings.HasPrefix(call, "logout") {
			t.Fatal("deleted another profile's shared session")
		}
	}
	raw, _ := os.ReadFile(config)
	if string(raw) != "[profile keep] # comment\nlogin_session=shared\n" {
		t.Fatal("commented section header lost")
	}
}
