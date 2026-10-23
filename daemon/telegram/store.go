package telegram

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const stateSchema = 4

const (
	maxCallbackRoutes = 64
	formattedVariant  = "formatted"
	plainVariant      = "plain"

	// Topic mapping lifecycle states. Routing authority is the Session ID;
	// these states mirror the Session's durable lifecycle so input can fail
	// closed exactly.
	topicStateActive    = "active"
	topicStateCompleted = "completed"
	topicStateStale     = "stale"

	// Topic operation kinds and states. Ops share the outbox's
	// pending -> dispatching -> sent/failed/ambiguous discipline because
	// createForumTopic has no caller-supplied idempotency key: only a definite
	// API rejection may retry, a transport-indeterminate outcome stays
	// ambiguous and is never retried automatically.
	topicOpCreate = "create"
	topicOpRename = "rename"
	topicOpClose  = "close"
	topicOpReopen = "reopen"
	topicOpDelete = "delete"
)

type updateRecord struct {
	Disposition     string    `json:"disposition"`
	HandledAt       time.Time `json:"handled_at"`
	SessionID       string    `json:"session_id,omitempty"`
	BrainThreadID   string    `json:"brain_thread_id,omitempty"`
	MessageThreadID int64     `json:"message_thread_id,omitempty"`
}

type outboxRecord struct {
	BrainThreadID   string                `json:"brain_thread_id,omitempty"`
	ID              string                `json:"id"`
	Kind            string                `json:"kind"`
	CanonicalID     string                `json:"canonical_id,omitempty"`
	WorkID          string                `json:"work_id,omitempty"`
	SessionID       string                `json:"session_id,omitempty"`
	TopicKey        string                `json:"topic_key,omitempty"`
	Text            string                `json:"text"`
	PlainText       string                `json:"plain_text,omitempty"`
	Entities        []MessageEntity       `json:"entities,omitempty"`
	Variant         string                `json:"variant,omitempty"`
	ReplyMessageID  int64                 `json:"reply_message_id,omitempty"`
	ReplyMarkup     *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
	MessageThreadID int64                 `json:"message_thread_id,omitempty"`
	MessageID       int64                 `json:"message_id,omitempty"`
	State           string                `json:"state"`
	AttemptAt       time.Time             `json:"attempt_at,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

type topicMapping struct {
	SessionID       string    `json:"session_id"`
	ThreadID        string    `json:"thread_id,omitempty"`
	WorkID          string    `json:"work_id,omitempty"`
	ChatID          int64     `json:"chat_id"`
	MessageThreadID int64     `json:"message_thread_id"`
	Label           string    `json:"label,omitempty"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type topicOpRecord struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	SessionID       string    `json:"session_id,omitempty"`
	MessageThreadID int64     `json:"message_thread_id,omitempty"`
	ChatID          int64     `json:"chat_id,omitempty"`
	BotID           int64     `json:"bot_id,omitempty"`
	Label           string    `json:"label,omitempty"`
	ThreadID        string    `json:"thread_id,omitempty"`
	WorkID          string    `json:"work_id,omitempty"`
	State           string    `json:"state"`
	AttemptAt       time.Time `json:"attempt_at,omitempty"`
	Attempts        int       `json:"attempts,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type durableState struct {
	MediaInputs         map[string]mediaInput    `json:"media_inputs,omitempty"`
	Feedback            map[int64]feedbackRecord `json:"feedback,omitempty"`
	MessageSources      map[int64]messageSource  `json:"message_sources,omitempty"`
	BrainEntryState     string                   `json:"brain_entry_state,omitempty"`
	BrainEntryMessageID int64                    `json:"brain_entry_message_id,omitempty"`
	Schema              int                      `json:"schema"`
	Enabled             bool                     `json:"enabled"`
	BotID               int64                    `json:"bot_id,omitempty"`
	BotName             string                   `json:"bot_name,omitempty"`
	BotUsername         string                   `json:"bot_username,omitempty"`
	TopicsAvailable     bool                     `json:"topics_available,omitempty"`
	UsersCreateTopics   bool                     `json:"users_create_topics,omitempty"`
	BrainTopicID        int64                    `json:"brain_topic_id,omitempty"`
	BrainReplyTopicID   int64                    `json:"brain_reply_topic_id,omitempty"`
	BrainTopics         []int64                  `json:"brain_topics,omitempty"`
	OwnerID             int64                    `json:"owner_id,omitempty"`
	OwnerHint           string                   `json:"owner_hint,omitempty"`
	ChatID              int64                    `json:"chat_id,omitempty"`
	ChallengeSHA256     string                   `json:"challenge_sha256,omitempty"`
	ChallengeExpiresAt  time.Time                `json:"challenge_expires_at,omitempty"`
	NextOffset          int64                    `json:"next_offset,omitempty"`
	Processed           map[string]updateRecord  `json:"processed,omitempty"`
	Outbox              []outboxRecord           `json:"outbox,omitempty"`
	Projection          map[string]string        `json:"projection,omitempty"`
	WorkMessages        map[string]int64         `json:"work_messages,omitempty"`
	Topics              []topicMapping           `json:"topics,omitempty"`
	TopicOps            []topicOpRecord          `json:"topic_ops,omitempty"`
	TopicProjection     map[string]string        `json:"topic_projection,omitempty"`
	TopicMessages       map[string]int64         `json:"topic_messages,omitempty"`
	FallbackSessionID   string                   `json:"fallback_session_id,omitempty"`
	FallbackStartedAt   time.Time                `json:"fallback_started_at,omitempty"`
	TopicNotice         string                   `json:"topic_notice,omitempty"`
	CallbackRoutes      map[string]string        `json:"callback_routes,omitempty"`
	SessionChoices      []string                 `json:"session_choices,omitempty"`
	CallbackIDs         map[string]int64         `json:"callback_ids,omitempty"`
	ReplySessions       map[int64]string         `json:"reply_sessions,omitempty"`
	RetryAt             time.Time                `json:"retry_at,omitempty"`
	DeliveryStartedAt   time.Time                `json:"delivery_started_at,omitempty"`
	LastReceiveAt       *time.Time               `json:"last_receive_at,omitempty"`
	LastSendAt          *time.Time               `json:"last_send_at,omitempty"`
	LastError           string                   `json:"last_error,omitempty"`
	WebhookConflict     bool                     `json:"webhook_conflict,omitempty"`
}

type store struct {
	mu        sync.Mutex
	dir       string
	db        *sql.DB
	tokenPath string
	state     durableState
	// stored is what the database holds, so a commit writes only the
	// difference (see db.go).
	stored durableState
}

func openStore(root string) (*store, error) {
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := openRoot(dir)
	if err != nil {
		return nil, err
	}
	s := &store{dir: dir, db: db, tokenPath: filepath.Join(dir, "token")}
	state, _, err := loadState(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s.state = state
	s.stored = cloneDurableState(state)
	changed := false
	for index := range s.state.Outbox {
		if s.state.Outbox[index].State == "dispatching" {
			s.state.Outbox[index].State = "ambiguous"
			changed = true
		}
	}
	for index := range s.state.TopicOps {
		if s.state.TopicOps[index].State == "dispatching" {
			s.state.TopicOps[index].State = "ambiguous"
			changed = true
		}
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

func newDurableState() durableState {
	return durableState{Schema: stateSchema, Processed: map[string]updateRecord{}, Projection: map[string]string{}, WorkMessages: map[string]int64{},
		TopicProjection: map[string]string{}, TopicMessages: map[string]int64{}}
}

func (s *store) ensureMapsLocked() {
	ensureDurableMaps(&s.state)
}

func (s *store) snapshot() durableState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneDurableState(s.state)
}

func (s *store) mutate(fn func(*durableState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneDurableState(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	ensureDurableMaps(&next)
	// Polling projections often have no new facts. Preserve the existing durable
	// image without serialization, rename, or fsync when nothing changed.
	if reflect.DeepEqual(s.state, next) {
		return nil
	}
	if err := commitState(s.db, s.stored, next); err != nil {
		return err
	}
	// next is never mutated once it is the state: every mutation starts from
	// a clone, so the committed image can be shared.
	s.state, s.stored = next, next
	return nil
}

func ensureDurableMaps(state *durableState) {
	if state.MediaInputs == nil {
		state.MediaInputs = map[string]mediaInput{}
	}
	if state.Feedback == nil {
		state.Feedback = map[int64]feedbackRecord{}
	}
	if state.MessageSources == nil {
		state.MessageSources = map[int64]messageSource{}
	}
	if state.Processed == nil {
		state.Processed = map[string]updateRecord{}
	}
	if state.Projection == nil {
		state.Projection = map[string]string{}
	}
	if state.WorkMessages == nil {
		state.WorkMessages = map[string]int64{}
	}
	if state.TopicProjection == nil {
		state.TopicProjection = map[string]string{}
	}
	if state.TopicMessages == nil {
		state.TopicMessages = map[string]int64{}
	}
	if state.CallbackRoutes == nil {
		state.CallbackRoutes = map[string]string{}
	}
	if state.CallbackIDs == nil {
		state.CallbackIDs = map[string]int64{}
	}
	if state.ReplySessions == nil {
		state.ReplySessions = map[int64]string{}
	}
}

// saveLocked commits s.state, which the caller changed in place.
func (s *store) saveLocked() error {
	if err := commitState(s.db, s.stored, s.state); err != nil {
		return err
	}
	s.stored = cloneDurableState(s.state)
	return nil
}

func (s *store) readToken() (string, error) {
	data, err := os.ReadFile(s.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *store) replaceToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("Telegram token is required")
	}
	return atomicPrivateWrite(s.tokenPath, []byte(token+"\n"))
}

func (s *store) removeToken() error {
	err := os.Remove(s.tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func atomicPrivateWrite(path string, data []byte) error {
	if err := atomicfile.Write(path, data, 0o600); err != nil && !errors.Is(err, atomicfile.ErrDirSync) {
		return err
	}
	return nil
}
