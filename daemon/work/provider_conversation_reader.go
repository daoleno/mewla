package work

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// ProviderConversationReader owns the parsed provider source used by one live
// conversation subscription. Its zero value is ready to use, but callers
// normally allocate one at the subscription boundary.
type ProviderConversationReader struct {
	bound   bool
	binding providerConversationBinding
	source  providerConversationSource

	// cursorProjectRoots caches only the CWD→project-root resolution for this
	// subscription. Transcript files are still enumerated on every poll.
	cursorProjectRootsCWD string
	cursorProjectRoots    []cursorProjectRoot

	// openCodeOwnedSessionID retains the unambiguously bound OpenCode ses_* for
	// this subscription so later polls never cross-bind another same-CWD row.
	openCodeOwnedSessionID string

	// openCodeOwnedCandidate retains the last revalidated session row for the
	// pinned OpenCode session so an unchanged-source poll can return the
	// cached conversation without any sqlite3 spawn.
	openCodeOwnedCandidate openCodeSessionCandidate

	// openCodeOwnedFromLaunch records that the pinned session id was declared
	// by an explicit -s/--session ses_* launch token. Declared ownership
	// outranks temporal origin: a resumed thread legitimately predates the
	// Mewla agent process, so launch-owned pins are never released by the
	// started-at origin gate that governs discovered bindings.
	openCodeOwnedFromLaunch bool

	// openCodeLastVersion is the shared cache content version of the last
	// returned OpenCode conversation. The server skips serialization work when
	// the version is unchanged. 0 means unknown (no OpenCode load yet).
	openCodeLastVersion int64

	// openCodeLastChangedIDs are the event ids changed by the last
	// content-changing OpenCode load, used for cheap memoized deltas.
	openCodeLastChangedIDs []string

	// piPinnedSessionPath retains the auto-bound shared-directory Pi transcript
	// for this subscription so a newer same-CWD session never leaks into the
	// active agent's Interface mid-turn.
	piPinnedSessionPath string
}

// ConversationVersion returns the content version of the last loaded provider
// conversation. 0 means unknown (no load or a non-cache provider); callers
// must fall back to full processing.
func (r *ProviderConversationReader) ConversationVersion() int64 {
	return r.openCodeLastVersion
}

// ChangedEventIDs returns the event ids changed by the last content-changing
// OpenCode load, or nil when unknown. Used for cheap memoized deltas.
func (r *ProviderConversationReader) ChangedEventIDs() []string {
	return r.openCodeLastChangedIDs
}

type providerConversationBinding struct {
	provider   string
	workerID   string
	workerName string
	cwd        string
	command    string
	startedAt  time.Time
	processID  int
	hidden     bool
}

type providerConversationSource struct {
	provider     string
	path         string
	size         int64
	modTime      time.Time
	fileInfo     os.FileInfo
	conversation CodexConversation

	grokStamp       grokConversationStamp
	grokUpdatesInfo os.FileInfo
	grokUpdates     *grokUpdateTracker
}

func NewProviderConversationReader() *ProviderConversationReader {
	return &ProviderConversationReader{}
}

// Load selects and reads the configured provider's current native source.
// Source selection intentionally runs on every call; only parsing of the
// selected unchanged source is reused.
func (r *ProviderConversationReader) Load(
	worker classifier.Worker,
	provider string,
	now time.Time,
) (CodexConversation, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	r.bind(worker, provider)

	switch provider {
	case WorkerProviderCursor:
		return r.loadCursorConversationForWorker(worker, now)
	case WorkerProviderGrok:
		return r.loadGrokConversationForWorker(worker, now)
	case WorkerProviderClaude:
		return r.loadClaudeConversationForWorker(worker, now)
	case WorkerProviderCodex:
		return r.loadCodexConversationForWorker(worker, now)
	case WorkerProviderDSH:
		return r.loadDSHConversationForWorker(worker, now)
	case WorkerProviderPi:
		return r.loadPiConversationForWorker(worker, now)
	case WorkerProviderOpenCode:
		return r.loadOpenCodeConversationForWorker(worker, now)
	default:
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "not_structured_agent",
			Events:    []CodexConversationEvent{},
		}, nil
	}
}

func (r *ProviderConversationReader) bind(worker classifier.Worker, provider string) {
	next := providerConversationBinding{
		provider:   provider,
		workerID:   strings.TrimSpace(worker.ID),
		workerName: strings.TrimSpace(worker.Name),
		cwd:        strings.TrimSpace(worker.Cwd),
		command:    strings.TrimSpace(worker.Command),
		startedAt:  worker.StartedAt,
		processID:  worker.ProcessID,
		hidden:     worker.Hidden,
	}
	if r.bound && r.binding.equal(next) {
		return
	}
	if !r.bound || r.binding.cwd != next.cwd {
		r.cursorProjectRootsCWD = ""
		r.cursorProjectRoots = nil
	}
	if !r.bound || r.binding.provider != next.provider || r.binding.workerID != next.workerID ||
		r.binding.command != next.command || r.binding.cwd != next.cwd {
		r.openCodeOwnedSessionID = ""
		r.openCodeOwnedCandidate = openCodeSessionCandidate{}
		r.openCodeLastVersion = 0
		r.openCodeLastChangedIDs = nil
		r.piPinnedSessionPath = ""
	}
	// The auto-bound Pi transcript pin belongs to a concrete process
	// instance: an in-pane Pi restart changes startedAt and/or the detected
	// provider process id (the watcher's startedAt comes from second-
	// granularity ps lstart, so a same-second restart can share startedAt).
	// Invalidate the pin on either instance signal so the next scan can bind
	// the new conversation through the new startedAt window. A transient
	// process-table wobble only causes a bounded rescan that re-pins the
	// same transcript with no wire change.
	if !r.bound || !r.binding.startedAt.Equal(next.startedAt) || r.binding.processID != next.processID {
		r.piPinnedSessionPath = ""
	}
	r.bound = true
	r.binding = next
	r.resetSource()
}

func (b providerConversationBinding) equal(other providerConversationBinding) bool {
	return b.provider == other.provider &&
		b.workerID == other.workerID &&
		b.workerName == other.workerName &&
		b.cwd == other.cwd &&
		b.command == other.command &&
		b.startedAt.Equal(other.startedAt) &&
		b.processID == other.processID &&
		b.hidden == other.hidden
}

func (r *ProviderConversationReader) resetSource() {
	r.source = providerConversationSource{}
}

func (r *ProviderConversationReader) loadFileConversation(
	provider string,
	path string,
	parse func(string) (CodexConversation, error),
) (CodexConversation, error) {
	info, err := os.Stat(path)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	if r.source.provider == provider &&
		r.source.path == path &&
		r.source.size == info.Size() &&
		r.source.modTime.Equal(info.ModTime()) &&
		sameProviderSourceFile(r.source.fileInfo, info) {
		return r.source.conversation, nil
	}

	// A changed or newly-selected source cannot retain the prior parsed value.
	r.resetSource()
	// The parse of one transcript is shared by every reader. A phone that
	// reconnects, or a second subscription, would otherwise parse a transcript
	// of hundreds of MB again from the start, and the snapshot it waits for
	// would take as long as the parse.
	shared := sharedFileConversationFor(provider, path)
	for {
		shared.mu.Lock()
		if shared.valid && shared.matches(info) {
			r.takeShared(shared)
			shared.mu.Unlock()
			return r.source.conversation, nil
		}
		if shared.valid && shared.mayServeStale(info) {
			// A transcript this large that is still being written changes
			// faster than it parses, so waiting for a parse of the current
			// bytes would never end. Answer with the last parse and refresh it
			// once in the background; the next poll reads the newer one.
			if shared.loading == nil {
				shared.loading = make(chan struct{})
				go shared.parse(provider, path, parse, info)
			}
			r.takeShared(shared)
			shared.mu.Unlock()
			return r.source.conversation, nil
		}
		if wait := shared.loading; wait != nil {
			shared.mu.Unlock()
			<-wait
			if info, err = os.Stat(path); err != nil {
				return CodexConversation{}, err
			}
			continue
		}
		shared.loading = make(chan struct{})
		shared.mu.Unlock()
		conversation, err := shared.parse(provider, path, parse, info)
		if err == nil {
			shared.mu.Lock()
			if shared.valid && shared.matches(info) {
				// This reader keeps the parse it made; the shared copy owns
				// its own events.
				r.source = shared.source
				r.source.conversation = conversation
			}
			shared.mu.Unlock()
		}
		return conversation, err
	}
}

// takeShared gives the reader the shared parse; the caller holds shared.mu.
func (r *ProviderConversationReader) takeShared(shared *sharedFileConversation) {
	r.source = shared.source
	r.source.conversation = withOwnEvents(r.source.conversation)
}

// withOwnEvents gives a reader its own event array, so one subscription's
// projection can never change what another reads from the shared parse.
func withOwnEvents(conversation CodexConversation) CodexConversation {
	conversation.Events = append([]CodexConversationEvent(nil), conversation.Events...)
	return conversation
}

// staleConversationMinBytes is the transcript size from which a reader may
// answer with the previous parse of a file that has only grown since.
var staleConversationMinBytes int64 = 32 << 20

// sharedFileConversation is the latest parse of one transcript file. loading is
// non-nil while one parse runs and closed when it ends, so concurrent loads of
// the same file wait for one parse instead of each running their own.
type sharedFileConversation struct {
	mu      sync.Mutex
	valid   bool
	source  providerConversationSource
	loading chan struct{}
	used    time.Time
}

func (e *sharedFileConversation) matches(info os.FileInfo) bool {
	return e.source.size == info.Size() &&
		e.source.modTime.Equal(info.ModTime()) &&
		sameProviderSourceFile(e.source.fileInfo, info)
}

func (e *sharedFileConversation) mayServeStale(info os.FileInfo) bool {
	return info.Size() >= staleConversationMinBytes &&
		info.Size() >= e.source.size &&
		sameProviderSourceFile(e.source.fileInfo, info)
}

// parse reads the transcript without holding mu, stores the result and ends the
// parse that the caller announced by setting e.loading.
func (e *sharedFileConversation) parse(
	provider, path string,
	parse func(string) (CodexConversation, error),
	before os.FileInfo,
) (CodexConversation, error) {
	conversation, err := parseRecovering(parse, path)
	after, statErr := os.Stat(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	defer func() {
		close(e.loading)
		e.loading = nil
	}()
	if err != nil {
		return CodexConversation{}, err
	}
	if statErr != nil {
		return CodexConversation{}, statErr
	}
	if !sameProviderSourceFile(before, after) {
		return conversation, nil
	}
	changed := before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime())
	if changed && before.Size() < staleConversationMinBytes {
		// The parser opened a coherent file, but its source changed during the
		// read. Return this poll's value without retaining it; the next poll
		// selects and parses the current source again.
		return conversation, nil
	}
	// A large file that grew during its parse is kept under the size it had
	// before: the parse holds at least that much, and the next load sees the
	// file is newer and refreshes it.
	e.source = providerConversationSource{
		provider:     provider,
		path:         path,
		size:         before.Size(),
		modTime:      before.ModTime(),
		fileInfo:     before,
		conversation: withOwnEvents(conversation),
	}
	e.valid = true
	return conversation, nil
}

// parseRecovering keeps a panicking parser from leaving the shared parse
// marked as running forever.
func parseRecovering(parse func(string) (CodexConversation, error), path string) (conversation CodexConversation, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("parse %s: %v", path, recovered)
		}
	}()
	return parse(path)
}

// maxSharedFileConversations bounds the parsed transcripts kept for reuse.
const maxSharedFileConversations = 8

var sharedFileConversations = struct {
	mu      sync.Mutex
	entries map[string]*sharedFileConversation
}{entries: map[string]*sharedFileConversation{}}

func sharedFileConversationFor(provider, path string) *sharedFileConversation {
	key := provider + "\x00" + path
	sharedFileConversations.mu.Lock()
	defer sharedFileConversations.mu.Unlock()
	entry := sharedFileConversations.entries[key]
	if entry == nil {
		entry = &sharedFileConversation{}
		sharedFileConversations.entries[key] = entry
		for len(sharedFileConversations.entries) > maxSharedFileConversations {
			var oldestKey string
			var oldest time.Time
			for candidateKey, candidate := range sharedFileConversations.entries {
				if candidate == entry {
					continue
				}
				if oldestKey == "" || candidate.used.Before(oldest) {
					oldestKey, oldest = candidateKey, candidate.used
				}
			}
			delete(sharedFileConversations.entries, oldestKey)
		}
	}
	entry.used = time.Now()
	return entry
}

func sameProviderSourceFile(left, right os.FileInfo) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return os.SameFile(left, right)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return strings.TrimSpace(string(runes[:max-3])) + "..."
}
