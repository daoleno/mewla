package connections

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Signing in to AWS from the App. The AWS CLI on the server's computer does
// the sign-in and keeps the session in its own cache, so Mewla holds no AWS
// secret here either:
//
//   - `aws login --remote` prints a console sign-in URL and waits for the
//     authorization code that page shows. The App opens the URL wherever it
//     runs and sends the code back (connect_finish with code).
//   - `aws sso login --use-device-code --no-browser` prints a verification URL
//     and a user code, and finishes by itself once the person approves.
//
// Neither is told to open a browser on the server's computer, which may not be
// the one the person is holding. The CLI process lives as long as the flow and
// ends with it. When it exits cleanly, the profile it signed in is connected
// the way a chosen profile is.

const (
	// A sign-in page and its code stay good for a few minutes; a flow outlives
	// a slow person without keeping a process around for good.
	awsLoginLifetime = 10 * time.Minute
	awsDefaultRegion = awsDefaultKeys
	awsNewProfile    = "mewla"

	awsModeLogin = "login"
	awsModeSSO   = "sso"
)

var (
	// The CLI prints its URL at once. Waiting longer means it asked something
	// else, such as a prompt, that nobody can answer.
	awsLoginStartWait = 15 * time.Second
	// connect_finish holds the manager only this long for the CLI to take the
	// code; the App's poll picks up the result after that.
	awsLoginFinishWait = 6 * time.Second

	// Where a sign-in page can be: the console sign-in of each partition.
	awsSignInHost = regexp.MustCompile(`^([a-z0-9-]+\.)*signin\.(aws\.amazon\.com|amazonaws-us-gov\.com|amazonaws\.cn)$`)
	// Where an Identity Center device page can be.
	awsDeviceHost = regexp.MustCompile(`^([a-z0-9-]+\.)*(amazonaws\.com|amazonaws\.com\.cn|awsapps\.com|awsapps\.cn)$`)
	awsUserCode   = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)
	awsURLText    = regexp.MustCompile(`https://\S+`)
)

// validAWSCode: the authorization code is one printable token; nothing in it
// can answer a second prompt or end the line.
func validAWSCode(code string) bool {
	if len(code) < 8 || len(code) > 4096 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 0x21 || code[i] > 0x7e {
			return false
		}
	}
	return true
}

// ---- what the CLI prints -------------------------------------------------

// awsLoginOutput collects what the person needs from the CLI's output.
type awsLoginOutput struct {
	mode string
	url  string
	code string
}

// awsSignInURL returns the URL a line holds if it is one of AWS's sign-in
// pages for this mode, and nothing otherwise.
func awsSignInURL(mode, line string) string {
	raw := awsURLText.FindString(line)
	if raw == "" || len(raw) > 4096 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	allowed := awsSignInHost
	if mode == awsModeSSO {
		allowed = awsDeviceHost
	}
	if !allowed.MatchString(host) {
		return ""
	}
	return raw
}

// feed reads one line of output and reports whether the person now has
// everything to continue: the URL, and for Identity Center the user code too.
func (o *awsLoginOutput) feed(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return o.done()
	}
	if u := awsSignInURL(o.mode, line); u != "" {
		// Identity Center also prints a second URL that carries the code;
		// the plain one is shown with the code beside it.
		if o.url == "" && !strings.Contains(u, "user_code=") {
			o.url = u
		}
	} else if o.mode == awsModeSSO && o.code == "" && awsUserCode.MatchString(line) {
		o.code = line
	}
	return o.done()
}

func (o *awsLoginOutput) done() bool {
	return o.url != "" && (o.mode != awsModeSSO || o.code != "")
}

// ---- the CLI process -----------------------------------------------------

// awsProc is a running CLI, which tests replace.
type awsProc struct {
	stdin  io.WriteCloser
	stdout io.Reader
	// wait blocks until the process ends, after stdout has been read to its
	// end, and returns an *awsExitError when it failed.
	wait func() error
	kill func()
}

type awsSpawn func(args, env []string) (*awsProc, error)

// spawnAWS starts the CLI in its own process group, so that ending the flow
// ends everything it started.
func spawnAWS(args, env []string) (*awsProc, error) {
	bin, err := exec.LookPath("aws")
	if err != nil {
		return nil, errAWSMissing
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &limitedBuffer{max: 8 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &awsProc{
		stdin:  stdin,
		stdout: stdout,
		wait: func() error {
			err := cmd.Wait()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return &awsExitError{stderr: stderr.buf.String()}
			}
			return err
		},
		kill: func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) },
	}, nil
}

// awsLogin is one sign-in in progress.
type awsLogin struct {
	mode    string
	profile string
	// created: the profile is new, so the CLI writes it and Mewla may add the
	// region it asked for.
	created bool
	region  string

	proc     *awsProc
	timer    *time.Timer
	ready    chan struct{} // URL (and code) known, or the process ended
	finished chan struct{} // the process ended
	readyOne sync.Once

	mu       sync.Mutex
	out      awsLoginOutput
	err      error
	codeSent bool
	stopped  bool
}

func newAWSLogin(spawn awsSpawn, mode, profile, region string, created bool, args, env []string) (*awsLogin, error) {
	proc, err := spawn(args, env)
	if err != nil {
		return nil, err
	}
	l := &awsLogin{mode: mode, profile: profile, region: region, created: created, proc: proc,
		ready: make(chan struct{}), finished: make(chan struct{}), out: awsLoginOutput{mode: mode}}
	l.timer = time.AfterFunc(awsLoginLifetime, l.stop)
	go l.run()
	return l, nil
}

func (l *awsLogin) markReady() { l.readyOne.Do(func() { close(l.ready) }) }

func (l *awsLogin) run() {
	scanner := bufio.NewScanner(l.proc.stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		l.mu.Lock()
		done := l.out.feed(scanner.Text())
		l.mu.Unlock()
		if done {
			l.markReady()
		}
	}
	// Keep reading so the CLI never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, l.proc.stdout)
	err := l.proc.wait()
	l.mu.Lock()
	l.err = err
	l.mu.Unlock()
	l.markReady()
	close(l.finished)
}

func (l *awsLogin) exited() bool {
	select {
	case <-l.finished:
		return true
	default:
		return false
	}
}

// stop ends the process if it still runs. It is safe to call again.
func (l *awsLogin) stop() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.stopped = true
	l.mu.Unlock()
	l.timer.Stop()
	if !l.exited() {
		l.proc.kill()
	}
	_ = l.proc.stdin.Close()
}

// sendCode hands the authorization code to the CLI, once.
func (l *awsLogin) sendCode(code string) error {
	if l.mode != awsModeLogin {
		return errors.New("this sign-in does not take a code")
	}
	if !validAWSCode(code) {
		return errors.New("that is not the code from AWS")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.codeSent {
		return errors.New("the code was already sent; wait a moment")
	}
	if _, err := io.WriteString(l.proc.stdin, code+"\n"); err != nil {
		return errors.New("this sign-in already ended; sign in again")
	}
	l.codeSent = true
	return nil
}

// ---- profiles and environment ---------------------------------------------

// awsNewProfileName picks the profile a new sign-in is saved under: "mewla", or
// the next free "mewla-N". A profile of that family that no live account uses
// is reused, so signing in again does not pile up profiles; any other profile
// of the person is never touched.
func (m *Manager) awsNewProfileName() string {
	used := map[string]bool{}
	for _, r := range m.records {
		if r.Account.Integration == "aws" && r.Account.Status != "disconnected" && r.Account.Profile != "" {
			used[r.Account.Profile] = true
		}
	}
	for _, f := range m.connectFlows {
		if f.aws != nil && f.Status == "waiting" {
			used[f.aws.profile] = true
		}
	}
	existing := map[string]AWSProfile{}
	for _, p := range awsProfiles() {
		existing[p.Name] = p
	}
	for n := 1; ; n++ {
		name := awsNewProfile
		if n > 1 {
			name = fmt.Sprintf("%s-%d", awsNewProfile, n)
		}
		if used[name] {
			continue
		}
		if p, ok := existing[name]; ok && p.Kind != "login" {
			continue
		}
		return name
	}
}

// ---- the flow -------------------------------------------------------------

func (f *connectFlow) stopAWS() {
	if f.aws != nil {
		f.aws.stop()
	}
}

// startAWSSignIn begins a browser sign-in: for a profile that exists (renewing
// its session in place, or connecting it at once if it is still signed in) or
// for a new one (connecting an account).
func (m *Manager) startAWSSignIn(ctx context.Context, in *Input) (Response, error) {
	region := strings.TrimSpace(in.Region)
	if region != "" && !awsRegionName.MatchString(region) {
		return Response{}, errors.New("that is not an AWS region")
	}
	mode, profile, created := awsModeLogin, strings.TrimSpace(in.Profile), false
	if profile != "" {
		if !awsProfileName.MatchString(profile) {
			return Response{}, errors.New("that is not a valid AWS profile name")
		}
		p, ok := awsProfile(profile)
		if !ok {
			return Response{}, fmt.Errorf("AWS profile %s is no longer in this computer's AWS config", profile)
		}
		switch p.Kind {
		case "login":
		case "sso":
			mode = awsModeSSO
		default:
			// aws login would rewrite a profile that holds keys or a role.
			return Response{}, fmt.Errorf("AWS profile %s does not sign in with a browser", profile)
		}
		if region == "" {
			region = p.Region
		}
	} else {
		profile, created = m.awsNewProfileName(), true
	}
	if region == "" {
		region = awsDefaultRegion
	}
	if !created {
		// A profile the person tapped may still be signed in: then it connects
		// at once, and only an expired session needs the browser.
		cand := &record{Account: Account{ID: uuid.NewString(), Integration: "aws", AuthMethod: awsMethodProfile, Profile: profile, Region: region, Enabled: true, History: []Event{}}, Grants: map[string]string{}}
		_, err := m.awsWhoami(ctx, cand, "")
		switch {
		case err == nil:
			added, err := addAWSDriver(m, ctx, &Input{Integration: "aws", Profile: profile})
			if err != nil {
				return Response{}, err
			}
			f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "aws", Status: "connected", AccountID: added.Account.ID, Expires: time.Now().Add(awsLoginLifetime)}}
			m.connectFlows[f.ID] = f
			return m.connectedFlow(f)
		case !errors.Is(err, errAuth):
			return Response{}, err
		}
	}
	// Two sign-ins for one profile would write the same cache.
	for _, other := range m.connectFlows {
		if other.aws != nil && other.aws.profile == profile && other.Status == "waiting" {
			other.stopAWS()
			other.Status, other.AuthorizationURL, other.UserCode = "cancelled", "", ""
		}
	}
	args := []string{"login", "--remote", "--profile", profile, "--region", region, "--no-cli-pager", "--no-cli-auto-prompt"}
	if mode == awsModeSSO {
		args = []string{"sso", "login", "--profile", profile, "--use-device-code", "--no-browser", "--no-cli-pager", "--no-cli-auto-prompt"}
	}
	env, err := awsEnv(&record{Account: Account{AuthMethod: awsMethodProfile}}, "", "")
	if err != nil {
		return Response{}, err
	}
	login, err := newAWSLogin(m.awsSpawn, mode, profile, region, created, args, env)
	if err != nil {
		return Response{}, err
	}
	select {
	case <-login.ready:
	case <-time.After(awsLoginStartWait):
	case <-ctx.Done():
	}
	login.mu.Lock()
	out, runErr := login.out, login.err
	login.mu.Unlock()
	if !out.done() {
		login.stop()
		if runErr != nil {
			log.Printf("plugins: aws sign-in did not start: %s", cutText(strings.TrimSpace(runErr.Error()), 300))
		}
		return Response{}, errors.New("AWS did not offer a sign-in page. Try again.")
	}
	f := &connectFlow{ConnectFlow: ConnectFlow{
		ID: uuid.NewString(), Integration: "aws", Status: "waiting",
		AuthorizationURL: out.url, UserCode: out.code, CodeRequired: mode == awsModeLogin,
		Expires: time.Now().Add(awsLoginLifetime),
	}, aws: login}
	m.connectFlows[f.ID] = f
	return flowResult(f), nil
}

// advanceAWSSignIn serves connect_status and connect_finish of an AWS sign-in:
// it hands over a code if there is one, and completes the connection once the
// CLI has finished.
func (m *Manager) advanceAWSSignIn(ctx context.Context, f *connectFlow, code string) (Response, error) {
	l := f.aws
	if f.Status != "waiting" {
		return m.connectedFlow(f)
	}
	if code != "" {
		if err := l.sendCode(code); err != nil {
			return Response{}, err
		}
		select {
		case <-l.finished:
		case <-time.After(awsLoginFinishWait):
		case <-ctx.Done():
		}
	}
	if !l.exited() {
		return flowResult(f), nil
	}
	return m.finishAWSSignIn(ctx, f, code)
}

func (m *Manager) finishAWSSignIn(ctx context.Context, f *connectFlow, code string) (Response, error) {
	l := f.aws
	l.stop()
	fail := func(message string) (Response, error) {
		f.Status, f.Message, f.AuthorizationURL, f.UserCode = "failed", message, "", ""
		return flowResult(f), nil
	}
	l.mu.Lock()
	runErr, sent := l.err, l.codeSent
	l.mu.Unlock()
	if runErr != nil {
		var exit *awsExitError
		stderr := runErr.Error()
		if errors.As(runErr, &exit) {
			stderr = exit.stderr
		}
		return fail(awsSignInFailure(stderr, code, sent))
	}
	p, ok := awsProfile(l.profile)
	if !ok || p.Kind != "login" && p.Kind != "sso" {
		return fail("AWS did not save the sign-in. Try again.")
	}
	if p.Region == "" {
		// The sign-in is saved under a region of its own; calls need a default.
		env, _ := awsEnv(&record{Account: Account{AuthMethod: awsMethodProfile}}, "", "")
		_, _ = m.awsExec(ctx, awsCommand{Args: []string{"configure", "set", "region", l.region, "--profile", l.profile}, Env: env})
	}
	added, err := addAWSDriver(m, ctx, &Input{Integration: "aws", Profile: l.profile})
	if err != nil {
		return fail(err.Error())
	}
	f.AccountID = added.Account.ID
	f.Status, f.AuthorizationURL, f.UserCode = "connected", "", ""
	return m.connectedFlow(f)
}

// awsSignInFailure is one line for the person. The CLI's own text can hold the
// code that was pasted, so it only reaches the daemon log, with the code cut out.
func awsSignInFailure(stderr, code string, codeSent bool) string {
	text := strings.TrimSpace(stderr)
	if code != "" {
		text = strings.ReplaceAll(text, code, "[code]")
	}
	log.Printf("plugins: aws sign-in failed: %s", cutText(text, 300))
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "verification code"), strings.Contains(lower, "authorization code"):
		return "That code did not work. Sign in again."
	case strings.Contains(lower, "expired"), strings.Contains(lower, "timed out"), strings.Contains(lower, "timeout"):
		return "That sign-in expired. Sign in again."
	case strings.Contains(lower, "denied"), strings.Contains(lower, "declined"):
		return "The sign-in was declined. Sign in again."
	}
	if codeSent {
		return "That code did not work. Sign in again."
	}
	return "The AWS sign-in did not finish. Try again."
}
