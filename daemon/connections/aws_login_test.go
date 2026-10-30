package connections

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	awsSignInPage = "https://us-east-1.signin.aws.amazon.com/v1/authorize?response_type=code&client_id=arn%3Aaws%3Asignin%3A%3A%3Adevtools%2Fcross-device&state=abc&code_challenge_method=SHA-256&scope=openid"
	awsGoodCode   = "eyJhbGciOiJSUzI1NiJ9.good-code-0001"
)

// ---- what the CLI prints ------------------------------------------------

func TestAWSLoginOutputLogin(t *testing.T) {
	o := awsLoginOutput{mode: awsModeLogin}
	for _, line := range []string{"Browser will not be automatically opened.", "Please visit the following URL:", ""} {
		if o.feed(line) {
			t.Fatalf("ready after %q", line)
		}
	}
	if !o.feed(awsSignInPage) || o.url != awsSignInPage || o.code != "" {
		t.Fatalf("%+v", o)
	}
	// The prompt that follows is not a code.
	o.feed("Enter the authorization code displayed in your browser: ")
	if o.code != "" {
		t.Fatalf("%+v", o)
	}
}

func TestAWSLoginOutputSSO(t *testing.T) {
	o := awsLoginOutput{mode: awsModeSSO}
	lines := []string{
		"Browser will not be automatically opened.",
		"Please visit the following URL:",
		"",
		"https://device.sso.us-east-1.amazonaws.com/",
		"",
		"Then enter the code:",
		"",
	}
	for _, line := range lines {
		if o.feed(line) {
			t.Fatalf("ready too soon at %q: %+v", line, o)
		}
	}
	if !o.feed("ABCD-EFGH") || o.url != "https://device.sso.us-east-1.amazonaws.com/" || o.code != "ABCD-EFGH" {
		t.Fatalf("%+v", o)
	}
	// The second URL carries the code itself; the plain page stays the one shown.
	o.feed("https://device.sso.us-east-1.amazonaws.com/?user_code=ABCD-EFGH")
	if o.url != "https://device.sso.us-east-1.amazonaws.com/" {
		t.Fatalf("%+v", o)
	}
	// The code may come before the URL.
	p := awsLoginOutput{mode: awsModeSSO}
	p.feed("ABCD-EFGH")
	if !p.feed("https://corp.awsapps.com/start/#/device") || p.url == "" {
		t.Fatalf("%+v", p)
	}
}

func TestAWSSignInURLOnlyAWS(t *testing.T) {
	for _, c := range []struct {
		mode, line string
		ok         bool
	}{
		{awsModeLogin, awsSignInPage, true},
		{awsModeLogin, "https://eu-west-1.signin.aws.amazon.com/v1/authorize?x=1", true},
		{awsModeLogin, "https://signin.aws.amazon.com/", true},
		{awsModeLogin, "https://signin.amazonaws.cn/x", true},
		{awsModeLogin, "http://us-east-1.signin.aws.amazon.com/x", false},
		{awsModeLogin, "https://evil.example/?u=signin.aws.amazon.com", false},
		{awsModeLogin, "https://signin.aws.amazon.com.evil.example/", false},
		{awsModeLogin, "https://user@us-east-1.signin.aws.amazon.com/x", false},
		{awsModeLogin, "https://device.sso.us-east-1.amazonaws.com/", false},
		{awsModeSSO, "https://device.sso.us-east-1.amazonaws.com/", true},
		{awsModeSSO, "https://d-123.awsapps.com/start", true},
		{awsModeSSO, "https://amazonaws.com.evil.example/", false},
		{awsModeSSO, "https://evil.example/", false},
	} {
		if got := awsSignInURL(c.mode, c.line) != ""; got != c.ok {
			t.Errorf("%s %s: %v", c.mode, c.line, got)
		}
	}
}

func TestValidAWSCode(t *testing.T) {
	for code, ok := range map[string]bool{
		awsGoodCode:                   true,
		"short":                       false,
		"has space in it ok":          false,
		"line\nbreak-and-more-text":   false,
		"tab\tbreak-and-more-text":    false,
		"":                            false,
		"é-not-ascii-but-long-enough": false,
		strings.Repeat("a", 4097):     false,
		strings.Repeat("a", 4096):     true,
	} {
		if validAWSCode(code) != ok {
			t.Errorf("%q: want %v", code, ok)
		}
	}
}

// ---- a CLI that can be scripted -------------------------------------------

// fakeLoginHost plays the AWS CLI's sign-in: it prints, then waits for the
// pasted code (login) or for approval (sso).
type fakeLoginHost struct {
	t        *testing.T
	print    []string
	needCode bool
	// verify decides what the CLI does with a code; nil accepts it.
	verify func(code string) error
	// onSuccess runs just before a clean exit, where the CLI saves its profile.
	onSuccess func()

	mu       sync.Mutex
	runs     []*fakeLoginRun
	approve  chan struct{}
	failSelf error // exit at once with this error, printing nothing
}

type fakeLoginRun struct {
	args, env []string
	codes     chan string
	killed    atomic.Bool
	killCh    chan struct{}
	stdinR    *io.PipeReader
	done      chan struct{}
}

func (h *fakeLoginHost) spawn(args, env []string) (*awsProc, error) {
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	run := &fakeLoginRun{args: args, env: env, codes: make(chan string, 4), killCh: make(chan struct{}), stdinR: stdinR, done: make(chan struct{})}
	h.mu.Lock()
	h.runs = append(h.runs, run)
	h.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		defer close(run.done)
		if h.failSelf != nil {
			stdoutW.Close()
			result <- h.failSelf
			return
		}
		for _, line := range h.print {
			fmt.Fprintln(stdoutW, line)
		}
		var err error
		switch {
		case h.needCode:
			lines := make(chan string, 1)
			go func() {
				line, rerr := bufio.NewReader(stdinR).ReadString('\n')
				if rerr == nil {
					lines <- strings.TrimSpace(line)
				}
			}()
			select {
			case code := <-lines:
				run.codes <- code
				if h.verify != nil {
					err = h.verify(code)
				}
			case <-run.killCh:
				err = &awsExitError{stderr: "killed"}
			}
		default:
			select {
			case <-h.approve:
			case <-run.killCh:
				err = &awsExitError{stderr: "killed"}
			}
		}
		if err == nil && h.onSuccess != nil {
			h.onSuccess()
		}
		fmt.Fprint(stdoutW, "Enter the authorization code displayed in your browser: ")
		stdoutW.Close()
		result <- err
	}()
	return &awsProc{
		stdin:  stdinW,
		stdout: stdoutR,
		wait:   func() error { return <-result },
		kill:   func() { run.killed.Store(true); close(run.killCh) },
	}, nil
}

func (h *fakeLoginHost) last() *fakeLoginRun {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runs[len(h.runs)-1]
}

func (r *fakeLoginRun) has(parts ...string) bool {
	have := map[string]bool{}
	for _, a := range r.args {
		have[a] = true
	}
	for _, p := range parts {
		if !have[p] {
			return false
		}
	}
	return true
}

func awsSignInManager(t *testing.T, h *fakeLoginHost) *Manager {
	t.Helper()
	m, _ := awsTestManager(t, awsOK)
	h.t = t
	m.awsSpawn = h.spawn
	return m
}

func startSignIn(t *testing.T, m *Manager, in Input) *ConnectFlow {
	t.Helper()
	in.Integration = "aws"
	f := mustHandle(t, m, Request{Action: "connect_start", Input: &in}).Flow
	if f == nil || f.Status != "waiting" {
		t.Fatalf("flow %+v", f)
	}
	return f
}

func flowAction(t *testing.T, m *Manager, action string, f *ConnectFlow, code string) Response {
	t.Helper()
	return mustHandle(t, m, Request{Action: action, FlowID: f.ID, Code: code})
}

func writeAWSProfile(t *testing.T, section string) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".aws", "config")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("\n" + section + "\n"); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

// ---- a new connection -------------------------------------------------------

func TestAWSSignInConnectsNewAccount(t *testing.T) {
	h := &fakeLoginHost{print: []string{"Browser will not be automatically opened.", "Please visit the following URL:", "", awsSignInPage, ""}, needCode: true,
		onSuccess: func() { writeAWSProfile(t, "[profile mewla]\nlogin_session = arn:aws:iam::123456789012:user/audit") }}
	m := awsSignInManager(t, h)
	t.Setenv("AWS_SECRET_ACCESS_KEY", "inherited-secret")
	t.Setenv("AWS_LOGIN_CACHE_DIRECTORY", "/tmp/mewla-login-cache")
	work := addAWSProfile(t, m, "work")

	f := startSignIn(t, m, Input{})
	if f.AuthorizationURL != awsSignInPage || !f.CodeRequired || f.UserCode != "" || !f.Expires.After(time.Now().Add(5*time.Minute)) {
		t.Fatalf("%+v", f)
	}
	run := h.last()
	// A new profile of its own, named and placed explicitly, so the CLI
	// never prompts and never reaches into another profile.
	if !run.has("login", "--remote", "--profile", "mewla", "--region", "us-east-1") || run.has("work") {
		t.Fatalf("args %v", run.args)
	}
	env := strings.Join(run.env, "\n")
	if strings.Contains(env, "inherited-secret") || !strings.Contains(env, "AWS_LOGIN_CACHE_DIRECTORY=/tmp/mewla-login-cache") {
		t.Fatalf("env %s", env)
	}

	// Waiting, nothing connected yet.
	if got := flowAction(t, m, "connect_status", f, "").Flow; got.Status != "waiting" {
		t.Fatalf("%+v", got)
	}
	// A pasted line that cannot be a code never reaches the CLI.
	for _, bad := range []string{"123", "two words here", "x\ny"} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Code: bad}); err == nil && bad != "x\ny" {
			t.Fatalf("accepted %q", bad)
		}
	}
	select {
	case code := <-run.codes:
		t.Fatalf("the CLI received %q", code)
	default:
	}

	done := flowAction(t, m, "connect_finish", f, awsGoodCode)
	if done.Flow.Status != "connected" || done.Account == nil {
		t.Fatalf("%+v", done.Flow)
	}
	if got := <-run.codes; got != awsGoodCode {
		t.Fatalf("the CLI received %q", got)
	}
	a := done.Account
	if a.Profile != "mewla" || a.AuthMethod != awsMethodProfile || a.Status != "connected" || a.ID == work.ID || a.Access == nil || a.Access.Read != "allowed" {
		t.Fatalf("%+v", a)
	}
	if run.killed.Load() {
		t.Fatal("killed after a clean exit")
	}
	// Later status checks keep answering connected.
	if got := flowAction(t, m, "connect_status", f, "").Flow; got.Status != "connected" || got.AccountID != a.ID {
		t.Fatalf("%+v", got)
	}
}

func TestAWSSignInSetsRegionOfANewProfile(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true,
		onSuccess: func() { writeAWSProfile(t, "[profile mewla]\nlogin_session = arn:aws:iam::123456789012:user/audit") }}
	m, fake := awsTestManager(t, awsOK)
	m.awsSpawn = h.spawn
	f := startSignIn(t, m, Input{Region: "eu-central-1"})
	flowAction(t, m, "connect_finish", f, awsGoodCode)
	found := false
	fake.mu.Lock()
	for _, c := range fake.calls {
		if awsArgsHave(c, "configure", "set", "region", "eu-central-1", "--profile", "mewla") {
			found = true
		}
	}
	fake.mu.Unlock()
	if !found {
		t.Fatal("the new profile has no region")
	}
	if !h.last().has("--region", "eu-central-1") {
		t.Fatalf("%v", h.last().args)
	}
}

func TestAWSSignInWrongCode(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true,
		verify: func(code string) error {
			return &awsExitError{stderr: "\naws: [ERROR]: Failed to decode the verification code (" + code + ").\n"}
		}}
	m := awsSignInManager(t, h)
	f := startSignIn(t, m, Input{})
	got := flowAction(t, m, "connect_finish", f, awsGoodCode)
	if got.Flow.Status != "failed" || !strings.Contains(got.Flow.Message, "That code did not work") || strings.Contains(got.Flow.Message, awsGoodCode) || got.Account != nil {
		t.Fatalf("%+v", got.Flow)
	}
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 0 {
		t.Fatal("an account was added")
	}
	// Signing in again starts fresh and the profile name is free.
	g := startSignIn(t, m, Input{})
	if g.ID == f.ID || !h.last().has("mewla") {
		t.Fatalf("%+v %v", g, h.last().args)
	}
}

func TestAWSSignInCodeIsSentOnce(t *testing.T) {
	release := make(chan struct{})
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true,
		verify:    func(string) error { <-release; return nil },
		onSuccess: func() { writeAWSProfile(t, "[profile mewla]\nlogin_session = arn:aws:iam::123456789012:user/audit") }}
	m := awsSignInManager(t, h)
	old := awsLoginFinishWait
	awsLoginFinishWait = 30 * time.Millisecond
	defer func() { awsLoginFinishWait = old }()
	f := startSignIn(t, m, Input{})
	// The CLI is slow to take the code: the call returns, the flow keeps waiting.
	if got := flowAction(t, m, "connect_finish", f, awsGoodCode).Flow; got.Status != "waiting" {
		t.Fatalf("%+v", got)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Code: awsGoodCode}); err == nil {
		t.Fatal("a second code was accepted")
	}
	close(release)
	var last Response
	eventually(t, "connected", func() bool {
		last = mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
		return last.Flow.Status == "connected"
	})
	if last.Account == nil || len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 1 {
		t.Fatalf("%+v", last)
	}
}

func TestAWSSignInStartFailures(t *testing.T) {
	// The CLI ends at once.
	h := &fakeLoginHost{failSelf: &awsExitError{stderr: "aws: [ERROR]: boom"}}
	m := awsSignInManager(t, h)
	if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws"}}); err == nil || !strings.Contains(err.Error(), "did not offer a sign-in page") {
		t.Fatalf("%v", err)
	}
	// The CLI asks something else and never prints a page.
	quiet := &fakeLoginHost{print: []string{"AWS Region [us-east-1]: "}, needCode: true}
	m.awsSpawn = quiet.spawn
	old := awsLoginStartWait
	awsLoginStartWait = 50 * time.Millisecond
	defer func() { awsLoginStartWait = old }()
	if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws"}}); err == nil {
		t.Fatal("no page, no flow")
	}
	eventually(t, "the stuck CLI is killed", func() bool { return quiet.last().killed.Load() })
	// A page on a foreign host is not offered.
	foreign := &fakeLoginHost{print: []string{"https://evil.example/signin"}, needCode: true}
	m.awsSpawn = foreign.spawn
	if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws"}}); err == nil {
		t.Fatal("a foreign page was offered")
	}
	eventually(t, "the CLI is killed", func() bool { return foreign.last().killed.Load() })
	if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws", Region: "nowhere"}}); err == nil {
		t.Fatal("bad region")
	}
	if len(m.connectFlows) != 0 {
		t.Fatalf("%d flows linger", len(m.connectFlows))
	}
}

// ---- renewing an expired session ------------------------------------------

func TestAWSSignInRenewsAProfileInPlace(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true}
	m := awsSignInManager(t, h)
	work := addAWSProfile(t, m, "work")
	f := startSignIn(t, m, Input{Profile: "work"})
	run := h.last()
	// The profile's own region, and no new profile.
	if !run.has("login", "--remote", "--profile", "work", "--region", "ap-southeast-1") {
		t.Fatalf("args %v", run.args)
	}
	got := flowAction(t, m, "connect_finish", f, awsGoodCode)
	if got.Flow.Status != "connected" || got.Account.ID != work.ID {
		t.Fatalf("%+v %+v", got.Flow, got.Account)
	}
	if n := len(mustHandle(t, m, Request{Action: "list"}).Accounts); n != 1 {
		t.Fatalf("%d accounts", n)
	}
}

func TestAWSSignInSSOApprovesByItself(t *testing.T) {
	h := &fakeLoginHost{print: []string{"Browser will not be automatically opened.", "Please visit the following URL:", "", "https://device.sso.eu-west-1.amazonaws.com/", "", "Then enter the code:", "", "WXYZ-1234", "", "Alternatively, you may visit the following URL which will autofill the code upon loading:", "https://device.sso.eu-west-1.amazonaws.com/?user_code=WXYZ-1234"},
		approve: make(chan struct{})}
	m := awsSignInManager(t, h)
	sso := addAWSProfile(t, m, "work-sso")
	f := startSignIn(t, m, Input{Profile: "work-sso"})
	if f.UserCode != "WXYZ-1234" || f.CodeRequired || f.AuthorizationURL != "https://device.sso.eu-west-1.amazonaws.com/" {
		t.Fatalf("%+v", f)
	}
	if !h.last().has("sso", "login", "--profile", "work-sso", "--use-device-code", "--no-browser") || h.last().has("--remote") {
		t.Fatalf("args %v", h.last().args)
	}
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Code: awsGoodCode}); err == nil {
		t.Fatal("an Identity Center sign-in took a code")
	}
	if got := flowAction(t, m, "connect_status", f, "").Flow; got.Status != "waiting" {
		t.Fatalf("%+v", got)
	}
	close(h.approve)
	var last Response
	eventually(t, "connected", func() bool {
		last = mustHandle(t, m, Request{Action: "connect_status", FlowID: f.ID})
		return last.Flow.Status == "connected"
	})
	if last.Account.ID != sso.ID {
		t.Fatalf("%+v", last.Account)
	}
}

func TestAWSSignInOnlyForBrowserProfiles(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true}
	m := awsSignInManager(t, h)
	for _, profile := range []string{"deploy", "static", "default", "missing", "bad name!"} {
		if _, err := m.Handle(context.Background(), Request{Action: "connect_start", Input: &Input{Integration: "aws", Profile: profile}}); err == nil {
			t.Errorf("%s was signed in", profile)
		}
	}
	if len(h.runs) != 0 {
		t.Fatal("the CLI ran for a profile it must not touch")
	}
}

func TestAWSSignInTwiceForOneProfile(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true}
	m := awsSignInManager(t, h)
	first := startSignIn(t, m, Input{Profile: "work"})
	run := h.last()
	second := startSignIn(t, m, Input{Profile: "work"})
	eventually(t, "the first CLI is killed", func() bool { return run.killed.Load() })
	if got := flowAction(t, m, "connect_status", first, "").Flow; got.Status != "cancelled" {
		t.Fatalf("%+v", got)
	}
	if h.last().killed.Load() || second.ID == first.ID {
		t.Fatal("the second sign-in was stopped")
	}
}

// ---- the end of a flow -------------------------------------------------------

func TestAWSSignInCancelEndsTheCLI(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true}
	m := awsSignInManager(t, h)
	f := startSignIn(t, m, Input{})
	got := flowAction(t, m, "connect_cancel", f, "").Flow
	if got.Status != "cancelled" || got.AuthorizationURL != "" {
		t.Fatalf("%+v", got)
	}
	eventually(t, "killed", func() bool { return h.last().killed.Load() })
	// A late code finds nothing waiting.
	if _, err := m.Handle(context.Background(), Request{Action: "connect_finish", FlowID: f.ID, Code: awsGoodCode}); err != nil {
		t.Fatalf("%v", err)
	}
	if len(mustHandle(t, m, Request{Action: "list"}).Accounts) != 0 {
		t.Fatal("a cancelled sign-in connected")
	}
}

func TestAWSSignInExpiryEndsTheCLI(t *testing.T) {
	h := &fakeLoginHost{print: []string{awsSignInPage}, needCode: true}
	m := awsSignInManager(t, h)
	f := startSignIn(t, m, Input{})
	m.mu.Lock()
	m.connectFlows[f.ID].Expires = time.Now().Add(-time.Second)
	m.mu.Unlock()
	mustHandle(t, m, Request{Action: "list"})
	eventually(t, "killed", func() bool { return h.last().killed.Load() })
	if _, err := m.Handle(context.Background(), Request{Action: "connect_status", FlowID: f.ID}); err == nil {
		t.Fatal("an expired flow answered")
	}
}

func TestAWSNewProfileName(t *testing.T) {
	m, _ := awsTestManager(t, awsOK)
	if got := m.awsNewProfileName(); got != "mewla" {
		t.Fatal(got)
	}
	// An earlier Mewla sign-in nobody uses is reused.
	writeAWSProfile(t, "[profile mewla]\nlogin_session = arn:aws:iam::123456789012:user/audit")
	if got := m.awsNewProfileName(); got != "mewla" {
		t.Fatal(got)
	}
	// One an account uses is kept.
	m.records["x"] = &record{Account: Account{ID: "x", Integration: "aws", Profile: "mewla", Status: "connected"}}
	if got := m.awsNewProfileName(); got != "mewla-2" {
		t.Fatal(got)
	}
	// A profile of the person's own is never taken over, whatever it is called.
	writeAWSProfile(t, "[profile mewla-2]\naws_access_key_id = AKIAEXAMPLEEXAMPLE1")
	if got := m.awsNewProfileName(); got != "mewla-3" {
		t.Fatal(got)
	}
	// A disconnected account frees its profile.
	m.records["x"].Account.Status = "disconnected"
	if got := m.awsNewProfileName(); got != "mewla" {
		t.Fatal(got)
	}
}

// ---- the real process boundary ------------------------------------------------

func TestSpawnAWSTalksToTheCLI(t *testing.T) {
	log := fakeAWS(t, `echo "Please visit the following URL:"
echo '`+awsSignInPage+`'
printf 'Enter the authorization code displayed in your browser: '
read code
echo "GOT:$code" >> `+"\"$0.code\""+`
[ "$code" = "`+awsGoodCode+`" ] || { echo "aws: [ERROR]: Failed to decode the verification code." >&2; exit 255; }
`)
	env, _ := awsEnv(&record{Account: Account{AuthMethod: awsMethodProfile}}, "", "")
	l, err := newAWSLogin(spawnAWS, awsModeLogin, "mewla", "us-east-1", true, []string{"login", "--remote", "--profile", "mewla", "--region", "us-east-1"}, env)
	if err != nil {
		t.Fatal(err)
	}
	defer l.stop()
	select {
	case <-l.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("no page")
	}
	if l.out.url != awsSignInPage {
		t.Fatalf("%+v", l.out)
	}
	if err := l.sendCode("wrong-code-0000"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the CLI did not end")
	}
	if l.err == nil || !strings.Contains(l.err.Error(), "verification code") {
		t.Fatalf("%v", l.err)
	}
	raw, _ := os.ReadFile(log)
	if !strings.Contains(string(raw), "ARGS:login --remote --profile mewla --region us-east-1") {
		t.Fatalf("argv: %s", raw)
	}
}

func TestSpawnAWSKillEndsTheWholeGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	fakeAWS(t, `echo '`+awsSignInPage+`'
sleep 300 &
echo $! > `+pidFile+`
wait
`)
	env, _ := awsEnv(&record{Account: Account{AuthMethod: awsMethodProfile}}, "", "")
	l, err := newAWSLogin(spawnAWS, awsModeLogin, "mewla", "us-east-1", true, []string{"login", "--remote"}, env)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("no page")
	}
	eventually(t, "child started", func() bool { _, err := os.Stat(pidFile); return err == nil })
	l.stop()
	select {
	case <-l.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the CLI survived")
	}
	raw, _ := os.ReadFile(pidFile)
	pid := strings.TrimSpace(string(raw))
	eventually(t, "the child of the CLI is gone", func() bool {
		_, err := os.Stat("/proc/" + pid + "/stat")
		if err != nil {
			return true
		}
		stat, _ := os.ReadFile("/proc/" + pid + "/stat")
		return strings.Contains(string(stat), ") Z ")
	})
}
