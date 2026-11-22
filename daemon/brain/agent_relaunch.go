package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// Agent relaunch, Brain's half (see watcher/agent_relaunch.go): which open
// Work an ended agent belongs to, how it resumes its conversation, the bound,
// and the durable record.

// workEventKindRelaunched is the Work Event of one relaunch.
const workEventKindRelaunched = "session.relaunched"

// The bound: at most agentRelaunchLimit relaunches of one Work's Worker in
// agentRelaunchWindow; the next end is reported to Brain with its cause.
// Waits before each relaunch grow so an agent that ends at once is not
// started in a tight loop.
const (
	agentRelaunchLimit  = 3
	agentRelaunchWindow = 30 * time.Minute
)

var agentRelaunchBackoff = []time.Duration{0, 10 * time.Second, 30 * time.Second}

type agentRelaunchDetails struct {
	SessionID    string    `json:"session_id"`
	Conversation string    `json:"conversation,omitempty"`
	Attempt      int       `json:"attempt"`
	Cause        string    `json:"cause"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	ExitSignal   string    `json:"exit_signal,omitempty"`
	EndedAt      time.Time `json:"ended_at"`
	FromTurnID   string    `json:"from_turn_id,omitempty"`
	TurnID       string    `json:"turn_id,omitempty"`
	Outcome      string    `json:"outcome"`
}

var _ watcher.AgentRelaunchLedger = (*Service)(nil)

// SetWorkerLaunchEnv gives Brain the environment a Worker is launched with
// (its progress command and state directory), for a relaunch.
func (s *Service) SetWorkerLaunchEnv(env func() map[string]string) {
	s.workerLaunchEnv.Store(&env)
}

// agentEndWork is the open Work an ended agent's Session belongs to: the one
// whose live Attempt runs on it (turnID is that Attempt's Turn), or else the
// open Work the Session last joined.
func (s *Store) agentEndWork(sessionID string) (item Work, turnID string, found bool, err error) {
	if st, ok := s.fsmWorkByAttemptSession(sessionID); ok && st.Attempt != nil && st.Attempt.Delegated && !st.Status.Terminal() {
		item, err = s.Work(string(st.ID))
		return item, string(st.Attempt.TurnToken), err == nil, err
	}
	records, err := s.workWorkerRecords()
	if err != nil {
		return Work{}, "", false, err
	}
	candidates := []WorkWorkerRecord{}
	for _, record := range records {
		if record.SessionID == sessionID {
			candidates = append(candidates, record)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].JoinedAt.After(candidates[j].JoinedAt) })
	for _, record := range candidates {
		current, readErr := s.Work(record.WorkID)
		if errors.Is(readErr, ErrWorkNotFound) {
			continue
		}
		if readErr != nil {
			return Work{}, "", false, readErr
		}
		if current.Status == WorkDone || current.Status == WorkCancelled {
			// The newest Work this Session joined is closed: it is no open
			// Work's Worker any more.
			return Work{}, "", false, nil
		}
		return current, "", true, nil
	}
	return Work{}, "", false, nil
}

// agentRelaunches are the relaunch records of a Work's Worker, oldest first.
func (s *Store) agentRelaunches(workID, sessionID string) ([]agentRelaunchDetails, []time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, nil, err
	}
	var records []agentRelaunchDetails
	var at []time.Time
	for _, event := range database.BrainWorkEvents {
		if event.WorkID != workID || event.Kind != workEventKindRelaunched {
			continue
		}
		var details agentRelaunchDetails
		if json.Unmarshal([]byte(event.DetailsJSON), &details) != nil || details.SessionID != sessionID {
			continue
		}
		records = append(records, details)
		at = append(at, event.CreatedAt)
	}
	return records, at, nil
}

// workConversation is the newest conversation recorded for a Work's Worker.
func (s *Store) workConversation(workID, sessionID string) (WorkTranscriptLink, bool) {
	links, err := s.WorkTranscripts(workID)
	if err != nil {
		return WorkTranscriptLink{}, false
	}
	var newest WorkTranscriptLink
	found := false
	for _, link := range links {
		if link.SessionID != sessionID {
			continue
		}
		if !found || !link.RecordedAt.Before(newest.RecordedAt) {
			newest, found = link, true
		}
	}
	return newest, found
}

// PlanAgentRelaunch decides what happens to an ended agent.
func (s *Service) PlanAgentRelaunch(end watcher.AgentEnd) (watcher.AgentRelaunchPlan, error) {
	if s == nil || s.store == nil {
		return watcher.AgentRelaunchPlan{}, nil
	}
	sessionID := strings.TrimSpace(end.SessionID)
	if host, err := s.store.HostSession(); err == nil && strings.TrimSpace(host.ID) == sessionID {
		return watcher.AgentRelaunchPlan{}, nil // Brain's own Host has its own restart
	}
	item, turnID, found, err := s.store.agentEndWork(sessionID)
	if err != nil || !found {
		return watcher.AgentRelaunchPlan{}, err
	}
	if turnID == "" {
		// A Turn in flight that the Work no longer holds (it was released in
		// the moment) is not continued.
		end.TurnID = ""
	}
	records, err := s.store.workWorkerRecords()
	if err != nil {
		return watcher.AgentRelaunchPlan{}, err
	}
	record := records[workWorkerKey(item.ID, sessionID)]
	launch := strings.TrimSpace(record.LaunchCommand)
	provider := work.InferWorkerProvider(firstNonEmpty(launch, end.Command))
	name := work.AgentProductName(provider, firstNonEmpty(launch, end.Command))
	plan := watcher.AgentRelaunchPlan{
		WorkID: item.ID, TurnID: turnID,
		Name: firstNonEmpty(record.Name, end.Name), Cwd: firstNonEmpty(record.Cwd, end.Cwd),
	}
	cause, short := end.Cause(), end.ShortCause()
	startedAt := end.Exit.EndedAt.Add(-end.Exit.Ran)
	if !end.Exit.EndedAt.IsZero() && work.ProgramUpdatedSince(firstNonEmpty(launch, end.Command), startedAt) {
		cause += " after it was updated"
		short = "an update"
	} else if end.Clean() {
		if turnID == "" {
			// It ended itself between Turns: finished, or the person exited it.
			return watcher.AgentRelaunchPlan{}, nil
		}
		return reportAgentEnd(plan, name, cause, "it ended itself before it reported", "Not restarted: it ended itself"), nil
	}
	if launch == "" {
		return reportAgentEnd(plan, name, cause, "Mewla has no record of how this Worker was launched", "Not restarted: no launch record"), nil
	}
	if missing := work.MissingAgentFor(launch); missing != nil {
		return reportAgentEnd(plan, name, cause, strings.TrimSuffix(missing.Message(), "."), "Not restarted: not installed"), nil
	}
	if authID := firstNonEmpty(record.ExecutorID, provider); authID != "" && ExecutorAuth(authID) == HostAuthUnauthenticated {
		return reportAgentEnd(plan, name, cause, name+" is not signed in on this computer", "Not restarted: signed out"), nil
	}
	link, _ := s.store.workConversation(item.ID, sessionID)
	token, known, resumable := s.hostProviderResumeToken(HostSession{
		ProviderSessionID: link.ProviderSessionID, TranscriptPath: link.TranscriptPath, ProviderDataRoot: link.ProviderDataRoot,
	}, work.WorkerExecutor{ID: record.ExecutorID, Provider: provider, Command: launch})
	if !known || !resumable {
		// Mewla's own id on the launch (Claude, OpenCode, Pi, DSH) names the
		// conversation when no link was recorded yet.
		if own, present, ownErr := work.ProviderResumeToken(provider, launch); ownErr == nil && present {
			token, resumable = own, true
		} else {
			return reportAgentEnd(plan, name, cause, "Mewla has no conversation of it to resume", "Not restarted: nothing to resume"), nil
		}
	}
	command, err := work.RelaunchCommand(provider, launch, token)
	if err != nil {
		return reportAgentEnd(plan, name, cause, "its conversation can't be resumed ("+err.Error()+")", "Not restarted: can't resume"), nil
	}
	env := map[string]string{}
	if base := s.workerLaunchEnv.Load(); base != nil && *base != nil {
		env = mergeStringMaps(env, (*base)())
	}
	if routes := s.sessionRoutes(); routes != nil {
		routeCommand, routeEnv, routed, routeErr := routes.ResumeLaunch(sessionID, command)
		if routeErr != nil {
			return reportAgentEnd(plan, name, cause, "its model route can't be restored ("+routeErr.Error()+")", "Not restarted: no model route"), nil
		}
		if routed {
			command = firstNonEmpty(routeCommand, command)
			env = mergeStringMaps(env, routeEnv)
		}
	}
	_, at, err := s.store.agentRelaunches(item.ID, sessionID)
	if err != nil {
		return watcher.AgentRelaunchPlan{}, err
	}
	now := s.nowUTC()
	recent := 0
	for _, when := range at {
		if now.Sub(when) < agentRelaunchWindow {
			recent++
		}
	}
	if recent >= agentRelaunchLimit {
		minutes := int(agentRelaunchWindow / time.Minute)
		return reportAgentEnd(plan, name, cause, fmt.Sprintf("it was started again %d times in the last %d minutes and ended each time", recent, minutes),
			fmt.Sprintf("Stopped after %d restarts", recent)), nil
	}
	plan.Action = watcher.AgentEndRelaunch
	plan.Command, plan.Env, plan.PrepareWorkspace = command, env, work.PrepareDelegatedWorkspace
	plan.Conversation, plan.Attempt = token, recent+1
	if recent < len(agentRelaunchBackoff) {
		plan.After = agentRelaunchBackoff[recent]
	}
	plan.Words = name + " " + cause + "; Mewla started it again in the same conversation"
	if turnID != "" {
		plan.Words += " and asked it to continue the task"
	}
	plan.Words += "."
	plan.Line = "Restarted after " + short
	return plan, nil
}

// reportAgentEnd is the plan for an agent not started again: words for Brain,
// and the Session's short line.
func reportAgentEnd(plan watcher.AgentRelaunchPlan, name, cause, reason, line string) watcher.AgentRelaunchPlan {
	plan.Action = watcher.AgentEndReport
	plan.Words = name + " " + cause + "; not started again: " + reason + "."
	plan.Line = line
	return plan
}

// RecordAgentRelaunch appends the relaunch as a Work Event. It is history and
// the bound's count: it does not wake Brain, whose Work goes on.
func (s *Service) RecordAgentRelaunch(record watcher.AgentRelaunch) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("brain service is not configured")
	}
	if strings.TrimSpace(record.WorkID) == "" || strings.TrimSpace(record.SessionID) == "" || record.At.IsZero() {
		return fmt.Errorf("agent relaunch requires work, session and time")
	}
	details, err := json.Marshal(agentRelaunchDetails{
		SessionID: record.SessionID, Conversation: record.Conversation, Attempt: record.Attempt, Cause: record.Cause,
		ExitCode: record.ExitCode, ExitSignal: record.ExitSignal, EndedAt: record.EndedAt.UTC(),
		FromTurnID: record.FromTurnID, TurnID: record.TurnID, Outcome: record.Outcome,
	})
	if err != nil {
		return err
	}
	summary := strings.TrimSpace(record.Words)
	switch record.Outcome {
	case "continuation_unknown":
		summary += " Whether it received the request to continue is unknown."
	case "not_ready":
		summary += " It did not become ready for the request to continue."
	}
	_, err = s.store.appendWorkAuditEvent(WorkEvent{
		WorkID: record.WorkID, Kind: workEventKindRelaunched,
		DedupeKey:  fmt.Sprintf("session:%s:relaunched:%d", record.SessionID, record.EndedAt.UTC().UnixNano()),
		PayloadRef: "session:" + record.SessionID, SourceName: record.SessionID,
		Summary: summary, DetailsJSON: string(details), CreatedAt: record.At.UTC(),
	})
	return err
}

// ReportAgentEnd tells Brain once that an ended agent is not started again.
// A Worker still holding a Turn (relaunched but never ready) loses it as
// turn_lost; an idle Worker's Work gets one turn_lost review of its own end.
func (s *Service) ReportAgentEnd(end watcher.AgentEnd, plan watcher.AgentRelaunchPlan) (bool, error) {
	if s == nil || s.store == nil || strings.TrimSpace(plan.WorkID) == "" {
		return true, nil
	}
	store := s.store
	sessionID := strings.TrimSpace(end.SessionID)
	ref := fmt.Sprintf("agent-end:%s:%d", sessionID, end.Exit.EndedAt.UTC().UnixNano())
	st, live := store.fsmWorkByAttemptSession(sessionID)
	if live && st.Attempt != nil && string(st.ID) == plan.WorkID {
		ref = string(st.Attempt.TurnToken)
	}
	if _, err := store.appendWorkAuditEvent(WorkEvent{
		WorkID: plan.WorkID, Kind: "session.uncertain",
		DedupeKey:  "session:" + sessionID + ":turn:" + ref + ":agent_ended",
		PayloadRef: ref, SourceName: sessionID,
		Summary: plan.Words, CreatedAt: s.nowUTC(),
	}); err != nil {
		return false, err
	}
	if live && st.Attempt != nil && string(st.ID) == plan.WorkID {
		identity := lifecycle.AttemptIdentity{SessionID: st.Attempt.SessionID, TurnToken: st.Attempt.TurnToken, Fence: st.Attempt.Generation}
		if _, err := store.fsm.ReportTurnLost(st.ID, identity, "agent_ended"); err != nil {
			return false, fsmObservationResult(err)
		}
		return true, store.SyncWorkProjection(plan.WorkID)
	}
	// Brain may still have an earlier review of this Work (its result, say):
	// the loss is opened once that one has left it, so Brain reads both.
	opened, err := store.fsm.OpenReview(lifecycle.WorkID(plan.WorkID), "turn_lost", ref)
	if err != nil {
		if errors.Is(err, lifecycle.ErrTerminal) || errors.Is(err, lifecycle.ErrUnknownWork) {
			return true, nil
		}
		return false, err
	}
	told := opened != nil && opened.Review != nil && opened.Review.Reason == "turn_lost" && opened.Review.Ref == ref
	if !told && opened != nil && opened.Status.Terminal() {
		told = true
	}
	return told, store.SyncWorkProjection(plan.WorkID)
}
