package watcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/google/uuid"
)

// Agent relaunch: a delegated Worker whose agent program ended while its Work
// is open is started again by Mewla in the same Session (same id, labels,
// resource unit and TMPDIR, input receipts) and in the same conversation, with
// no action from the person or Brain.
//
// The watcher sees the ended program (the session host keeps a Session whose
// program exited). Before any liveness fact of that death is applied, it asks
// the AgentRelaunchLedger (Brain) for a plan: whether the Session belongs to an
// open Work, how the agent resumes its conversation (one path for every agent:
// the Work's recorded launch and conversation id through
// work.WithProviderResumeToken), and whether the bound allows another
// relaunch. Then either
//   - relaunch: the death is held back from the Turn ledger; the dead agent's
//     leftover processes are stopped; the Session's program is replaced in
//     place; a Turn that was in flight gets one continuation prompt through
//     the normal delegated admission path (as a follow-up of that Turn, never
//     its prompt again); one durable Work Event records it; or
//   - report: the death goes to the Turn ledger as today, with plain words of
//     how the agent ended and why it was not started again (Brain gets one
//     turn_lost review), or, with no Turn in flight, one review on the Work; or
//   - ignore: not Mewla's to act on (no open Work), as before.

// errSessionGone means the Session a relaunch was for no longer exists: it was
// closed, so it is not started again.
var errSessionGone = errors.New("the Session is gone")

// errHostCannotRespawn: the running session host is from before respawn, so
// it cannot start an agent again in place; it is replaced only when no agent
// runs in it.
var errHostCannotRespawn = errors.New("this computer's session host is older than Mewla and cannot start an agent again in place")

// AgentEnd is one ended agent program in a delegated Session.
type AgentEnd struct {
	SessionID  string
	Name       string // the Session's name as it was created
	Cwd        string
	Command    string // the Worker's observed command
	ExecutorID string
	// Control is the Worker's agent control socket (a Codex app-server's).
	Control string
	Exit    SessionExit
	// TurnID is the live delegated Turn the agent was in when it ended, if any.
	TurnID string
	// PendingTurnID is a delegated prompt in flight that the agent never
	// checked in on (no Turn yet), and PromptDelivery whether its
	// conversation holds it. A held prompt is accepted as TurnID before the
	// plan is asked for.
	PendingTurnID  string
	PromptDelivery PromptDelivery
	pending        InputAdmission
	// OutOfMemory is set when the kernel's OOM killer ran at the time the
	// agent was killed (Linux; a likely, not a proven, cause).
	OutOfMemory bool
}

// Cause says how the agent ended, in words: "was killed (SIGKILL)", "exited
// with code 1", "was stopped by the out-of-memory killer (SIGKILL)".
func (e AgentEnd) Cause() string {
	signal := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(e.Exit.Signal), "SIG"))
	switch {
	case signal != "" && e.OutOfMemory:
		return "was stopped by the out-of-memory killer (SIG" + signal + ")"
	case signal != "":
		return "was killed (SIG" + signal + ")"
	case e.Exit.Code != nil && *e.Exit.Code > 128 && *e.Exit.Code < 160:
		return fmt.Sprintf("exited with code %d (signal %d)", *e.Exit.Code, *e.Exit.Code-128)
	case e.Exit.Code != nil:
		return fmt.Sprintf("exited with code %d", *e.Exit.Code)
	}
	return "ended"
}

// ShortCause is Cause in a person's words, for the Session's line in the
// app: "running out of memory", else "a crash". The exact signal or exit
// code stays in Cause, for Brain and the log.
func (e AgentEnd) ShortCause() string {
	if e.OutOfMemory {
		return "running out of memory"
	}
	return "a crash"
}

// Clean reports an exit with code 0 and no signal: the agent ended itself.
func (e AgentEnd) Clean() bool {
	return e.Exit.Signal == "" && e.Exit.Code != nil && *e.Exit.Code == 0
}

// AgentEndAction is what Mewla does about an ended agent.
type AgentEndAction string

const (
	// AgentEndIgnore: the Session is no open Work's; nothing changes.
	AgentEndIgnore AgentEndAction = ""
	// AgentEndRelaunch: start the agent again in the same Session and
	// conversation.
	AgentEndRelaunch AgentEndAction = "relaunch"
	// AgentEndReport: not started again; Brain is told once, with Words.
	AgentEndReport AgentEndAction = "report"
	// AgentEndWait: the agent's provider is refusing every request (a
	// provider outage): the end is held back and no try is used; it is
	// planned again after agentEndWaitEvery.
	AgentEndWait AgentEndAction = "wait"
)

// agentEndWaitEvery is how often an end waiting out a provider outage is
// planned again.
const agentEndWaitEvery = 30 * time.Second

// AgentRelaunchPlan is the ledger's decision for one ended agent.
type AgentRelaunchPlan struct {
	Action AgentEndAction
	WorkID string
	// TurnID is the Turn in flight the relaunched agent is asked to continue;
	// "" when the Worker was idle. PendingTurnID is a prompt in flight the
	// agent never checked in on: sent again when its conversation does not
	// hold it (AgentEnd.PromptDelivery), else reported to Brain.
	TurnID        string
	PendingTurnID string
	// Name and Cwd are what the Session was created with; Agent names the
	// agent ("Claude Code").
	Name  string
	Cwd   string
	Agent string
	// Command is the agent's launch, resuming its conversation; Env is the
	// launch environment (progress tools, a Model Profile route).
	Command          string
	Env              map[string]string
	PrepareWorkspace func(command, cwd string, env map[string]string) (string, error)
	Conversation     string
	// Attempt is this relaunch's number within the bound's window.
	Attempt int
	// After is how long to wait before this relaunch.
	After time.Duration
	// Words say, for Brain and the Work's history, what happened.
	Words string
	// Line is what the Session's own line says meanwhile, short: "Restarting
	// after it was killed (SIGKILL)", "Not restarted after it exited with code
	// 1: 3 restarts in 30 minutes".
	Line string
}

// AgentRelaunch is the durable record of one relaunch.
type AgentRelaunch struct {
	WorkID       string
	SessionID    string
	Conversation string
	Attempt      int
	Cause        string
	ExitCode     *int
	ExitSignal   string
	EndedAt      time.Time
	// FromTurnID is the Turn in flight when the agent ended; TurnID the
	// continuation's Turn ("" when idle or not submitted).
	FromTurnID string
	TurnID     string
	// Outcome is "continued", "idle", "continuation_unknown", "not_ready",
	// or "start_failed" (Error says why): every try counts toward the bound.
	Outcome string
	Error   string
	// Name is the agent's product name ("Claude Code").
	Name  string
	Words string
	At    time.Time
}

// AgentRelaunchLedger is Brain's half of agent relaunch.
type AgentRelaunchLedger interface {
	PlanAgentRelaunch(end AgentEnd) (AgentRelaunchPlan, error)
	RecordAgentRelaunch(record AgentRelaunch) error
	// ReportAgentEnd tells Brain once that an ended agent is not started
	// again, for a Worker that has no Turn whose death the Turn ledger will
	// report itself (an idle Worker, or a relaunched one that never became
	// ready). told is false while an earlier review of the Work is still
	// with Brain: the report is asked again until Brain has it.
	ReportAgentEnd(end AgentEnd, plan AgentRelaunchPlan) (told bool, err error)
}

// AgentContinuationPrompt is what a relaunched agent that was in a Turn is
// sent, with a fresh turn contract. survivors is survivorList's lines: what
// the dead agent left running, untouched.
func AgentContinuationPrompt(cause, survivors string) string {
	prompt := "Your agent process " + strings.TrimSpace(cause) + " in the middle of this task, and Mewla started it again in the same conversation. " +
		"Mewla stopped nothing: the command you were running when it ended may have finished, may still be running, or may have run only in part."
	if strings.TrimSpace(survivors) != "" {
		prompt += "\n\nStill running from before it ended:\n" + survivors + "\n\n"
	} else {
		prompt += " Nothing it started is still running. "
	}
	return prompt + "Look at the state of the work (files, outputs, what is still running) before you repeat any step, then continue the task from where you stopped."
}

// survivorWords is the survivors in one line for Brain's event: "Still
// running from before: python3 -m http.server 18473 (running 12m)."
func survivorWords(survivors []agentSurvivor, now time.Time) string {
	if len(survivors) == 0 {
		return ""
	}
	list := strings.ReplaceAll(strings.TrimPrefix(survivorList(survivors, now), "- "), "\n- ", "; ")
	return " Still running from before: " + list + "."
}

// agentSurvivors lists what the Session's dead agent left running in its
// resource unit; nothing on a Session without one. The agent's own control
// server (named by its control socket) is the agent's, not something it
// started: a relaunch attaches to it again.
func (w *Watcher) agentSurvivors(sessionID, control string) []agentSurvivor {
	delegated, unit := w.sessions().delegatedResource(sessionID)
	if !delegated || unit == "" {
		return nil
	}
	reuser, ok := w.resourceManager().(delegatedResourceReuser)
	if !ok {
		return nil
	}
	survivors, err := reuser.Survivors(unit)
	if err != nil {
		log.Printf("agent relaunch: list what %s left running: %v", sessionID, err)
	}
	control = strings.TrimSpace(control)
	if control == "" {
		return survivors
	}
	out := survivors[:0]
	for _, survivor := range survivors {
		if !strings.Contains(strings.Join(survivor.args, " "), control) {
			out = append(out, survivor)
		}
	}
	return out
}

// agentRelaunchReadyBudget bounds how long a relaunched agent may take to
// show its input before its continuation counts as not delivered.
const agentRelaunchReadyBudget = 90 * time.Second

// agentRelaunchHold is how long a relaunched Session's old Turn is kept from
// liveness facts while its continuation is admitted.
const agentRelaunchHold = 10 * time.Minute

type agentEndDecision struct {
	endedAt time.Time
	plan    AgentRelaunchPlan
	// untold is the end of a Worker not started again whose report waits for
	// an earlier review of its Work to leave Brain; asked again at nextTell.
	untold   *AgentEnd
	nextTell time.Time
	// recheck is when a wait (AgentEndWait) is planned again.
	recheck time.Time
}

// agentAfterReport is a report made after a relaunch (its task was not
// resumed) that Brain does not have yet: asked again every agentEndTellEvery
// while the relaunched Session lives, until Brain has it.
type agentAfterReport struct {
	end  AgentEnd
	plan AgentRelaunchPlan
	next time.Time
}

// reportAfterRelaunch tells Brain that a relaunched agent's task is not
// running, and shows it on the Session's line until the agent works again.
func (w *Watcher) reportAfterRelaunch(ledger AgentRelaunchLedger, end AgentEnd, plan AgentRelaunchPlan, line string) {
	supervisor := w.agentRelaunch()
	supervisor.setLine(end.SessionID, agentRelaunchLine{kind: agentLineHalted, text: line})
	w.retellAfterRelaunch(ledger, agentAfterReport{end: end, plan: plan})
}

func (w *Watcher) retellAfterRelaunch(ledger AgentRelaunchLedger, report agentAfterReport) {
	supervisor := w.agentRelaunch()
	told, err := ledger.ReportAgentEnd(report.end, report.plan)
	if err != nil {
		log.Printf("agent relaunch: report %s: %v", report.end.SessionID, err)
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if told {
		delete(supervisor.after, report.end.SessionID)
		return
	}
	report.next = supervisor.now().Add(agentEndTellEvery)
	supervisor.after[report.end.SessionID] = report
}

// agentEndTellEvery is how often a not-started-again report that waits behind
// an earlier review is asked again.
const agentEndTellEvery = 10 * time.Second

type agentRelaunchRun struct {
	// swapping is set while the old Session is replaced: the Session may
	// briefly be absent and must not read as gone.
	swapping bool
	fromTurn string
}

type agentRelaunchSupervisor struct {
	mu      sync.Mutex
	decided map[string]agentEndDecision
	running map[string]*agentRelaunchRun
	// held is the old Turn of a relaunched Session and until when it is kept
	// from liveness facts and stall supervision.
	held map[string]agentRelaunchHeld
	// lines is what each Session's line says about its agent's end.
	lines map[string]agentRelaunchLine
	// swap serializes a Session's replacement with KillSession.
	swap map[string]*sync.Mutex
	// after holds reports made after a relaunch that Brain does not have yet.
	after map[string]agentAfterReport
	oom   oomSamples
	// submit delivers a continuation once the relaunched agent is ready.
	submit func(sessionID, command, payload, workID, turnID string, at time.Time) (InputResult, error)
	run    func(func())
	now    func() time.Time
	// sleep waits before a relaunch with a backoff.
	sleep func(time.Duration)
	// nextStart and lastStarter space relaunches of different Sessions by
	// agentRelaunchStagger: after a mass death (an OOM sweep, a reboot of the
	// agents' host) the agents do not all start, load and resume at once.
	nextStart   time.Time
	lastStarter string
}

// agentRelaunchStagger is the least time between two Sessions' relaunches.
const agentRelaunchStagger = 5 * time.Second

// startSlot reserves sessionID's start and says how long to wait for it. A
// Session's own successive relaunches are spaced by their backoff instead.
func (s *agentRelaunchSupervisor) startSlot(sessionID string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	slot := now
	if s.lastStarter != sessionID && s.nextStart.After(now) {
		slot = s.nextStart
	}
	if next := slot.Add(agentRelaunchStagger); next.After(s.nextStart) {
		s.nextStart = next
	}
	s.lastStarter = sessionID
	return slot.Sub(now)
}

type agentRelaunchHeld struct {
	turnID string
	until  time.Time
}

// agentRelaunchLine is what a Session's line says about its agent's end.
type agentRelaunchLine struct {
	kind  agentLineKind
	text  string
	until time.Time // restarted lines only
}

type agentLineKind uint8

const (
	agentLineRestarting agentLineKind = iota + 1
	agentLineRestarted
	agentLineStopped
	// agentLineHalted: started again, but nothing runs for its open task
	// (Brain was told to decide). Unlike stopped it stays on a live Session,
	// until the agent works again.
	agentLineHalted
)

// agentRelaunchShown is how long "Restarted after …" stays on a relaunched
// Session's line while its agent shows nothing newer.
const agentRelaunchShown = 10 * time.Minute

func (s *agentRelaunchSupervisor) setLine(sessionID string, line agentRelaunchLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if line.text == "" {
		delete(s.lines, sessionID)
		return
	}
	s.lines[sessionID] = line
}

// line is the Session's state and line while its agent's end is what there
// is to say: restarting (shown as working), not restarted (failed), and for a
// while after a restart, "Restarted after …" until the agent works or asks.
func (s *agentRelaunchSupervisor) line(sessionID string, alive bool, state classifier.WorkerState, now time.Time) (classifier.WorkerState, classifier.Relaunch, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, ok := s.lines[sessionID]
	if !ok {
		return state, classifier.Relaunch{}, false
	}
	switch line.kind {
	case agentLineRestarting:
		return classifier.StateRunning, classifier.Relaunch{State: "restarting", Line: line.text}, true
	case agentLineStopped:
		if alive {
			delete(s.lines, sessionID)
			return state, classifier.Relaunch{}, false
		}
		return classifier.StateFailed, classifier.Relaunch{State: "stopped", Line: line.text}, true
	case agentLineHalted:
		if state == classifier.StateRunning {
			delete(s.lines, sessionID)
			return state, classifier.Relaunch{}, false
		}
		return classifier.StateBlocked, classifier.Relaunch{State: "stopped", Line: line.text}, true
	case agentLineRestarted:
		if !alive || now.After(line.until) {
			delete(s.lines, sessionID)
			return state, classifier.Relaunch{}, false
		}
		switch state {
		case classifier.StateRunning, classifier.StateBlocked, classifier.StateFailed:
			// The agent shows what it does now.
			delete(s.lines, sessionID)
			return state, classifier.Relaunch{}, false
		}
		return state, classifier.Relaunch{State: "restarted", Line: line.text}, true
	}
	return state, classifier.Relaunch{}, false
}

func newAgentRelaunchSupervisor() *agentRelaunchSupervisor {
	return &agentRelaunchSupervisor{
		decided: map[string]agentEndDecision{},
		running: map[string]*agentRelaunchRun{},
		held:    map[string]agentRelaunchHeld{},
		lines:   map[string]agentRelaunchLine{},
		swap:    map[string]*sync.Mutex{},
		after:   map[string]agentAfterReport{},
		run:     func(fn func()) { go fn() },
		now:     time.Now,
		sleep:   time.Sleep,
	}
}

func (w *Watcher) agentRelaunch() *agentRelaunchSupervisor {
	w.relaunchOnce.Do(func() {
		if w.relaunch == nil {
			w.relaunch = newAgentRelaunchSupervisor()
		}
		if w.relaunch.submit == nil {
			w.relaunch.submit = func(sessionID, command, payload, workID, turnID string, at time.Time) (InputResult, error) {
				return w.SubmitDelegatedInputWhenReadyBudgeted(sessionID, command, payload, workID, turnID, at, agentRelaunchReadyBudget)
			}
		}
	})
	return w.relaunch
}

func (s *agentRelaunchSupervisor) swapLock(sessionID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.swap[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.swap[sessionID] = lock
	}
	return lock
}

// busy reports a relaunch in progress for the Session.
func (s *agentRelaunchSupervisor) busy(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[sessionID] != nil
}

// swapping reports a Session being replaced right now: its absence proves
// nothing.
func (s *agentRelaunchSupervisor) swapping(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.running[sessionID]
	return run != nil && run.swapping
}

// holds reports that turnID on the Session belongs to an agent Mewla is
// relaunching or relaunched: its old process is gone on purpose, so no
// liveness fact or stall decision applies to it.
func (s *agentRelaunchSupervisor) holds(sessionID, turnID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.running[sessionID]; run != nil {
		return true
	}
	held, ok := s.held[sessionID]
	if !ok {
		return false
	}
	if s.now().After(held.until) {
		delete(s.held, sessionID)
		return false
	}
	return turnID == "" || held.turnID == turnID
}

// observeAgentEnd is called for every polled delegated Session before its
// Turn facts are applied. It returns hold=true when the ended program is being
// relaunched, so its death must not reach the Turn ledger, and words to carry
// on the death when it does.
func (w *Watcher) observeAgentEnd(item preparedPollWorker, turn TurnSnapshot, hasTurn bool, pending []InputAdmission, provider ProviderActivityObservation) (hold bool, words string) {
	supervisor := w.agentRelaunch()
	if item.alive {
		supervisor.mu.Lock()
		delete(supervisor.decided, item.id)
		report, untold := supervisor.after[item.id]
		supervisor.mu.Unlock()
		if untold && !supervisor.now().Before(report.next) {
			if ledger, ok := w.turnLedger.(AgentRelaunchLedger); ok {
				w.retellAfterRelaunch(ledger, report)
			}
		}
		return supervisor.holds(item.id, turn.TurnID), ""
	}
	if supervisor.busy(item.id) {
		return true, ""
	}
	ledger, ok := w.turnLedger.(AgentRelaunchLedger)
	if !ok || !item.workerSnap.Delegated || item.workerSnap.Hidden {
		return false, ""
	}
	exit, ended := w.SessionExitReport(item.id)
	if !ended {
		return false, ""
	}
	supervisor.mu.Lock()
	decision, seen := supervisor.decided[item.id]
	supervisor.mu.Unlock()
	if seen && decision.endedAt.Equal(exit.EndedAt) && decision.plan.Action == AgentEndWait {
		if supervisor.now().Before(decision.recheck) {
			return true, ""
		}
		seen = false // planned again below
	}
	if seen && decision.endedAt.Equal(exit.EndedAt) {
		if decision.untold != nil && !supervisor.now().Before(decision.nextTell) {
			w.tellAgentEnd(ledger, *decision.untold, decision.plan)
		}
		return decision.plan.Action == AgentEndRelaunch, decision.plan.Words
	}
	end := AgentEnd{
		SessionID: item.id, Name: item.workerSnap.Window, Cwd: item.workerSnap.Cwd, Command: item.workerSnap.Command,
		ExecutorID: item.workerSnap.ExecutorID, Control: item.workerSnap.AgentControl, Exit: exit,
	}
	if hasTurn && !TurnImmutable(turn.Status) {
		end.TurnID = turn.TurnID
	}
	// An end decided before (by an earlier daemon) keeps its decision: a
	// restart never judges the same end again.
	if stored, ok := w.storedAgentEnd(item.id, exit.EndedAt); ok {
		plan := AgentRelaunchPlan{Action: stored.Action, WorkID: stored.WorkID, TurnID: stored.TurnID, PendingTurnID: stored.Pending, Words: stored.Words, Line: stored.Line}
		supervisor.mu.Lock()
		supervisor.decided[item.id] = agentEndDecision{endedAt: exit.EndedAt, plan: plan}
		supervisor.mu.Unlock()
		if plan.Action != AgentEndReport {
			return false, ""
		}
		supervisor.setLine(item.id, agentRelaunchLine{kind: agentLineStopped, text: plan.Line})
		if stored.TurnID == "" && !stored.Told {
			w.tellAgentEnd(ledger, end, plan)
		}
		return false, plan.Words
	}
	if exit.Signal != "" {
		end.OutOfMemory = supervisor.oom.killedAround(exit.EndedAt, supervisor.now())
	}
	if end.TurnID == "" {
		w.readPromptInFlight(&end, pending, provider)
	}
	plan, err := ledger.PlanAgentRelaunch(end)
	if err != nil {
		log.Printf("agent relaunch: plan for %s: %v", item.id, err)
		return false, ""
	}
	supervisor.mu.Lock()
	supervisor.decided[item.id] = agentEndDecision{endedAt: exit.EndedAt, plan: plan}
	supervisor.mu.Unlock()
	if plan.Action != AgentEndRelaunch && plan.Action != AgentEndWait {
		// Final: kept on the Session for any later daemon.
		w.storeAgentEnd(item.id, agentEndRecord{EndedAt: exit.EndedAt, Action: plan.Action, WorkID: plan.WorkID,
			TurnID: end.TurnID, Pending: plan.PendingTurnID, Words: plan.Words, Line: plan.Line})
	}
	switch plan.Action {
	case AgentEndWait:
		log.Printf("agent relaunch: %s %s", item.id, plan.Words)
		supervisor.setLine(item.id, agentRelaunchLine{kind: agentLineRestarting, text: plan.Line})
		supervisor.mu.Lock()
		supervisor.decided[item.id] = agentEndDecision{endedAt: exit.EndedAt, plan: plan, recheck: supervisor.now().Add(agentEndWaitEvery)}
		supervisor.mu.Unlock()
		return true, ""
	case AgentEndRelaunch:
		supervisor.setLine(item.id, agentRelaunchLine{kind: agentLineRestarting, text: restartingLine(plan.Line)})
		supervisor.mu.Lock()
		supervisor.running[item.id] = &agentRelaunchRun{fromTurn: end.TurnID}
		supervisor.mu.Unlock()
		supervisor.run(func() { w.relaunchAgent(ledger, end, plan) })
		return true, plan.Words
	case AgentEndReport:
		supervisor.setLine(item.id, agentRelaunchLine{kind: agentLineStopped, text: plan.Line})
		if end.TurnID == "" {
			// No Turn will carry this death to Brain: the ledger tells it once.
			w.tellAgentEnd(ledger, end, plan)
		}
		return false, plan.Words
	}
	return false, ""
}

// tellAgentEnd reports an agent not started again to Brain; while an earlier
// review of its Work is still with Brain, the report is kept and asked again.
func (w *Watcher) tellAgentEnd(ledger AgentRelaunchLedger, end AgentEnd, plan AgentRelaunchPlan) {
	supervisor := w.agentRelaunch()
	told, err := ledger.ReportAgentEnd(end, plan)
	if err != nil {
		log.Printf("agent relaunch: report %s: %v", end.SessionID, err)
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	decision, ok := supervisor.decided[end.SessionID]
	if !ok || !decision.endedAt.Equal(end.Exit.EndedAt) {
		return
	}
	decision.untold = nil
	if !told {
		kept := end
		decision.untold = &kept
		decision.nextTell = supervisor.now().Add(agentEndTellEvery)
	}
	supervisor.decided[end.SessionID] = decision
	if told {
		go w.storeAgentEnd(end.SessionID, agentEndRecord{EndedAt: end.Exit.EndedAt, Action: plan.Action, WorkID: plan.WorkID,
			Pending: plan.PendingTurnID, Words: plan.Words, Line: plan.Line, Told: true})
	}
}

// decideAgentEnd makes plan, a report, the decision about end after a
// relaunch was planned: kept, shown, and told (an idle Worker) or carried on
// the dead Turn by the next poll.
func (w *Watcher) decideAgentEnd(ledger AgentRelaunchLedger, end AgentEnd, plan AgentRelaunchPlan) {
	supervisor := w.agentRelaunch()
	supervisor.mu.Lock()
	supervisor.decided[end.SessionID] = agentEndDecision{endedAt: end.Exit.EndedAt, plan: plan}
	supervisor.mu.Unlock()
	w.storeAgentEnd(end.SessionID, agentEndRecord{EndedAt: end.Exit.EndedAt, Action: plan.Action, WorkID: plan.WorkID,
		TurnID: plan.TurnID, Pending: plan.PendingTurnID, Words: plan.Words, Line: plan.Line})
	supervisor.setLine(end.SessionID, agentRelaunchLine{kind: agentLineStopped, text: plan.Line})
	if plan.TurnID == "" {
		w.tellAgentEnd(ledger, end, plan)
	}
}

// agentEndRecord is a final decision about one ended agent, kept on its
// Session (sessionBackend.relaunchData).
type agentEndRecord struct {
	EndedAt time.Time      `json:"ended_at"`
	Action  AgentEndAction `json:"action"`
	WorkID  string         `json:"work_id,omitempty"`
	TurnID  string         `json:"turn_id,omitempty"`
	Pending string         `json:"pending_turn_id,omitempty"`
	Words   string         `json:"words,omitempty"`
	Line    string         `json:"line,omitempty"`
	// Told is set once Brain has the report.
	Told bool `json:"told,omitempty"`
}

func (w *Watcher) storedAgentEnd(sessionID string, endedAt time.Time) (agentEndRecord, bool) {
	raw, err := w.sessions().relaunchData(sessionID, hostDataAgentEnd)
	if err != nil || strings.TrimSpace(raw) == "" {
		return agentEndRecord{}, false
	}
	var record agentEndRecord
	if json.Unmarshal([]byte(raw), &record) != nil || !record.EndedAt.Equal(endedAt) {
		return agentEndRecord{}, false
	}
	return record, true
}

func (w *Watcher) storeAgentEnd(sessionID string, record agentEndRecord) {
	raw, err := json.Marshal(record)
	if err == nil {
		err = w.sessions().setRelaunchData(sessionID, hostDataAgentEnd, string(raw))
	}
	if err != nil {
		log.Printf("agent relaunch: keep the decision on %s: %v", sessionID, err)
	}
}

// relaunchAgent starts the ended agent again in its Session and conversation.
func (w *Watcher) relaunchAgent(ledger AgentRelaunchLedger, end AgentEnd, plan AgentRelaunchPlan) {
	supervisor := w.agentRelaunch()
	finish := func(fromTurn string) {
		supervisor.mu.Lock()
		delete(supervisor.running, end.SessionID)
		if fromTurn != "" {
			supervisor.held[end.SessionID] = agentRelaunchHeld{turnID: fromTurn, until: supervisor.now().Add(agentRelaunchHold)}
		}
		supervisor.mu.Unlock()
	}
	if plan.After > 0 {
		supervisor.sleep(plan.After)
	}
	if wait := supervisor.startSlot(end.SessionID); wait > 0 {
		supervisor.sleep(wait)
	}
	// What was true when the agent ended may not be now: the Work may have
	// closed or the Worker been closed while we waited.
	fresh, err := ledger.PlanAgentRelaunch(end)
	if err != nil || fresh.Action != AgentEndRelaunch {
		if err != nil {
			log.Printf("agent relaunch: re-plan for %s: %v", end.SessionID, err)
		}
		supervisor.setLine(end.SessionID, agentRelaunchLine{})
		supervisor.mu.Lock()
		delete(supervisor.decided, end.SessionID)
		supervisor.mu.Unlock()
		finish("")
		return
	}
	plan = fresh
	survivors := w.agentSurvivors(end.SessionID, end.Control)
	plan.Words = strings.TrimSuffix(plan.Words, ".") + "." + survivorWords(survivors, supervisor.now())
	reattached, err := w.replaceEndedSession(end, plan)
	if errors.Is(err, errHostCannotRespawn) {
		// Not the agent's failure, and no try: say it once and leave the
		// ended Session as it is.
		w.decideAgentEnd(ledger, end, AgentRelaunchPlan{
			Action: AgentEndReport, WorkID: plan.WorkID, TurnID: plan.TurnID, PendingTurnID: plan.PendingTurnID,
			Words: strings.TrimSpace(plan.Agent+" "+end.Cause()) + "; not started again: " + errHostCannotRespawn.Error() + ".",
			Line:  "Not restarted: needs an update",
		})
		finish("")
		return
	} else if err != nil && w.sessionClosedDuring(end.SessionID, err) {
		// The Worker was closed while it waited: nothing to start, no try.
		log.Printf("agent relaunch: %s was closed before it was started again", end.SessionID)
		supervisor.setLine(end.SessionID, agentRelaunchLine{})
		supervisor.mu.Lock()
		delete(supervisor.decided, end.SessionID)
		supervisor.mu.Unlock()
		finish("")
		return
	} else if err != nil {
		// A start that fails is a try: it counts toward the bound, so the next
		// poll waits its backoff, and at the bound Brain hears why.
		log.Printf("agent relaunch: %s could not start again: %v", end.SessionID, err)
		if recordErr := ledger.RecordAgentRelaunch(AgentRelaunch{
			WorkID: plan.WorkID, SessionID: end.SessionID, Conversation: plan.Conversation, Attempt: plan.Attempt,
			Cause: end.Cause(), ExitCode: end.Exit.Code, ExitSignal: end.Exit.Signal, EndedAt: end.Exit.EndedAt,
			FromTurnID: plan.TurnID, Outcome: "start_failed", Error: err.Error(), Name: plan.Agent, At: supervisor.now().UTC(),
		}); recordErr != nil {
			log.Printf("agent relaunch: record %s: %v", end.SessionID, recordErr)
		}
		supervisor.mu.Lock()
		delete(supervisor.decided, end.SessionID)
		supervisor.mu.Unlock()
		finish("")
		return
	}
	supervisor.setLine(end.SessionID, agentRelaunchLine{kind: agentLineRestarted, text: plan.Line, until: supervisor.now().Add(agentRelaunchShown)})
	record := AgentRelaunch{
		WorkID: plan.WorkID, SessionID: end.SessionID, Conversation: plan.Conversation, Attempt: plan.Attempt,
		Cause: end.Cause(), ExitCode: end.Exit.Code, ExitSignal: end.Exit.Signal, EndedAt: end.Exit.EndedAt,
		FromTurnID: plan.TurnID, Outcome: "idle", Name: plan.Agent, Words: plan.Words, At: supervisor.now().UTC(),
	}
	if reattached {
		// Only the agent's screen had died: its control server kept the
		// conversation and any turn running. Nothing is sent to it.
		record.Outcome = "reattached"
		record.Words = strings.TrimSpace(plan.Agent+" "+end.Cause()) + "; Mewla attached it again to its app-server, which kept the conversation"
		if plan.TurnID != "" {
			record.Words += " and its turn running, so nothing was sent to it"
		}
		record.Words += "." + survivorWords(survivors, supervisor.now())
	} else if plan.TurnID != "" {
		turnID := "turn:" + uuid.NewString()
		at := supervisor.now().UTC()
		result, submitErr := supervisor.submit(
			end.SessionID, plan.Command, DelegatedLifecyclePayload(AgentContinuationPrompt(end.Cause(), survivorList(survivors, at)), turnID),
			plan.WorkID, turnID, at,
		)
		outcome := result.Outcome
		if submitErr != nil {
			outcome = InputOutcomeFromError(submitErr)
		}
		switch outcome {
		case InputAccepted:
			record.TurnID, record.Outcome = turnID, "continued"
		case InputNotSubmitted:
			record.Outcome = "not_ready"
		default:
			record.TurnID, record.Outcome = turnID, "continuation_unknown"
		}
		if submitErr != nil {
			log.Printf("agent relaunch: continuation for %s: %v", end.SessionID, submitErr)
		}
	} else if plan.PendingTurnID != "" {
		w.deliverPromptInFlight(ledger, end, plan, &record)
	}
	// The record's time is when the agent was back: started, and for a Turn
	// in flight, its continuation taken.
	record.At = supervisor.now().UTC()
	if err := ledger.RecordAgentRelaunch(record); err != nil {
		log.Printf("agent relaunch: record %s: %v", end.SessionID, err)
	}
	if record.Outcome == "not_ready" {
		// Started but never showed its input: a dead agent is seen again on the
		// next poll and counted; a live one that is stuck (a sign-in screen, an
		// update prompt) is Brain's to see now.
		if presence, err := w.ProbeSession(end.SessionID); err == nil && presence == SessionPresencePresent && !w.SessionExited(end.SessionID) {
			plan.Words = strings.TrimSpace(plan.Words + " It was started again but did not become ready for input.")
			w.reportAfterRelaunch(ledger, end, plan, "Restarted; it is not ready")
		}
		finish("")
		return
	}
	finish(plan.TurnID)
}

// sessionClosedDuring says a relaunch failed with err because its Session
// was closed meanwhile (by the person or Brain), not because the agent could
// not start.
func (w *Watcher) sessionClosedDuring(sessionID string, err error) bool {
	if errors.Is(err, errSessionGone) {
		return true
	}
	presence, probeErr := w.ProbeSession(sessionID)
	return probeErr == nil && presence == SessionPresenceAbsent
}

// replaceEndedSession starts plan's launch in place of the ended Session,
// under the same id and resource unit.
func (w *Watcher) replaceEndedSession(end AgentEnd, plan AgentRelaunchPlan) (reattached bool, err error) {
	sessionID := end.SessionID
	supervisor := w.agentRelaunch()
	backend := w.sessions()
	delegated, unit := backend.delegatedResource(sessionID)
	if !delegated {
		return false, fmt.Errorf("Session %s is not a delegated Session", sessionID)
	}
	manager := w.resourceManager()
	var resource *delegatedResourceSpec
	if unit != "" {
		reuser, ok := manager.(delegatedResourceReuser)
		if !ok {
			return false, fmt.Errorf("this computer cannot keep the Worker's resource unit across a relaunch")
		}
		spec, err := reuser.Reuse(unit)
		if err != nil {
			return false, err
		}
		// Nothing the dead agent left running is stopped (a server the Worker
		// was told to leave up, a deploy half-way): relaunchAgent tells the
		// agent and Brain what is still running.
		resource = spec
	}
	opts := CreateSessionOptions{
		Cwd:              plan.Cwd,
		Command:          plan.Command,
		Name:             plan.Name,
		Detached:         true,
		ProgressEnv:      true,
		PrepareWorkspace: plan.PrepareWorkspace,
		Delegated:        true,
		Env:              plan.Env,
		ExecutorID:       end.ExecutorID,
		resource:         resource,
		relaunch:         true,
		relaunchControl:  end.Control,
	}
	prepared, err := w.prepareSessionCreate("", opts)
	if err != nil {
		return false, err
	}
	lock := supervisor.swapLock(sessionID)
	lock.Lock()
	supervisor.mu.Lock()
	if run := supervisor.running[sessionID]; run != nil {
		run.swapping = true
	}
	supervisor.mu.Unlock()
	defer func() {
		supervisor.mu.Lock()
		if run := supervisor.running[sessionID]; run != nil {
			run.swapping = false
		}
		supervisor.mu.Unlock()
		lock.Unlock()
	}()
	// The poll's inventory waits for the replacement: it lists the ended
	// Session or the new one, never the moment between.
	w.inventoryMu.Lock()
	err = backend.replace(sessionID, prepared.spec)
	w.inventoryMu.Unlock()
	if err != nil {
		return false, err
	}
	w.registerCreatedSession(sessionID, prepared.cwd, prepared.opts, supervisor.now().UTC())
	return prepared.reattached, nil
}

// restartingLine is the line while the agent is about to start again:
// "Restarted after …" reads "Restarting after …" until it has.
func restartingLine(line string) string {
	if rest, ok := strings.CutPrefix(line, "Restarted "); ok {
		return "Restarting " + rest
	}
	return line
}
