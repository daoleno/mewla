package work

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/providerpaths"
)

// OpenCode 2 conversations are read through the OpenCode server API
// (session.get, session.list, session.message.list). The database file is
// only stat'ed as a change stamp so unchanged polls cost no API call; its
// tables are never read.
const (
	opencodeConversationSource = "opencode_api"
	maxOpenCodeConversationAge = 72 * time.Hour

	// openCodeStartOriginSlack is how far before the Mewla agent's start a
	// provider session may be created and still belong to it. It matches the
	// lower bound of the StartedAt admission window: the agent's own thread is
	// created at or after its process start, and a session created earlier
	// than that (minus clock slack) provably belongs to a different
	// conversation.
	openCodeStartOriginSlack = 5 * time.Second

	// openCodeCacheMaxSessions bounds the process-wide parsed-conversation
	// cache so many bindings cannot accumulate unbounded message text.
	openCodeCacheMaxSessions = 8

	// openCodeTailPageSize is the newest-first page used to refresh a cached
	// session; only the tail of a session changes while it runs.
	openCodeTailPageSize = 50
)

type openCodeSessionCandidate struct {
	ID        string
	CWD       string
	ParentID  string
	CreatedAt time.Time
	Updated   time.Time
}

func openCodeCandidateFromSession(session opencodeapi.Session) openCodeSessionCandidate {
	return openCodeSessionCandidate{
		ID:        session.ID,
		CWD:       session.Location.Directory,
		ParentID:  strings.TrimSpace(session.ParentID),
		CreatedAt: session.Created().UTC(),
		Updated:   session.Updated().UTC(),
	}
}

func (r *ProviderConversationReader) loadOpenCodeConversationForWorker(worker classifier.Worker, now time.Time) (CodexConversation, error) {
	if strings.TrimSpace(worker.Cwd) == "" {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "missing_cwd",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	dbPath := openCodeDBPath()

	// Stamp-first fast path: an unchanged database stamp means OpenCode wrote
	// nothing, so the pinned session and the cached conversation are both
	// still valid and the poll makes no API call. The fast path is
	// origin-gated like every other binding path: a pinned session that
	// predates a now-known agent start must re-run discovery.
	if owned := strings.TrimSpace(r.openCodeOwnedSessionID); owned != "" &&
		r.openCodePinRespectsWorkerStart(worker.StartedAt) {
		if conversation, version, changedIDs, ok := openCodeConversationCache.read(dbPath, owned); ok {
			r.openCodeLastVersion = version
			r.openCodeLastChangedIDs = changedIDs
			return r.openCodeConversationResult(conversation, r.openCodeOwnedCandidate, dbPath), nil
		}
	}

	candidate, ok, err := r.findOpenCodeSession(worker, now)
	if err != nil {
		r.resetSource()
		if errors.Is(err, opencodeapi.ErrUnavailable) {
			return CodexConversation{
				Available: false,
				Reason:    "service_unavailable",
				Events:    []CodexConversationEvent{},
			}, nil
		}
		return CodexConversation{}, err
	}
	if !ok {
		r.resetSource()
		return CodexConversation{
			Available: false,
			Reason:    "session_not_found",
			Events:    []CodexConversationEvent{},
		}, nil
	}

	conversation, version, changedIDs, err := openCodeConversationCache.load(dbPath, candidate.ID)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	r.openCodeOwnedSessionID = candidate.ID
	r.openCodeOwnedCandidate = candidate
	r.openCodeLastVersion = version
	r.openCodeLastChangedIDs = changedIDs
	return r.openCodeConversationResult(conversation, candidate, dbPath), nil
}

// openCodePinRespectsWorkerStart reports whether the current pin may keep
// serving under the agent's started-at evidence. Launch-declared pins are
// exempt (a resumed thread predates the process by design); discovered pins
// must never predate the agent they were bound to.
func (r *ProviderConversationReader) openCodePinRespectsWorkerStart(startedAt time.Time) bool {
	if r.openCodeOwnedFromLaunch || startedAt.IsZero() {
		return true
	}
	return !openCodeCandidatePredatesWorkerStart(r.openCodeOwnedCandidate, startedAt)
}

func (r *ProviderConversationReader) openCodeConversationResult(conversation CodexConversation, candidate openCodeSessionCandidate, dbPath string) CodexConversation {
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = dbPath
	conversation.SessionID = firstNonEmpty(conversation.SessionID, candidate.ID)
	conversation.CWD = firstNonEmpty(conversation.CWD, candidate.CWD)
	conversation.Updated = &candidate.Updated
	if conversation.Events == nil {
		conversation.Events = []CodexConversationEvent{}
	}
	return conversation
}

func (r *ProviderConversationReader) findOpenCodeSession(worker classifier.Worker, now time.Time) (openCodeSessionCandidate, bool, error) {
	if owned := strings.TrimSpace(r.openCodeOwnedSessionID); owned != "" {
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, worker.Cwd); ok &&
			r.openCodePinRespectsWorkerStart(worker.StartedAt) {
			return candidate, true, nil
		}
		r.openCodeOwnedSessionID = ""
		r.openCodeOwnedCandidate = openCodeSessionCandidate{}
		r.openCodeOwnedFromLaunch = false
	}
	if owned := OpenCodeOwnedSessionID(worker.Command); owned != "" {
		// Mewla launches delegated OpenCode Workers with an explicit
		// --session; OpenCode creates that session on the first prompt.
		if candidate, ok := r.revalidateOpenCodeOwnedSession(owned, worker.Cwd); ok {
			r.openCodeOwnedSessionID = candidate.ID
			r.openCodeOwnedCandidate = candidate
			r.openCodeOwnedFromLaunch = true
			return candidate, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}

	candidates, err := queryOpenCodeSessions(worker.Cwd)
	if err != nil {
		return openCodeSessionCandidate{}, false, err
	}
	// Child sessions (parentID set) are Task/sub-agent activity, not the
	// Mewla agent's own transcript thread. Only root sessions can bind.
	roots := rootOpenCodeCandidates(candidates)
	fresh := freshOpenCodeSessionCandidates(roots, now)
	if len(openCodeWindowCandidates(fresh, worker.StartedAt)) > 0 {
		// A StartedAt window exists: unique min-delta bind, else refuse.
		if matched, ok := matchOpenCodeSessionToWorkerStart(fresh, worker.StartedAt); ok {
			r.bindDiscoveredOpenCodeSession(matched)
			return matched, true, nil
		}
		return openCodeSessionCandidate{}, false, nil
	}
	// No window candidate: the agent's own session has not appeared yet
	// (OpenCode creates it with the first prompt), or startedAt is unknown. A
	// session created before the agent started provably belongs to a
	// different conversation, so the freshest-root fallback only considers
	// sessions created at or after the agent's start.
	eligible := openCodeCandidatesNotBeforeStart(fresh, worker.StartedAt)
	if matched, ok := freshestOpenCodeRoot(eligible); ok {
		r.bindDiscoveredOpenCodeSession(matched)
		return matched, true, nil
	}
	return openCodeSessionCandidate{}, false, nil
}

// bindDiscoveredOpenCodeSession pins a session proven by temporal discovery
// (started-at window or post-start fallback). The pin never carries launch
// provenance, so a later known started-at can still release it.
func (r *ProviderConversationReader) bindDiscoveredOpenCodeSession(candidate openCodeSessionCandidate) {
	r.openCodeOwnedSessionID = candidate.ID
	r.openCodeOwnedCandidate = candidate
	r.openCodeOwnedFromLaunch = false
}

func (r *ProviderConversationReader) revalidateOpenCodeOwnedSession(sessionID, workerCWD string) (openCodeSessionCandidate, bool) {
	session, err := opencodeapi.GetSession(context.Background(), sessionID)
	if err != nil {
		return openCodeSessionCandidate{}, false
	}
	candidate := openCodeCandidateFromSession(session)
	// A child (subtask) session must never anchor a Mewla agent transcript.
	if candidate.ParentID != "" {
		return openCodeSessionCandidate{}, false
	}
	if !openCodeDirectoryMatches(candidate.CWD, workerCWD) {
		return openCodeSessionCandidate{}, false
	}
	return candidate, true
}

func queryOpenCodeSessions(cwd string) ([]openCodeSessionCandidate, error) {
	var candidates []openCodeSessionCandidate
	seen := map[string]struct{}{}
	for _, directory := range transcriptCWDCandidates(cwd) {
		sessions, err := opencodeapi.ListSessions(context.Background(), opencodeapi.SessionQuery{
			Directory: directory,
			RootsOnly: true,
			Limit:     50,
		})
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if _, ok := seen[session.ID]; ok {
				continue
			}
			seen[session.ID] = struct{}{}
			candidates = append(candidates, openCodeCandidateFromSession(session))
		}
	}
	return candidates, nil
}

// openCodeDBPath is OpenCode's database file, used only as a change stamp.
func openCodeDBPath() string {
	home, _ := os.UserHomeDir()
	return providerpaths.OpenCodeDB(home)
}

// ---------------------------------------------------------------------------
// Process-wide parsed-conversation cache.
//
// The cache owns OpenCode's parsed structured data per session. Readers share
// it so a reconnect or a second device never re-fetches the full history, and
// every poll that finds the database stamp unchanged makes no API call. A
// changed stamp refreshes only the session tail (newest-first pages until
// they overlap the cache). A content version (and the event ids it changed)
// is exposed so the server can skip serialization work and compute cheap
// deltas.
// ---------------------------------------------------------------------------

type openCodeStamp struct {
	size       int64
	modTime    time.Time
	walSize    int64
	walModTime time.Time
	fileInfo   os.FileInfo
	walFile    os.FileInfo
}

func openCodeConversationStamp(dbPath string) (openCodeStamp, bool) {
	if dbPath == "" {
		return openCodeStamp{}, false
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		return openCodeStamp{}, false
	}
	stamp := openCodeStamp{size: info.Size(), modTime: info.ModTime(), fileInfo: info}
	// SQLite WAL mode keeps new writes in the -wal file; the main file's stat
	// changes only at checkpoint.
	if wal, err := os.Stat(dbPath + "-wal"); err == nil {
		stamp.walSize = wal.Size()
		stamp.walModTime = wal.ModTime()
		stamp.walFile = wal
	}
	return stamp, true
}

func (s openCodeStamp) equal(other openCodeStamp) bool {
	return s.size == other.size &&
		s.modTime.Equal(other.modTime) &&
		s.walSize == other.walSize &&
		s.walModTime.Equal(other.walModTime) &&
		sameProviderSourceFile(s.fileInfo, other.fileInfo) &&
		sameProviderSourceFile(s.walFile, other.walFile)
}

type openCodeCacheEntry struct {
	stamp    openCodeStamp
	hasStamp bool

	// messages are the session's messages oldest first.
	messages []opencodeapi.Message

	conversation CodexConversation
	version      int64

	// lastChangedIDs are the event ids affected by the last content-changing
	// load, used by the server for cheap memoized fingerprint deltas.
	lastChangedIDs []string

	// generation counts applied refreshes. load() captures it before its API
	// calls and serves the cached result when another refresh applied first.
	generation uint64
}

// openCodeConversationCache is a process-wide bounded cache of parsed OpenCode
// conversations keyed by session id. API calls happen outside the lock.
var openCodeConversationCache = newOpenCodeConversationCache()

type openCodeConversationCacheImpl struct {
	mu         sync.Mutex
	entries    map[string]*openCodeCacheEntry
	entryOrder []string

	// versionSeq is the process-global monotonic content-version source, so a
	// reloaded session can never reuse a version the server already observed.
	versionSeq atomic.Uint64
}

func newOpenCodeConversationCache() *openCodeConversationCacheImpl {
	return &openCodeConversationCacheImpl{entries: map[string]*openCodeCacheEntry{}}
}

// read returns the cached conversation when the database stamp is unchanged.
func (c *openCodeConversationCacheImpl) read(dbPath, sessionID string) (CodexConversation, int64, []string, bool) {
	stamp, ok := openCodeConversationStamp(dbPath)
	if !ok {
		return CodexConversation{}, 0, nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[sessionID]
	if !ok || !entry.hasStamp || !entry.stamp.equal(stamp) {
		return CodexConversation{}, 0, nil, false
	}
	return entry.conversation, entry.version, entry.lastChangedIDs, true
}

// load refreshes one session and rebuilds its conversation when messages
// changed. The stamp is taken before the API calls, so a write during the
// refresh leaves the stamp stale and the next poll refreshes again.
func (c *openCodeConversationCacheImpl) load(dbPath, sessionID string) (CodexConversation, int64, []string, error) {
	stamp, hasStamp := openCodeConversationStamp(dbPath)
	c.mu.Lock()
	entry, cached := c.entries[sessionID]
	var known []opencodeapi.Message
	var generation uint64
	if cached {
		known = entry.messages
		generation = entry.generation
	}
	c.mu.Unlock()

	messages, err := fetchOpenCodeMessages(sessionID, known)
	if err != nil {
		return CodexConversation{}, 0, nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	entry, cached = c.entries[sessionID]
	if cached && entry.generation != generation {
		return entry.conversation, entry.version, entry.lastChangedIDs, nil
	}
	if !cached {
		entry = &openCodeCacheEntry{}
		c.entries[sessionID] = entry
		c.entryOrder = append(c.entryOrder, sessionID)
		c.evictIfNeeded()
	}
	conversation := buildOpenCodeConversation(sessionID, messages)
	changed := changedOpenCodeEventIDs(entry.conversation.Events, conversation.Events)
	entry.stamp, entry.hasStamp = stamp, hasStamp
	entry.messages = messages
	entry.generation++
	if !cached || len(changed) > 0 || !reflect.DeepEqual(entry.conversation.Activity, conversation.Activity) {
		entry.conversation = conversation
		entry.version = int64(c.versionSeq.Add(1))
		entry.lastChangedIDs = changed
	}
	return entry.conversation, entry.version, entry.lastChangedIDs, nil
}

func (c *openCodeConversationCacheImpl) remove(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
	for index, id := range c.entryOrder {
		if id == sessionID {
			c.entryOrder = append(c.entryOrder[:index], c.entryOrder[index+1:]...)
			break
		}
	}
}

func (c *openCodeConversationCacheImpl) evictIfNeeded() {
	for len(c.entryOrder) > openCodeCacheMaxSessions {
		oldest := c.entryOrder[0]
		c.entryOrder = c.entryOrder[1:]
		delete(c.entries, oldest)
	}
}

// fetchOpenCodeMessages returns the session's messages oldest first. Without
// a cache it reads the whole session; with one it reads newest-first pages
// until a page reaches a cached message, then splices that tail over the
// cache. Messages inside the refreshed tail that the server no longer returns
// (revert, compaction) disappear with it.
func fetchOpenCodeMessages(sessionID string, known []opencodeapi.Message) ([]opencodeapi.Message, error) {
	ctx := context.Background()
	if len(known) == 0 {
		return opencodeapi.ListMessages(ctx, sessionID, "")
	}
	position := make(map[string]int, len(known))
	for index, message := range known {
		position[message.ID] = index
	}
	var tail []opencodeapi.Message // newest first
	pageCursor := ""
	for {
		page, err := opencodeapi.ListMessagesPage(ctx, sessionID, "desc", pageCursor, "", openCodeTailPageSize)
		if err != nil {
			return nil, err
		}
		tail = append(tail, page.Messages...)
		prefix := -1
		if page.Last {
			// The whole session was read: nothing cached survives outside it.
			prefix = 0
		} else if len(page.Messages) > 0 {
			if overlap, ok := position[page.Messages[len(page.Messages)-1].ID]; ok {
				prefix = overlap
			}
		}
		if prefix >= 0 {
			merged := append([]opencodeapi.Message{}, known[:prefix]...)
			for index := len(tail) - 1; index >= 0; index-- {
				merged = append(merged, tail[index])
			}
			return merged, nil
		}
		pageCursor = page.Next
	}
}

// changedOpenCodeEventIDs reports events added, changed, or removed between
// two builds.
func changedOpenCodeEventIDs(previous, next []CodexConversationEvent) []string {
	previousByID := make(map[string]CodexConversationEvent, len(previous))
	for _, event := range previous {
		previousByID[event.ID] = event
	}
	var changed []string
	for _, event := range next {
		old, ok := previousByID[event.ID]
		delete(previousByID, event.ID)
		if !ok || !reflect.DeepEqual(old, event) {
			changed = append(changed, event.ID)
		}
	}
	for id := range previousByID {
		changed = append(changed, id)
	}
	return changed
}

// parseOpenCodeConversation reads one whole session without the shared cache
// (transcript scans and tests).
func parseOpenCodeConversation(sessionID string) (CodexConversation, error) {
	messages, err := opencodeapi.ListMessages(context.Background(), sessionID, "")
	if err != nil {
		return CodexConversation{}, err
	}
	conversation := buildOpenCodeConversation(sessionID, messages)
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = openCodeDBPath()
	return conversation, nil
}

// loadOpenCodeConversation loads one bound session through the shared cache.
func (r *ProviderConversationReader) loadOpenCodeConversation(sessionID string) (CodexConversation, error) {
	conversation, version, changedIDs, err := openCodeConversationCache.load(openCodeDBPath(), sessionID)
	if err != nil {
		r.resetSource()
		return CodexConversation{}, err
	}
	r.openCodeLastVersion = version
	r.openCodeLastChangedIDs = changedIDs
	conversation.Available = true
	conversation.Source = opencodeConversationSource
	conversation.Path = openCodeDBPath()
	return conversation, nil
}

// ---------------------------------------------------------------------------
// Session discovery helpers.
// ---------------------------------------------------------------------------

func freshOpenCodeSessionCandidates(candidates []openCodeSessionCandidate, now time.Time) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	fresh := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isOpenCodeSessionFresh(candidate.Updated, now) {
			fresh = append(fresh, candidate)
		}
	}
	return fresh
}

func isOpenCodeSessionFresh(updated, now time.Time) bool {
	if updated.IsZero() || now.IsZero() {
		return true
	}
	if updated.After(now.Add(10 * time.Minute)) {
		return true
	}
	return now.Sub(updated) <= maxOpenCodeConversationAge
}

func rootOpenCodeCandidates(candidates []openCodeSessionCandidate) []openCodeSessionCandidate {
	if len(candidates) == 0 {
		return nil
	}
	roots := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ParentID) == "" {
			roots = append(roots, candidate)
		}
	}
	return roots
}

func openCodeWindowCandidates(candidates []openCodeSessionCandidate, startedAt time.Time) []openCodeSessionCandidate {
	if startedAt.IsZero() {
		return nil
	}
	startedAt = startedAt.UTC()
	minCreatedAt := startedAt.Add(-5 * time.Second)
	maxCreatedAt := startedAt.Add(2 * time.Minute)
	out := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		createdAt := candidate.CreatedAt.UTC()
		if createdAt.IsZero() || createdAt.Before(minCreatedAt) || createdAt.After(maxCreatedAt) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// openCodeCandidatePredatesWorkerStart reports whether a provider session was
// created before the Mewla agent started (beyond shared slack). Unknown
// evidence on either side never claims precedence.
func openCodeCandidatePredatesWorkerStart(candidate openCodeSessionCandidate, startedAt time.Time) bool {
	if startedAt.IsZero() || candidate.CreatedAt.IsZero() {
		return false
	}
	return candidate.CreatedAt.UTC().Before(startedAt.UTC().Add(-openCodeStartOriginSlack))
}

// openCodeCandidatesNotBeforeStart filters out sessions that provably predate
// the agent. With an unknown start every fresh root stays eligible.
func openCodeCandidatesNotBeforeStart(candidates []openCodeSessionCandidate, startedAt time.Time) []openCodeSessionCandidate {
	if startedAt.IsZero() || len(candidates) == 0 {
		return candidates
	}
	eligible := make([]openCodeSessionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !openCodeCandidatePredatesWorkerStart(candidate, startedAt) {
			eligible = append(eligible, candidate)
		}
	}
	return eligible
}

func matchOpenCodeSessionToWorkerStart(candidates []openCodeSessionCandidate, startedAt time.Time) (openCodeSessionCandidate, bool) {
	window := openCodeWindowCandidates(candidates, startedAt)
	if len(window) == 0 {
		return openCodeSessionCandidate{}, false
	}
	delta := func(candidate openCodeSessionCandidate) time.Duration {
		value := candidate.CreatedAt.UTC().Sub(startedAt.UTC())
		if value < 0 {
			return -value
		}
		return value
	}
	bestIndex := 0
	for index, candidate := range window {
		if delta(candidate) < delta(window[bestIndex]) {
			bestIndex = index
		}
	}
	for index, candidate := range window {
		if index != bestIndex && delta(candidate) == delta(window[bestIndex]) {
			return openCodeSessionCandidate{}, false
		}
	}
	return window[bestIndex], true
}

// freshestOpenCodeRoot binds the most recently updated eligible root session.
// Equal-updated candidates refuse as ambiguous rather than guessing.
func freshestOpenCodeRoot(candidates []openCodeSessionCandidate) (openCodeSessionCandidate, bool) {
	if len(candidates) == 0 {
		return openCodeSessionCandidate{}, false
	}
	bestIndex := 0
	for index, candidate := range candidates {
		if candidate.Updated.After(candidates[bestIndex].Updated) {
			bestIndex = index
		}
	}
	for index, candidate := range candidates {
		if index != bestIndex && candidate.Updated.Equal(candidates[bestIndex].Updated) {
			return openCodeSessionCandidate{}, false
		}
	}
	return candidates[bestIndex], true
}

func openCodeDirectoryMatches(sessionDir, workerCWD string) bool {
	sessionDir = strings.TrimSpace(sessionDir)
	workerCWD = strings.TrimSpace(workerCWD)
	if sessionDir == "" || workerCWD == "" {
		return false
	}
	for _, candidate := range transcriptCWDCandidates(workerCWD) {
		if pathsEquivalent(sessionDir, candidate) || pathsEquivalent(sessionDir, workerCWD) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Conversation projection from OpenCode 2 Session.Message.Info values.
// ---------------------------------------------------------------------------

type openCodeMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Time struct {
		Created   float64 `json:"created"`
		Completed float64 `json:"completed"`
	} `json:"time"`
	// user
	Text  string `json:"text"`
	Files []struct {
		Name string `json:"name"`
		URI  string `json:"uri"`
	} `json:"files"`
	// assistant
	Content []openCodeContent `json:"content"`
	Finish  string            `json:"finish"`
	// idle
	Outcome string              `json:"outcome"`
	Error   *openCodeStructured `json:"error"`
	// shell
	Command string `json:"command"`
	Status  string `json:"status"`
	Output  *struct {
		Output string `json:"output"`
	} `json:"output"`
}

type openCodeContent struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	State struct {
		Status  string              `json:"status"`
		Input   json.RawMessage     `json:"input"`
		Content []openCodeContent   `json:"content"`
		Error   *openCodeStructured `json:"error"`
	} `json:"state"`
	Time struct {
		Created   float64 `json:"created"`
		Completed float64 `json:"completed"`
	} `json:"time"`
}

type openCodeStructured struct {
	Tag     string `json:"_tag"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func openCodeTimestamp(milliseconds float64) string {
	if milliseconds <= 0 {
		return ""
	}
	return normalizeCodexTimestamp(time.UnixMilli(int64(milliseconds)).UTC().Format(time.RFC3339Nano))
}

type openCodeConversationBuilder struct {
	sessionID         string
	events            []CodexConversationEvent
	seq               int
	activityLifecycle providerActivityLifecycle
}

// buildOpenCodeConversation projects one session's messages. OpenCode 2
// closes every turn with an idle message carrying its outcome; that is the
// only turn-terminal fact.
func buildOpenCodeConversation(sessionID string, messages []opencodeapi.Message) CodexConversation {
	builder := &openCodeConversationBuilder{sessionID: strings.TrimSpace(sessionID)}
	for _, raw := range messages {
		var message openCodeMessage
		if json.Unmarshal(raw.Raw, &message) != nil {
			continue
		}
		builder.consume(message)
	}
	return builder.result()
}

func (b *openCodeConversationBuilder) startActivity(id, timestamp string) {
	if !b.activityLifecycle.running() {
		b.activityLifecycle.start(providerActivityID(b.sessionID, id, b.seq), timestamp)
	}
}

func (b *openCodeConversationBuilder) consume(message openCodeMessage) {
	timestamp := openCodeTimestamp(message.Time.Created)
	switch message.Type {
	case "user":
		body := message.Text
		for _, file := range message.Files {
			if name := firstNonEmpty(file.Name, file.URI); name != "" {
				body = strings.TrimSpace(body + "\n[" + name + "]")
			}
		}
		if body == "" {
			return
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:              message.ID,
			Seq:             b.seq,
			Timestamp:       timestamp,
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(body))),
		})
		b.startActivity(message.ID, timestamp)
	case "assistant":
		b.startActivity(message.ID, timestamp)
		completed := message.Time.Completed > 0
		for index, content := range message.Content {
			b.projectContent(message.ID, index, content, timestamp, completed)
		}
	case "shell":
		b.startActivity(message.ID, timestamp)
		status := "completed"
		switch message.Status {
		case "running":
			status = "running"
		case "timeout", "killed":
			status = "failed"
		}
		input, _ := json.Marshal(map[string]string{"command": message.Command})
		output := ""
		if message.Output != nil {
			output = strings.TrimSpace(message.Output.Output)
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:        message.ID,
			Seq:       b.seq,
			Timestamp: timestamp,
			Kind:      "tool_call",
			ToolName:  "shell",
			CallID:    message.ID,
			Input:     string(input),
			Output:    output,
			Status:    status,
			Partial:   status == "running",
		})
	case "idle":
		status := ProviderActivityCompleted
		switch message.Outcome {
		case "failed":
			status = ProviderActivityFailed
		case "interrupted":
			status = ProviderActivityInterrupted
		}
		b.activityLifecycle.settle("", status, timestamp)
		if message.Error != nil {
			b.activityLifecycle.recordError(timestamp, ProviderActivityError{
				Code:    firstNonEmpty(message.Error.Tag, message.Error.Type),
				Message: message.Error.Message,
			}, false)
		}
	}
}

func (b *openCodeConversationBuilder) projectContent(messageID string, index int, content openCodeContent, timestamp string, completed bool) {
	partTime := firstNonEmpty(openCodeTimestamp(content.Time.Created), timestamp)
	switch content.Type {
	case "text":
		text := strings.TrimSpace(content.Text)
		if text == "" {
			return
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:        fmt.Sprintf("%s:text:%d", messageID, index),
			Seq:       b.seq,
			Timestamp: partTime,
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      text,
			Partial:   !completed,
		})
	case "reasoning":
		text := strings.TrimSpace(content.Text)
		if text == "" {
			return
		}
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:        fmt.Sprintf("%s:reasoning:%d", messageID, index),
			Seq:       b.seq,
			Timestamp: partTime,
			Kind:      "reasoning",
			Body:      text,
			Partial:   !completed && content.Time.Completed <= 0,
			Transient: true,
		})
	case "tool":
		status := "running"
		switch content.State.Status {
		case "completed":
			status = "completed"
		case "error":
			status = "failed"
		}
		input := strings.TrimSpace(string(content.State.Input))
		// A streaming tool's input is still a partial JSON string.
		var streamed string
		if json.Unmarshal(content.State.Input, &streamed) == nil {
			input = strings.TrimSpace(streamed)
		}
		var outputs []string
		for _, item := range content.State.Content {
			if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
				outputs = append(outputs, strings.TrimSpace(item.Text))
			}
		}
		output := strings.Join(outputs, "\n")
		if output == "" && content.State.Error != nil {
			output = strings.TrimSpace(content.State.Error.Message)
		}
		callID := firstNonEmpty(content.ID, fmt.Sprintf("%s:tool:%d", messageID, index))
		b.seq++
		b.events = append(b.events, CodexConversationEvent{
			ID:        callID,
			Seq:       b.seq,
			Timestamp: partTime,
			Kind:      "tool_call",
			ToolName:  strings.TrimSpace(content.Name),
			CallID:    callID,
			Input:     input,
			Output:    output,
			Status:    status,
			Partial:   status == "running",
		})
	}
}

func (b *openCodeConversationBuilder) result() CodexConversation {
	// A settled turn is final: clear in-flight flags so the Interface never
	// renders a finished turn as still streaming.
	if !b.activityLifecycle.running() {
		for index := range b.events {
			b.events[index].Partial = false
			if b.events[index].Kind == "tool_call" && b.events[index].Status == "running" {
				b.events[index].Status = "interrupted"
			}
		}
	}
	if b.events == nil {
		b.events = []CodexConversationEvent{}
	}
	return conversationWithActivity(CodexConversation{
		SessionID: b.sessionID,
		Events:    b.events,
	}, &b.activityLifecycle)
}
