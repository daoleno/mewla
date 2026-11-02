package connections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (m *Manager) startCloud(ctx context.Context, in *Input) (Response, error) {
	if m.cloud == nil {
		return Response{}, errors.New("Cloud CLI is unavailable")
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
		profile = "mewla-" + uuid.NewString()[:8]
	}
	if !m.cloud.providers[in.Integration].validProfile(profile) {
		return Response{}, errors.New("Use a profile name with letters, numbers, dots, underscores or hyphens")
	}
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: in.Integration, Status: "waiting", Expires: time.Now().Add(10 * time.Minute)}, profile: profile, renewID: in.AccountID}
	m.connectFlows[f.ID] = f
	if _, err := m.cloud.executable(in.Integration); err != nil {
		f.InstallRequired = true
		return flowResult(f), nil
	}
	if err := m.launchCloudLogin(ctx, f); err != nil {
		delete(m.connectFlows, f.ID)
		return Response{}, err
	}
	return flowResult(f), nil
}
func (m *Manager) launchCloudLogin(ctx context.Context, f *connectFlow) error {
	binary, err := m.cloud.executable(f.Integration)
	if err != nil {
		return err
	}
	p := m.cloud.providers[f.Integration]
	login, err := startCloudLogin(binary, p.loginArgs(f.profile), p.environment())
	if err != nil {
		return err
	}
	f.cli = login
	f.InstallRequired = false
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		if address := p.loginURL(login.output.String()); address != "" {
			f.AuthorizationURL = address
			f.CodeRequired = true
			return nil
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return nil
		case <-login.done:
			return errors.New("AWS CLI could not start remote sign-in. Install or update AWS CLI v2 and try again.")
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
			// Install is owned by this bounded flow and reaped even if the UI closes.
			installCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			login := &cloudLogin{cancel: cancel, output: &boundedBuffer{limit: 32768}, done: make(chan struct{})}
			f.cli = login
			f.InstallRequired = false
			f.Message = "Installing AWS CLI…"
			go func() { login.err = p.install(installCtx, m.cloud.dir+"/"+f.Integration); cancel(); close(login.done) }()
		}
		return flowResult(f), nil
	}
	if f.cli == nil {
		return Response{}, errors.New("Start sign-in again")
	}
	if f.Message == "Installing AWS CLI…" {
		select {
		case <-f.cli.done:
			if f.cli.err != nil {
				f.Status = "failed"
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
	if f.AuthorizationURL == "" {
		f.AuthorizationURL = p.loginURL(f.cli.output.String())
		f.CodeRequired = f.AuthorizationURL != ""
	}
	if q.Action == "connect_finish" && !f.cli.sent {
		code := strings.TrimSpace(q.Callback)
		if !p.validateCode(f.AuthorizationURL, code) {
			return Response{}, errors.New("Paste the code AWS shows after sign-in. It must belong to this sign-in.")
		}
		if _, err := io.WriteString(f.cli.stdin, code+"\n"); err != nil {
			return Response{}, errors.New("This sign-in ended. Start again.")
		}
		f.cli.sent = true
		f.CodeRequired = false
		f.Message = "Checking your AWS account…"
	}
	select {
	case <-f.cli.done:
		if f.cli.err != nil {
			f.Status = "failed"
			f.Message = "AWS CLI did not finish sign-in. Start again."
			return flowResult(f), nil
		}
		delete(m.cloud.checked, f.profile)
		if err := p.syncProfiles(m, ctx, true); err != nil {
			return Response{}, err
		}
		r := p.profileRecord(m, f.profile)
		if r == nil || r.Account.Status != "connected" {
			f.Status = "failed"
			f.Message = "AWS signed in, but its profile could not be verified. Refresh and try again."
			return flowResult(f), nil
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
	if err := m.cloud.providers[q.Integration].syncProfiles(m, ctx, false); err != nil {
		return Response{}, err
	}
	var r *record
	for _, candidate := range m.records {
		if candidate.Account.Integration != q.Integration || candidate.CLI == nil || !candidate.Account.Enabled {
			continue
		}
		if q.Profile != "" && candidate.CLI.Profile != q.Profile {
			continue
		}
		if r != nil {
			return Response{}, errors.New("Choose --profile; more than one profile is enabled")
		}
		r = candidate
	}
	if r == nil {
		return Response{}, errors.New("Enable a profile in Extensions first")
	}
	read, err := m.cloud.providers[q.Integration].classify(q.CLIArgs)
	if err != nil {
		return Response{}, err
	}
	tool := "execute"
	if read {
		tool = "read"
	}
	args, _ := json.Marshal(map[string]any{"args": q.CLIArgs, "authorize": q.Authorize})
	_, _, err = checkInvoke(r, Request{Tool: tool, Arguments: args})
	if err != nil {
		return Response{}, err
	}
	if !read && !q.Authorize {
		return Response{}, callRefused("This command needs task-specific user authorization. With that authorization, pass --mewla-authorize.")
	}
	out, err := m.cloudCommand(ctx, q.Integration, r.CLI.Profile, q.CLIArgs)
	if err != nil {
		return Response{}, err
	}
	if out.ExitCode != 0 && awsExpired(out.Stderr) {
		r.Account.Status = "authorization_required"
		r.Account.ProfileStatus = "expired"
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
