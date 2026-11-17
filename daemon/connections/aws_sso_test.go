package connections

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAWSDeviceCodeParsing(t *testing.T) {
	p := awsCLI{}
	classic := "Browser will not be automatically opened.\nPlease visit the following URL:\n\nhttps://device.sso.us-east-1.amazonaws.com/\n\nThen enter the code:\n\nABCD-EFGH\n\nAlternatively, you may visit the following URL which will autofill the code upon click:\nhttps://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH\n"
	if address, code := p.deviceCode(classic); code != "ABCD-EFGH" || address != "https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH" {
		t.Fatalf("classic = %q %q", address, code)
	}
	portal := "Then enter the code:\n\nWXYZ-1234\n\nhttps://d-1234567890.awsapps.com/start/#/device?user_code=WXYZ-1234\n"
	if address, code := p.deviceCode(portal); code != "WXYZ-1234" || !strings.HasPrefix(address, "https://d-1234567890.awsapps.com/start/") {
		t.Fatalf("portal = %q %q", address, code)
	}
	// AWS CLI 2.40.4 against an Identity Center instance without a d- alias
	// (2026-11-18; request URI and code replaced): no user_code in the address.
	real := "Browser will not be automatically opened.\nPlease visit the following URL:\n\nhttps://us-east-1.signin.aws/platform/authorize?request_uri=urn%3Aietf%3Aparams%3Aoauth%3Arequest_uri%3AAAAAexample&identitystore_id=2488e418-0000-0000-90cc-6215cc6e0438\n\nThen enter the code:\n\nQRST-WXYZ\n\nAlternatively, you may visit the following URL which will autofill the code upon loading:\nhttps://us-east-1.signin.aws/platform/authorize?request_uri=urn%3Aietf%3Aparams%3Aoauth%3Arequest_uri%3AAAAAexample&identitystore_id=2488e418-0000-0000-90cc-6215cc6e0438\n"
	if address, code := p.deviceCode(real); code != "QRST-WXYZ" || !strings.HasPrefix(address, "https://us-east-1.signin.aws/platform/authorize?request_uri=") {
		t.Fatalf("CLI 2.40 = %q %q", address, code)
	}
	// Until the code is printed there is nothing to show.
	if _, code := p.deviceCode(real[:strings.Index(real, "Then enter")]); code != "" {
		t.Fatal("address shown without its code")
	}
	for _, foreign := range []string{
		"Then enter the code:\n\nABCD-EFGH\n\nhttps://evil.example/?x=1\n",
		"https://evil.example/?user_code=ABCD-EFGH",
		"http://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH",
		"https://device.sso.us-east-1.amazonaws.com/?user_code=<script>",
		"https://us-east-1.signin.aws.amazon.com/v1/authorize?client_id=arn:aws:signin:::devtools/cross-device&state=x",
	} {
		if _, code := p.deviceCode(foreign); code != "" {
			t.Fatalf("accepted %q", foreign)
		}
	}
}

func TestAWSSignInMatchesProfileKind(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[sso-session work]\nsso_start_url = https://d-1234567890.awsapps.com/start\nsso_region = us-east-1\n[profile sso]\nsso_session = work\nsso_account_id = 123456789012\nsso_role_name = Admin\n[profile legacy]\nsso_start_url = https://d-1234567890.awsapps.com/start\n[profile console]\nlogin_session = arn:aws:iam::123456789012:root\n"), 0600)
	p := awsCLI{}
	for _, profile := range []string{"sso", "legacy"} {
		if got := strings.Join(p.loginArgsFor(m, profile), " "); got != "sso login --use-device-code --no-browser --profile "+profile+" --no-cli-auto-prompt" {
			t.Fatalf("%s signs in with %q", profile, got)
		}
	}
	// aws login on an SSO profile would rewrite it into a 12 h login session.
	for _, profile := range []string{"console", "new-profile"} {
		if got := p.loginArgsFor(m, profile); got[0] != "login" || !strings.Contains(strings.Join(got, " "), "--remote") {
			t.Fatalf("%s signs in with %v", profile, got)
		}
	}
}

func TestAWSSSOSignInIsADeviceCodeWithNoPaste(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[sso-session work]\nsso_start_url = https://d-1234567890.awsapps.com/start\nsso_region = us-east-1\n[profile work]\nsso_session = work\nsso_account_id = 123456789012\nsso_role_name = Admin\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	id := m.awsProfileRecord("work").Account.ID
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	approved := filepath.Join(dir, "approved")
	script := filepath.Join(dir, "aws")
	// Like the CLI: print the code, wait for the approval, exit 0.
	os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" > '"+argsFile+"'\nprintf '%s\\n' 'Then enter the code:' '' 'ABCD-EFGH' '' 'https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH'\nwhile [ ! -f '"+approved+"' ]; do sleep 0.05; done\n"), 0700)
	m.cloud.providers["aws"] = testAWSCLI{path: script}
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws", AccountID: id}}).Flow
	if f.UserCode != "ABCD-EFGH" || f.CodeRequired || f.AddressRequired || f.AuthorizationURL != "https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH" {
		t.Fatalf("flow = %+v", f)
	}
	if raw, _ := os.ReadFile(argsFile); !strings.HasPrefix(string(raw), "sso login --use-device-code --no-browser --profile work") {
		t.Fatalf("CLI ran %q", raw)
	}
	if got := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID}).Flow; got.Status != "waiting" {
		t.Fatalf("finished before approval: %+v", got)
	}
	os.WriteFile(approved, nil, 0600)
	<-m.connectFlows[f.ID].cli.done
	result := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
	if result.Flow.Status != "connected" || result.Account == nil || result.Account.ID != id {
		t.Fatalf("result = %+v", result.Flow)
	}
	if raw, _ := os.ReadFile(config); !strings.Contains(string(raw), "sso_session = work") || strings.Contains(string(raw), "login_session") {
		t.Fatal("SSO profile changed by its sign-in")
	}
}

func TestAWSSignInNoticesOncePerSignIn(t *testing.T) {
	m, _, _, _ := testCloudManager(t)
	var notices []SignInLost
	m.SetSignInLost(func(n SignInLost) { notices = append(notices, n) })
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	verified := now.Add(-time.Hour)
	end := now.Add(20 * time.Hour)
	r := &record{CLI: &cloudAccount{Profile: "prod"}, Account: Account{ID: "aws-prod", Integration: "aws", Name: "prod", Profile: "prod", Enabled: true, ProfileKind: "SSO", ProfileStatus: "valid", VerifiedAt: &verified, ProfileSignInBy: &end, ProfileSignInLimit: "maximum"}}
	m.records = map[string]*record{r.Account.ID: r}

	far := now.Add(30 * 24 * time.Hour)
	r.Account.ProfileSignInBy = &far
	if m.noteAWSSignIns(now) || len(notices) != 0 {
		t.Fatal("warned a month ahead")
	}
	r.Account.ProfileSignInBy = &end
	m.noteAWSSignIns(now)
	m.noteAWSSignIns(now.Add(time.Hour))
	if len(notices) != 1 || notices[0].Title != "AWS sign-in ends soon" || !strings.Contains(notices[0].Body, "Extensions › AWS › prod") {
		t.Fatalf("soon notices = %+v", notices)
	}
	r.Account.ProfileStatus = "expired"
	m.noteAWSSignIns(now.Add(21 * time.Hour))
	m.noteAWSSignIns(now.Add(22 * time.Hour))
	if len(notices) != 2 || notices[1].Title != "AWS sign-in ended" || !strings.Contains(notices[1].Body, "approve the code") {
		t.Fatalf("ended notices = %+v", notices)
	}
	// Signing in again re-arms both notices for the new sign-in.
	again := now.Add(23 * time.Hour)
	r.Account.ProfileStatus, r.Account.VerifiedAt = "expired", &again
	m.noteAWSSignIns(now.Add(24 * time.Hour))
	if len(notices) != 3 {
		t.Fatalf("new sign-in's end not told: %d", len(notices))
	}

	// aws login lasts at most 12 h: no day-ahead warning, and its end says why.
	login := &record{CLI: &cloudAccount{Profile: "console"}, Account: Account{ID: "aws-console", Integration: "aws", Profile: "console", Enabled: true, ProfileKind: "aws login", ProfileStatus: "valid", VerifiedAt: &verified, ProfileSignInBy: &end, ProfileSignInLimit: "maximum"}}
	m.records = map[string]*record{login.Account.ID: login}
	m.noteAWSSignIns(now)
	if len(notices) != 3 {
		t.Fatal("warned about a 12 h aws login")
	}
	login.Account.ProfileStatus = "expired"
	m.noteAWSSignIns(now)
	if len(notices) != 4 || !strings.Contains(notices[3].Body, "12 hours") || !strings.Contains(notices[3].Body, "IAM Identity Center") {
		t.Fatalf("aws login end = %+v", notices[3])
	}
	// Off profiles and ones that never worked stay quiet.
	login.Account.Enabled = false
	login.CLI.Notified = ""
	never := &record{CLI: &cloudAccount{Profile: "never"}, Account: Account{ID: "aws-never", Integration: "aws", Profile: "never", Enabled: true, ProfileKind: "SSO", ProfileStatus: "expired"}}
	m.records[never.Account.ID] = never
	m.noteAWSSignIns(now)
	if len(notices) != 4 {
		t.Fatal("quiet profile told Chat")
	}
}

func TestAWSExpiredCommandTellsChat(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[profile prod]\nsso_session = work\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	r := m.awsProfileRecord("prod")
	r.Account.Enabled = true
	var notices []SignInLost
	m.SetSignInLost(func(n SignInLost) { notices = append(notices, n) })
	m.cloud.run = func(_ context.Context, _ string, args, _ []string) (cloudOutput, error) {
		return cloudOutput{ExitCode: 255, Stderr: "Error when retrieving token from sso: Token has expired and refresh failed"}, nil
	}
	m.cloud.checked["prod"] = time.Now()
	out, err := m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "aws", CLIArgs: []string{"s3", "ls"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.CLIOutput.Stderr, "Token has expired") || !strings.Contains(out.CLIOutput.Stderr, "Extensions › AWS › prod › Sign in") {
		t.Fatalf("an agent is not told where the fix is: %q", out.CLIOutput.Stderr)
	}
	if len(notices) != 1 || notices[0].Title != "AWS sign-in ended" {
		t.Fatalf("notices = %+v", notices)
	}
}

// A command for a known profile must not wait for every profile's identity
// check: right after an SSO token expires each check renews it, which takes
// seconds, and the command ran out of time behind them.
func TestAWSCommandDoesNotWaitForIdentityChecks(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[profile prod]\nsso_session = work\n[profile other]\nsso_session = work\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	m.snapshots.wg.Wait()
	m.awsProfileRecord("prod").Account.Enabled = true
	release := make(chan struct{})
	t.Cleanup(func() { close(release); m.Close() })
	m.cloud.run = func(ctx context.Context, _ string, args, _ []string) (cloudOutput, error) {
		if args[0] == "sts" {
			select { // an identity check that is renewing a token
			case <-release:
			case <-ctx.Done():
				return cloudOutput{}, ctx.Err()
			}
		}
		return cloudOutput{Stdout: "ok"}, nil
	}
	m.cloud.checked = map[string]time.Time{}
	done := make(chan Response, 1)
	go func() {
		out, err := m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "aws", Profile: "prod", CLIArgs: []string{"s3", "ls"}})
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	select {
	case out := <-done:
		if out.CLIOutput == nil || out.CLIOutput.Stdout != "ok" {
			t.Fatalf("command = %+v", out.CLIOutput)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the command waited for identity checks")
	}
}

func TestAWSCommandForUnknownProfileDiscoversItFirst(t *testing.T) {
	m, config, _, calls := testCloudManager(t)
	os.WriteFile(config, []byte("[profile prod]\nsso_session = work\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	m.snapshots.wg.Wait()
	m.awsProfileRecord("prod").Account.Enabled = true
	os.WriteFile(config, []byte("[profile prod]\nsso_session = work\n[profile fresh]\nsso_session = work\n"), 0600)
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_run", Integration: "aws", Profile: "fresh", CLIArgs: []string{"s3", "ls"}}); err == nil || !strings.Contains(err.Error(), "Enable a profile") {
		t.Fatalf("unknown profile = %v", err)
	}
	if m.awsProfileRecord("fresh") == nil {
		t.Fatalf("profile added in a terminal was not discovered: %v", *calls)
	}
}
