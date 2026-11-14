package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/debuglog"
	"github.com/daoleno/mewla/daemon/live"
	"github.com/daoleno/mewla/daemon/push"
)

// Live state: the phone's ongoing notification and widgets. This file owns the
// daemon side; server.go carries only the hook lines that reach it.

const (
	liveSafetyNetInterval = 20 * time.Second
	maxLiveAnswersKept    = 64
	widgetSnapshotPath    = "/widget/snapshot"
)

// livePusher is the part of the push client that carries live state. A pusher
// without it (tests, no Expo) simply never receives live state.
type livePusher interface {
	SendLive(data map[string]any) error
	SetLiveCapable(deviceID string, capable bool)
	LiveCapable() bool
}

// liveHub is the Server's live state owner, created on first use.
type liveHub struct {
	once      sync.Once
	projector *live.Projector
	tokens    *live.TokenStore
	initErr   error
	stopRevoc func()

	mu       sync.Mutex
	prevKind string
	// last is the newest Brain snapshot observed, so a Session's change
	// re-projects without building a new snapshot.
	last    brain.Snapshot
	hasLast bool

	answerMu sync.Mutex
	answers  map[string]map[string]any
	order    []string

	// Test seams; production reads the Brain service.
	snapshotOverride func() (brain.Snapshot, error)
	replyOverride    func(brain.WorkUserActionRequest) (brain.WorkUserActionResult, error)
}

func (s *Server) liveRuntime() *liveHub {
	hub := &s.live
	hub.once.Do(func() {
		hub.answers = map[string]map[string]any{}
		dir := ""
		if s.auth != nil {
			dir = s.auth.StorageDir()
		}
		if dir == "" {
			hub.initErr = errors.New("no state directory for widget tokens")
		} else {
			hub.tokens, hub.initErr = live.OpenTokenStore(dir)
		}
		hub.projector = live.NewProjector(time.Now().UnixMilli(), live.DefaultDebounce, s.onLiveChange)
		if hub.tokens != nil && s.auth != nil {
			hub.stopRevoc = s.auth.SubscribeRevocations(func(deviceID string) {
				hub.tokens.RevokeDevice(deviceID)
				if s.pusher != nil {
					// A revoked phone must stop hearing what its agents say.
					s.pusher.ForgetDevice(deviceID)
				}
			})
		}
	})
	return hub
}

// startLive opens the store (so revocations are observed from boot) and runs
// the safety-net ticker that catches changes no broadcast announced.
func (s *Server) startLive(ctx context.Context) {
	hub := s.liveRuntime()
	go func() {
		ticker := time.NewTicker(liveSafetyNetInterval)
		defer ticker.Stop()
		defer hub.projector.Stop()
		defer func() {
			if hub.stopRevoc != nil {
				hub.stopRevoc()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if snapshot, err := s.liveSnapshot(); err == nil {
					s.observeLive(snapshot)
				}
			}
		}
	}()
}

func (s *Server) liveSnapshot() (brain.Snapshot, error) {
	if s.live.snapshotOverride != nil {
		return s.live.snapshotOverride()
	}
	if s.brain == nil {
		return brain.Snapshot{}, nil
	}
	return s.brain.Snapshot()
}

// observeLive is called wherever a Brain snapshot is broadcast.
func (s *Server) observeLive(snapshot brain.Snapshot) {
	hub := s.liveRuntime()
	hub.mu.Lock()
	hub.last, hub.hasLast = snapshot, true
	hub.mu.Unlock()
	hub.projector.Observe(s.liveInput(snapshot))
}

// observeLiveSessions re-projects after a Session changed (it started or
// stopped running, or began or stopped asking), on the newest snapshot.
func (s *Server) observeLiveSessions() {
	hub := s.liveRuntime()
	hub.mu.Lock()
	snapshot, ok := hub.last, hub.hasLast
	hub.mu.Unlock()
	if ok {
		hub.projector.Observe(s.liveInput(snapshot))
	}
}

// liveInput is what the live state is projected from: the Brain snapshot,
// every visible Session and Brain's own prompt, read the way the open
// request count reads them (computers_wire.go).
func (s *Server) liveInput(snapshot brain.Snapshot) live.Input {
	in := live.Input{Snapshot: snapshot}
	providers := map[string]string{}
	for _, ref := range snapshot.Workers {
		providers[ref.ID] = ref.Provider
	}
	hostID := ""
	if s.brain != nil {
		hostID = s.brain.CurrentHostSessionID()
		if hostID != "" {
			if host := s.lookupWorker(hostID); host != nil && host.State == classifier.StateBlocked && host.Prompt != nil {
				in.HostAsk = firstLiveText(host.Prompt.Summary, host.Prompt.Title, host.Prompt.Request, "Brain asks")
				in.HostAskSince = host.UpdatedAt
			}
		}
	}
	for _, worker := range s.currentVisibleWorkerSessions() {
		if worker == nil || worker.ID == hostID {
			continue
		}
		session := live.Session{
			ID: worker.ID, Title: liveSessionTitle(worker), Provider: providers[worker.ID],
			Line: liveReportedLine(worker), Delegated: worker.Delegated,
			Running: worker.State == classifier.StateRunning, StartedAt: worker.StartedAt,
		}
		if worker.RunningSince != nil {
			session.StartedAt = *worker.RunningSince
		}
		approvals := s.pendingApprovals(worker.ID)
		if asksOwner(worker, len(approvals)) {
			session.AskSince = worker.UpdatedAt
			session.Requests = len(approvals)
			switch {
			case len(approvals) > 0:
				session.Asks = live.NeedsApprove
				session.Ask = firstLiveText(approvals[0].Summary, approvals[0].Tool, "Wants your approval")
			case worker.Prompt != nil:
				session.Asks = live.NeedsAnswer
				if worker.Prompt.Kind == classifier.PromptPermission || worker.Prompt.Kind == classifier.PromptApproval {
					session.Asks = live.NeedsApprove
				}
				session.Ask = firstLiveText(worker.Prompt.Summary, worker.Prompt.Title, worker.Prompt.Request)
			default:
				session.Asks = live.NeedsAnswer
				session.Ask = firstLiveText(worker.Summary, "Waiting for you")
			}
		}
		in.Sessions = append(in.Sessions, session)
	}
	return in
}

// liveSessionTitle names a Session as the app's rows do (client core/sessions/listRow.ts): its own name
// without a trailing "(…)", or, when that name is generated (empty, its id, an mh- id) or only the
// agent's or shell's name, the folder it works in.
func liveSessionTitle(worker *classifier.Worker) string {
	name := strings.TrimSpace(liveTrailingParen.ReplaceAllString(strings.TrimSpace(worker.Name), ""))
	lower := strings.ToLower(name)
	generated := name == "" || name == worker.ID || liveGeneratedName.MatchString(name) || liveGenericNames[lower]
	if !generated {
		return name
	}
	if folder := filepath.Base(strings.TrimSpace(worker.Cwd)); folder != "" && folder != "." && folder != "/" {
		return folder
	}
	return firstLiveText(name, "Session")
}

var (
	liveTrailingParen = regexp.MustCompile(`\s+\([^)]+\)\s*$`)
	liveGeneratedName = regexp.MustCompile(`^mh-[0-9a-f]{24}$`)
	liveGenericNames  = map[string]bool{
		"claude": true, "claude code": true, "claude-code": true, "codex": true, "openai codex": true,
		"cursor": true, "cursor agent": true, "agent": true, "grok": true, "grok cli": true, "pi": true,
		"opencode": true, "open code": true, "zsh": true, "bash": true, "sh": true, "fish": true,
		"shell": true, "terminal": true,
	}
)

// liveReportedLine is what a Session said it does, only when it reported it (a phase or an event):
// the terminal's own text is never shown (client core/sessions/activity.ts).
func liveReportedLine(worker *classifier.Worker) string {
	if strings.TrimSpace(worker.Phase) == "" && strings.TrimSpace(worker.EventKind) == "" {
		return ""
	}
	return strings.TrimSpace(worker.Summary)
}

func firstLiveText(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// onLiveChange reports one new state to connected phones and, for a phone that
// asked for it, to its push channel. Going idle after idle is not news.
func (s *Server) onLiveChange(state live.State) {
	s.broadcastJSON(map[string]any{"type": "live_state", "live": state})

	hub := &s.live
	hub.mu.Lock()
	previous := hub.prevKind
	hub.prevKind = state.Kind
	hub.mu.Unlock()
	if state.Kind == live.KindIdle && (previous == "" || previous == live.KindIdle) {
		return
	}
	pusher, ok := s.pusher.(livePusher)
	if !ok || !pusher.LiveCapable() {
		return
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return
	}
	data := map[string]any{
		"payload": string(payload),
		"epoch":   int64String(state.Epoch),
		"seq":     int64String(int64(state.Seq)),
	}
	go func() {
		if err := pusher.SendLive(data); err != nil {
			if errors.Is(err, push.ErrNotLiveCapable) {
				return
			}
			logPushFailure("live-state", "seq="+int64String(int64(state.Seq)), err)
		}
	}()
}

func int64String(value int64) string {
	data, _ := json.Marshal(value)
	return string(data)
}

type liveClientMessage struct {
	RequestID   string `json:"request_id"`
	QuestionID  string `json:"question_id"`
	ChoiceIndex *int   `json:"choice_index"`
	LiveState   bool   `json:"live_state"`
	PushToken   string `json:"push_token"`
}

// handleLiveMessage serves the live state messages. It returns true when it
// consumed the message; register_push is only observed (for its live_state
// capability) and still reaches its own handler.
func (s *Server) handleLiveMessage(conn *websocket.Conn, msg []byte, raw clientMessage) bool {
	switch raw.Type {
	case "register_push", "live_state_get", "widget_token_issue", "widget_token_revoke", "live_answer":
	default:
		return false
	}
	var message liveClientMessage
	if err := json.Unmarshal(msg, &message); err != nil {
		return false
	}
	hub := s.liveRuntime()
	switch raw.Type {
	case "register_push":
		if message.PushToken != "" {
			if pusher, ok := s.pusher.(livePusher); ok {
				pusher.SetLiveCapable(s.deviceIDOf(conn), message.LiveState)
			}
		}
		return false
	case "live_state_get":
		snapshot, err := s.liveSnapshot()
		if err != nil {
			s.sendErrorWithRequestID(conn, message.RequestID, "live_state_failed", err.Error())
			return true
		}
		s.sendJSON(conn, map[string]any{
			"type": "live_state", "request_id": message.RequestID,
			"live": hub.projector.ObserveNow(s.liveInput(snapshot)),
		})
	case "widget_token_issue":
		s.handleWidgetTokenIssue(conn, message)
	case "widget_token_revoke":
		deviceID := s.deviceIDForConn(conn)
		revoked := 0
		if hub.tokens != nil && deviceID != "" {
			revoked = hub.tokens.RevokeDevice(deviceID)
		}
		s.sendJSON(conn, map[string]any{"type": "widget_token_revoked", "request_id": message.RequestID, "revoked": revoked})
	case "live_answer":
		s.handleLiveAnswer(conn, message)
	}
	return true
}

func (s *Server) deviceIDForConn(conn *websocket.Conn) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner := s.clients[conn]; owner != nil {
		return owner.deviceID
	}
	return ""
}

func (s *Server) handleWidgetTokenIssue(conn *websocket.Conn, message liveClientMessage) {
	hub := s.liveRuntime()
	deviceID := s.deviceIDForConn(conn)
	if hub.tokens == nil || deviceID == "" {
		s.sendErrorWithRequestID(conn, message.RequestID, "widget_token_unavailable", "Widget tokens are not available on this server.")
		return
	}
	token, entry, err := hub.tokens.Issue(deviceID)
	if err != nil {
		s.sendErrorWithRequestID(conn, message.RequestID, "widget_token_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type": "widget_token", "request_id": message.RequestID,
		"token": token, "id": entry.ID, "url_path": widgetSnapshotPath,
	})
}

// handleLiveAnswer answers the question on the phone's notification. The
// question id must still be the CURRENT one; a stale tap changes nothing.
// A Work's question is answered through the Work (the existing reply path).
func (s *Server) handleLiveAnswer(conn *websocket.Conn, message liveClientMessage) {
	hub := s.liveRuntime()
	requestID := strings.TrimSpace(message.RequestID)
	if requestID == "" || message.QuestionID == "" || message.ChoiceIndex == nil {
		s.sendErrorWithRequestID(conn, requestID, "invalid_answer", "An answer needs a request, a question and a choice.")
		return
	}
	hub.answerMu.Lock()
	defer hub.answerMu.Unlock()
	if previous, ok := hub.answers[requestID]; ok {
		reply := map[string]any{}
		for key, value := range previous {
			reply[key] = value
		}
		reply["duplicate"] = true
		s.sendJSON(conn, reply)
		return
	}
	snapshot, err := s.liveSnapshot()
	if err != nil {
		s.sendErrorWithRequestID(conn, requestID, "live_answer_failed", err.Error())
		return
	}
	state := live.Project(snapshot, time.Now())
	question := state.Question
	if question == nil || question.ID != message.QuestionID {
		s.sendErrorWithRequestID(conn, requestID, "answer_stale", "That question is no longer open, so nothing was sent.")
		return
	}
	choice, ok := question.FullChoice(*message.ChoiceIndex)
	if !ok {
		s.sendErrorWithRequestID(conn, requestID, "invalid_choice", "That choice is not on the question.")
		return
	}
	reply := map[string]any{
		"type": "live_answer", "request_id": requestID, "ok": true,
		"kind": question.Kind, "question_id": question.ID, "text": choice,
	}
	asked, _ := s.workQuestionBefore(question.WorkID)
	result, err := s.liveReply(brain.WorkUserActionRequest{
		WorkID: question.WorkID, Kind: brain.WorkUserReply, Text: choice, RequestID: requestID,
	})
	if err != nil && !errors.Is(err, brain.ErrWorkCleanupPending) {
		s.sendErrorWithRequestID(conn, requestID, "live_answer_failed", brainWorkActionMessage(err))
		return
	}
	s.recordWorkReply(asked, result, s.answeredByConn(conn))
	reply["work_id"] = question.WorkID
	reply["status"] = result.Work.Status
	reply["admission"] = result.Admission
	hub.remember(requestID, reply)
	s.sendJSON(conn, reply)
	s.broadcastBrainSnapshot()
}

func (s *Server) liveReply(request brain.WorkUserActionRequest) (brain.WorkUserActionResult, error) {
	if s.live.replyOverride != nil {
		return s.live.replyOverride(request)
	}
	if s.brain == nil {
		return brain.WorkUserActionResult{}, errors.New("Brain is not configured")
	}
	return s.brain.ActOnWork(request)
}

// remember keeps the last few answers by request id so a re-delivered tap is
// the same answer, not a second one.
func (h *liveHub) remember(requestID string, reply map[string]any) {
	h.answers[requestID] = reply
	h.order = append(h.order, requestID)
	for len(h.order) > maxLiveAnswersKept {
		delete(h.answers, h.order[0])
		h.order = h.order[1:]
	}
}

// handleWidgetSnapshot is the one read-only route a home-screen widget may
// call. Only a widget snapshot token opens it; pairing and device credentials
// are never accepted here, and the token opens nothing else.
func (s *Server) handleWidgetSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	hub := s.liveRuntime()
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || hub.tokens == nil {
		unauthorizedWidget(w)
		return
	}
	entry, ok := hub.tokens.Verify(strings.TrimSpace(token))
	if !ok || s.auth == nil || !s.auth.IsDeviceTrusted(entry.DeviceID) {
		unauthorizedWidget(w)
		return
	}
	state := hub.projector.Current()
	if state.Seq == 0 {
		if snapshot, err := s.liveSnapshot(); err == nil {
			state = hub.projector.ObserveNow(s.liveInput(snapshot))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"live": state, "generated_at": time.Now().UnixMilli(),
	}); err != nil {
		debuglog.Printf("widget snapshot: write failed: %v", err)
	}
}

func unauthorizedWidget(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
