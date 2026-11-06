package connections

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// SetTailscaleBrowser gives Connect the person's browser. Without one, or while
// it isn't ready, Connect asks the person to create the client themselves.
func (m *Manager) SetTailscaleBrowser(b TailscaleBrowser) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tailscaleBrowser = b
}

const tailscalePasteMessage = "Create an OAuth client named " + tailscaleClientName + " with the scopes all:read, devices:core, auth_keys, policy_file and dns, then paste its client secret."

// startTailscale begins Connect. With a ready browser the job runs in the
// background and the flow reports on it; otherwise the person is sent to the
// console's credential page to paste the secret.
func (m *Manager) startTailscale(in *Input) (Response, error) {
	f := &connectFlow{ConnectFlow: ConnectFlow{ID: uuid.NewString(), Integration: "tailscale", Status: "waiting", Expires: time.Now().Add(10 * time.Minute)}, allowWrites: in.AllowWrites}
	b := m.tailscaleBrowser
	if b == nil || !b.Ready() || m.tailscaleJobs > 0 {
		m.tailscalePaste(f, "")
	} else {
		f.Message = "Creating the credential in Chrome…"
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		f.jobCancel = cancel
		m.tailscaleJobs++
		go m.runTailscaleBrowser(ctx, f, b)
	}
	m.connectFlows[f.ID] = f
	return flowResult(f), nil
}

func (m *Manager) tailscalePaste(f *connectFlow, why string) {
	f.CodeRequired = true
	f.AuthorizationURL = TailscaleCredentialsURL
	f.Message = tailscalePasteMessage
	if why != "" {
		f.Message = why + " " + tailscalePasteMessage
	}
}

func (m *Manager) runTailscaleBrowser(ctx context.Context, f *connectFlow, b TailscaleBrowser) {
	client, err := b.Create(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.cacheListLocked()
	m.tailscaleJobs--
	f.jobCancel()
	if m.connectFlows[f.ID] != f || f.Status != "waiting" {
		return
	}
	if err != nil {
		var failure *TailscaleBrowserError
		if errors.As(err, &failure) {
			m.tailscalePaste(f, failure.Message)
		} else {
			m.tailscalePaste(f, "Chrome could not finish.")
		}
		return
	}
	vctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.finishTailscale(vctx, f, client.ID+":"+client.Secret); err != nil {
		m.tailscalePaste(f, "Tailscale didn't accept the new credential.")
	}
}

func (m *Manager) finishTailscale(ctx context.Context, f *connectFlow, stored string) error {
	r, err := m.verifyTailscale(ctx, stored)
	if err != nil {
		return err
	}
	a, err := m.saveTailscale(r, stored, f.allowWrites)
	if err != nil {
		return err
	}
	f.AccountID, f.Status = a.ID, "connected"
	f.AuthorizationURL, f.CodeRequired, f.Message = "", false, ""
	return nil
}

// continueTailscale answers connect_status, connect_finish (the pasted secret)
// and connect_cancel for a Tailscale flow.
func (m *Manager) continueTailscale(ctx context.Context, q Request, f *connectFlow) (Response, error) {
	if f.Status != "waiting" {
		return m.connectedFlow(f)
	}
	switch q.Action {
	case "connect_cancel":
		if f.jobCancel != nil {
			f.jobCancel()
		}
		f.Status, f.AuthorizationURL, f.CodeRequired = "cancelled", "", false
		return flowResult(f), nil
	case "connect_finish":
		_, _, stored, err := parseTailscaleCredential(q.Callback)
		if err != nil {
			return Response{}, err
		}
		if err := m.finishTailscale(ctx, f, stored); err != nil {
			return Response{}, err
		}
		if f.jobCancel != nil {
			f.jobCancel()
		}
		return m.connectedFlow(f)
	}
	return flowResult(f), nil
}
