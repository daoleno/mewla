//go:build ghosttyvt && cgo && (linux || darwin)

package watcher

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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
}

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
