package work

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// The reader for an agy conversation.
//
// agy keeps a conversation in conversations/<id>.db as protobuf blobs that
// Google does not document, and agy updates itself daily. Mewla therefore does
// not read those files for content. It asks agy: `agy acp` speaks the Agent
// Client Protocol (a public, versioned JSON-RPC protocol over stdio), and its
// `session/load` replays a stored conversation as `session/update`
// notifications. Only the conversation id Mewla owns is ever asked for;
// `session/list`, which names every conversation of the person, is never sent.
//
// The database file is used for one thing only: its size and modification time
// say that the conversation changed, so an unchanged conversation is not
// replayed again.
//
// When agy no longer answers the way this reader expects (no `agy` on PATH, a
// different protocol version, `session/load` gone or failing, a reply that is
// not JSON-RPC) the conversation is reported unavailable with
// antigravityUnreadableReason and the cause in ReasonDetail. It is never an
// error that crashes the reader and never an empty chat that looks like a
// conversation with nothing in it.

const (
	antigravityConversationSource = "antigravity_acp"
	// antigravityUnreadableReason starts CodexConversation.Reason when agy
	// cannot replay the conversation: "conversation_unreadable: <why>". The
	// reason is the one string every client already carries, so the cause
	// reaches the person without a new field on the wire.
	antigravityUnreadableReason = "conversation_unreadable"

	antigravityACPProtocolVersion = 1
	antigravityACPTimeout         = 30 * time.Second
	antigravityACPMaxBytes        = 64 << 20
	// antigravityACPRetryAfter is how long a failed replay is remembered, so a
	// broken agy is not started on every poll.
	antigravityACPRetryAfter = 10 * time.Second
	maxAntigravityToolOutput = 8000
)

// antigravityUnreadableError is a replay failure with the words a person sees.
type antigravityUnreadableError struct{ detail string }

func (e *antigravityUnreadableError) Error() string { return e.detail }

type antigravityACPUpdate struct {
	Kind       string          `json:"sessionUpdate"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Title      string          `json:"title"`
	Status     string          `json:"status"`
	ToolKind   string          `json:"kind"`
	Args       json.RawMessage `json:"args"`
	RawInput   json.RawMessage `json:"rawInput"`
	RawOutput  json.RawMessage `json:"rawOutput"`
	Locations  []struct {
		Path string `json:"path"`
	} `json:"locations"`
}

type antigravityACPMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// antigravityBinary finds agy the way the Session that runs it does.
func antigravityBinary() (string, error) {
	path, err := exec.LookPath("agy")
	if err != nil {
		return "", &antigravityUnreadableError{detail: "agy is not on PATH, so its conversation cannot be read"}
	}
	return path, nil
}

// replayAntigravityConversation asks `agy acp` to load one conversation and
// returns the updates it streams.
func replayAntigravityConversation(binary, cwd, id string) ([]antigravityACPUpdate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), antigravityACPTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "acp")
	if info, err := os.Stat(cwd); err == nil && info.IsDir() {
		cmd.Dir = cwd
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, &antigravityUnreadableError{detail: "agy acp did not start: " + err.Error()}
	}
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()

	reader := bufio.NewReaderSize(stdout, 1<<20)
	var read int
	next := func() (antigravityACPMessage, error) {
		for {
			line, err := reader.ReadBytes('\n')
			read += len(line)
			if read > antigravityACPMaxBytes {
				return antigravityACPMessage{}, &antigravityUnreadableError{detail: "the conversation is larger than Mewla reads (64 MiB)"}
			}
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				var message antigravityACPMessage
				if json.Unmarshal(trimmed, &message) == nil && (message.Method != "" || len(message.ID) > 0) {
					return message, nil
				}
				// A line that is not a JSON-RPC message is not part of the
				// protocol; a stream of nothing else ends at EOF below.
			}
			if err != nil {
				if ctx.Err() != nil {
					return antigravityACPMessage{}, &antigravityUnreadableError{detail: "agy acp did not answer within " + antigravityACPTimeout.String()}
				}
				return antigravityACPMessage{}, &antigravityUnreadableError{detail: "agy acp stopped before it answered"}
			}
		}
	}
	send := func(id int, method string, params any) error {
		payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			return err
		}
		_, err = stdin.Write(append(payload, '\n'))
		return err
	}
	isResponse := func(message antigravityACPMessage, id int) bool {
		return message.Method == "" && strings.TrimSpace(string(message.ID)) == strconv.Itoa(id)
	}
	// await returns the response to request id, handing every notification to
	// onNotification and refusing requests agy makes of the client.
	await := func(id int, onNotification func(antigravityACPMessage)) (antigravityACPMessage, error) {
		for {
			message, err := next()
			if err != nil {
				return antigravityACPMessage{}, err
			}
			switch {
			case isResponse(message, id):
				return message, nil
			case message.Method != "" && len(message.ID) > 0:
				refusal, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": json.RawMessage(message.ID),
					"error": map[string]any{"code": -32601, "message": "method not supported"},
				})
				_, _ = stdin.Write(append(refusal, '\n'))
			case message.Method != "" && onNotification != nil:
				onNotification(message)
			}
		}
	}

	if err := send(1, "initialize", map[string]any{
		"protocolVersion":    antigravityACPProtocolVersion,
		"clientCapabilities": map[string]any{},
	}); err != nil {
		return nil, &antigravityUnreadableError{detail: "agy acp did not accept input: " + err.Error()}
	}
	response, err := await(1, nil)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, &antigravityUnreadableError{detail: "agy acp refused initialize: " + response.Error.Message}
	}
	var initialized struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(response.Result, &initialized) != nil || initialized.ProtocolVersion == 0 {
		return nil, &antigravityUnreadableError{detail: "agy acp's initialize reply is not the Agent Client Protocol"}
	}
	if initialized.ProtocolVersion != antigravityACPProtocolVersion {
		return nil, &antigravityUnreadableError{detail: fmt.Sprintf(
			"agy acp speaks protocol version %d; Mewla reads version %d", initialized.ProtocolVersion, antigravityACPProtocolVersion)}
	}
	if !initialized.AgentCapabilities.LoadSession {
		return nil, &antigravityUnreadableError{detail: "this agy no longer offers session/load"}
	}

	var updates []antigravityACPUpdate
	if err := send(2, "session/load", map[string]any{"sessionId": id, "cwd": cwd, "mcpServers": []any{}}); err != nil {
		return nil, &antigravityUnreadableError{detail: "agy acp did not accept input: " + err.Error()}
	}
	response, err = await(2, func(message antigravityACPMessage) {
		if message.Method != "session/update" {
			return
		}
		var params struct {
			SessionID string               `json:"sessionId"`
			Update    antigravityACPUpdate `json:"update"`
		}
		if json.Unmarshal(message.Params, &params) != nil || (params.SessionID != "" && params.SessionID != id) {
			return
		}
		updates = append(updates, params.Update)
	})
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		// agy answers an id it does not know with an empty success, so an error
		// (even "not found") for a conversation whose file exists is a replay
		// that failed, not a conversation that is missing.
		text := strings.TrimSpace(response.Error.Message)
		return nil, &antigravityUnreadableError{detail: "agy could not load the conversation: " + firstNonEmpty(text, "error "+strconv.Itoa(response.Error.Code))}
	}
	return updates, nil
}

var antigravityExitCodePattern = regexp.MustCompile(`Command exited with code (-?\d+)`)

// projectAntigravityConversation turns the updates of a replayed conversation
// into the events of the Brain chat. Consecutive chunks of one kind are one
// message; a tool call and its later updates are one event.
func projectAntigravityConversation(id, cwd, path string, updates []antigravityACPUpdate) CodexConversation {
	conversation := CodexConversation{
		Available: true, Source: antigravityConversationSource, SessionID: id, CWD: cwd, Path: path,
		Events: []CodexConversationEvent{},
	}
	var (
		chunkKind string
		chunkAt   = -1
		tools     = map[string]int{}
		users     int
	)
	appendEvent := func(event CodexConversationEvent) int {
		event.Seq = len(conversation.Events) + 1
		event.Source = antigravityConversationSource
		conversation.Events = append(conversation.Events, event)
		return len(conversation.Events) - 1
	}
	for _, update := range updates {
		switch update.Kind {
		case "user_message_chunk", "agent_message_chunk", "agent_thought_chunk":
			text := antigravityChunkText(update.Content)
			if text == "" {
				continue
			}
			if chunkKind == update.Kind && chunkAt >= 0 {
				conversation.Events[chunkAt].Body += text
				continue
			}
			event := CodexConversationEvent{Body: text}
			switch update.Kind {
			case "user_message_chunk":
				users++
				event.ID = fmt.Sprintf("%s:user:%d", id, users)
				event.Kind, event.Role = "user_message", "user"
			case "agent_thought_chunk":
				event.ID = fmt.Sprintf("%s:reasoning:%d", id, len(conversation.Events)+1)
				event.Kind, event.Role = "reasoning", "assistant"
			default:
				event.ID = fmt.Sprintf("%s:assistant:%d", id, len(conversation.Events)+1)
				event.Kind, event.Role = "assistant_message", "assistant"
			}
			chunkKind, chunkAt = update.Kind, appendEvent(event)
		case "tool_call", "tool_call_update":
			chunkKind, chunkAt = "", -1
			callID := strings.TrimSpace(update.ToolCallID)
			if callID == "" {
				continue
			}
			index, known := tools[callID]
			if !known {
				index = appendEvent(CodexConversationEvent{ID: id + ":tool:" + callID, Kind: "tool_call", CallID: callID})
				tools[callID] = index
			}
			event := &conversation.Events[index]
			event.ToolName = firstNonEmpty(update.Name, event.ToolName)
			event.Title = firstNonEmpty(update.Title, event.Title)
			if input := firstNonEmptyRaw(update.Args, update.RawInput); input != "" {
				event.Input = input
				var args map[string]any
				if json.Unmarshal([]byte(input), &args) == nil {
					if command, ok := args["CommandLine"].(string); ok && update.ToolKind != "edit" {
						event.Command = command
					}
				}
			}
			for _, location := range update.Locations {
				if location.Path != "" && !containsString(event.Files, location.Path) {
					event.Files = append(event.Files, location.Path)
				}
			}
			if output := antigravityToolOutput(update); output != "" {
				event.Output = truncateRunes(output, maxAntigravityToolOutput)
				if match := antigravityExitCodePattern.FindStringSubmatch(output); match != nil {
					if code, err := strconv.Atoi(match[1]); err == nil {
						event.ExitCode = &code
					}
				}
			}
			switch strings.ToLower(strings.TrimSpace(update.Status)) {
			case "completed":
				event.Status = "completed"
				if event.ExitCode != nil && *event.ExitCode != 0 {
					event.Status = "failed"
				}
			case "failed":
				event.Status = "failed"
			default:
				event.Status = "running"
			}
			event.Partial = event.Status == "running"
		default:
			// usage_update, plan, available_commands_update and any kind a later
			// agy adds are not chat rows.
		}
	}
	for index := range conversation.Events {
		event := &conversation.Events[index]
		if event.Kind == "user_message" {
			event.AdmissionSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(event.Body)))
		}
	}
	return conversation
}

func antigravityChunkText(raw json.RawMessage) string {
	var content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &content) != nil || content.Type != "text" {
		return ""
	}
	return content.Text
}

func antigravityToolOutput(update antigravityACPUpdate) string {
	var output struct {
		Output string `json:"output"`
	}
	if json.Unmarshal(update.RawOutput, &output) == nil && strings.TrimSpace(output.Output) != "" {
		return output.Output
	}
	var blocks []struct {
		Type    string `json:"type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(update.Content, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if block.Type == "content" && strings.TrimSpace(block.Content.Text) != "" {
				parts = append(parts, block.Content.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func firstNonEmptyRaw(values ...json.RawMessage) string {
	for _, value := range values {
		if trimmed := bytes.TrimSpace(value); len(trimmed) > 0 && string(trimmed) != "null" && string(trimmed) != "{}" {
			return string(trimmed)
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// antigravityConversationPath is the file agy stores one conversation in. Mewla
// never reads its content; it is the identity of the conversation and the
// change marker of the replay.
func antigravityConversationPath(home, id string) string {
	return filepath.Join(AntigravityDataDir(home), "conversations", id+".db")
}

// antigravityConversationStat fingerprints the conversation file and the size
// of its write-ahead log, which agy appends to while the file itself stays
// unchanged until it checkpoints. The log's and the shared-memory file's times
// are deliberately left out: opening the database to replay it touches them, so
// a replay would change the fingerprint and start the next replay, forever.
func antigravityConversationStat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if wal, err := os.Stat(path + "-wal"); err == nil {
		size += wal.Size()
	}
	return fingerprintedFileInfo{FileInfo: info, size: size, modTime: info.ModTime()}, nil
}

var antigravityReplayFailures = struct {
	mu      sync.Mutex
	entries map[string]antigravityReplayFailure
}{entries: map[string]antigravityReplayFailure{}}

type antigravityReplayFailure struct {
	at     time.Time
	detail string
}

func recentAntigravityFailure(id string, now time.Time) (string, bool) {
	antigravityReplayFailures.mu.Lock()
	defer antigravityReplayFailures.mu.Unlock()
	failure, ok := antigravityReplayFailures.entries[id]
	if !ok || now.Sub(failure.at) > antigravityACPRetryAfter {
		return "", false
	}
	return failure.detail, true
}

func recordAntigravityFailure(id, detail string, now time.Time) {
	antigravityReplayFailures.mu.Lock()
	defer antigravityReplayFailures.mu.Unlock()
	if len(antigravityReplayFailures.entries) > 64 {
		antigravityReplayFailures.entries = map[string]antigravityReplayFailure{}
	}
	antigravityReplayFailures.entries[id] = antigravityReplayFailure{at: now, detail: detail}
}

func clearAntigravityFailure(id string) {
	antigravityReplayFailures.mu.Lock()
	defer antigravityReplayFailures.mu.Unlock()
	delete(antigravityReplayFailures.entries, id)
}

func unreadableAntigravityConversation(id, path, detail string) CodexConversation {
	return CodexConversation{
		Available: false,
		Reason:    antigravityUnreadableReason + ": " + detail,
		Source:    antigravityConversationSource,
		SessionID: id,
		Path:      path,
		Events:    []CodexConversationEvent{},
	}
}

// parseAntigravityConversation replays the conversation stored at path (the
// file's name is the conversation id). Failures return an error whose text is
// the reason, so they are not kept by the parse cache.
func parseAntigravityConversation(path string) (CodexConversation, error) {
	id := strings.TrimSuffix(filepath.Base(path), ".db")
	if !antigravityConversationIDPattern.MatchString(id) {
		return CodexConversation{}, &antigravityUnreadableError{detail: "not an agy conversation id"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return CodexConversation{}, &antigravityUnreadableError{detail: "no home directory to find agy's conversations"}
	}
	cwd, preview := "", ""
	if summary, ok, _ := GetAntigravityConversation(home, id); ok {
		preview = summary.Preview
		if len(summary.CWDs) > 0 {
			cwd = summary.CWDs[0]
		}
	}
	binary, err := antigravityBinary()
	if err != nil {
		return CodexConversation{}, err
	}
	updates, err := replayAntigravityConversation(binary, cwd, id)
	if err != nil && retryableAntigravityReplay(err) {
		// agy writes the conversation while it runs a turn; a replay that lands
		// in the middle of that can fail once and succeed a moment later.
		time.Sleep(antigravityACPRetryDelay)
		updates, err = replayAntigravityConversation(binary, cwd, id)
	}
	if err != nil {
		return CodexConversation{}, err
	}
	conversation := projectAntigravityConversation(id, cwd, path, updates)
	// agy answers a replay of an id it does not know, or of a conversation in a
	// shape this reader does not know, with success and nothing in it. A chat
	// that is empty although agy says the conversation has had a turn is a
	// reader that no longer understands agy, never a conversation with nothing
	// in it.
	if len(conversation.Events) == 0 && (len(updates) > 0 || strings.TrimSpace(preview) != "") {
		return CodexConversation{}, &antigravityUnreadableError{detail: fmt.Sprintf(
			"agy replayed %d updates and none of them is a message Mewla knows", len(updates))}
	}
	return conversation, nil
}

// antigravityACPRetryDelay is the pause before a failed replay is tried once more.
var antigravityACPRetryDelay = 750 * time.Millisecond

// retryableAntigravityReplay says a failed replay may be transient: agy stopped
// or failed while loading. A different protocol, no session/load or no agy is
// the same on the next try.
func retryableAntigravityReplay(err error) bool {
	var unreadable *antigravityUnreadableError
	if !errors.As(err, &unreadable) {
		return false
	}
	for _, permanent := range []string{"not on PATH", "protocol version", "session/load", "not the Agent Client Protocol", "refused initialize", "larger than Mewla reads"} {
		if strings.Contains(unreadable.detail, permanent) {
			return false
		}
	}
	return true
}

// loadAntigravityConversationByID reads the conversation agy stored under id.
func (r *ProviderConversationReader) loadAntigravityConversationByID(id string) (CodexConversation, error) {
	id = strings.TrimSpace(id)
	if !antigravityConversationIDPattern.MatchString(id) {
		r.resetSource()
		return CodexConversation{Reason: "transcript_not_found", Events: []CodexConversationEvent{}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return unreadableAntigravityConversation(id, "", "no home directory to find agy's conversations"), nil
	}
	path := antigravityConversationPath(home, id)
	if _, err := os.Stat(path); err != nil {
		r.resetSource()
		return CodexConversation{Reason: "transcript_not_found", SessionID: id, Events: []CodexConversationEvent{}}, nil
	}
	now := time.Now()
	if detail, failed := recentAntigravityFailure(id, now); failed {
		r.resetSource()
		return unreadableAntigravityConversation(id, path, detail), nil
	}
	conversation, err := r.loadFileConversationStat(WorkerProviderAntigravity, path, parseAntigravityConversation, antigravityConversationStat)
	if err != nil {
		detail := err.Error()
		var unreadable *antigravityUnreadableError
		if !errors.As(err, &unreadable) {
			detail = "reading the conversation failed: " + detail
		}
		recordAntigravityFailure(id, detail, now)
		log.Printf("WARN agy conversation %s cannot be read: %s", id, detail)
		return unreadableAntigravityConversation(id, path, detail), nil
	}
	clearAntigravityFailure(id)
	conversation = withOwnEvents(conversation)
	var modified time.Time
	if info, err := antigravityConversationStat(path); err == nil {
		modified = info.ModTime()
	}
	conversation.Activity = observeAntigravityConversation(home, id, &conversation, modified, time.Now())
	if conversation.Activity != nil && conversation.Activity.Status == ProviderActivityRunning {
		if last := len(conversation.Events) - 1; last >= 0 && conversation.Events[last].Kind == "assistant_message" {
			conversation.Events[last].Partial = true
		}
	}
	return conversation, nil
}

// antigravityObservation is what Mewla has seen of one conversation in this
// process. The replay carries no times, and agy's conversation_summaries.db
// moves its last-modified time only now and then (not during a short turn), so
// the times Brain needs are the ones Mewla saw things happen: when an event was
// first in a replay, and when a turn was first seen running and first seen
// settled.
type antigravityObservation struct {
	eventAt map[string]string
	started map[string]string
	settled map[string]string
}

var antigravityObservations = struct {
	mu     sync.Mutex
	byConv map[string]*antigravityObservation
}{byConv: map[string]*antigravityObservation{}}

// observeAntigravityConversation stamps the events with the time they were
// first seen and returns the Activity of the newest turn from agy's own run
// status. History seen for the first time (a daemon that has just started)
// carries the time the conversation was last written, the latest it can have
// happened, so a restart does not make old messages look new.
func observeAntigravityConversation(home, id string, conversation *CodexConversation, modified, now time.Time) *ProviderActivity {
	antigravityObservations.mu.Lock()
	defer antigravityObservations.mu.Unlock()
	obs, warm := antigravityObservations.byConv[id]
	if !warm {
		if len(antigravityObservations.byConv) >= 64 {
			antigravityObservations.byConv = map[string]*antigravityObservation{}
		}
		obs = &antigravityObservation{eventAt: map[string]string{}, started: map[string]string{}, settled: map[string]string{}}
		antigravityObservations.byConv[id] = obs
	}
	stamp := now
	if !warm && !modified.IsZero() && modified.Before(now) {
		stamp = modified
	}
	at := stamp.UTC().Format(time.RFC3339Nano)
	turns := 0
	for index := range conversation.Events {
		event := &conversation.Events[index]
		if event.Kind == "user_message" {
			turns++
		}
		seen, ok := obs.eventAt[event.ID]
		if !ok {
			seen = at
			obs.eventAt[event.ID] = seen
		}
		event.Timestamp = seen
	}
	if turns == 0 {
		return nil
	}
	summary, ok, err := GetAntigravityConversation(home, id)
	if err != nil || !ok {
		return nil
	}
	activityID := fmt.Sprintf("%s:turn:%d", id, turns)
	started, ok := obs.started[activityID]
	if !ok {
		started = at
		obs.started[activityID] = started
	}
	activity := &ProviderActivity{ID: activityID, Status: ProviderActivityRunning, StartedAt: started}
	switch {
	case summary.Killed:
		activity.Status = ProviderActivityInterrupted
	case summary.Idle():
		activity.Status = ProviderActivityCompleted
	}
	if activity.Status == ProviderActivityRunning {
		delete(obs.settled, activityID)
		return activity
	}
	settled, ok := obs.settled[activityID]
	if !ok {
		settled = at
		obs.settled[activityID] = settled
	}
	activity.SettledAt = settled
	return activity
}

// loadAntigravityConversationForWorker finds the conversation a live agy
// Session is in: the id on its command line when Mewla resumed it, otherwise
// the newest conversation in its folder written since it launched.
func (r *ProviderConversationReader) loadAntigravityConversationForWorker(worker classifier.Worker, now time.Time) (CodexConversation, error) {
	if token, present, err := ProviderResumeToken(WorkerProviderAntigravity, worker.Command); err == nil && present {
		return r.loadAntigravityConversationByID(token)
	}
	if strings.TrimSpace(worker.Cwd) == "" {
		r.resetSource()
		return CodexConversation{Reason: "missing_cwd", Events: []CodexConversationEvent{}}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return unreadableAntigravityConversation("", "", "no home directory to find agy's conversations"), nil
	}
	found, ok, err := FindAntigravityConversation(home, worker.Cwd, worker.StartedAt)
	if err != nil {
		return unreadableAntigravityConversation("", "", "agy's conversation index could not be read: "+err.Error()), nil
	}
	if !ok {
		r.resetSource()
		return CodexConversation{Reason: "transcript_not_found", Events: []CodexConversationEvent{}}, nil
	}
	return r.loadAntigravityConversationByID(found.ID)
}

// resolveAntigravityTranscriptIdentity binds a live agy Session to its
// conversation: the id on its command line when Mewla resumed it, otherwise the
// newest conversation in its folder written since it launched. Unbound until
// agy has stored the first turn.
func resolveAntigravityTranscriptIdentity(worker classifier.Worker) HostTranscriptIdentity {
	unbound := HostTranscriptIdentity{Provider: WorkerProviderAntigravity}
	home, err := os.UserHomeDir()
	if err != nil {
		return unbound
	}
	bound := func(id string) (HostTranscriptIdentity, bool) {
		if !antigravityConversationIDPattern.MatchString(id) {
			return unbound, false
		}
		path := antigravityConversationPath(home, id)
		if _, err := os.Stat(path); err != nil {
			return unbound, false
		}
		return HostTranscriptIdentity{Provider: WorkerProviderAntigravity, SessionID: id, Path: path}, true
	}
	if token, present, err := ProviderResumeToken(WorkerProviderAntigravity, worker.Command); err == nil && present {
		if identity, ok := bound(token); ok {
			return identity
		}
		// agy starts a new conversation for an id it does not know.
	}
	if strings.TrimSpace(worker.Cwd) != "" {
		if found, ok, err := FindAntigravityConversation(home, worker.Cwd, worker.StartedAt); err == nil && ok {
			if identity, ok := bound(found.ID); ok {
				return identity
			}
		}
	}
	return unbound
}
