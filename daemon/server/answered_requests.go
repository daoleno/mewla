package server

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/answered"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

// Answered requests: every answer path records what it answered, and the
// apps read the records three ways: as rows in the request's own thread
// (source request_answered, at the time it was asked), as a request_answered
// broadcast the moment one is written (so a second device seals its card),
// and as answered_requests, the history the tray lists.

const answeredRequestsDefaultLimit = 100

// answeredConversationKind is the kind and source of an answered request's
// row in its thread.
const answeredConversationKind = "request_answered"

// SetAnsweredRequests gives the server the answered-request ledger.
func (s *Server) SetAnsweredRequests(ledger *answered.Ledger) {
	s.mu.Lock()
	s.answered = ledger
	s.mu.Unlock()
	ledger.OnStamp(s.stampAnsweredThread)
	ledger.OnRecord(func(record answered.Request) {
		s.broadcastJSON(map[string]any{"type": "request_answered", "request": record})
	})
}

// stampAnsweredThread names the Work a delegated Worker asked for and the
// Brain thread a request was answered in: the current chat for Brain's own
// Session, the Work's own thread for its question.
func (s *Server) stampAnsweredThread(record *answered.Request) {
	if s.brain == nil {
		return
	}
	if record.WorkID == "" && record.SessionID != "" {
		if workID, ok := s.brain.ActiveWorkOfSession(record.SessionID); ok {
			record.WorkID = workID
		}
	}
	if record.ThreadID != "" {
		return
	}
	switch {
	case record.Kind == answered.KindWorkQuestion && record.WorkID != "":
		if item, err := s.brain.Work(record.WorkID); err == nil {
			record.ThreadID = strings.TrimSpace(item.SourceThreadID)
		}
	case record.SessionID != "" && record.SessionID == s.brain.CurrentHostSessionID():
		if threadID, err := s.brain.ChatThreadID(); err == nil {
			record.ThreadID = strings.TrimSpace(threadID)
		}
	}
}

func (s *Server) answeredLedger() *answered.Ledger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answered
}

func (s *Server) recordAnswered(record answered.Request) {
	ledger := s.answeredLedger()
	if ledger == nil {
		return
	}
	if _, _, err := ledger.Record(record); err != nil {
		log.Printf("answered request %s not recorded: %v", record.ID, err)
	}
}

// answeredByConn is the paired device a WebSocket answer came from.
func (s *Server) answeredByConn(conn *websocket.Conn) answered.Answerer {
	by := answered.Answerer{Kind: answered.ByDevice, DeviceID: s.deviceIDOf(conn)}
	if by.DeviceID != "" && s.auth != nil {
		for _, device := range s.auth.ListDevices() {
			if device.ID == by.DeviceID {
				by.DeviceName = device.Name
			}
		}
	}
	return by
}

// handleAnsweredRequests answers the tray's history: the newest answered
// requests, of one Session when session_id names it.
func (s *Server) handleAnsweredRequests(conn *websocket.Conn, raw clientMessage) {
	limit := raw.Limit
	if limit <= 0 || limit > 500 {
		limit = answeredRequestsDefaultLimit
	}
	records := s.answeredLedger().List(answered.Filter{SessionID: strings.TrimSpace(raw.SessionID), Limit: limit})
	if records == nil {
		records = []answered.Request{}
	}
	s.sendJSON(conn, map[string]any{"type": "answered_requests", "request_id": raw.RequestID, "requests": records})
}

// answeredConversationEvent is a record as a row of its thread.
func answeredConversationEvent(record answered.Request) work.CodexConversationEvent {
	copied := record
	return work.CodexConversationEvent{
		ID:        "answered:" + record.ID,
		Timestamp: record.At().Format(time.RFC3339Nano),
		// Its own kind: an app that does not know it drops the row.
		Kind:    answeredConversationKind,
		Role:    "system",
		Title:   record.Title,
		Body:    record.Answer,
		Status:  record.Outcome,
		Source:  answeredConversationKind,
		WorkID:  record.WorkID,
		Request: &copied,
	}
}

// withAnsweredRequests adds a Session's answered requests to its thread.
func (s *Server) withAnsweredRequests(sessionID string, conversation work.CodexConversation) work.CodexConversation {
	ledger := s.answeredLedger()
	sessionID = strings.TrimSpace(sessionID)
	if ledger == nil || sessionID == "" {
		return conversation
	}
	records := ledger.List(answered.Filter{SessionID: sessionID})
	if len(records) == 0 {
		return conversation
	}
	events := make([]work.CodexConversationEvent, 0, len(conversation.Events)+len(records))
	events = append(events, conversation.Events...)
	for index := len(records) - 1; index >= 0; index-- {
		events = append(events, answeredConversationEvent(records[index]))
	}
	conversation.Events = events
	return conversation
}

// brainThreadAnsweredRequests are the answered requests Brain's thread
// shows: those answered in it (Brain's own prompts and the questions of the
// Work the thread started).
func (s *Server) brainThreadAnsweredRequests(threadID string) []work.CodexConversationEvent {
	ledger := s.answeredLedger()
	if ledger == nil || threadID == "" {
		return nil
	}
	records := ledger.List(answered.Filter{ThreadID: threadID})
	out := make([]work.CodexConversationEvent, 0, len(records))
	for index := len(records) - 1; index >= 0; index-- {
		out = append(out, answeredConversationEvent(records[index]))
	}
	return out
}

// promptRecordID names one time a prompt was shown. A prompt's id is drawn
// from what it shows, so a Session that asks the same thing again shows the
// same id; when it appeared tells the two apart. askedAt is the prompt
// watch's time for it (PromptAskedAt), which every answer path reads.
func promptRecordID(sessionID, promptID string, askedAt *time.Time) string {
	id := "prompt:" + sessionID + ":" + promptID
	if askedAt != nil && !askedAt.IsZero() {
		id += ":" + strconv.FormatInt(askedAt.UnixMilli(), 10)
	}
	return id
}

// PromptAskedAt is when the prompt promptID was shown in sessionID, while it
// is open or is the last one that left the screen; nil when not seen.
func (s *Server) PromptAskedAt(sessionID, promptID string) *time.Time {
	return s.prompts.since(strings.TrimSpace(sessionID), strings.TrimSpace(promptID))
}

// PromptAnswerRecord is the record of a screen prompt answered with answer.
// recorded are the answers a choice prompt's agent wrote, when known.
func PromptAnswerRecord(sessionID string, prompt classifier.Prompt, answer PromptAnswer, recorded []string, by answered.Answerer, askedAt *time.Time, now time.Time) answered.Request {
	record := answered.Request{
		ID: promptRecordID(sessionID, prompt.ID, askedAt), Kind: string(prompt.Kind), SessionID: sessionID,
		Title: prompt.Title, Tool: prompt.Tool, Summary: prompt.Summary,
		Asked: firstNonEmpty(prompt.Summary, prompt.Request, prompt.Title), Outcome: answered.OutcomeAnswered,
		By: by, AskedAt: askedAt, AnsweredAt: now,
		Anchor: answered.Anchor{PromptID: prompt.ID},
	}
	if prompt.Kind == classifier.PromptChoice {
		record.Anchor.CallID = prompt.CallID
		record.Kind = answered.KindChoice
		if len(prompt.Questions) > 0 {
			record.Title = firstNonEmpty(prompt.Questions[0].Header, prompt.Title)
			record.Asked = prompt.Questions[0].Question
		}
		answers := recorded
		if len(answers) == 0 {
			answers = promptChoiceLabels(prompt, answer.Answers)
		}
		record.Answer = strings.Join(answers, "; ")
		return record
	}
	for _, option := range prompt.Options {
		if option.Number == answer.Number {
			record.Answer = option.Label
		}
	}
	if prompt.Kind == classifier.PromptPermission || prompt.Kind == classifier.PromptApproval || prompt.Kind == classifier.PromptPlan {
		record.Outcome = menuOutcome(record.Answer)
	}
	return record
}

// promptChoiceLabels are the option labels (or free text) a choice answer picked.
func promptChoiceLabels(prompt classifier.Prompt, selections []ChoiceSelection) []string {
	var out []string
	for index, selection := range selections {
		if index >= len(prompt.Questions) {
			break
		}
		var picked []string
		for _, number := range selection.Selected {
			for _, option := range prompt.Questions[index].Options {
				if option.Number == number {
					picked = append(picked, option.Label)
				}
			}
		}
		if other := strings.TrimSpace(selection.Other); other != "" {
			picked = append(picked, other)
		}
		out = append(out, strings.Join(picked, ", "))
	}
	return out
}

// menuOutcome reads an approval menu's answer: "Yes…" approves, "No…"
// refuses; any other row (keep planning, …) is an answer.
func menuOutcome(label string) string {
	lower := strings.ToLower(strings.TrimSpace(label))
	switch {
	case lower == "yes" || strings.HasPrefix(lower, "yes,") || strings.HasPrefix(lower, "yes "):
		return answered.OutcomeApproved
	case lower == "no" || strings.HasPrefix(lower, "no,") || strings.HasPrefix(lower, "no "):
		return answered.OutcomeDenied
	}
	return answered.OutcomeAnswered
}

// ChoiceAnswerRecord is the record of an agent's question answered in its
// transcript (call_id), with the answers the agent recorded.
func ChoiceAnswerRecord(sessionID string, event work.CodexConversationEvent, answers []string, by answered.Answerer, now time.Time) answered.Request {
	record := answered.Request{
		ID: "choice:" + sessionID + ":" + event.CallID, Kind: answered.KindChoice, SessionID: sessionID,
		Answer: strings.Join(answers, "; "), Outcome: answered.OutcomeAnswered, By: by, AnsweredAt: now,
		Anchor: answered.Anchor{CallID: event.CallID},
	}
	if at, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
		record.AskedAt = &at
	}
	if event.Choice != nil && len(event.Choice.Questions) > 0 {
		record.Title = event.Choice.Questions[0].Header
		record.Asked = event.Choice.Questions[0].Question
	}
	return record
}

// ApprovalAnswerRecord is the record of an agent approval answered with one
// of its options.
func ApprovalAnswerRecord(sessionID string, approval ApprovalWire, optionID string, by answered.Answerer, now time.Time) answered.Request {
	record := answered.Request{
		ID: "approval:" + sessionID + ":" + approval.ID, Kind: answered.KindApproval, SessionID: sessionID,
		Title: approval.Tool, Tool: approval.Tool, Summary: approval.Summary,
		Asked:   firstNonEmpty(approval.Summary, approval.Tool, approval.Kind),
		Outcome: answered.OutcomeAnswered, By: by, AnsweredAt: now,
		Anchor: answered.Anchor{ApprovalID: approval.ID},
	}
	for _, option := range approval.Options {
		if option.ID == optionID {
			record.Answer = option.Label
		}
	}
	switch optionID {
	case "allow":
		record.Outcome = answered.OutcomeApproved
	case "deny":
		record.Outcome = answered.OutcomeDenied
	default:
		record.Outcome = menuOutcome(firstNonEmpty(record.Answer, optionID))
	}
	return record
}

// WorkQuestionRecord is the record of Brain's question on a Work answered
// with text.
func WorkQuestionRecord(item brain.Work, text string, by answered.Answerer, now time.Time) answered.Request {
	stamp := item.UpdatedAt
	if item.QuestionAt != nil {
		stamp = *item.QuestionAt
	}
	return answered.Request{
		ID: "work:" + item.ID + ":" + stamp.UTC().Format(time.RFC3339Nano), Kind: answered.KindWorkQuestion,
		WorkID: item.ID, Title: item.Title, Asked: item.Question, Answer: text,
		Outcome: answered.OutcomeAnswered, By: by, AskedAt: item.QuestionAt, AnsweredAt: now,
	}
}

// workQuestionBefore reads the Work a reply answers while Brain's question
// is still on it.
func (s *Server) workQuestionBefore(workID string) (brain.Work, bool) {
	if s.brain == nil || s.answeredLedger() == nil {
		return brain.Work{}, false
	}
	item, err := s.brain.Work(strings.TrimSpace(workID))
	if err != nil || strings.TrimSpace(item.Question) == "" {
		return brain.Work{}, false
	}
	return item, true
}

// recordWorkReply records the answer to the question a Work carried, once
// Brain has the reply.
func (s *Server) recordWorkReply(asked brain.Work, result brain.WorkUserActionResult, by answered.Answerer) {
	if asked.ID == "" || result.Event.Kind != "user.replied" {
		return
	}
	s.recordAnswered(WorkQuestionRecord(asked, result.Event.Summary, by, result.Event.CreatedAt))
}

// DSHAnswerRecord is the record of a DSH approval or question answered with
// payload (the dsh_answer the app sent). item is the pending request.
func DSHAnswerRecord(sessionID string, item work.DSHInteraction, payload map[string]any, by answered.Answerer, now time.Time) (answered.Request, bool) {
	var asked struct {
		Type      string `json:"type"`
		ToolName  string `json:"toolName"`
		Reason    string `json:"reason"`
		CallID    string `json:"callId"`
		Questions []struct {
			Question string `json:"question"`
			Header   string `json:"header"`
		} `json:"questions"`
	}
	if json.Unmarshal(item.Payload, &asked) != nil {
		return answered.Request{}, false
	}
	record := answered.Request{
		ID: "dsh:" + sessionID + ":" + item.RPCID, SessionID: sessionID, By: by, AnsweredAt: now,
		Anchor: answered.Anchor{CallID: asked.CallID, RPCID: item.RPCID},
	}
	switch asked.Type {
	case "approval/requested":
		record.Kind, record.Title, record.Asked = answered.KindApproval, asked.ToolName, firstNonEmpty(asked.Reason, asked.ToolName)
		switch payload["outcome"] {
		case "allowed-once":
			record.Outcome, record.Answer = answered.OutcomeApproved, "Allow once"
		case "rejected":
			record.Outcome, record.Answer = answered.OutcomeDenied, "Reject"
		default:
			return answered.Request{}, false
		}
	case "question/requested":
		record.Kind = answered.KindQuestion
		if len(asked.Questions) > 0 {
			record.Title, record.Asked = asked.Questions[0].Header, asked.Questions[0].Question
		}
		if payload["cancel"] == true {
			record.Outcome = answered.OutcomeDismissed
			break
		}
		record.Outcome = answered.OutcomeAnswered
		record.Answer = strings.Join(answerStrings(payload["answer"]), "; ")
	default:
		return answered.Request{}, false
	}
	return record, true
}

// answerStrings are the words of an answer value, in a stable order.
func answerStrings(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, answerStrings(item)...)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var out []string
		for _, key := range keys {
			out = append(out, answerStrings(typed[key])...)
		}
		return out
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// promptWatch remembers each Session's open screen prompt, so a prompt that
// leaves the screen with no answer recorded was answered in the terminal, and
// a recorded answer knows when the prompt appeared.
type promptWatch struct {
	mu   sync.Mutex
	open map[string]watchedPrompt
	// left is each Session's prompt that last left the screen: an answer that
	// lands after it left still names the same showing.
	left map[string]watchedPrompt
}

type watchedPrompt struct {
	prompt classifier.Prompt
	since  time.Time
}

func (w *promptWatch) since(sessionID, promptID string) *time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if open, ok := w.open[sessionID]; ok && open.prompt.ID == promptID {
		at := open.since
		return &at
	}
	if left, ok := w.left[sessionID]; ok && left.prompt.ID == promptID {
		at := left.since
		return &at
	}
	return nil
}

// observe takes a Session's current prompt (nil when none) and returns the
// prompt that just left its screen, if one did.
func (w *promptWatch) observe(sessionID string, prompt *classifier.Prompt, now time.Time) (watchedPrompt, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open == nil {
		w.open, w.left = map[string]watchedPrompt{}, map[string]watchedPrompt{}
	}
	previous, had := w.open[sessionID]
	if prompt != nil && had && previous.prompt.ID == prompt.ID {
		// The same showing, read again: the transcript may have named its tool
		// call since it was first seen, and the record of its answer says it.
		previous.prompt = *prompt
		w.open[sessionID] = previous
		return watchedPrompt{}, false
	}
	if had {
		w.left[sessionID] = previous
	}
	if prompt != nil {
		w.open[sessionID] = watchedPrompt{prompt: *prompt, since: now}
	} else {
		delete(w.open, sessionID)
	}
	return previous, had
}

func (w *promptWatch) isOpen(sessionID, promptID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	open, ok := w.open[sessionID]
	return ok && open.prompt.ID == promptID
}

func (w *promptWatch) forget(sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.open, sessionID)
	delete(w.left, sessionID)
}

// answeredWhileShown reports whether something the Session asked was
// answered through Mewla while its screen prompt showed: a permission its hook
// held, or the agent's approval, is drawn as that prompt and recorded as
// itself, so the prompt leaving is that answer.
func answeredWhileShown(ledger *answered.Ledger, sessionID string, since time.Time) bool {
	newest := ledger.List(answered.Filter{SessionID: sessionID, Limit: 1})
	return len(newest) == 1 && !newest[0].AnsweredAt.Before(since)
}

// promptLeftGrace is how long a prompt must stay off the screen before it
// counts as answered there: a screen read that misses it once must not seal
// it, and the app's own answer lands well inside it.
const promptLeftGrace = 5 * time.Second

// observePromptLifecycle records a screen prompt that left a live Session's
// screen without an answer through Mewla: it was answered in the terminal.
// The answer itself is not known; an explicit answer recorded later (the
// app's, Brain's) replaces this one. A Session that ended takes its prompt
// with it and records nothing. A choice is not inferred here: its transcript
// records the answer itself.
func (s *Server) observePromptLifecycle(ev watcher.SessionEvent) {
	if ev.Worker == nil || s.answeredLedger() == nil {
		return
	}
	sessionID := strings.TrimSpace(ev.Worker.ID)
	if ev.Type == "worker_removed" {
		s.prompts.forget(sessionID)
		return
	}
	var current *classifier.Prompt
	if ev.Worker.State == classifier.StateBlocked && ev.Worker.Prompt != nil {
		current = ev.Worker.Prompt
	}
	now := time.Now().UTC()
	left, ok := s.prompts.observe(sessionID, current, now)
	if !ok || left.prompt.ID == "" || left.prompt.Kind == classifier.PromptChoice {
		return
	}
	grace := s.promptLeftGrace
	if grace == 0 {
		grace = promptLeftGrace
	}
	time.AfterFunc(grace, func() {
		if s.prompts.isOpen(sessionID, left.prompt.ID) || s.lookupWorker(sessionID) == nil {
			return
		}
		ledger := s.answeredLedger()
		id := promptRecordID(sessionID, left.prompt.ID, &left.since)
		if _, recorded := ledger.Get(id); recorded || answeredWhileShown(ledger, sessionID, left.since) {
			return
		}
		record := PromptAnswerRecord(sessionID, left.prompt, PromptAnswer{}, nil, answered.Answerer{Kind: answered.ByTerminal}, &left.since, now)
		record.Answer = ""
		record.Outcome = answered.OutcomeAnswered
		s.recordAnswered(s.SettleTerminalAnswer(record, left.prompt.CallID))
	})
}

// SettleTerminalAnswer says how a permission or approval answered in the
// terminal ended, from what the provider recorded for its tool call: the call
// ran (approved) or was refused (denied). The option the person picked is not
// recorded anywhere, so Answer stays empty. A call with no result yet, a
// request the transcript cannot name, or another provider leave the record as
// it is: answered, not known how. callID names the call when the prompt knew
// it; without it the newest call with the record's tool and line is taken.
func (s *Server) SettleTerminalAnswer(record answered.Request, callID string) answered.Request {
	if !awaitsOutcome(record) || s.watcher == nil {
		return record
	}
	resolved := s.resolveCodexConversationWorker(clientMessage{WorkerID: record.SessionID})
	if !resolved.ready || !resolved.fromWatcher || resolved.provider != work.WorkerProviderClaude {
		return record
	}
	conversation, err := s.choiceConversationLoader(resolved)()
	if err != nil {
		return record
	}
	return settleFromConversation(record, callID, conversation)
}

// awaitsOutcome is a record of a permission or approval answered in the
// terminal, whose outcome is not known yet.
func awaitsOutcome(record answered.Request) bool {
	return record.By.Kind == answered.ByTerminal && record.Outcome == answered.OutcomeAnswered && record.SessionID != "" &&
		(record.Kind == string(classifier.PromptPermission) || record.Kind == answered.KindApproval)
}

// settleFromConversation is SettleTerminalAnswer over a loaded conversation.
func settleFromConversation(record answered.Request, callID string, conversation work.CodexConversation) answered.Request {
	if !awaitsOutcome(record) {
		return record
	}
	var call work.ConversationToolCall
	var found bool
	if callID != "" {
		call, found = work.ConversationToolCallByID(conversation, callID)
	} else if record.Tool != "" && record.Summary != "" {
		call, found = work.ConversationToolCallByLine(conversation, record.Tool, record.Summary)
	}
	if !found || !call.Done {
		return record
	}
	if call.Rejected {
		record.Outcome = answered.OutcomeDenied
	} else {
		record.Outcome = answered.OutcomeApproved
	}
	return record
}
