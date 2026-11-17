package connections

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSSOLogin is an aws CLI whose `sso login` prints a device code, waits for
// the approval file, then writes the session's cache like the real CLI.
func fakeSSOLogin(t *testing.T, config string, refresh string) (script, approved, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	approved = filepath.Join(dir, "approved")
	cache := filepath.Join(filepath.Dir(config), "sso", "cache")
	script = filepath.Join(dir, "aws")
	body := `#!/bin/sh
echo "$@" > '` + argsFile + `'
session=""
while [ $# -gt 0 ]; do [ "$1" = "--sso-session" ] && session="$2"; shift; done
printf '%s\n' 'Then enter the code:' '' 'QRST-WXYZ' '' 'https://us-east-1.signin.aws/platform/authorize?request_uri=urn%3Aexample'
while [ ! -f '` + approved + `' ]; do sleep 0.05; done
mkdir -p '` + cache + `'
[ -n "$session" ] && cp '` + filepath.Join(dir, "token.json") + `' "` + cache + `/$(printf '%s' "$session" | sha1sum | cut -c1-40).json"
exit 0
`
	os.WriteFile(filepath.Join(dir, "token.json"), []byte(`{"accessToken":"portal-token","refreshToken":"`+refresh+`","expiresAt":"2026-11-18T10:00:00Z"}`), 0600)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return script, approved, argsFile
}

// fakePortal answers the access portal's account and role lists.
func fakePortal(t *testing.T, m *Manager) *[]string {
	t.Helper()
	seen := []string{}
	original := awsPortalBase
	awsPortalBase = func(region string) string { return "https://portal.test/" + region }
	t.Cleanup(func() { awsPortalBase = original })
	m.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery+" token="+r.Header.Get("x-amz-sso_bearer_token"))
		body := `{"accountList":[{"accountId":"111111111111","accountName":"Prod"},{"accountId":"222222222222","accountName":"Identity Admin"},{"accountId":"333333333333","accountName":"Sandbox"}]}`
		if strings.HasSuffix(r.URL.Path, "/assignment/roles") {
			if r.URL.Query().Get("account_id") == "111111111111" {
				body = `{"roleList":[{"roleName":"ProdAdmin","accountId":"111111111111"}]}`
			} else if r.URL.Query().Get("account_id") == "222222222222" {
				body = `{"roleList":[{"roleName":"AccountFullAccessRole","accountId":"222222222222"},{"roleName":"ReadOnly","accountId":"222222222222"}]}`
			} else {
				body = `{"roleList":[{"roleName":"Developer","accountId":"333333333333"}]}`
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	return &seen
}

func TestAWSAddAccountUsesIdentityCenterWhenTheConfigHasIt(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[sso-session work]\nsso_start_url = https://d-1234567890.awsapps.com/start\nsso_region = eu-west-1\nsso_registration_scopes = sso:account:access\n[profile prod-prodadmin]\nsso_session = work\nsso_account_id = 111111111111\nsso_role_name = ProdAdmin\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	script, approved, argsFile := fakeSSOLogin(t, config, "first")
	m.cloud.providers["aws"] = testAWSCLI{path: script}
	seen := fakePortal(t, m)

	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
	if f.UserCode != "QRST-WXYZ" || f.PortalRequired || f.CodeRequired {
		t.Fatalf("Add account = %+v", f)
	}
	if raw, _ := os.ReadFile(argsFile); !strings.HasPrefix(string(raw), "sso login --use-device-code --no-browser --sso-session work") {
		t.Fatalf("CLI ran %q", raw)
	}
	os.WriteFile(approved, nil, 0600)
	<-m.connectFlows[f.ID].cli.done
	start := time.Now()
	result := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
	if result.Flow.Status != "connected" || result.Account == nil {
		t.Fatalf("result = %+v", result.Flow)
	}
	if len(*seen) != 4 || !strings.HasPrefix((*seen)[0], "/eu-west-1/assignment/accounts?max_result=100 token=portal-token") {
		t.Fatalf("portal calls = %q", *seen)
	}
	raw, _ := os.ReadFile(config)
	// The existing profile is kept; each other assignment gets one, named for
	// its account, and for its role too where the account has several.
	if strings.Count(string(raw), "[profile ") != 4 || !strings.Contains(string(raw), "[profile identity-admin-readonly]\n") || !strings.Contains(string(raw), "[profile sandbox]\nsso_session = work\nsso_account_id = 333333333333\nsso_role_name = Developer\n") || !strings.Contains(string(raw), "[profile identity-admin-accountfullaccessrole]\nsso_session = work\nsso_account_id = 222222222222\nsso_role_name = AccountFullAccessRole\n") || strings.Contains(string(raw), "login_session") {
		t.Fatalf("config = %s", raw)
	}
	if result.Account.Profile != "identity-admin-accountfullaccessrole" || !result.Account.Enabled {
		t.Fatalf("connected %+v", result.Account)
	}
	// The sign-in is the session's: both profiles know when it started.
	for _, name := range []string{"prod-prodadmin", "identity-admin-accountfullaccessrole"} {
		r := m.awsProfileRecord(name)
		if r == nil || r.CLI.SignInAt == nil || r.CLI.SignInAt.Before(start.Add(-5*time.Second)) || r.Account.ProfileSignedInAt == nil {
			t.Fatalf("%s sign-in start = %+v", name, r)
		}
	}
}

func TestAWSAddAccountAsksForThePortalOrFallsBackToAWSLogin(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, nil, 0600)
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws"}}).Flow
	if !f.PortalRequired || f.UserCode != "" || f.AuthorizationURL != "" {
		t.Fatalf("no Identity Center known: %+v", f)
	}
	if again := mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID}).Flow; again.Status != "waiting" || !again.PortalRequired {
		t.Fatalf("waiting for the portal: %+v", again)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws", SSOStartURL: "https://evil.example/start"}}); err == nil {
		t.Fatal("a foreign address became a portal")
	}
	script, _, argsFile := fakeSSOLogin(t, config, "first")
	m.cloud.providers["aws"] = testAWSCLI{path: script}
	sso := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws", SSOStartURL: "https://ssoins-72237cadedf53e0b.portal.us-east-1.app.aws/"}}).Flow
	if sso.UserCode != "QRST-WXYZ" {
		t.Fatalf("portal sign-in = %+v", sso)
	}
	raw, _ := os.ReadFile(config)
	if string(raw) != "[sso-session ssoins-72237cadedf53e0b]\nsso_start_url = https://ssoins-72237cadedf53e0b.portal.us-east-1.app.aws\nsso_region = us-east-1\nsso_registration_scopes = sso:account:access\n" {
		t.Fatalf("config = %q", raw)
	}
	if args, _ := os.ReadFile(argsFile); !strings.Contains(string(args), "--sso-session ssoins-72237cadedf53e0b") {
		t.Fatalf("CLI ran %q", args)
	}
	mustHandle(t, m, Request{Action: "connect_cancel", FlowID: sso.ID})
	// An account without Identity Center: aws login, the fallback.
	login := filepath.Join(t.TempDir(), "aws")
	os.WriteFile(login, []byte("#!/bin/sh\necho \"$@\" > '"+argsFile+"'\nprintf '%s\\n' 'https://us-east-1.signin.aws.amazon.com/v1/authorize?client_id=arn:aws:signin:::devtools/cross-device&state=expected'\nread code\n"), 0700)
	m.cloud.providers["aws"] = testAWSCLI{path: login}
	plain := mustHandle(t, m, Request{Action: "connect_start", Input: &Input{Integration: "aws", AWSLogin: true}}).Flow
	if !plain.CodeRequired || plain.UserCode != "" {
		t.Fatalf("aws login fallback = %+v", plain)
	}
	if args, _ := os.ReadFile(argsFile); !strings.HasPrefix(string(args), "login --remote --profile mewla-") {
		t.Fatalf("fallback ran %q", args)
	}
	mustHandle(t, m, Request{Action: "connect_cancel", FlowID: plain.ID})
}

func TestAWSPortalAddresses(t *testing.T) {
	for _, c := range []struct{ in, region, url, want string }{
		{"https://d-1234567890.awsapps.com/start", "", "https://d-1234567890.awsapps.com/start", "us-east-1"},
		{"https://d-1234567890.awsapps.com/start/#/", "eu-west-1", "https://d-1234567890.awsapps.com/start", "eu-west-1"},
		{"https://ssoins-72237cadedf53e0b.portal.us-east-1.app.aws", "", "https://ssoins-72237cadedf53e0b.portal.us-east-1.app.aws", "us-east-1"},
		{"https://ssoins-72237cadedf53e0b.ap-southeast-1.portal.amazonaws.com", "eu-west-1", "https://ssoins-72237cadedf53e0b.ap-southeast-1.portal.amazonaws.com", "ap-southeast-1"},
	} {
		got, region, err := awsPortal(c.in, c.region)
		if err != nil || got != c.url || region != c.want {
			t.Errorf("%s = %q %q %v", c.in, got, region, err)
		}
	}
	for _, bad := range []string{"http://d-1234567890.awsapps.com/start", "https://evil.example/start", "https://user@d-1.awsapps.com/start", "https://a.b.awsapps.com/start", "not a url"} {
		if _, _, err := awsPortal(bad, ""); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, _, err := awsPortal("https://d-1234567890.awsapps.com/start", "us-east-1; rm"); err == nil {
		t.Error("accepted a bad region")
	}
}

func writeSSOCache(t *testing.T, config, session, refresh string) {
	t.Helper()
	sum := sha1.Sum([]byte(session))
	dir := filepath.Join(filepath.Dir(config), "sso", "cache")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), []byte(`{"accessToken":"a","refreshToken":"`+refresh+`","expiresAt":"2026-11-18T10:00:00Z","registrationExpiresAt":"2027-02-15T16:28:15Z"}`), 0600)
}

// The card shows when a sign-in really ends: its start and the organization's
// length. The length is learned from a sign-in Mewla started that ended, or set.
func TestAWSSignInEndIsLearnedOrSetNeverTheRegistration(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	os.WriteFile(config, []byte("[sso-session work]\nsso_start_url = https://d-1234567890.awsapps.com/start\n[profile a]\nsso_session = work\n[profile b]\nsso_session = work\n"), 0600)
	writeSSOCache(t, config, "work", "first")
	mustHandle(t, m, Request{Action: "list"})
	a, b := m.awsProfileRecord("a"), m.awsProfileRecord("b")
	a.Account.Enabled = true
	if a.Account.ProfileSignInBy != nil || a.Account.ProfileSignedInAt != nil {
		t.Fatal("a terminal sign-in got an end or a start")
	}
	start := time.Now().Add(-8*time.Hour - 10*time.Minute)
	m.awsSSOSignedIn("work", start)
	if a.Account.ProfileSignedInAt == nil || b.CLI.SignInAt == nil || a.Account.ProfileSignInBy != nil {
		t.Fatalf("start known, length not: %+v", a.Account)
	}
	// The organization's 8 h session ends; the next check finds it expired.
	m.cloud.run = func(_ context.Context, _ string, args, _ []string) (cloudOutput, error) {
		return cloudOutput{ExitCode: 255, Stderr: "Error when retrieving token from sso: Token has expired and refresh failed"}, nil
	}
	a.Account.ProfileStatus = "valid"
	m.cloud.checked = map[string]time.Time{}
	if err := m.syncAWSProfiles(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if b.CLI.SessionLength != int64(8*time.Hour/time.Second) || a.Account.ProfileSessionLength != b.CLI.SessionLength {
		t.Fatalf("learned length = %d", b.CLI.SessionLength)
	}
	// A new sign-in by Mewla: its end is its start plus the learned length.
	writeSSOCache(t, config, "work", "second")
	again := time.Now()
	m.awsSSOSignedIn("work", again)
	if a.Account.ProfileSignInBy == nil || !a.Account.ProfileSignInBy.Equal(again.Add(8*time.Hour)) || a.Account.ProfileSignInLimit != "session" {
		t.Fatalf("end = %v %q", a.Account.ProfileSignInBy, a.Account.ProfileSignInLimit)
	}
	// The person says their organization allows 90 days.
	out := mustHandle(t, m, Request{Action: "cloud_session_length", ID: b.Account.ID, Seconds: int64(90 * 24 * time.Hour / time.Second)})
	if out.Account == nil || out.Account.ProfileSignInBy == nil || !out.Account.ProfileSignInBy.Equal(again.Add(90*24*time.Hour)) || a.CLI.SessionLength != b.CLI.SessionLength {
		t.Fatalf("set length = %+v", out.Account)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "cloud_session_length", ID: b.Account.ID, Seconds: 1234}); err == nil {
		t.Fatal("a length Identity Center does not offer")
	}
	// With 90 days the day-before warning is due a day before the real end.
	var notices []SignInLost
	m.SetSignInLost(func(n SignInLost) { notices = append(notices, n) })
	a.Account.ProfileStatus = "valid"
	m.noteAWSSignIns(again.Add(88 * 24 * time.Hour))
	m.noteAWSSignIns(again.Add(89*24*time.Hour + time.Hour))
	if len(notices) != 1 || notices[0].Title != "AWS sign-in ends soon" {
		t.Fatalf("notices = %+v", notices)
	}
	// An 8 h organization would be warned about every sign-in: only its end is said.
	m.setAWSSessionLength("work", 8*time.Hour)
	a.CLI.Notified = ""
	end := again.Add(8 * time.Hour)
	a.Account.ProfileSignInBy = &end
	m.noteAWSSignIns(again.Add(7 * time.Hour))
	if len(notices) != 1 {
		t.Fatal("warned about an 8 h sign-in")
	}
}

func TestAWSSessionLengthSnapsToIdentityCenterPresets(t *testing.T) {
	for observed, want := range map[time.Duration]time.Duration{
		8*time.Hour + 25*time.Minute: 8 * time.Hour,
		7*time.Hour + 58*time.Minute: 8 * time.Hour,
		7*time.Hour + 30*time.Minute: 0,
		90*24*time.Hour + time.Hour:  90 * 24 * time.Hour,
		29 * time.Minute:             0,
	} {
		if got := awsSessionLengthOf(observed); got != want {
			t.Errorf("%v → %v, want %v", observed, got, want)
		}
	}
}
