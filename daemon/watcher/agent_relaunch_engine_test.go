//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/classifier"
)

// relaunchLedger is a TurnLedger and AgentRelaunchLedger for the relaunch
// tests: it answers with plan and records what the watcher tells it.
type relaunchLedger struct {
	mu       sync.Mutex
	turn     TurnSnapshot
	hasTurn  bool
	plan     AgentRelaunchPlan
	plans    []AgentEnd
	facts    []TurnFact
	records  []AgentRelaunch
	reported []AgentEnd
	// untold is how many reports Brain does not have yet (an earlier review
	// of the Work is still with it).
	untold int
	// bound, when set, makes the plan a report once that many relaunches
	// (started or failed) are recorded, worded boundWords.
	bound      int
	boundWords string
	// pending are the prompts in flight the ledger holds; aborted and
	// resolved the ones the watcher aborted or accepted.
	pending  []InputAdmission
	aborted  []string
	resolved []string
	// planFn, when set, is the plan (it must not take mu).
	planFn func(AgentEnd) AgentRelaunchPlan
}

func (l *relaunchLedger) PrepareInputAdmission(submission InputAdmission) (InputAdmission, bool, error) {
	return submission, true, nil
}

func (l *relaunchLedger) InputAdmission(sessionID, turnID string) (InputAdmission, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, pending := range l.pending {
		if pending.SessionID == sessionID && pending.ProposedTurnID == turnID {
			return pending, true, nil
		}
	}
	return InputAdmission{}, false, nil
}

func (l *relaunchLedger) PendingInputAdmissions(sessionID string) ([]InputAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []InputAdmission
	for _, pending := range l.pending {
		if pending.SessionID == sessionID {
			out = append(out, pending)
		}
	}
	return out, nil
}

func (l *relaunchLedger) ResolveInputAdmission(resolution InputAdmissionResolution) (InputAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resolved = append(l.resolved, resolution.ProposedTurnID)
	l.pending = nil
	l.turn = TurnSnapshot{SessionID: resolution.SessionID, TurnID: resolution.ProposedTurnID, Status: TurnAdmitted, SignalProtocol: true}
	l.hasTurn = true
	return InputAdmission{SessionID: resolution.SessionID, ProposedTurnID: resolution.ProposedTurnID,
		State: InputAdmissionResolved, ResolvedTurnID: resolution.ProposedTurnID}, nil
}

func (l *relaunchLedger) AbortInputAdmission(sessionID, turnID, receipt, digest string) (InputAdmission, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.aborted = append(l.aborted, turnID)
	l.pending = nil
	return InputAdmission{SessionID: sessionID, ProposedTurnID: turnID, State: InputAdmissionAborted}, nil
}

// staticProviderProbe answers every Worker with one observation.
type staticProviderProbe struct{ observation ProviderActivityObservation }

func (p staticProviderProbe) ObserveProviderActivity(classifier.Worker, time.Time) ProviderActivityObservation {
	return p.observation
}
func (staticProviderProbe) ForgetProviderActivity(string) {}

func (l *relaunchLedger) Turn(string) (TurnSnapshot, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.turn, l.hasTurn, nil
}

func (l *relaunchLedger) ApplyTurnFact(fact TurnFact) (TurnSnapshot, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.facts = append(l.facts, fact)
	return l.turn, false, nil
}

func (l *relaunchLedger) ApplyDelegatedTurnProgress(TurnFact) (TurnProgressResult, error) {
	return TurnProgressResult{}, nil
}

func (l *relaunchLedger) PlanAgentRelaunch(end AgentEnd) (AgentRelaunchPlan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.plans = append(l.plans, end)
	if l.bound > 0 && len(l.records) >= l.bound {
		return AgentRelaunchPlan{Action: AgentEndReport, WorkID: l.plan.WorkID, TurnID: l.plan.TurnID, Words: l.boundWords, Line: "Stopped: it could not start"}, nil
	}
	if l.planFn != nil {
		return l.planFn(end), nil
	}
	return l.plan, nil
}

func (l *relaunchLedger) RecordAgentRelaunch(record AgentRelaunch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, record)
	return nil
}

func (l *relaunchLedger) ReportAgentEnd(end AgentEnd, _ AgentRelaunchPlan) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reported = append(l.reported, end)
	if l.untold > 0 {
		l.untold--
		return false, nil
	}
	return true, nil
}

func (l *relaunchLedger) snapshot() (facts []TurnFact, records []AgentRelaunch, reported []AgentEnd, plans []AgentEnd) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]TurnFact(nil), l.facts...), append([]AgentRelaunch(nil), l.records...),
		append([]AgentEnd(nil), l.reported...), append([]AgentEnd(nil), l.plans...)
}

func deadFacts(facts []TurnFact) []TurnFact {
	var dead []TurnFact
	for _, fact := range facts {
		if fact.ProcessDead || fact.SessionReplaced {
			dead = append(dead, fact)
		}
	}
	return dead
}

// startDelegatedAgent starts a delegated Session running `sleep 300` (the
// "agent") and returns its id and pid.
func startDelegatedAgent(t *testing.T, h *hostHarness) (string, int) {
	t.Helper()
	id := createHostSession(t, h.w, backendCreateSpec{
		name: "Relaunchee", command: "exec sleep 300",
		labels: sessionLabels{delegated: true, executorID: "claude"},
	})
	h.w.mu.Lock()
	h.w.delegated[id] = true
	h.w.mu.Unlock()
	info := waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID > 0 })
	return id, info.PID
}

type hostInfo struct {
	Alive bool
	PID   int
}

func waitForHostInfo(t *testing.T, w *Watcher, id string, ok func(hostInfo) bool) hostInfo {
	t.Helper()
	var last hostInfo
	waitUntil(t, 10*time.Second, "Session "+id+" state", func() bool {
		info, present, err := w.host().info(id)
		if err != nil || !present {
			return false
		}
		last = hostInfo{Alive: info.Alive, PID: info.PID}
		return ok(last)
	})
	return last
}

func killAgent(t *testing.T, w *Watcher, id string, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitForHostInfo(t, w, id, func(info hostInfo) bool { return !info.Alive })
}

// An agent killed by a signal while its Worker was idle is started again in
// the same Session, with the Session's input receipts kept, and its death
// never reaches the Turn ledger.
func TestHostAgentKilledIdleIsRelaunchedInTheSameSession(t *testing.T) {
	h := newHostHarness(t)
	ledger := &relaunchLedger{}
	h.w.SetTurnLedger(ledger)
	id, pid := startDelegatedAgent(t, h)
	// What the input owner recorded on the Session: a submission that landed.
	client, err := h.w.host().conn(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetData(context.Background(), id, hostDataInputReceipts, `{"version":1}`); err != nil {
		t.Fatal(err)
	}
	ledger.plan = AgentRelaunchPlan{
		Action: AgentEndRelaunch, WorkID: "work-1", Name: "Relaunchee", Command: "exec sleep 301",
		Conversation: "conv-1", Attempt: 1, Words: "sleep was killed (SIGKILL); Mewla started it again in the same conversation.",
	}
	killAgent(t, h.w, id, pid)

	h.w.poll()
	info := waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
	waitUntil(t, 10*time.Second, "relaunch record", func() bool {
		_, records, _, _ := ledger.snapshot()
		return len(records) == 1
	})
	facts, records, reported, plans := ledger.snapshot()
	if len(plans) == 0 || plans[0].Exit.Signal == "" || plans[0].Cause() != "was killed (SIGKILL)" || plans[0].TurnID != "" {
		t.Fatalf("plan asked for %+v", plans)
	}
	if got := records[0]; got.SessionID != id || got.WorkID != "work-1" || got.Outcome != "idle" || got.Attempt != 1 ||
		got.ExitSignal == "" || got.Conversation != "conv-1" || got.TurnID != "" {
		t.Fatalf("relaunch record = %+v", got)
	}
	if len(deadFacts(facts)) != 0 || len(reported) != 0 {
		t.Fatalf("a relaunched agent's death reached Brain: facts %+v reported %+v", facts, reported)
	}
	receipts, err := client.GetData(context.Background(), id, hostDataInputReceipts)
	if err != nil || receipts.Value != `{"version":1}` {
		t.Fatalf("input receipts after relaunch = %q, %v", receipts.Value, err)
	}
	// The new program is the relaunch's own launch.
	got, _, _ := h.w.host().info(id)
	if !strings.Contains(strings.Join(got.Argv, " "), "sleep 301") || info.PID == pid {
		t.Fatalf("relaunched argv %q pid %d (was %d)", got.Argv, info.PID, pid)
	}
	// The next polls see a live Session: nothing more is decided or reported.
	h.w.poll()
	h.w.poll()
	facts, records, _, _ = ledger.snapshot()
	if len(records) != 1 || len(deadFacts(facts)) != 0 {
		t.Fatalf("after more polls: records %d dead facts %+v", len(records), deadFacts(facts))
	}
}

// An agent that ends in a Turn and is not relaunched ends that Turn with the
// plain words of how it ended; a death by signal counts.
func TestHostAgentKilledMidTurnNotRelaunchedEndsTheTurnWithItsCause(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{
		turn: TurnSnapshot{SessionID: id, TurnID: "turn:live", Status: TurnRunning, ProcessIdentity: "proc"}, hasTurn: true,
		plan: AgentRelaunchPlan{Action: AgentEndReport, WorkID: "work-1",
			Words: "sleep was killed (SIGKILL); not started again: it was started again 3 times in the last 30 minutes and ended each time."},
	}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	h.w.poll()
	facts, records, reported, plans := ledger.snapshot()
	dead := deadFacts(facts)
	if len(plans) != 1 || plans[0].TurnID != "turn:live" {
		t.Fatalf("plans = %+v", plans)
	}
	if len(dead) != 1 || !dead[0].ProcessDead || dead[0].TurnID != "turn:live" || dead[0].Ended != ledger.plan.Words {
		t.Fatalf("death facts = %+v", dead)
	}
	if len(records) != 0 || len(reported) != 0 {
		t.Fatalf("records %+v reported %+v", records, reported)
	}
	h.w.poll()
	if _, _, _, plans := ledger.snapshot(); len(plans) != 1 {
		t.Fatalf("one end was decided %d times", len(plans))
	}
}

// An agent that ends in a Turn and is relaunched is sent one continuation, as a
// new Turn following the one it was in, and the old Turn's process being gone
// (or replaced by the relaunch) is no liveness fact.
func TestHostAgentKilledMidTurnIsRelaunchedAndContinued(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{
		turn: TurnSnapshot{SessionID: id, TurnID: "turn:live", Status: TurnRunning, ProcessIdentity: "proc", PaneGeneration: "old"}, hasTurn: true,
		plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", TurnID: "turn:live", Command: "exec sleep 302", Attempt: 1,
			Words: "sleep was killed (SIGKILL); Mewla started it again in the same conversation and asked it to continue the task."},
	}
	h.w.SetTurnLedger(ledger)
	var mu sync.Mutex
	var sent []string
	supervisor := h.w.agentRelaunch()
	supervisor.submit = func(sessionID, command, payload, workID, turnID string, at time.Time) (InputResult, error) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, sessionID+"|"+workID+"|"+turnID+"|"+payload)
		return InputResult{Outcome: InputAccepted, TurnID: turnID}, nil
	}
	killAgent(t, h.w, id, pid)
	h.w.poll()
	waitUntil(t, 10*time.Second, "relaunch record", func() bool {
		_, records, _, _ := ledger.snapshot()
		return len(records) == 1
	})
	_, records, _, _ := ledger.snapshot()
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || !strings.HasPrefix(sent[0], id+"|work-1|turn:") || strings.Contains(sent[0], "|turn:live|") ||
		!strings.Contains(sent[0], "Mewla started it again in the same conversation") ||
		!strings.Contains(sent[0], "--turn-id "+records[0].TurnID) {
		t.Fatalf("continuations = %q", sent)
	}
	if got := records[0]; got.Outcome != "continued" || got.FromTurnID != "turn:live" || got.TurnID == "" || got.TurnID == "turn:live" {
		t.Fatalf("record = %+v", got)
	}
	// The live old Turn on the relaunched Session gets no liveness fact.
	h.w.poll()
	h.w.poll()
	facts, _, _, _ := ledger.snapshot()
	if dead := deadFacts(facts); len(dead) != 0 {
		t.Fatalf("relaunched Turn got liveness facts %+v", dead)
	}
}

// A close of the Worker that lands while its ended agent is being relaunched
// wins: the replacement is never started, or is closed after it.
func TestHostAgentRelaunchLosesToAClose(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 303", Attempt: 1}}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	supervisor := h.w.agentRelaunch()
	release := make(chan struct{})
	supervisor.sleep = func(time.Duration) { <-release }
	ledger.plan.After = time.Second
	h.w.poll()
	if err := h.w.KillSession(id); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitUntil(t, 10*time.Second, "relaunch to give up", func() bool { return !supervisor.busy(id) })
	if _, present, err := h.w.host().info(id); err != nil || present {
		t.Fatalf("closed Session came back: present=%v err=%v", present, err)
	}
	if _, records, _, _ := ledger.snapshot(); len(records) != 0 {
		t.Fatalf("records %+v", records)
	}
}

// An idle Worker's agent that is not started again is told to Brain; while an
// earlier review of its Work is still with Brain the report is asked again,
// and once Brain has it, never again.
func TestHostAgentNotRestartedIdleIsToldUntilBrainHasIt(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{untold: 1, plan: AgentRelaunchPlan{Action: AgentEndReport, WorkID: "work-1",
		Words: "sleep was killed (SIGKILL); not started again: Mewla has no conversation of it to resume.",
		Line:  "Not restarted: nothing to resume"}}
	h.w.SetTurnLedger(ledger)
	now := time.Now()
	supervisor := h.w.agentRelaunch()
	supervisor.now = func() time.Time { return now }
	killAgent(t, h.w, id, pid)

	h.w.poll()
	if _, _, reported, _ := ledger.snapshot(); len(reported) != 1 || reported[0].TurnID != "" {
		t.Fatalf("reported = %+v", reported)
	}
	h.w.poll()
	if _, _, reported, _ := ledger.snapshot(); len(reported) != 1 {
		t.Fatalf("asked again before %s: %d", agentEndTellEvery, len(reported))
	}
	now = now.Add(agentEndTellEvery)
	h.w.poll()
	now = now.Add(agentEndTellEvery)
	h.w.poll()
	_, _, reported, plans := ledger.snapshot()
	if len(reported) != 2 || len(plans) != 1 {
		t.Fatalf("after Brain had it: reported %d, planned %d", len(reported), len(plans))
	}
}

// A decision about an ended agent is kept on its Session: a daemon that starts
// later (here: a fresh supervisor) reads it and never judges the same end
// again, and a report Brain already has is not told again.
func TestHostAgentEndDecisionOutlivesADaemonRestart(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndReport, WorkID: "work-1",
		Words: "sleep was killed (SIGKILL); not started again: it was started again 3 times in the last 30 minutes and ended each time.",
		Line:  "Stopped after 3 restarts"}}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	h.w.poll()
	waitUntil(t, 5*time.Second, "the told decision kept", func() bool {
		record, ok := h.w.storedAgentEnd(id, mustExit(t, h.w, id).EndedAt)
		return ok && record.Told
	})
	// The daemon restarts: what it knew in memory is gone, and the ledger
	// would now decide otherwise (the bound's window has passed).
	h.w.relaunch = newAgentRelaunchSupervisor()
	ledger.mu.Lock()
	ledger.plan = AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 302", Words: "started again"}
	ledger.mu.Unlock()
	h.w.poll()
	h.w.poll()
	_, records, reported, plans := ledger.snapshot()
	if len(plans) != 1 || len(records) != 0 || len(reported) != 1 {
		t.Fatalf("after a restart: planned %d, relaunched %d, reported %d", len(plans), len(records), len(reported))
	}
	if info, _, _ := h.w.host().info(id); info.Alive {
		t.Fatal("a judged end was started again after a restart")
	}
}

func mustExit(t *testing.T, w *Watcher, id string) SessionExit {
	t.Helper()
	exit, ok := w.SessionExitReport(id)
	if !ok {
		t.Fatalf("Session %s has not ended", id)
	}
	return exit
}

// A relaunch whose start fails (the workspace refused, the folder gone) is a
// try: it is recorded with why, counts toward the bound, and at the bound the
// dead Turn reaches the Turn ledger with the words; it never loops silently
// (the reviewer's probe, 11-22).
func TestHostAgentRelaunchThatCannotStartIsCountedAndReported(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{
		turn: TurnSnapshot{SessionID: id, TurnID: "turn:live", Status: TurnRunning, ProcessIdentity: "x"}, hasTurn: true,
		bound: 3, boundWords: "sleep was killed (SIGKILL); not started again: it could not be started again (workspace refused), 3 times in the last 30 minutes.",
		plan: AgentRelaunchPlan{
			Action: AgentEndRelaunch, WorkID: "work-1", TurnID: "turn:live", Name: "Relaunchee", Command: "exec sleep 301",
			Conversation: "conv-1", Attempt: 1, Words: "killed; started again",
			PrepareWorkspace: func(string, string, map[string]string) (string, error) {
				return "", errors.New("workspace refused")
			},
		},
	}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	waitUntil(t, 10*time.Second, "three failed starts and the report", func() bool {
		h.w.poll()
		facts, _, _, _ := ledger.snapshot()
		return len(deadFacts(facts)) > 0
	})
	facts, records, _, _ := ledger.snapshot()
	if len(records) != 3 {
		t.Fatalf("records = %+v", records)
	}
	for _, record := range records {
		if record.Outcome != "start_failed" || !strings.Contains(record.Error, "workspace refused") {
			t.Fatalf("record = %+v", record)
		}
	}
	dead := deadFacts(facts)
	if dead[0].Ended != ledger.boundWords || dead[0].TurnID != "turn:live" {
		t.Fatalf("dead fact = %+v", dead[0])
	}
	// The old Session is still there, ended: nothing removed it.
	if info, present, err := h.w.host().info(id); err != nil || !present || info.Alive {
		t.Fatalf("the ended Session: present %v alive %v err %v", present, info.Alive, err)
	}
	for range 5 {
		h.w.poll()
	}
	if _, more, _, _ := ledger.snapshot(); len(more) != 3 {
		t.Fatalf("tries after the report: %d", len(more))
	}
}

// The host replaces an ended Session only once the new program runs: a
// replacement that cannot start leaves the ended Session as it was, with its
// data; one that starts takes its id and data; a live one is never replaced.
func TestHostAgentReplaceNeverRemovesBeforeTheNewProgramRuns(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	backend := h.w.sessions()
	spec := func(cwd string) backendCreateSpec {
		return backendCreateSpec{name: "Relaunchee", cwd: cwd, command: "exec sleep 303",
			env: []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}, labels: sessionLabels{delegated: true, executorID: "claude"}}
	}
	if err := backend.replace(id, spec(t.TempDir())); err == nil {
		t.Fatal("a Session whose program still runs was replaced")
	}
	if err := backend.setRelaunchData(id, hostDataAgentEnd, `{"kept":true}`); err != nil {
		t.Fatal(err)
	}
	killAgent(t, h.w, id, pid)
	// The folder is gone: the new program cannot start.
	if err := backend.replace(id, spec("/nonexistent/mewla-relaunch-folder")); err == nil {
		t.Fatal("a replacement into a missing folder started")
	}
	info, present, err := h.w.host().info(id)
	if err != nil || !present || info.Alive || info.PID != pid {
		t.Fatalf("after a failed replacement the ended Session is present %v alive %v pid %d (was %d), %v", present, info.Alive, info.PID, pid, err)
	}
	if err := backend.replace(id, spec(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	respawned := waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive })
	if respawned.PID == pid {
		t.Fatal("the replacement runs the old program")
	}
	if kept, err := backend.relaunchData(id, hostDataAgentEnd); err != nil || kept != `{"kept":true}` {
		t.Fatalf("data after the replacement = %q, %v", kept, err)
	}
}

// An agent killed before it checked in on its prompt: the Turn ledger's
// prompt in flight is the Turn, never an idle Worker. The dead agent's
// conversation decides: it holds the prompt (accepted and continued), it
// provably does not (aborted and sent again under a new turn id; nothing
// ran), or that is unknown (started again, not sent again, Brain told).
func TestHostAgentKilledBeforeItsFirstCheckIn(t *testing.T) {
	sent := time.Now().UTC().Add(-30 * time.Second)
	older := ProviderActivityObservation{ID: "msg-0", Status: "completed", AdmissionAt: sent.Add(-time.Hour), InputSHA256: "digest-earlier", ProbeState: ProbeStateOK}
	held := ProviderActivityObservation{ID: "msg-1", Status: "running", AdmissionStream: "transcript", AdmissionID: "msg-1", AdmissionCursor: 4,
		AdmissionAt: sent.Add(time.Second), InputSHA256: "digest-first", ProbeState: ProbeStateOK}
	unreadable := ProviderActivityObservation{ProbeState: ProbeStateUnreadable}
	const prompt = "render the trailer\n--turn-id turn:first --status running"
	for _, tc := range []struct {
		name     string
		provider ProviderActivityObservation
		plan     AgentRelaunchPlan
		delivery PromptDelivery
		turnID   string
		outcome  string
		resent   bool
		reported int
		untold   int
		aborted  []string
		resolved []string
	}{
		{name: "not held", provider: older, delivery: PromptNotHeld, outcome: "resent", resent: true, aborted: []string{"turn:first"},
			plan: AgentRelaunchPlan{PendingTurnID: "turn:first", Words: "sleep was killed (SIGKILL); Mewla started it again in the same conversation and sent it its task again."}},
		{name: "unknown", provider: unreadable, delivery: PromptUnknown, outcome: "prompt_unknown", reported: 1, untold: 1,
			plan: AgentRelaunchPlan{PendingTurnID: "turn:first", Words: "sleep was killed (SIGKILL); Mewla started it again. Whether its task (turn:first) had reached the conversation is unknown."}},
		{name: "held", provider: held, delivery: PromptHeld, turnID: "turn:first", outcome: "continued", resolved: []string{"turn:first"},
			plan: AgentRelaunchPlan{TurnID: "turn:first", Words: "sleep was killed (SIGKILL); Mewla started it again in the same conversation and asked it to continue the task."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHostHarness(t)
			id, pid := startDelegatedAgent(t, h)
			plan := tc.plan
			plan.Action, plan.WorkID, plan.Command, plan.Attempt, plan.Agent = AgentEndRelaunch, "work-1", "exec sleep 304", 1, "sleep"
			ledger := &relaunchLedger{plan: plan, untold: tc.untold, pending: []InputAdmission{{WorkID: "work-1", SessionID: id, ProposedTurnID: "turn:first",
				Receipt: "turn:first", PayloadSHA256: "digest-first", AcceptedAt: sent, State: InputAdmissionPending, SignalProtocol: true,
				PaneGeneration: h.w.currentPaneGeneration(id)}}}
			h.w.SetTurnLedger(ledger)
			h.w.SetProviderActivityProbe(staticProviderProbe{observation: tc.provider})
			h.w.keepDelegatedPrompt(id, prompt, delegatedTurnDraft{ID: "turn:first", WorkID: "work-1"})
			var mu sync.Mutex
			var payloads []string
			h.w.agentRelaunch().submit = func(sessionID, command, payload, workID, turnID string, at time.Time) (InputResult, error) {
				mu.Lock()
				defer mu.Unlock()
				payloads = append(payloads, turnID+"|"+payload)
				return InputResult{Outcome: InputAccepted, TurnID: turnID}, nil
			}
			killAgent(t, h.w, id, pid)
			h.w.poll()
			waitUntil(t, 10*time.Second, "relaunch record", func() bool {
				_, records, _, _ := ledger.snapshot()
				return len(records) == 1
			})
			_, records, reported, plans := ledger.snapshot()
			if len(plans) == 0 || plans[0].PromptDelivery != tc.delivery || plans[0].TurnID != tc.turnID {
				t.Fatalf("plan asked for %+v", plans)
			}
			record := records[0]
			if record.Outcome != tc.outcome || record.FromTurnID != "turn:first" {
				t.Fatalf("record = %+v", record)
			}
			ledger.mu.Lock()
			aborted, resolved := ledger.aborted, ledger.resolved
			ledger.mu.Unlock()
			if strings.Join(aborted, ",") != strings.Join(tc.aborted, ",") || strings.Join(resolved, ",") != strings.Join(tc.resolved, ",") {
				t.Fatalf("aborted %q resolved %q", aborted, resolved)
			}
			if len(reported) != tc.reported {
				t.Fatalf("reported %+v", reported)
			}
			if tc.reported > 0 {
				// Nothing runs for the task: the line does not read as healthy,
				// and a report Brain did not have yet is asked again.
				supervisor := h.w.agentRelaunch()
				if state, relaunch, ok := supervisor.line(id, true, classifier.StateDone, time.Now()); !ok || relaunch.State != "stopped" || state != classifier.StateBlocked {
					t.Fatalf("line = %v %+v %v", state, relaunch, ok)
				}
				supervisor.now = func() time.Time { return time.Now().Add(time.Minute) }
				h.w.poll()
				if _, _, again, _ := ledger.snapshot(); len(again) != tc.reported+tc.untold {
					t.Fatalf("asked %d times, want %d", len(again), tc.reported+tc.untold)
				}
				h.w.poll()
				if _, _, again, _ := ledger.snapshot(); len(again) != tc.reported+tc.untold {
					t.Fatalf("asked again once Brain had it: %d", len(again))
				}
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case tc.resent:
				// The same prompt, under its new turn id only.
				want := strings.ReplaceAll(prompt, "turn:first", record.TurnID)
				if len(payloads) != 1 || record.TurnID == "" || record.TurnID == "turn:first" || payloads[0] != record.TurnID+"|"+want {
					t.Fatalf("resent %q, record %+v", payloads, record)
				}
			case tc.turnID != "":
				if len(payloads) != 1 || !strings.Contains(payloads[0], "Mewla stopped nothing") {
					t.Fatalf("continuation %q", payloads)
				}
			default:
				if len(payloads) != 0 {
					t.Fatalf("an unknown prompt was sent again: %q", payloads)
				}
			}
		})
	}
}

// The plan is asked again after the wait: a Work closed while its Worker's
// agent waited to start is not started for (M1).
func TestHostAgentRelaunchAsksAgainAfterTheWait(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 305", Attempt: 2, After: time.Second}}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	supervisor := h.w.agentRelaunch()
	waiting, release := make(chan struct{}), make(chan struct{})
	supervisor.sleep = func(time.Duration) { close(waiting); <-release }
	h.w.poll()
	<-waiting
	ledger.mu.Lock()
	ledger.plan = AgentRelaunchPlan{} // the Work was closed meanwhile
	ledger.mu.Unlock()
	close(release)
	waitUntil(t, 10*time.Second, "relaunch to give up", func() bool { return !supervisor.busy(id) })
	if info, present, err := h.w.host().info(id); err != nil || !present || info.Alive {
		t.Fatalf("the ended Session was started for a closed Work: %+v present=%v err=%v", info, present, err)
	}
	if _, records, _, plans := ledger.snapshot(); len(records) != 0 || len(plans) != 2 {
		t.Fatalf("records %+v plans %d", records, len(plans))
	}
}

// A close that lands while the ended agent is being replaced waits for the
// replacement and then closes the new program: the close wins (M8).
func TestHostAgentCloseDuringTheSwapWaitsAndWins(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	killAgent(t, h.w, id, pid)
	swap := h.w.agentRelaunch().swapLock(id)
	swap.Lock()
	closed := make(chan error, 1)
	go func() { closed <- h.w.KillSession(id) }()
	select {
	case err := <-closed:
		swap.Unlock()
		t.Fatalf("the close did not wait for the swap: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	// The swap starts the new program, then lets go.
	if err := h.w.sessions().replace(id, backendCreateSpec{name: "Relaunchee", cwd: t.TempDir(), command: "exec sleep 306",
		env: []string{"PATH=/usr/bin:/bin"}, labels: sessionLabels{delegated: true, executorID: "claude"}}); err != nil {
		swap.Unlock()
		t.Fatal(err)
	}
	swap.Unlock()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, present, err := h.w.host().info(id); err != nil || present {
		t.Fatalf("the replacement outlived the close: present=%v err=%v", present, err)
	}
}

// A provider outage holds the end back: nothing is started, no try is used,
// and the line says what it waits for; once the provider answers it is
// started again.
func TestHostAgentEndWaitsOutAProviderOutage(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndWait, WorkID: "work-1", Line: "Waiting for sleep's provider"}}
	h.w.SetTurnLedger(ledger)
	killAgent(t, h.w, id, pid)
	supervisor := h.w.agentRelaunch()
	at := time.Now()
	supervisor.now = func() time.Time { return at }
	h.w.poll()
	h.w.poll()
	facts, records, reported, plans := ledger.snapshot()
	if len(plans) != 1 || len(records) != 0 || len(reported) != 0 || len(deadFacts(facts)) != 0 {
		t.Fatalf("during the outage: plans %d records %+v reported %+v facts %+v", len(plans), records, reported, deadFacts(facts))
	}
	if state, relaunch, ok := supervisor.line(id, false, "", at); !ok || state != classifier.StateBlocked || relaunch.State != "waiting" || relaunch.Line != "Waiting for sleep's provider" {
		t.Fatalf("line = %+v %v", relaunch, ok)
	}
	ledger.mu.Lock()
	ledger.plan = AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 307", Attempt: 1}
	ledger.mu.Unlock()
	at = at.Add(agentEndWaitEvery + time.Second)
	h.w.poll()
	waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
}

// outagePlans is Brain's outage rule over the watcher's own availability:
// wait while Claude's provider refuses requests, unless the end is the test.
func outagePlans(w *Watcher) func(AgentEnd) AgentRelaunchPlan {
	return func(end AgentEnd) AgentRelaunchPlan {
		if _, down := w.ProviderOutage("claude"); down && !end.ProviderTest {
			return AgentRelaunchPlan{Action: AgentEndWait, WorkID: "work-1", Provider: "claude", Line: "Waiting for Claude Code's provider"}
		}
		return AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 307", Attempt: 1}
	}
}

func startOutage(w *Watcher, at time.Time) {
	w.noteProviderObservation("claude", refused(at.Add(-time.Minute)))
	w.noteProviderObservation("claude", refused(at))
}

// The reviewer's sequence (delta of a8ba41b9b): the outage starts, a Worker
// dies and is held, the outage ends, and a different Claude Worker finishes a
// request after its own Turn settled. The held Worker is back on the next
// recheck; while held it looks like waiting, never like work.
func TestHostAgentHeldForAnOutageComesBackWhenAnotherWorkerIsAnswered(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{}
	ledger.planFn = outagePlans(h.w)
	h.w.SetTurnLedger(ledger)
	at := time.Now().UTC()
	startOutage(h.w, at)
	supervisor := h.w.agentRelaunch()
	supervisor.now = func() time.Time { return at }
	killAgent(t, h.w, id, pid)
	h.w.poll()
	if state, relaunch, ok := supervisor.line(id, false, "", at); !ok || state != classifier.StateBlocked || relaunch.State != "waiting" {
		t.Fatalf("held: state %q line %+v", state, relaunch)
	}
	// Another Claude Worker's Turn settled (done) before its request finished.
	ledger.mu.Lock()
	ledger.turn, ledger.hasTurn = TurnSnapshot{SessionID: "p5", TurnID: "turn:p5", Status: TurnDone, SignalProtocol: true}, true
	ledger.mu.Unlock()
	answer := at.Add(90 * time.Second)
	h.w.collectPollEvidence([]preparedPollWorker{{id: "p5", workerSnap: classifier.Worker{ID: "p5", Command: "claude --session-id p5", Delegated: true}, alive: true, now: answer}},
		nil, nil, byWorkerProbe{"p5": answered(answer)})
	if _, down := h.w.ProviderOutage("claude"); down {
		t.Fatal("the other Worker's answer did not end the outage")
	}
	at = answer.Add(agentEndWaitEvery)
	h.w.poll()
	waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
	_, records, reported, plans := ledger.snapshot()
	if len(records) != 1 || len(reported) != 0 || plans[len(plans)-1].ProviderTest {
		t.Fatalf("after the outage: records %+v reported %+v last plan %+v", records, reported, plans[len(plans)-1])
	}
}

// An outage that never ends: the held end asks for the family's probe on its
// backoff and is started again at the latest at agentProviderWaitLimit; that
// start is counted like any other, so the usual bound ends a lasting outage.
func TestHostAgentHeldForAnOutageThatNeverEndsIsStartedAtTheBound(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{}
	ledger.planFn = outagePlans(h.w)
	h.w.SetTurnLedger(ledger)
	at := time.Now().UTC()
	startOutage(h.w, at)
	// The family's probe was taken by another held Turn just now, so this
	// end's asks are refused until its own bound.
	if granted, _ := h.w.ProviderProbe("claude", at.Add(providerProbeIntervals[0])); !granted {
		t.Fatal("setup: the first probe was not granted")
	}
	supervisor := h.w.agentRelaunch()
	supervisor.now = func() time.Time { return at }
	killAgent(t, h.w, id, pid)
	h.w.poll()
	for step := agentEndWaitEvery; step < agentProviderWaitLimit; step += agentEndWaitEvery {
		at = at.Add(agentEndWaitEvery)
		h.w.poll()
		if _, records, _, _ := ledger.snapshot(); len(records) != 0 {
			t.Fatalf("started at %s, before the bound: %+v", step, records)
		}
	}
	at = at.Add(agentEndWaitEvery)
	h.w.poll()
	waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
	_, records, reported, plans := ledger.snapshot()
	if len(records) != 1 || len(reported) != 0 || !plans[len(plans)-1].ProviderTest {
		t.Fatalf("at the bound: records %+v reported %+v last plan %+v", records, reported, plans[len(plans)-1])
	}
	if _, down := h.w.ProviderOutage("claude"); !down {
		t.Fatal("setup: the outage ended")
	}
}

// Holding the family's probe starts the held end before the bound.
func TestHostAgentHeldForAnOutageIsStartedWithTheFamilysProbe(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{}
	ledger.planFn = outagePlans(h.w)
	h.w.SetTurnLedger(ledger)
	at := time.Now().UTC()
	startOutage(h.w, at)
	supervisor := h.w.agentRelaunch()
	supervisor.now = func() time.Time { return at }
	killAgent(t, h.w, id, pid)
	h.w.poll()
	// The family's first probe is due five minutes into the outage; the
	// end's asks at 1, 3 and 7 minutes find it at 7.
	started := time.Duration(0)
	for step := agentEndWaitEvery; step <= agentProviderWaitLimit; step += agentEndWaitEvery {
		at = at.Add(agentEndWaitEvery)
		h.w.poll()
		if _, records, _, _ := ledger.snapshot(); len(records) != 0 {
			started = step
			break
		}
	}
	if started == 0 || started >= agentProviderWaitLimit || started < providerProbeIntervals[0] {
		t.Fatalf("started with the probe at %s", started)
	}
	waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
	if _, _, _, plans := ledger.snapshot(); !plans[len(plans)-1].ProviderTest {
		t.Fatalf("last plan %+v", plans[len(plans)-1])
	}
}

// A relaunch stops nothing it did not start: a server the dead agent left
// running in its resource unit is still running after the relaunch, and
// Brain's record and the continuation both name it (point 2).
func TestHostAgentRelaunchLeavesWhatTheAgentStartedRunning(t *testing.T) {
	h := newHostHarness(t)
	owner := "abc123"
	unit := delegatedResourceUnit(owner, "0123456789abcdef0123456789abcdef")
	root := t.TempDir()
	leaseDir := filepath.Join(root, "leases")
	if err := os.MkdirAll(leaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.w.resources = &portableDelegatedResourceManager{owner: owner, leaseDir: leaseDir, tempRoot: root,
		byTarget: map[string]string{}, reserved: map[string]time.Time{}, now: time.Now}
	marker := filepath.Join(t.TempDir(), "server.pid")
	cwd := t.TempDir()
	id := createHostSession(t, h.w, backendCreateSpec{
		name: "Relaunchee", cwd: cwd,
		command: "setsid sleep 391 </dev/null >/dev/null 2>&1 & echo $! > " + marker + "; exec sleep 300",
		env:     []string{"PATH=/usr/bin:/bin", "HOME=" + cwd, delegatedResourceUnitEnv + "=" + unit},
		labels:  sessionLabels{delegated: true, executorID: "claude", resourceUnit: unit, resourceOwner: owner},
	})
	h.w.mu.Lock()
	h.w.delegated[id] = true
	h.w.mu.Unlock()
	info := waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID > 0 })
	var server int
	waitUntil(t, 5*time.Second, "server pid", func() bool {
		raw, err := os.ReadFile(marker)
		if err != nil {
			return false
		}
		server, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && server > 0
	})
	t.Cleanup(func() { _ = syscall.Kill(server, syscall.SIGKILL) })
	ledger := &relaunchLedger{
		turn: TurnSnapshot{SessionID: id, TurnID: "turn:live", Status: TurnRunning}, hasTurn: true,
		plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", TurnID: "turn:live", Command: "exec sleep 308", Attempt: 1, Agent: "sleep",
			Words: "sleep was killed (SIGKILL); Mewla started it again in the same conversation and asked it to continue the task."},
	}
	h.w.SetTurnLedger(ledger)
	var mu sync.Mutex
	var sent []string
	h.w.agentRelaunch().submit = func(sessionID, command, payload, workID, turnID string, at time.Time) (InputResult, error) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, payload)
		return InputResult{Outcome: InputAccepted, TurnID: turnID}, nil
	}
	killAgent(t, h.w, id, info.PID)
	h.w.poll()
	waitUntil(t, 10*time.Second, "relaunch record", func() bool {
		_, records, _, _ := ledger.snapshot()
		return len(records) == 1
	})
	if err := syscall.Kill(server, 0); err != nil {
		t.Fatalf("the relaunch stopped the server the agent left running: %v", err)
	}
	_, records, _, _ := ledger.snapshot()
	if records[0].Outcome != "continued" {
		t.Fatalf("record = %+v", records[0])
	}
	if !strings.Contains(records[0].Words, "Still running from before: setsid sleep 391") && !strings.Contains(records[0].Words, "Still running from before: sleep 391") {
		t.Fatalf("record words = %q", records[0].Words)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0], "Mewla stopped nothing") || !strings.Contains(sent[0], "sleep 391 (running ") {
		t.Fatalf("continuation = %q", sent)
	}
}

// Two agents that end together are not started together: the second
// relaunch waits the stagger.
func TestHostAgentRelaunchesAfterAMassDeathAreStaggered(t *testing.T) {
	h := newHostHarness(t)
	first, firstPID := startDelegatedAgent(t, h)
	second, secondPID := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 309", Attempt: 1}}
	h.w.SetTurnLedger(ledger)
	supervisor := h.w.agentRelaunch()
	var mu sync.Mutex
	var waits []time.Duration
	supervisor.sleep = func(d time.Duration) {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
	}
	killAgent(t, h.w, first, firstPID)
	killAgent(t, h.w, second, secondPID)
	h.w.poll()
	waitUntil(t, 10*time.Second, "both relaunched", func() bool {
		_, records, _, _ := ledger.snapshot()
		return len(records) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	staggered := 0
	for _, wait := range waits {
		if wait > agentRelaunchStagger-time.Second && wait <= agentRelaunchStagger {
			staggered++
		}
	}
	if staggered != 1 {
		t.Fatalf("waits = %v, want one of about %v", waits, agentRelaunchStagger)
	}
}

// A Codex whose app-server outlived its TUI is draining that turn and holds
// the thread: the relaunch waits for it, uses no try, says so on the line,
// and starts once it is gone.
func TestHostAgentRelaunchWaitsForItsControlServer(t *testing.T) {
	h := newHostHarness(t)
	id, pid := startDelegatedAgent(t, h)
	ledger := &relaunchLedger{plan: AgentRelaunchPlan{Action: AgentEndRelaunch, WorkID: "work-1", Command: "exec sleep 310", Attempt: 1}}
	h.w.SetTurnLedger(ledger)
	var mu sync.Mutex
	busy, asked := true, 0
	h.w.SetRelaunchAdapter(func(executorID, command, control string) (agentevents.Launch, error) {
		mu.Lock()
		defer mu.Unlock()
		asked++
		if busy {
			return agentevents.Launch{}, agentevents.ErrControlBusy
		}
		return agentevents.Launch{Command: command}, nil
	})
	supervisor := h.w.agentRelaunch()
	at := time.Now()
	supervisor.now = func() time.Time { return at }
	killAgent(t, h.w, id, pid)
	h.w.poll()
	waitUntil(t, 10*time.Second, "the wait", func() bool { return !supervisor.busy(id) })
	h.w.poll()
	if _, records, reported, _ := ledger.snapshot(); len(records) != 0 || len(reported) != 0 {
		t.Fatalf("while it drains: records %+v reported %+v", records, reported)
	}
	if _, relaunch, ok := supervisor.line(id, false, "", at); !ok || relaunch.State != "restarting" || relaunch.Line != "Restarting after its turn ends" {
		t.Fatalf("line = %+v %v", relaunch, ok)
	}
	mu.Lock()
	busy = false
	mu.Unlock()
	at = at.Add(agentEndWaitEvery + time.Second)
	h.w.poll()
	waitForHostInfo(t, h.w, id, func(info hostInfo) bool { return info.Alive && info.PID != pid })
	waitUntil(t, 10*time.Second, "relaunch record", func() bool {
		_, records, _, _ := ledger.snapshot()
		return len(records) == 1
	})
	if _, records, _, _ := ledger.snapshot(); records[0].Outcome != "idle" {
		t.Fatalf("record = %+v", records[0])
	}
}
