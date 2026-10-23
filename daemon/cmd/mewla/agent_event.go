package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/claude"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/google/uuid"
)

// agentApprovalWait is how long a hook waits for a person to decide before it
// leaves the decision to the agent's own prompt. It matches the timeout the
// hook file gives PermissionRequest.
const agentApprovalWait = 590 * time.Second

// agentEventService is the daemon side of an agent's hooks: it turns each
// payload into normalized events on the Hub, and carries approval answers back.
type agentEventService struct {
	hub     *agentevents.Hub
	workers func() []*classifier.Worker
	// approvalWait bounds one wait on a person; tests shorten it.
	approvalWait time.Duration
	now          func() time.Time
	pruneMu      sync.Mutex
	prunedAt     time.Time
}

func newAgentEventService(hub *agentevents.Hub, workers func() []*classifier.Worker) *agentEventService {
	return &agentEventService{hub: hub, workers: workers, approvalWait: agentApprovalWait, now: time.Now}
}

// Handle processes one hook call and returns what the hook should print.
func (s *agentEventService) Handle(ctx context.Context, req control.AgentEventRequest) control.Response {
	s.prune()
	switch req.Source {
	case claude.Provider:
		return s.handleClaude(ctx, req)
	}
	return control.ErrorResponse("unknown_agent_source", fmt.Sprintf("no adapter for %q", req.Source))
}

func (s *agentEventService) handleClaude(ctx context.Context, req control.AgentEventRequest) control.Response {
	payload, err := claude.Parse(req.Payload)
	if err != nil {
		return control.ErrorResponse("invalid_agent_event", err.Error())
	}
	workerID := s.claudeWorker(req.WorkerID, payload)
	if workerID == "" {
		// A Claude Mewla did not launch (or already closed). Say nothing and
		// let Claude proceed.
		return control.Response{OK: true}
	}
	if payload.HookEvent == "PermissionRequest" {
		return s.askClaude(ctx, workerID, payload)
	}
	for _, event := range claude.Map(workerID, payload, s.now().UTC()) {
		s.hub.Publish(event)
	}
	return control.Response{OK: true}
}

// askClaude publishes the approval and waits for a decision. With none, the
// hook prints nothing and Claude's own permission prompt decides.
func (s *agentEventService) askClaude(ctx context.Context, workerID string, payload claude.Payload) control.Response {
	ctx, cancel := context.WithTimeout(ctx, s.approvalWait)
	defer cancel()
	decision, ok := s.hub.Ask(ctx, workerID, claude.Approval(uuid.NewString(), payload), s.now().UTC())
	if !ok {
		return control.Response{OK: true}
	}
	output, ok := claude.Decision(decision.OptionID)
	if !ok {
		return control.Response{OK: true}
	}
	return control.Response{OK: true, HookOutput: output}
}

// agentEventPruneEvery bounds how often Handle looks for vanished Workers.
const agentEventPruneEvery = time.Minute

// prune drops what the Hub holds for Workers the daemon no longer lists.
func (s *agentEventService) prune() {
	now := s.now()
	s.pruneMu.Lock()
	due := now.Sub(s.prunedAt) >= agentEventPruneEvery
	if due {
		s.prunedAt = now
	}
	s.pruneMu.Unlock()
	if !due {
		return
	}
	live := map[string]bool{}
	for _, w := range s.workers() {
		if w != nil {
			live[w.ID] = true
		}
	}
	for _, id := range s.hub.Workers() {
		if !live[id] {
			s.hub.Forget(id)
		}
	}
}

// claudeWorker resolves the Worker a Claude hook belongs to and whether the
// hook may speak for it. The Worker is the one its environment named, or else
// the one launched with the hook's session id. Either way the session id must
// be the one Mewla launched the Worker with, or the conversation the Worker is
// already bound to; otherwise another Claude that merely inherited the
// Worker's id could move its turns or answer its approvals.
func (s *agentEventService) claudeWorker(envID string, p claude.Payload) string {
	workers := s.workers()
	var worker *classifier.Worker
	if envID = strings.TrimSpace(envID); envID != "" {
		for _, w := range workers {
			if w != nil && w.ID == envID {
				worker = w
			}
		}
	} else {
		for _, w := range workers {
			if w != nil && launchSessionID(w) == p.SessionID && p.SessionID != "" {
				worker = w
			}
		}
	}
	if worker == nil || !s.speaksFor(worker, p) {
		return ""
	}
	return worker.ID
}

// launchSessionID is the conversation id Mewla launched the Worker with.
func launchSessionID(w *classifier.Worker) string {
	id, present, err := work.ProviderResumeToken(work.WorkerProviderClaude, w.Command)
	if err != nil || !present {
		return ""
	}
	return id
}

// speaksFor reports whether the payload's session belongs to the Worker. A
// conversation the Worker moves to later (/clear, /compact, a resume) is
// accepted only when its SessionStart says so and its transcript sits beside
// the one the Worker is already bound to.
func (s *agentEventService) speaksFor(w *classifier.Worker, p claude.Payload) bool {
	if p.SessionID == "" {
		return false
	}
	state, _ := s.hub.State(w.ID)
	if p.SessionID == launchSessionID(w) || (state.Bound && p.SessionID == state.Binding.ConversationID) {
		return true
	}
	if p.HookEvent != "SessionStart" || !state.Bound || state.Binding.Ref == "" || p.TranscriptPath == "" {
		return false
	}
	switch p.Source {
	case "clear", "compact", "resume":
		return filepath.Dir(p.TranscriptPath) == filepath.Dir(state.Binding.Ref)
	}
	return false
}

// runAgentEvent is the command an agent's hook runs: `mewla agent-event
// <source>`. It forwards the hook payload from stdin to the daemon and prints
// whatever the daemon answers. It never fails the agent: any trouble reaching
// the daemon leaves the agent to proceed as if no hook were installed.
func runAgentEvent(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla agent-event", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{stateDir: strings.TrimSpace(os.Getenv("MEWLA_STATE_DIR"))}
	fs.StringVar(&cfg.stateDir, "state-dir", cfg.stateDir, "state directory for the daemon control socket")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Usage: mewla agent-event [-state-dir dir] <source> < payload.json")
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || !json.Valid(payload) {
		return nil
	}
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return nil
	}
	resp, err := control.CallWithTimeout(socketPath, control.Request{
		Type: "agent_event",
		AgentEvent: &control.AgentEventRequest{
			Source:   fs.Arg(0),
			WorkerID: strings.TrimSpace(os.Getenv("MEWLA_WORKER_ID")),
			Payload:  payload,
		},
	}, 595*time.Second)
	if err != nil || !resp.OK {
		return nil
	}
	if resp.HookOutput != "" {
		fmt.Fprint(os.Stdout, resp.HookOutput)
	}
	return nil
}

// claudeHookCommand is the command line the hook file runs for Claude.
func claudeHookCommand(executable, stateDir string) string {
	return shellquote.Word(executable) + " agent-event -state-dir " + shellquote.Word(stateDir) + " " + claude.Provider
}

// claudeSettingsPath is where the daemon keeps Claude's hook file.
func claudeSettingsPath(stateDir string) string {
	return filepath.Join(stateDir, "agent-events", "claude-settings.json")
}

// approvals lists the decisions agents are waiting on, for one Worker or all.
func (s *agentEventService) approvals(workerID string) []control.Approval {
	out := []control.Approval{}
	for _, id := range s.hub.Workers() {
		if workerID != "" && id != workerID {
			continue
		}
		state, _ := s.hub.State(id)
		for _, a := range state.Approvals {
			item := control.Approval{ID: a.ID, WorkerID: id, Kind: a.Kind, Tool: a.Tool, Summary: a.Summary, Options: []control.ApprovalOption{}}
			for _, o := range a.Options {
				item.Options = append(item.Options, control.ApprovalOption{ID: o.ID, Label: o.Label, Scope: o.Scope})
			}
			out = append(out, item)
		}
	}
	return out
}

// decide answers a pending approval with one of its options. A late or
// unknown answer is refused; it is never turned into a keystroke.
func (s *agentEventService) decide(workerID, approvalID, optionID string) control.Response {
	state, ok := s.hub.State(workerID)
	if !ok {
		return control.ErrorResponse("approval_not_pending", "That approval is not pending.")
	}
	var chosen *agentevents.Approval
	for i := range state.Approvals {
		if state.Approvals[i].ID == approvalID {
			chosen = &state.Approvals[i]
		}
	}
	// An answer resolves only a request the Worker's current conversation made.
	if chosen != nil && state.Bound && chosen.ConversationID != state.Binding.ConversationID {
		chosen = nil
	}
	if chosen == nil {
		return control.ErrorResponse("approval_not_pending", "That approval is not pending.")
	}
	offered := false
	for _, o := range chosen.Options {
		offered = offered || o.ID == optionID
	}
	if !offered {
		return control.ErrorResponse("invalid_approval_option", "The agent did not offer that option.")
	}
	if err := s.hub.Decide(workerID, approvalID, optionID, s.now().UTC()); err != nil {
		return control.ErrorResponse("approval_not_pending", "That approval is not pending.")
	}
	return control.Response{OK: true}
}

// agentLevelsFor says, for one Worker, how Mewla performs each capability: by
// the adapter of the Worker's executor once that adapter's events are flowing
// for it, and by screen and keystrokes until then.
func agentLevelsFor(execs *work.ExecutorConfig, adapters *agentevents.Registry, hub *agentevents.Hub) func(*classifier.Worker) map[string]string {
	return func(worker *classifier.Worker) map[string]string {
		if worker == nil {
			return nil
		}
		provider := providerOf(execs, worker.ExecutorID, worker.Command)
		state, known := hub.State(worker.ID)
		return adapters.Levels(provider, known && !state.Exited)
	}
}

// providerOf is the provider of an executor, or of the program in a command no
// executor claims.
func providerOf(execs *work.ExecutorConfig, executorID, command string) string {
	if execs != nil {
		if executor, ok := execs.WorkerExecutor(executorID); ok && executor.Provider != "" {
			return executor.Provider
		}
	}
	return work.InferWorkerProvider(command)
}

// launchAdapterFor lets the adapter of a session's agent add what lets Mewla
// follow it from its first moment (a session id, a hook file).
func launchAdapterFor(execs *work.ExecutorConfig, adapters *agentevents.Registry) func(executorID, command string) (agentevents.Launch, error) {
	return func(executorID, command string) (agentevents.Launch, error) {
		if adapter, ok := adapters.For(providerOf(execs, executorID, command)); ok {
			return adapter.PrepareLaunch(command)
		}
		return agentevents.Launch{Command: command}, nil
	}
}

// appApprovals is what the app sees of an agent's pending decisions, and how
// its answer gets back.
type appApprovals struct{ service *agentEventService }

func (a appApprovals) Pending(workerID string) []server.ApprovalWire {
	pending := a.service.approvals(workerID)
	if len(pending) == 0 {
		return nil
	}
	out := make([]server.ApprovalWire, 0, len(pending))
	for _, item := range pending {
		wire := server.ApprovalWire{ID: item.ID, Kind: item.Kind, Tool: item.Tool, Summary: item.Summary, Options: []server.ApprovalOptionWire{}}
		for _, o := range item.Options {
			wire.Options = append(wire.Options, server.ApprovalOptionWire{ID: o.ID, Label: o.Label, Scope: o.Scope})
		}
		out = append(out, wire)
	}
	return out
}

func (a appApprovals) Decide(workerID, approvalID, optionID string) error {
	response := a.service.decide(workerID, approvalID, optionID)
	if response.OK {
		return nil
	}
	if response.Error == nil {
		return &server.ApprovalDecisionError{Code: "approval_failed", Message: "The answer was not taken."}
	}
	return &server.ApprovalDecisionError{Code: response.Error.Code, Message: response.Error.Message}
}
