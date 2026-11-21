package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/grants"
	"github.com/daoleno/mewla/daemon/screen"
)

// The permission card (daemon/grants): a card in Brain's thread for a grant of
// this computer the person has not given, asked for by `mewla agent-use need`
// or by a Worker launch that went without a tool only for want of it.

// grantsManager is the daemon's card owner; nil until the daemon has built it.
var grantsManager atomic.Pointer[grants.Manager]

// grantBackend is this computer's grants as the screen and agent use services
// see them.
type grantBackend struct {
	screen   *screen.Service
	agentUse *agentuse.Service
}

// Supported: the card is Mewla for Mac's (DESIGN-MAC.md §2.4): on another system a card would sit in Brain's thread
// where the app has no way to act on it.
func (b grantBackend) Supported(permission string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if permission == grants.Chrome {
		return b.agentUse != nil
	}
	return b.screen != nil
}

func (b grantBackend) State(ctx context.Context, permission string) grants.State {
	if permission == grants.Chrome {
		if b.agentUse != nil && b.agentUse.Status().Browser.State == agentuse.BrowserReady {
			return grants.Granted
		}
		return grants.Missing
	}
	if b.screen == nil {
		return grants.Missing
	}
	capability, _ := b.screen.AgentReady(ctx)
	for _, p := range capability.Permissions {
		if p.ID != permission {
			continue
		}
		switch {
		case p.Granted:
			return grants.Granted
		case p.RestartRequired:
			return grants.Stale
		}
	}
	return grants.Missing
}

func (b grantBackend) OpenSettings(ctx context.Context, permission string) error {
	if permission == grants.Chrome {
		_, err := b.agentUse.Fix(agentuse.FixInstallExtension)
		return err
	}
	return b.screen.OpenAgentSettings(ctx, permission)
}

// grantCards joins the timeline's cards and the lane to Brain.
type grantCards struct {
	*brain.Store
	*brain.Service
}

// grantRestarter makes a granted Accessibility count: in place at a quiet
// moment first (the daemon's own restart, Sessions and Workers untouched), the
// process restart under launchd or systemd (the Screen page's restart) when
// that did not do it.
type grantRestarter struct {
	quiet    func() bool
	inPlace  func() error
	screen   *screen.Service
	supervis func() bool
}

func (r grantRestarter) Quiet() bool { return r.quiet() }

func (r grantRestarter) InPlace() error { return r.inPlace() }

// Process stops the daemon so its service manager starts it again, the one
// mechanism the Screen page's restart uses; a daemon nothing supervises
// restarts in place instead.
func (r grantRestarter) Process() error {
	if r.supervis() && r.screen != nil {
		return r.screen.RestartNow()
	}
	return r.inPlace()
}

// inPlaceRestart is the daemon_upgrade restart (daemon_upgrade.go) asked from
// inside the daemon: it accepts whatever is installed, shuts down as on any
// stop and runs its executable again in its own place.
func inPlaceRestart(restarter *inPlaceRestarter) error {
	if restarter == nil {
		return errors.New("this daemon cannot restart itself")
	}
	resp := restarter.handle("")
	if !resp.OK {
		message := "the restart was refused"
		if resp.Error != nil {
			message = resp.Error.Message
		}
		return errors.New(message)
	}
	if resp.After != nil {
		resp.After()
	}
	return nil
}

// missingGrant is the grant a Worker launch went without a tool for, with the
// reason its card gives; ok is false when nothing is missing only for want of
// a grant.
func missingGrant(status agentuse.Status, where string) (permission, reason string, ok bool) {
	if computer := status.Computer; computer != nil && computer.State == agentuse.ComputerPermission {
		switch strings.TrimSpace(computer.Permission) {
		case grants.ScreenRecording:
			return grants.ScreenRecording, "A Worker needs to see " + where + "'s screen.", true
		case grants.Accessibility:
			return grants.Accessibility, "A Worker needs to click and type on " + where + ".", true
		}
	}
	if status.Enabled && status.Browser.State == agentuse.BrowserNoExtension {
		return grants.Chrome, "A Worker needs your Chrome.", true
	}
	return "", "", false
}

// noteMissingGrants runs for a Worker's launch, off its path: when its tools
// lack the screen or Chrome only because a grant is missing, the card exists
// (one per grant, never one per launch).
func noteMissingGrants(agentUse *agentuse.Service) {
	manager := grantsManager.Load()
	if manager == nil || runtime.GOOS != "darwin" {
		return
	}
	go func() {
		if permission, reason, ok := missingGrant(agentUse.Status(), manager.Where); ok {
			manager.Runtime(permission, reason)
		}
	}()
}

// handleAgentUseNeed is `mewla agent-use need`: the outcome (asked, already
// asked, already granted) in Confirmation.
func (a *controlApp) handleAgentUseNeed(req control.Request) control.Response {
	manager := grantsManager.Load()
	if manager == nil {
		return control.ErrorResponse("grants_unavailable", "Permission cards are not available in this Mewla.")
	}
	result, err := manager.Need(context.Background(), strings.TrimSpace(req.Name), req.Reason)
	switch {
	case errors.Is(err, grants.ErrUnknownPermission), errors.Is(err, grants.ErrUnsupported):
		return control.ErrorResponse("invalid_grant", err.Error())
	case err != nil:
		return control.ErrorResponse("grant_request_failed", err.Error())
	}
	return control.Response{OK: true, Confirmation: result.Outcome}
}
