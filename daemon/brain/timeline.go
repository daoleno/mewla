package brain

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/sqlitedb"
	"github.com/daoleno/mewla/daemon/work"
)

const (
	defaultThreadTimelineLimit = 240

	timelineKindUserMessage      = "user_message"
	timelineKindAssistantMessage = "assistant_message"
	timelineKindUpdate           = work.ConversationKindUpdate
	timelineKindWorkCard         = "work_card"
	timelineKindCalendarResult   = "calendar_result"

	workResultConversationSource = "work_result"
)

// AdmissionTimelineItemID returns the durable timeline identity for a successful
// Brain input receipt. The accepted request_id is the canonical user-row id.
func AdmissionTimelineItemID(receipt string) string {
	return strings.TrimSpace(receipt)
}

// AdmissionDigest returns the stable SHA-256 hex digest of exact admitted UTF-8.
func AdmissionDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum)
}

// AdmitUserMessage durably records the exact user payload for a successful
// Brain input admission/receipt. Idempotent by request_id.
func (s *Store) AdmitUserMessage(threadID, sessionID, receipt, body string) (TimelineItem, error) {
	threadID = strings.TrimSpace(threadID)
	sessionID = strings.TrimSpace(sessionID)
	receipt = strings.TrimSpace(receipt)
	body = strings.TrimSpace(body)
	id := AdmissionTimelineItemID(receipt)
	if threadID == "" || sessionID == "" || id == "" || body == "" {
		return TimelineItem{}, fmt.Errorf("admit user message requires thread, session, receipt, and body")
	}
	return s.AppendTimelineItem(TimelineItem{
		ID:              id,
		ThreadID:        threadID,
		SessionID:       sessionID,
		Role:            "user",
		Body:            body,
		Kind:            timelineKindUserMessage,
		BrainAdmission:  true,
		AdmissionSHA256: AdmissionDigest(body),
	})
}

func timelineItemMatchesBrainInputAdmission(item TimelineItem, admission BrainInputAdmission) bool {
	return strings.TrimSpace(item.ID) == AdmissionTimelineItemID(admission.RequestID) &&
		strings.TrimSpace(item.ThreadID) == admission.ThreadID &&
		strings.TrimSpace(item.SessionID) == admission.SessionID &&
		strings.TrimSpace(item.Role) == "user" && strings.TrimSpace(item.Body) == admission.DisplayBody &&
		strings.TrimSpace(item.Kind) == timelineKindUserMessage && item.BrainAdmission &&
		strings.TrimSpace(item.AdmissionSHA256) == admission.BodySHA256
}

func brainInputAdmissionTimelineItem(admission BrainInputAdmission) TimelineItem {
	return TimelineItem{
		ID:              AdmissionTimelineItemID(admission.RequestID),
		ThreadID:        admission.ThreadID,
		SessionID:       admission.SessionID,
		Role:            "user",
		Body:            admission.DisplayBody,
		CreatedAt:       admission.AcceptedAt.UTC(),
		Kind:            timelineKindUserMessage,
		BrainAdmission:  true,
		AdmissionSHA256: admission.BodySHA256,
	}
}

// errBrainInputAdmissionReplay marks an admission whose receipt was already
// admitted, with the identical input, on another thread or Session. The input
// owns exactly one presentation row; the replay has none of its own.
var errBrainInputAdmissionReplay = errors.New("Brain input admission replays an input projected elsewhere")

// admissionIdentityConflict is a timeline identity held by a different input
// than the admission claiming it. Retrying never changes it, so callers report
// it once instead of retrying.
type admissionIdentityConflict struct{ identity string }

func (e *admissionIdentityConflict) Error() string {
	return fmt.Sprintf("timeline identity %q belongs to different Brain input admission", e.identity)
}

// timelineItemReplaysBrainInputAdmission reports whether the row holding the
// admission's identity carries the very same input (exact body and digest) but
// was projected for another thread or Session: the same receipt delivered again
// after the conversation changed.
func timelineItemReplaysBrainInputAdmission(item TimelineItem, admission BrainInputAdmission) bool {
	return strings.TrimSpace(item.ID) == AdmissionTimelineItemID(admission.RequestID) &&
		strings.TrimSpace(item.Role) == "user" && strings.TrimSpace(item.Body) == admission.DisplayBody &&
		strings.TrimSpace(item.Kind) == timelineKindUserMessage && item.BrainAdmission &&
		strings.TrimSpace(item.AdmissionSHA256) == admission.BodySHA256
}

// brainInputAdmissionCanonicalIndex finds the admission's own timeline row.
// A row holding its identity for the same input on another thread yields
// errBrainInputAdmissionReplay; one holding it for any other input is an
// *admissionIdentityConflict.
func brainInputAdmissionCanonicalIndex(allItems []TimelineItem, admission BrainInputAdmission) (int, error) {
	canonicalID := AdmissionTimelineItemID(admission.RequestID)
	for index, existing := range allItems {
		if strings.TrimSpace(existing.ID) != canonicalID {
			continue
		}
		if !timelineItemMatchesBrainInputAdmission(existing, admission) {
			if timelineItemReplaysBrainInputAdmission(existing, admission) {
				return -1, errBrainInputAdmissionReplay
			}
			return -1, &admissionIdentityConflict{identity: existing.ID}
		}
		return index, nil
	}
	return -1, nil
}

// ProjectBrainInputAdmission idempotently materializes the presentation row
// from accepted lifecycle authority, then lets reconcileAdmissionEchoes credit
// a provider echo that became durable first. Failure never rolls back
// acceptance or Attention; the existing bounded startup pass retries this
// projection.
func (s *Store) ProjectBrainInputAdmission(admission BrainInputAdmission) error {
	persisted, found, err := s.BrainInputAdmission(admission.RequestID, admission.ThreadID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Brain input admission is not durable")
	}
	if !samePersistedBrainInputAdmission(persisted, admission) {
		return fmt.Errorf("Brain input admission projection identity differs from durable authority")
	}
	admission = persisted
	if admission.State != BrainInputAdmissionAccepted || admission.AcceptedAt == nil {
		return fmt.Errorf("only accepted Brain input admission can be projected")
	}
	if err := validateBrainInputAdmissions([]BrainInputAdmission{admission}); err != nil {
		return err
	}
	if s.projectBrainInputAdmission != nil {
		return s.projectBrainInputAdmission(admission)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	allItems, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	canonicalIndex, err := brainInputAdmissionCanonicalIndex(allItems, admission)
	if errors.Is(err, errBrainInputAdmissionReplay) {
		return nil
	}
	if err != nil {
		return err
	}
	if canonicalIndex >= 0 && strings.TrimSpace(allItems[canonicalIndex].AdmissionEchoEventID) != "" {
		return nil
	}
	database, err := s.loadPresentationLocked()
	if err != nil {
		return err
	}
	item := brainInputAdmissionTimelineItem(admission)
	if canonicalIndex < 0 {
		allItems = append(allItems, item)
	}
	_, claims, next := reconcileThreadAdmissionEchoes(allItems, database.BrainInputAdmissions, admission.ThreadID, "", nil)
	if len(claims) > 0 {
		return s.rewriteTimelineLocked(next)
	}
	if canonicalIndex < 0 {
		_, err := s.appendTimelineItemLocked(item)
		return err
	}
	return nil
}

// Values of TimelineItem.Origin. memorysearch reads the same strings from
// messages.jsonl.
const (
	TimelineOriginTranscript = "transcript"
	TimelineOriginProvider   = "provider"
)

// TimelineItem is one durable Brain-thread timeline row. The append-only
// timeline table is the sole presentation truth for Brain chat history
// and Work cards. Work Events remain the scheduler audit log and are never
// reprojected into the UI on every snapshot.
//
// Cards are immutable messages: every newly materialized presentable event
// remains chronologically visible. There is no current-per-Work collapse.
type TimelineItem struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id,omitempty"`
	SessionID  string    `json:"session_id"`
	ExecutorID string    `json:"executor_id,omitempty"`
	Role       string    `json:"role"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	Kind       string    `json:"kind,omitempty"`
	Status     string    `json:"status,omitempty"`
	Title      string    `json:"title,omitempty"`

	// Origin says where a provider-materialized row came from:
	// TimelineOriginTranscript for what the person typed into the host
	// provider's own UI, TimelineOriginProvider for a record the provider wrote
	// itself (Claude's compaction summary). Rows Brain admitted or built
	// itself leave it empty. Readers use it instead of guessing from the text.
	Origin string `json:"origin,omitempty"`

	// BrainAdmission marks rows written by AdmitUserMessage (Interface
	// send_input). Provider materialization never sets this. Digests alone are
	// not provenance.
	BrainAdmission bool `json:"brain_admission,omitempty"`

	// AdmissionSHA256 is correlation data for BrainAdmission rows only: the
	// exact UTF-8 digest used to match one provider echo. It is never message
	// identity and must not appear on provider-native durable rows.
	AdmissionSHA256 string `json:"admission_sha256,omitempty"`

	// ProviderInputSHA256 is correlation data for provider-native user rows
	// only: the provider's exact native input digests (raw bytes, and the
	// payload inside a recognized transport envelope). Display cleaning changes
	// Body, so an echo that becomes durable before its admission is projected
	// can be proven only by these. Never provenance, never on the wire.
	ProviderInputSHA256 []string `json:"provider_input_sha256,omitempty"`

	// AdmissionEchoEventID records the provider event id that consumed this
	// admission's single echo credit. Empty means unmatched.
	AdmissionEchoEventID string `json:"admission_echo_event_id,omitempty"`
	// Native Session proven when consuming an echo after late Host binding.
	// It does not rewrite the immutable admission or its original SessionID.
	AdmissionEchoSessionID string `json:"admission_echo_session_id,omitempty"`

	// Legacy calendar rows retained for orphan-message migration compatibility.
	CalendarItemID string     `json:"calendar_item_id,omitempty"`
	CalendarRunID  string     `json:"calendar_run_id,omitempty"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`

	// Work-card presentation fields. Identity is ID == Work Event ID.
	WorkID          string `json:"work_id,omitempty"`
	EventKind       string `json:"event_kind,omitempty"`
	Summary         string `json:"summary,omitempty"`
	SessionName     string `json:"session_name,omitempty"`
	Unread          bool   `json:"unread,omitempty"`
	Phase           string `json:"phase,omitempty"`
	Attention       string `json:"attention,omitempty"`
	WorkerEventKind string `json:"worker_event_kind,omitempty"`
	DetailsJSON     string `json:"details_json,omitempty"`
	NextAction      string `json:"next_action,omitempty"`
	WaitFor         string `json:"wait_for,omitempty"`
}

// timelineReadCache is the whole timeline in seq order. It is filled on first
// read and then kept current by every append and rewrite, so a commit never
// costs a re-read.
type timelineReadCache struct {
	valid bool
	items []TimelineItem
	// threads holds what a reader of one thread derives from items. Replacing
	// or extending the timeline drops it with the rows it was built from.
	threads map[string]*timelineThreadView
}

// timelineThreadView is one thread's rows as the readers of a thread need
// them. It is built from the cached timeline when first asked for and shared
// by every reader until the timeline changes; it is never modified after it is
// built except for filling in a window, and its readers never modify it.
type timelineThreadView struct {
	// rows are the thread's rows in storage order.
	rows []TimelineItem
	// ids are the ids of rows.
	ids map[string]bool
	// claimed are the provider inputs that Brain admission rows already name as
	// their echo; openAdmissions counts the admission rows still waiting for one.
	claimed        map[string]bool
	openAdmissions int
	// windows are the newest rows in chronological order by limit.
	windows map[int][]TimelineItem
}

// window returns the newest limit rows in chronological order, shared with the
// view: the caller must not change them.
func (v *timelineThreadView) window(limit int) []TimelineItem {
	if window, ok := v.windows[limit]; ok {
		return window
	}
	out := append([]TimelineItem{}, v.rows...)
	sortTimelineItems(out)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	if v.windows == nil {
		v.windows = map[int][]TimelineItem{}
	}
	v.windows[limit] = out
	return out
}

// timelineThreadViewLocked is the view of one thread of the cached timeline.
func (s *Store) timelineThreadViewLocked(threadID string) (*timelineThreadView, error) {
	if err := s.loadTimelineCacheLocked(); err != nil {
		return nil, err
	}
	if view := s.timelineCache.threads[threadID]; view != nil {
		return view, nil
	}
	view := &timelineThreadView{ids: map[string]bool{}, claimed: map[string]bool{}}
	for _, item := range s.timelineCache.items {
		if strings.TrimSpace(item.ThreadID) != threadID {
			continue
		}
		item.ID = strings.TrimSpace(item.ID)
		item.ThreadID = threadID
		view.rows = append(view.rows, item)
		view.ids[item.ID] = true
		if IsBrainInputAdmission(item) {
			if echoID := strings.TrimSpace(item.AdmissionEchoEventID); echoID != "" {
				view.claimed[echoID] = true
			} else {
				view.openAdmissions++
			}
		}
	}
	if s.timelineCache.threads == nil {
		s.timelineCache.threads = map[string]*timelineThreadView{}
	}
	s.timelineCache.threads[threadID] = view
	return view, nil
}

func (s *Store) AppendTimelineItem(item TimelineItem) (TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendTimelineItemLocked(item)
}

func (s *Store) appendTimelineItemLocked(item TimelineItem) (TimelineItem, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = fmt.Sprintf("timeline_%d", time.Now().UTC().UnixNano())
	}
	item.ThreadID = strings.TrimSpace(item.ThreadID)
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.ExecutorID = strings.TrimSpace(item.ExecutorID)
	item.Role = strings.TrimSpace(item.Role)
	item.Body = strings.TrimSpace(item.Body)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Status = strings.TrimSpace(item.Status)
	item.Title = strings.TrimSpace(item.Title)
	item.AdmissionSHA256 = strings.TrimSpace(item.AdmissionSHA256)
	item.ProviderInputSHA256 = providerInputDigests(item.ProviderInputSHA256...)
	item.AdmissionEchoEventID = strings.TrimSpace(item.AdmissionEchoEventID)
	item.WorkID = strings.TrimSpace(item.WorkID)
	item.EventKind = strings.TrimSpace(item.EventKind)
	item.Summary = strings.TrimSpace(item.Summary)
	item.SessionName = strings.TrimSpace(item.SessionName)
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	} else {
		item.CreatedAt = item.CreatedAt.UTC()
	}
	if item.ThreadID == "" {
		return TimelineItem{}, fmt.Errorf("timeline thread id required")
	}
	if item.SessionID == "" {
		return TimelineItem{}, fmt.Errorf("timeline session id required")
	}
	if item.Role == "" {
		return TimelineItem{}, fmt.Errorf("timeline role required")
	}
	if item.Kind == timelineKindWorkCard {
		if item.Body == "" {
			item.Body = firstNonEmpty(item.Summary, item.Title, item.EventKind, "Work update")
		}
		if item.EventKind == "" {
			return TimelineItem{}, fmt.Errorf("work card event kind required")
		}
		if item.WorkID == "" {
			return TimelineItem{}, fmt.Errorf("work card work id required")
		}
	} else if item.Body == "" && item.Kind != timelineKindCalendarResult {
		return TimelineItem{}, fmt.Errorf("timeline body required")
	}
	if existing, ok, err := s.timelineItemByIDLocked(item.ID); err != nil {
		return TimelineItem{}, err
	} else if ok {
		return existing, nil
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return TimelineItem{}, err
	}
	if _, err := s.db.Exec(
		`INSERT INTO timeline(id, thread_id, body) VALUES(?, ?, ?)`, item.ID, item.ThreadID, string(raw),
	); err != nil {
		return TimelineItem{}, err
	}
	if s.timelineCache.valid {
		// The cache holds what a read of the stored row returns.
		var stored TimelineItem
		if err := json.Unmarshal(raw, &stored); err != nil {
			s.timelineCache = timelineReadCache{}
		} else {
			s.timelineCache.items = append(s.timelineCache.items, stored)
			s.timelineCache.threads = nil
		}
	}
	s.rev.Add(1)
	return item, nil
}

func (s *Store) timelineItemByIDLocked(id string) (TimelineItem, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TimelineItem{}, false, nil
	}
	var body string
	err := s.db.QueryRow(`SELECT body FROM timeline WHERE id = ?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return TimelineItem{}, false, nil
	}
	if err != nil {
		return TimelineItem{}, false, err
	}
	var item TimelineItem
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		return TimelineItem{}, false, fmt.Errorf("decode timeline item %q: %w", id, err)
	}
	return item, true, nil
}

func (s *Store) ThreadTimeline(threadID string, limit int) ([]TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadTimelineLocked(threadID, limit)
}

func (s *Store) threadTimelineLocked(threadID string, limit int) ([]TimelineItem, error) {
	window, err := s.threadTimelineViewLocked(threadID, limit)
	if err != nil {
		return nil, err
	}
	return cloneTimelineItems(window), nil
}

// ThreadTimelineView is ThreadTimeline for a reader that only looks: the rows
// are shared with every other reader of the thread and must not be changed,
// and it costs nothing while the timeline has not.
func (s *Store) ThreadTimelineView(threadID string, limit int) ([]TimelineItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadTimelineViewLocked(threadID, limit)
}

func (s *Store) threadTimelineViewLocked(threadID string, limit int) ([]TimelineItem, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []TimelineItem{}, nil
	}
	if limit <= 0 {
		limit = defaultThreadTimelineLimit
	}
	view, err := s.timelineThreadViewLocked(threadID)
	if err != nil {
		return nil, err
	}
	return view.window(limit), nil
}

func sortTimelineItems(items []TimelineItem) {
	sort.SliceStable(items, func(left, right int) bool {
		if !items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].CreatedAt.Before(items[right].CreatedAt)
		}
		return items[left].ID < items[right].ID
	})
}

// MaterializeProviderConversation appends durable provider transcript events
// into the Brain thread timeline exactly once by event ID. Partial/transient
// events stay live-only until they finalize. Provider user echoes reconcile
// one-to-one against Brain input admissions and never create rows.
func (s *Store) MaterializeProviderConversation(threadID string, conversation work.CodexConversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.materializeProviderConversationLocked(threadID, conversation)
}

// materializeProviderConversationLocked is the timeline-locked form. Callers
// must hold s.mu.
func (s *Store) materializeProviderConversationLocked(threadID string, conversation work.CodexConversation) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	conversation.Events = work.SuppressPrivateHostTurns(conversation.Events)
	sessionID := strings.TrimSpace(conversation.SessionID)
	if sessionID == "" {
		sessionID = "provider"
	}
	view, err := s.timelineThreadViewLocked(threadID)
	if err != nil {
		return err
	}
	// Credit echoes before appending so restart/replay stays idempotent. With no
	// admission row waiting for its echo there is nothing to credit, and the
	// rows already named as echoes are the claimed ones; that is every call but
	// the few that follow a Brain input, and it reads nothing but the view.
	claimed := view.claimed
	if view.openAdmissions > 0 {
		var claims map[string]string
		claimed, claims, _ = reconcileAdmissionEchoes(
			view.rows,
			s.readPresentationLocked().BrainInputAdmissions,
			threadID,
			sessionID,
			conversation.Events,
		)
		if len(claims) > 0 {
			// Crediting rewrites the thread's rows, which is the one thing here
			// that changes rows already stored: it works on its own copy.
			allItems, err := s.readAllTimelineItemsLocked()
			if err != nil {
				return err
			}
			admissions := s.readPresentationLocked().BrainInputAdmissions
			claimed, claims, allItems = reconcileThreadAdmissionEchoes(allItems, admissions, threadID, sessionID, conversation.Events)
			if len(claims) > 0 {
				if err := s.rewriteTimelineLocked(allItems); err != nil {
					return err
				}
				if view, err = s.timelineThreadViewLocked(threadID); err != nil {
					return err
				}
			}
		}
	}
	var added map[string]bool
	for _, event := range conversation.Events {
		if !providerEventMaterializable(event) {
			continue
		}
		id := strings.TrimSpace(event.ID)
		if id == "" || view.ids[id] || added[id] {
			continue
		}
		if strings.TrimSpace(event.Kind) == timelineKindUserMessage && claimed[id] {
			continue
		}
		item, ok := timelineItemFromProviderEvent(threadID, sessionID, event)
		if !ok {
			continue
		}
		if _, err := s.appendTimelineItemLocked(item); err != nil {
			return err
		}
		if added == nil {
			added = map[string]bool{}
		}
		added[id] = true
	}
	return nil
}

// materializeProviderConversationBound appends provider events into the exact
// thread only while the thread and Host transcript binding still match the
// caller's read snapshot. false means NewChat or a Host replacement raced the
// read: the caller must re-read, retry, and must not advance any checkpoint.
// Provider inference is not store authority, so the provider name is not
// compared here; the transcript identity fields are.
func (s *Store) materializeProviderConversationBound(binding hostTranscriptBinding, conversation work.CodexConversation) (bool, error) {
	threadID := strings.TrimSpace(binding.ThreadID)
	if s == nil || threadID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readChatStateLocked("")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(state.ThreadID) != threadID {
		return false, nil
	}
	host, err := s.readHostSessionLocked()
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(host.ProviderSessionID) != strings.TrimSpace(binding.ProviderSessionID) ||
		strings.TrimSpace(host.TranscriptPath) != strings.TrimSpace(binding.TranscriptPath) ||
		strings.TrimSpace(host.ProviderDataRoot) != strings.TrimSpace(binding.ProviderDataRoot) {
		return false, nil
	}
	return true, s.materializeProviderConversationLocked(threadID, conversation)
}

func providerEventMaterializable(event work.CodexConversationEvent) bool {
	if event.Partial || event.Transient {
		return false
	}
	switch strings.TrimSpace(event.Kind) {
	case timelineKindUserMessage:
		if work.IsDirectWorkEventPresentationInput(event.Body) {
			return false
		}
		_, hasExactTimestamp := exactProviderEventTimestamp(event)
		return strings.TrimSpace(event.Body) != "" && hasExactTimestamp
	case timelineKindAssistantMessage, timelineKindUpdate:
		return strings.TrimSpace(event.Body) != ""
	default:
		return false
	}
}

func timelineItemFromProviderEvent(threadID, sessionID string, event work.CodexConversationEvent) (TimelineItem, bool) {
	id := strings.TrimSpace(event.ID)
	body := strings.TrimSpace(event.Body)
	if id == "" || body == "" {
		return TimelineItem{}, false
	}
	role := strings.TrimSpace(event.Role)
	kind := strings.TrimSpace(event.Kind)
	switch kind {
	case timelineKindUserMessage:
		if role == "" {
			role = "user"
		}
	case timelineKindAssistantMessage, timelineKindUpdate:
		if role == "" {
			role = "assistant"
		}
	default:
		return TimelineItem{}, false
	}
	createdAt := time.Now().UTC()
	if ts := strings.TrimSpace(event.Timestamp); ts != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			createdAt = parsed.UTC()
		} else if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			createdAt = parsed.UTC()
		}
	}
	origin := TimelineOriginTranscript
	if event.Origin == work.ConversationOriginProvider {
		origin = TimelineOriginProvider
	}
	item := TimelineItem{
		ID:        id,
		ThreadID:  threadID,
		SessionID: sessionID,
		Role:      role,
		Body:      body,
		CreatedAt: createdAt,
		Kind:      kind,
		Title:     strings.TrimSpace(event.Title),
		Status:    strings.TrimSpace(event.Status),
		Origin:    origin,
		// Provider-native rows never receive Brain admission provenance.
	}
	if kind == timelineKindUserMessage {
		// Keep the native input digests so a projection that races this row
		// can still prove the echo after display cleaning changed Body.
		item.ProviderInputSHA256 = providerEventInputDigests(event)
	}
	return item, true
}

func providerEventInputDigests(event work.CodexConversationEvent) []string {
	return providerInputDigests(event.AdmissionSHA256, event.AdmissionUnwrappedSHA256)
}

func providerInputDigests(digests ...string) []string {
	var out []string
	for _, digest := range digests {
		if digest = strings.TrimSpace(digest); digest != "" && !slices.Contains(out, digest) {
			out = append(out, digest)
		}
	}
	return out
}

func workCardTimelineItem(workItem Work, event WorkEvent, unread bool) TimelineItem {
	threadID := strings.TrimSpace(workItem.SourceThreadID)
	sessionID := strings.TrimSpace(workItem.AttemptSessionID)
	if payloadSessionID := strings.TrimPrefix(event.PayloadRef, "session:"); payloadSessionID != event.PayloadRef {
		sessionID = strings.TrimSpace(payloadSessionID)
	}
	if sessionID == "" {
		sessionID = "work"
	}
	summary := strings.TrimSpace(event.Summary)
	if summary == "" {
		// Work.NextAction is Brain's own note and is never shown to a person.
		summary = strings.TrimSpace(workItem.Objective)
	}
	attention := strings.TrimSpace(event.Attention)
	if failure := reviewDeliveryFailureSummary(workItem.Review); failure != "" {
		summary = failure + "\n" + summary
		attention = "failed" // Keep delivery diagnostics visible on minimal result cards too.
	}
	summary = compactWorkResultText(summary)
	title := strings.TrimSpace(workItem.Title)
	body := firstNonEmpty(summary, title, event.Kind)

	return TimelineItem{
		ID:              event.ID,
		ThreadID:        threadID,
		SessionID:       sessionID,
		Role:            "assistant",
		Body:            body,
		CreatedAt:       event.CreatedAt.UTC(),
		Kind:            timelineKindWorkCard,
		Status:          event.Kind,
		Title:           title,
		WorkID:          workItem.ID,
		EventKind:       event.Kind,
		Summary:         summary,
		SessionName:     strings.TrimSpace(event.SourceName),
		Phase:           strings.TrimSpace(event.Phase),
		Attention:       attention,
		WorkerEventKind: strings.TrimSpace(event.EventKind),
		DetailsJSON:     strings.TrimSpace(event.DetailsJSON),
		NextAction:      strings.TrimSpace(workItem.NextAction),
		WaitFor:         strings.TrimSpace(workItem.WaitFor),
		// Read state lives in the timeline projection. A freshly materialized
		// card is unread; MarkWorkRead clears it without touching the Event.
		Unread: unread,
	}
}

// MarkTimelineWorkCardsRead clears unread emphasis on materialized work cards
// for one Work. It is the only read-state owner; Work Events never carry
// read_at.
func (s *Store) MarkTimelineWorkCardsRead(workID string) error {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	changed := false
	for index := range items {
		item := &items[index]
		if item.Kind != timelineKindWorkCard || item.WorkID != workID || !item.Unread {
			continue
		}
		item.Unread = false
		changed = true
	}
	if !changed {
		return nil
	}
	return s.rewriteTimelineLocked(items)
}

func (s *Store) readAllTimelineItemsLocked() ([]TimelineItem, error) {
	if err := s.loadTimelineCacheLocked(); err != nil {
		return nil, err
	}
	return cloneTimelineItems(s.timelineCache.items), nil
}

// loadTimelineCacheLocked makes the timeline cache hold every stored row.
func (s *Store) loadTimelineCacheLocked() error {
	if s.timelineCache.valid {
		return nil
	}
	out := []TimelineItem{}
	if err := scanBodies(s.db, `SELECT body FROM timeline ORDER BY seq`, func(body []byte) error {
		var item TimelineItem
		if err := json.Unmarshal(body, &item); err != nil {
			return fmt.Errorf("decode timeline item: %w", err)
		}
		out = append(out, item)
		return nil
	}); err != nil {
		return err
	}
	s.timelineCache = timelineReadCache{valid: true, items: out}
	return nil
}

func cloneTimelineItems(items []TimelineItem) []TimelineItem {
	out := append(make([]TimelineItem, 0, len(items)), items...)
	for index := range out {
		if out[index].ScheduledFor != nil {
			scheduledFor := *out[index].ScheduledFor
			out[index].ScheduledFor = &scheduledFor
		}
	}
	return out
}

// rewriteTimelineLocked makes items the whole timeline, in that order, by
// changing only the rows that differ. Rows keep their place while they match
// the new order; the first row that does not is deleted and every item the
// walk has not matched is inserted after the last row, so reordering costs
// writes only for what actually moved. A projection that edits a row in place,
// drops rows or appends new ones therefore writes just those rows.
func (s *Store) rewriteTimelineLocked(items []TimelineItem) error {
	current, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return err
	}
	for index := range items {
		items[index].ID = strings.TrimSpace(items[index].ID)
		items[index].ThreadID = strings.TrimSpace(items[index].ThreadID)
	}
	// result is what a read of the rewritten timeline returns: rows that were
	// written are decoded back from the JSON that was stored.
	result := make([]TimelineItem, len(items))
	write := func(index int) (string, error) {
		body, err := json.Marshal(items[index])
		if err != nil {
			return "", err
		}
		var stored TimelineItem
		if err := json.Unmarshal(body, &stored); err != nil {
			return "", err
		}
		result[index] = stored
		return string(body), nil
	}
	err = sqlitedb.Tx(s.db, func(tx *sql.Tx) error {
		next := 0
		for _, row := range current {
			if next < len(items) && items[next].ID == strings.TrimSpace(row.ID) {
				if reflect.DeepEqual(row, items[next]) {
					result[next] = row
				} else {
					body, err := write(next)
					if err != nil {
						return err
					}
					if _, err := tx.Exec(`UPDATE timeline SET thread_id = ?, body = ? WHERE id = ?`,
						items[next].ThreadID, body, items[next].ID); err != nil {
						return err
					}
				}
				next++
				continue
			}
			if _, err := tx.Exec(`DELETE FROM timeline WHERE id = ?`, strings.TrimSpace(row.ID)); err != nil {
				return err
			}
		}
		for ; next < len(items); next++ {
			body, err := write(next)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO timeline(id, thread_id, body) VALUES(?, ?, ?)`,
				items[next].ID, items[next].ThreadID, body); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.timelineCache = timelineReadCache{}
		return err
	}
	s.timelineCache = timelineReadCache{valid: true, items: result}
	s.rev.Add(1)
	return nil
}

func timelineItemsToConversationEvents(items []TimelineItem) []work.CodexConversationEvent {
	out := make([]work.CodexConversationEvent, 0, len(items))
	for index, item := range items {
		if notifications, ok := durableTaskNotifications(item); ok {
			for offset, notification := range notifications {
				out = append(out, notification.ConversationEvent(
					work.TaskNotificationEventID(item.ID, offset),
					index+1,
					item.CreatedAt.Format(time.RFC3339Nano),
				))
			}
			continue
		}
		event, ok := timelineItemToConversationEvent(item, index+1)
		if !ok {
			continue
		}
		out = append(out, event)
	}
	return out
}

// durableTaskNotifications re-projects provider user rows materialized before
// task notifications had their own card. Brain admissions are always the
// user's own words and never become cards.
func durableTaskNotifications(item TimelineItem) ([]work.TaskNotification, bool) {
	if item.Kind != timelineKindUserMessage || IsBrainInputAdmission(item) {
		return nil, false
	}
	return work.ParseTaskNotifications(item.Body)
}

// TimelineItemsToConversationEvents projects durable timeline rows into the
// shared conversation wire contract. Every materialized presentable card stays
// chronologically visible.
func TimelineItemsToConversationEvents(items []TimelineItem) []work.CodexConversationEvent {
	return timelineItemsToConversationEvents(items)
}

func timelineItemToConversationEvent(item TimelineItem, seq int) (work.CodexConversationEvent, bool) {
	switch item.Kind {
	case timelineKindUserMessage:
		if work.IsDirectWorkEventPresentationInput(item.Body) {
			// Transport-only Session Input must never become a visible user row.
			// Presentable Work outcomes own the work_card / work_result path.
			return work.CodexConversationEvent{}, false
		}
		event := work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindUserMessage,
			Role:      "user",
			Body:      item.Body,
			Source:    "brain_timeline",
		}
		if IsBrainInputAdmission(item) {
			event.AdmissionSHA256 = admissionCorrelationDigest(item)
		} else if inner, ok := work.UnwrapClaudePasteDisplay(strings.TrimSpace(item.Body)); ok && strings.TrimSpace(inner) != "" {
			event.Body = strings.TrimSpace(inner)
		}
		// The durable row keeps the exact admitted provider input (it is what
		// the admission digest covers); the phone gets the files as a field.
		return work.ProjectUserAttachments(event), true
	case timelineKindAssistantMessage:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindAssistantMessage,
			Role:      "assistant",
			Body:      item.Body,
			Title:     item.Title,
			Source:    "brain_timeline",
		}, true
	case timelineKindUpdate:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      timelineKindUpdate,
			Role:      "assistant",
			Body:      item.Body,
			Source:    "brain_timeline",
		}, true
	case timelineKindWorkCard:
		return work.CodexConversationEvent{
			ID:              item.ID,
			Seq:             seq,
			Timestamp:       item.CreatedAt.Format(time.RFC3339Nano),
			Kind:            "status",
			Role:            "assistant",
			Title:           item.Title,
			Body:            compactWorkResultText(firstNonEmpty(item.Summary, item.Body)),
			Status:          firstNonEmpty(item.EventKind, item.Status),
			Source:          workResultConversationSource,
			WorkID:          item.WorkID,
			WorkSession:     item.SessionID,
			SessionName:     item.SessionName,
			Unread:          item.Unread,
			WorkPhase:       item.Phase,
			WorkAttention:   item.Attention,
			WorkEventKind:   item.WorkerEventKind,
			WorkDetailsJSON: item.DetailsJSON,
			WorkWaitFor:     item.WaitFor,
		}, true
	case timelineKindCalendarResult:
		return work.CodexConversationEvent{
			ID:        item.ID,
			Seq:       seq,
			Timestamp: item.CreatedAt.Format(time.RFC3339Nano),
			Kind:      "status",
			Role:      "assistant",
			Title:     item.Title,
			Body:      item.Body,
			Status:    item.Status,
			Source:    "calendar_result",
		}, true
	default:
		return work.CodexConversationEvent{}, false
	}
}
