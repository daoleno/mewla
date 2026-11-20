package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (m *Manager) startCloud(ctx context.Context, in *Input) (Response, error) {
	if m.cloud == nil {
		return Response{}, errors.New("Cloud CLI is unavailable")
	}
	// Adding an AWS account signs in through the organization's IAM Identity
	// Center when it has one: a device code, renewed for as long as the
	// organization allows. aws login (12 h at most) is for accounts without it.
	if in.Integration == "aws" && in.AccountID == "" && in.Profile == "" && !in.AWSLogin {
		return m.startAWSSSO(ctx, in)
	}
	profile := in.Profile
	if in.AccountID != "" {
		r := m.records[in.AccountID]
		if r == nil || r.CLI == nil || r.Account.Integration != in.Integration {
			return Response{}, errors.New("Refresh the local profiles and try again")
		}
		profile = r.CLI.Profile
	}
	if profile == "" {
		if d, ok := m.cloud.providers[in.Integration].(interface{ defaultProfile() string }); ok {
			profile = d.defaultProfile()
		} else {
			profile = "mewla-" + uuid.NewString()[:8]
		}
	}
	if !m.cloud.providers[in.Integration].validProfile(profile) {
		return Response{}, errors.New("Use a profile name with letters, numbers, dots, underscores or hyphens")
	}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: in.Integration, Status: "waiting", Expires: time.Now().Add(10 * time.Minute)}, profile: profile, renewID: in.AccountID}
	m.connectFlows[f.ID] = f
	if _, err := m.cloud.executable(in.Integration); err != nil {
		m.cloudInstallPrompt(f)
		if m.cloud.updates != nil && !f.InstallManual {
			m.startManagedCloudInstall(f, false)
		}
		return flowResult(f), nil
	}
	if err := m.launchCloudLogin(ctx, f); err != nil {
		delete(m.connectFlows, f.ID)
		return Response{}, err
	}
	return flowResult(f), nil
}

// Providers describe manual setup only when their installer cannot run here.
// The shared flow owns retry/check-again, deadlines and process cleanup.
func (m *Manager) cloudInstallPrompt(f *connectFlow) {
	f.InstallRequired = true
	if provider, ok := m.cloud.providers[f.Integration].(interface{ installSupport() (string, string) }); ok {
		message, address := provider.installSupport()
		f.InstallManual = message != ""
		f.Message, f.InstallURL = message, address
	}
}

// An onboarding flow observes the shared update without owning or cancelling it.
func (m *Manager) startManagedCloudInstall(f *connectFlow, force bool) {
	ctx, cancel := context.WithCancel(context.Background())
	login := &cloudLogin{cancel: cancel, output: &boundedBuffer{limit: 32768}, done: make(chan struct{})}
	f.cli = login
	f.InstallRequired = false
	f.InstallFailed = false
	f.Installing = true
	f.Message = ""
	done := m.cloud.refresh(f.Integration, force)
	go func() {
		defer close(login.done)
		defer cancel()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				login.err = ctx.Err()
				return
			}
		}
		_, login.err = m.cloud.resolveExecutable(f.Integration)
	}()
}

func (m *Manager) launchCloudLogin(ctx context.Context, f *connectFlow) error {
	binary, lease, err := m.cloud.acquireExecutable(f.Integration)
	defer closeCloudLease(lease)
	if err != nil {
		return err
	}
	p := m.cloud.providers[f.Integration]
	if preparer, ok := p.(interface {
		prepareLogin(*Manager, context.Context, string) error
	}); ok {
		if err := preparer.prepareLogin(m, ctx, f.profile); err != nil {
			return err
		}
	}
	// Some CLIs need the person's own client before they can sign in.
	if s, ok := p.(interface {
		setupFlow(*Manager, context.Context, *connectFlow) (bool, error)
	}); ok {
		need, err := s.setupFlow(m, ctx, f)
		if err != nil {
			return err
		}
		if need {
			f.InstallRequired, f.InstallManual, f.InstallFailed, f.Installing = false, false, false, false
			return nil
		}
	}
	args := p.loginArgs(f.profile)
	if chooser, ok := p.(interface {
		loginArgsFor(*Manager, *connectFlow) []string
	}); ok {
		args = chooser.loginArgsFor(m, f)
	}
	login, err := startCloudLogin(binary, args, p.environment())
	if err != nil {
		return err
	}
	f.cli = login
	f.InstallRequired = false
	f.InstallManual = false
	f.InstallFailed = false
	f.Installing = false
	f.Message = ""
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		if markDeviceCode(f, p, login.output.String()) {
			return nil
		}
		if address := p.loginURL(login.output.String()); address != "" {
			markLoginURL(f, p, address)
			return nil
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return nil
		case <-login.done:
			return errors.New("Cloud CLI could not start remote sign-in. Install or update the CLI and try again.")
		}
	}
}
func (m *Manager) continueCloud(ctx context.Context, q Request, f *connectFlow) (Response, error) {
	if q.Action == "connect_cancel" {
		if f.Status == "connected" {
			return m.connectedFlow(f)
		}
		if f.cli != nil {
			f.cli.cancel()
		}
		f.Status = "cancelled"
		f.AuthorizationURL = ""
		return flowResult(f), nil
	}
	if f.Status != "waiting" {
		return m.connectedFlow(f)
	}
	p := m.cloud.providers[f.Integration]
	if f.InstallRequired {
		if q.Action == "connect_finish" {
			// Re-resolve PATH and vendor install locations on every attempt. Manual
			// installation and a recovered installation need no daemon restart.
			if _, err := m.cloud.executable(f.Integration); err == nil {
				if err = m.launchCloudLogin(ctx, f); err != nil {
					return Response{}, err
				}
				return flowResult(f), nil
			}
			if f.InstallManual {
				return flowResult(f), nil
			}
			if m.cloud.updates != nil {
				m.startManagedCloudInstall(f, true)
				return flowResult(f), nil
			}
			installCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			login := &cloudLogin{cancel: cancel, output: &boundedBuffer{limit: 32768}, done: make(chan struct{})}
			f.cli = login
			f.InstallRequired = false
			f.InstallFailed = false
			f.Installing = true
			f.Expires = time.Now().Add(10 * time.Minute)
			f.Message = ""
			go func() { login.err = p.install(installCtx, m.cloud.dir+"/"+f.Integration); cancel(); close(login.done) }()
		}
		return flowResult(f), nil
	}
	if f.PortalRequired {
		// The card asks for the portal and starts a new sign-in with it.
		return flowResult(f), nil
	}
	if f.SetupRequired {
		if q.Action == "connect_finish" {
			if err := p.(interface {
				finishSetup(*Manager, context.Context, *connectFlow, string) error
			}).finishSetup(m, ctx, f, q.Callback); err != nil {
				return Response{}, err
			}
			f.SetupRequired, f.CodeRequired, f.AuthorizationURL, f.Message = false, false, "", ""
			f.Expires = time.Now().Add(10 * time.Minute)
			if err := m.launchCloudLogin(ctx, f); err != nil {
				return Response{}, err
			}
		}
		return flowResult(f), nil
	}
	if f.cli == nil {
		return Response{}, errors.New("Start sign-in again")
	}
	if f.Installing {
		select {
		case <-f.cli.done:
			f.Installing = false
			if f.cli.err != nil {
				f.InstallRequired = true
				f.InstallFailed = true
				f.Message = f.cli.err.Error()
				return flowResult(f), nil
			}
			f.Message = ""
			if err := m.launchCloudLogin(ctx, f); err != nil {
				return Response{}, err
			}
		default:
		}
		return flowResult(f), nil
	}
	if f.AuthorizationURL == "" && !markDeviceCode(f, p, f.cli.output.String()) {
		if address := p.loginURL(f.cli.output.String()); address != "" {
			markLoginURL(f, p, address)
		}
	}
	// A device code is approved on the service's page; nothing is pasted back.
	if q.Action == "connect_finish" && !f.cli.sent && f.UserCode == "" {
		code := strings.TrimSpace(q.Callback)
		if !p.validateCode(f.AuthorizationURL, code) {
			message := "Paste the authorization code shown after sign-in."
			if h, ok := p.(interface{ pasteError() string }); ok {
				message = h.pasteError()
			}
			return Response{}, errors.New(message)
		}
		if d, ok := p.(interface {
			deliverCode(string, string) error
		}); ok {
			// The CLI listens on a loopback port; the code reaches it the way
			// the browser's redirect would have.
			if err := d.deliverCode(f.AuthorizationURL, code); err != nil {
				return Response{}, err
			}
		} else if _, err := io.WriteString(f.cli.stdin, code+"\n"); err != nil {
			return Response{}, errors.New("This sign-in ended. Start again.")
		}
		f.cli.sent = true
		f.CodeRequired, f.AddressRequired = false, false
		f.Message = "Checking your account…"
	}
	select {
	case <-f.cli.done:
		if f.cli.err != nil {
			f.Status = "failed"
			f.Message = "Cloud CLI did not finish sign-in. Start again."
			return flowResult(f), nil
		}
		if f.ssoAdd {
			// The accounts this sign-in may use become profiles; the sign-in
			// connects one of them.
			profile, _, err := m.addAWSSSOProfiles(ctx, f.ssoSession)
			if err != nil {
				f.Status = "failed"
				f.Message = err.Error()
				return flowResult(f), nil
			}
			f.profile = profile
		}
		delete(m.cloud.checked, f.profile)
		if err := p.syncProfiles(m, ctx, true); err != nil {
			return Response{}, err
		}
		r := p.profileRecord(m, f.profile)
		if r == nil || r.Account.Status != "connected" {
			f.Status = "failed"
			f.Message = "Sign-in finished, but the profile could not be verified. Refresh and try again."
			return flowResult(f), nil
		}
		if recorder, ok := p.(interface{ loginCompleted(*Manager, *record) }); ok {
			recorder.loginCompleted(m, r)
		}
		r.Account.Enabled = true
		m.setGroup(r, "read", true)
		if err := m.save(); err != nil {
			return Response{}, err
		}
		f.AccountID = r.Account.ID
		f.Status = "connected"
		f.AuthorizationURL = ""
		return m.connectedFlow(f)
	default:
		return flowResult(f), nil
	}
}
func (m *Manager) runCloud(ctx context.Context, q Request) (Response, error) {
	if m.cloud == nil || m.cloud.providers[q.Integration] == nil {
		return Response{}, errors.New("Cloud CLI is unavailable")
	}
	// Version probes do not need an account or cloud authorization.
	if len(q.CLIArgs) == 1 && q.CLIArgs[0] == "--version" {
		binary, lease, err := m.cloud.acquireExecutable(q.Integration)
		defer closeCloudLease(lease)
		if err != nil {
			return Response{}, err
		}
		out, err := m.cloud.run(ctx, binary, q.CLIArgs, m.cloud.providers[q.Integration].environment())
		return Response{CLIOutput: &out}, err
	}
	// A command for a profile Mewla already knows runs at once. Identity checks
	// of every profile would spend its time (an expired SSO token alone takes
	// seconds to renew) and run in the background instead.
	if m.cloudTarget(q) == nil {
		if err := m.syncCloudProfiles(ctx, q.Integration); err != nil {
			return Response{}, err
		}
	} else {
		m.refreshCloudProfiles()
	}
	r, tool, err := m.cloudRunPlan(q)
	if err != nil {
		return Response{}, err
	}
	out, err := m.cloudCommand(ctx, q.Integration, firstNonEmpty(q.Profile, r.CLI.Profile), q.CLIArgs)
	if err != nil {
		return Response{}, err
	}
	if out.ExitCode != 0 && (awsExpired(out.Stderr) || q.Integration == "gws" && out.ExitCode == 2) {
		r.Account.Status = "authorization_required"
		r.Account.ProfileStatus = "expired"
		if q.Integration == "aws" {
			m.awsSessionEnded(r, time.Now())
			m.noteAWSSignIns(time.Now())
			// The CLI's own line names a login command agents cannot run.
			out.Stderr = strings.TrimRight(out.Stderr, "\n") + "\nmewla: the AWS sign-in for profile " + r.CLI.Profile + " has ended. Ask the person to sign in again in Mewla: Extensions › AWS › " + r.CLI.Profile + " › Sign in. Chat has the notice.\n"
		}
	}
	// History records the classification, never arguments/output (which may contain secrets).
	m.event(r, tool, nil)
	if err := m.save(); err != nil {
		return Response{}, err
	}
	return Response{CLIOutput: &out}, nil
}
func awsOutputArgs(args []string) []string {
	out := append([]string{}, args...)
	for _, a := range args {
		if a == "--output" || strings.HasPrefix(a, "--output=") {
			return out
		}
	}
	return append(out, "--output", "json")
}

// markLoginURL shows the sign-in address. A CLI that listens on a loopback port
// asks for the address the browser ended on; the others ask for a code.
func markLoginURL(f *connectFlow, p cloudProvider, address string) {
	f.AuthorizationURL = address
	if _, ok := p.(interface{ pasteError() string }); ok {
		f.AddressRequired, f.CodeRequired = true, false
		return
	}
	f.CodeRequired = true
}

// markDeviceCode shows a device code a CLI printed (AWS IAM Identity Center).
// The person approves it on any device; the flow completes when the CLI exits.
func markDeviceCode(f *connectFlow, p cloudProvider, output string) bool {
	d, ok := p.(interface{ deviceCode(string) (string, string) })
	if !ok {
		return false
	}
	address, code := d.deviceCode(output)
	if code == "" {
		return false
	}
	f.AuthorizationURL, f.UserCode = address, code
	f.CodeRequired, f.AddressRequired = false, false
	return true
}

// cloudTarget is the enabled profile a command names, or the only enabled one
// when it names none; nil when that takes discovering profiles first.
// cloudRunPlan picks the account a CLI command runs as and says whether the
// command may run at all. It reads only what the daemon already holds, so a
// command that will be refused is known before anything is installed.
func (m *Manager) cloudRunPlan(q Request) (*record, string, error) {
	var r *record
	for _, candidate := range m.records {
		if candidate.Account.Integration != q.Integration || candidate.CLI == nil || !candidate.Account.Enabled {
			continue
		}
		if q.Profile != "" && m.cloud.providers[q.Integration].profileRecord(m, q.Profile) != candidate {
			continue
		}
		if r != nil {
			return nil, "", errors.New("Choose --profile; more than one profile is enabled")
		}
		r = candidate
	}
	if r == nil {
		return nil, "", errors.New("Enable a profile in Extensions first")
	}
	read, err := m.cloud.providers[q.Integration].classify(q.CLIArgs)
	if err != nil {
		return nil, "", err
	}
	tool := "execute"
	if read {
		tool = "read"
	}
	args, _ := json.Marshal(map[string]any{"args": q.CLIArgs, "authorize": q.Authorize})
	_, _, err = checkInvoke(r, Request{Tool: tool, Arguments: args})
	if err != nil {
		return nil, "", err
	}
	if !read && !q.Authorize {
		return nil, "", callRefused("This command needs task-specific user authorization. With that authorization, pass --mewla-authorize.")
	}
	return r, tool, nil
}

func (m *Manager) cloudTarget(q Request) *record {
	var found *record
	for _, r := range m.records {
		if r.Account.Integration != q.Integration || r.CLI == nil || !r.Account.Enabled {
			continue
		}
		if q.Profile != "" && m.cloud.providers[q.Integration].profileRecord(m, q.Profile) != r {
			continue
		}
		if found != nil {
			return nil
		}
		found = r
	}
	return found
}

// startAWSSSO begins adding AWS accounts from an access portal: the one the
// person named, else the config's own sso-session. With neither, the card asks
// for the portal first (or the choice of aws login).
func (m *Manager) startAWSSSO(ctx context.Context, in *Input) (Response, error) {
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "aws", Status: "waiting", Expires: time.Now().Add(10 * time.Minute)}, ssoAdd: true}
	if strings.TrimSpace(in.SSOStartURL) != "" {
		startURL, region, err := awsPortal(in.SSOStartURL, in.SSORegion)
		if err != nil {
			return Response{}, err
		}
		if f.ssoSession, err = m.ensureAWSSSOSession(startURL, region); err != nil {
			return Response{}, err
		}
	} else {
		config, _, err := m.cloud.profileFiles()
		if err != nil {
			return Response{}, err
		}
		raw, err := readOptional(config)
		if err != nil {
			return Response{}, errors.New("AWS config could not be read")
		}
		names := []string{}
		for name, fields := range awsSSOSessions(raw) {
			if fields["sso_start_url"] != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			f.PortalRequired = true
			m.connectFlows[f.ID] = f
			return flowResult(f), nil
		}
		f.ssoSession = names[0]
	}
	m.connectFlows[f.ID] = f
	if _, err := m.cloud.executable("aws"); err != nil {
		m.cloudInstallPrompt(f)
		if m.cloud.updates != nil && !f.InstallManual {
			m.startManagedCloudInstall(f, false)
		}
		return flowResult(f), nil
	}
	if err := m.launchCloudLogin(ctx, f); err != nil {
		delete(m.connectFlows, f.ID)
		return Response{}, err
	}
	return flowResult(f), nil
}
