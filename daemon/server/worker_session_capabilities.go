package server

import (
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

type workerSessionWireCapabilities struct {
	StructuredEvents         bool `json:"structured_events"`
	ModelProfileManaged      bool `json:"model_profile_managed"`
	ModelProfileActiveSwitch bool `json:"model_profile_active_switch"`
	// MessageRewind is set when the Session's own CLI can step the conversation
	// back to an earlier prompt, so editing a message can rewind instead of
	// sending an edited copy as a new message.
	MessageRewind bool `json:"message_rewind"`
	// Levels says how Mewla performs each thing for this Worker: binding,
	// events, send, approve and interrupt, each one of structured, hook, file,
	// keystroke or none. The app offers an action by its level, not by
	// guessing the agent from its command line.
	Levels map[string]string `json:"levels,omitempty"`
}

type workerSessionWire struct {
	*classifier.Worker
	// Provider is the adapter family of the Worker's executor (claude, codex,
	// cursor, ...), empty for a plain terminal. The app presents the Worker by
	// it instead of reading the agent out of the command line.
	Provider string `json:"provider,omitempty"`
	// Approvals are the decisions the agent is waiting on right now.
	Approvals    []ApprovalWire                `json:"approvals,omitempty"`
	Capabilities workerSessionWireCapabilities `json:"capabilities"`
}

func (s *Server) workerSessionWire(worker *classifier.Worker) *workerSessionWire {
	if worker == nil {
		return nil
	}
	managed, activeSwitch := s.modelProfileSessionCapabilities(worker.ID)
	if work.DSHSessionID(worker.Command) != "" {
		managed = true
		activeSwitch = true
	}
	executorID, provider := s.workerExecutor(worker)
	if executorID != worker.ExecutorID {
		named := *worker
		named.ExecutorID = executorID
		worker = &named
	}
	return &workerSessionWire{
		Worker:    worker,
		Provider:  provider,
		Approvals: approvalsOf(worker, s),
		Capabilities: workerSessionWireCapabilities{
			StructuredEvents:         s.workerSupportsStructuredEvents(worker),
			ModelProfileManaged:      managed,
			ModelProfileActiveSwitch: activeSwitch,
			MessageRewind:            s.workerSupportsMessageRewind(worker),
			Levels:                   s.agentLevels(worker),
		},
	}
}

// lookupAgent returns the live watcher Agent for sessionID. Missing watcher /
// agent fails closed (nil) so Brain Host capabilities never invent presence.
func (s *Server) lookupWorker(sessionID string) *classifier.Worker {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s == nil {
		return nil
	}
	if s.getWorkerOverride != nil {
		return s.getWorkerOverride(sessionID)
	}
	if s.watcher == nil {
		return nil
	}
	return s.watcher.GetWorker(sessionID)
}

func (s *Server) hostWorkerWireCapabilities(sessionID string) workerSessionWireCapabilities {
	worker := s.lookupWorker(sessionID)
	if worker == nil {
		return workerSessionWireCapabilities{}
	}
	wire := s.workerSessionWire(worker)
	if wire == nil {
		return workerSessionWireCapabilities{}
	}
	return wire.Capabilities
}

// modelProfileSessionCapabilities reads the authoritative Model Profiles route
// table only. Command/name heuristics must never authorize App actions.
func (s *Server) modelProfileSessionCapabilities(sessionID string) (managed, activeSwitch bool) {
	if s == nil {
		return false, false
	}
	owner := s.modelProfiles()
	if owner == nil {
		return false, false
	}
	caps := owner.SessionRouteCapabilities(sessionID)
	return caps.Managed, caps.ActiveSwitch
}

func (s *Server) workerSessionsWire(workers []*classifier.Worker) []*workerSessionWire {
	if len(workers) == 0 {
		return nil
	}
	out := make([]*workerSessionWire, 0, len(workers))
	for _, worker := range workers {
		if wire := s.workerSessionWire(worker); wire != nil {
			out = append(out, wire)
		}
	}
	return out
}

func (s *Server) workerSupportsStructuredEvents(worker *classifier.Worker) bool {
	return s.structuredProviderForWorker(worker) != ""
}

func (s *Server) structuredProviderForWorker(worker *classifier.Worker) string {
	if worker == nil {
		return ""
	}
	// A real process command outranks the display title. Titles are user-facing
	// and may mention a provider without the shell actually running one.
	provider := work.InferWorkerProvider(worker.Command)
	if strings.TrimSpace(worker.Command) == "" {
		provider = work.InferWorkerProvider(worker.Name)
	}
	if provider != "" {
		portable := work.NewWorkerExecutor(provider, work.Executor{
			Name:    provider,
			Kind:    provider,
			Command: worker.Command,
		})
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	if s == nil || s.execs == nil {
		return ""
	}
	for name, configured := range s.execs.ByName {
		if !configuredExecutorMatchesWorker(name, configured, worker) {
			continue
		}
		portable := work.NewWorkerExecutor(name, configured)
		if portable.Capabilities.StructuredEvents {
			return portable.Provider
		}
	}
	return ""
}

func configuredExecutorMatchesWorker(name string, configured work.Executor, worker *classifier.Worker) bool {
	if worker == nil {
		return false
	}
	workerCommand := strings.TrimSpace(worker.Command)
	configuredCommand := strings.TrimSpace(configured.Command)
	if workerCommand != "" && configuredCommand != "" && workerCommand == configuredCommand {
		return true
	}
	workerExecutable := workerCommandExecutable(workerCommand)
	configuredExecutable := workerCommandExecutable(configuredCommand)
	if workerExecutable != "" && workerExecutable == configuredExecutable && !ambiguousWorkerExecutable(workerExecutable) {
		return true
	}
	if workerCommand != "" {
		configuredName := strings.TrimSpace(name)
		if configuredName == "" {
			configuredName = strings.TrimSpace(configured.Name)
		}
		return workerExecutable != "" && workerExecutable == filepath.Base(configuredName)
	}
	configuredName := strings.TrimSpace(name)
	if configuredName == "" {
		configuredName = strings.TrimSpace(configured.Name)
	}
	workerName := strings.TrimSpace(worker.Name)
	if workerName != "" && (workerName == configuredName || workerName == strings.TrimSpace(configured.Name)) {
		return true
	}
	return false
}

func workerCommandExecutable(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(strings.Trim(fields[0], `"'`))
}

func ambiguousWorkerExecutable(executable string) bool {
	switch strings.ToLower(strings.TrimSpace(executable)) {
	case "bash", "bun", "deno", "fish", "node", "python", "python3", "sh", "zsh":
		return true
	default:
		return false
	}
}

func (s *Server) workerSupportsMessageRewind(worker *classifier.Worker) bool {
	if worker == nil || work.DSHSessionID(worker.Command) != "" {
		return false
	}
	_, ok := messageRewindProvider(s.structuredProviderForWorker(worker))
	return ok
}

// SetAgentLevels installs the function that says, for one Worker, how Mewla
// performs binding, events, send, approve and interrupt.
func (s *Server) SetAgentLevels(levels func(*classifier.Worker) map[string]string) {
	s.mu.Lock()
	s.agentLevelsFn = levels
	s.mu.Unlock()
}

func (s *Server) agentLevels(worker *classifier.Worker) map[string]string {
	s.mu.Lock()
	levels := s.agentLevelsFn
	s.mu.Unlock()
	if levels == nil {
		return nil
	}
	return levels(worker)
}

// workerExecutor names the executor a Worker runs and its provider. A Worker
// started before panes recorded their executor is placed by the configured
// executor that starts the same program; no other source is consulted, so an
// unclaimed command is a plain terminal.
func (s *Server) workerExecutor(worker *classifier.Worker) (executorID, provider string) {
	if s == nil || s.execs == nil || worker == nil {
		return "", ""
	}
	executorID = worker.ExecutorID
	if executorID == "" {
		executorID = s.execs.IDForCommand(worker.Command)
	}
	if executor, ok := s.execs.WorkerExecutor(executorID); ok {
		return executorID, executor.Provider
	}
	return executorID, ""
}
