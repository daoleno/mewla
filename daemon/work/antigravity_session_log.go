package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// antigravitySessionSource names a conversation read from an Antigravity
// Session's own log (antigravity_session.go), not replayed from agy.
const antigravitySessionSource = "antigravity_session"

// loadAntigravitySessionForWorker reads the log of the Session program a
// Worker runs.
func (r *ProviderConversationReader) loadAntigravitySessionForWorker(worker classifier.Worker) (CodexConversation, error) {
	id := AntigravitySessionID(worker.Command)
	path := AntigravitySessionLogForCommand(worker.Command)
	if path == "" {
		r.resetSource()
		return CodexConversation{Reason: "transcript_not_found", SessionID: id, Events: []CodexConversationEvent{}}, nil
	}
	if _, err := os.Stat(path); err != nil {
		r.resetSource()
		return CodexConversation{Reason: "transcript_not_found", SessionID: id, Events: []CodexConversationEvent{}}, nil
	}
	conversation, err := r.loadFileConversation(WorkerProviderAntigravity, path, ParseAntigravitySessionLog)
	if err != nil {
		return CodexConversation{}, err
	}
	conversation.CWD = firstNonEmpty(conversation.CWD, strings.TrimSpace(worker.Cwd))
	return conversation, nil
}

// AntigravitySessionTurn is one turn of a Session's log: the message that
// started it, how it ended and what it used.
type AntigravitySessionTurn struct {
	ID        string
	StartedAt time.Time
	SettledAt time.Time
	Stop      string
	Error     string
	Usage     AntigravityUsage
	Model     string
}

// AntigravityUsage is the token count agy reports with a turn's end.
type AntigravityUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedReadTokens  int64 `json:"cachedReadTokens"`
	CachedWriteTokens int64 `json:"cachedWriteTokens"`
	ThoughtTokens     int64 `json:"thoughtTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

// ReadAntigravitySessionLog reads every record of a log, oldest first. A line
// that is not a record (a write cut short) is skipped.
func ReadAntigravitySessionLog(path string) ([]AntigravityLogRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []AntigravityLogRecord
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && err == nil {
			var record AntigravityLogRecord
			if json.Unmarshal(trimmed, &record) == nil && record.T != "" {
				records = append(records, record)
			}
		}
		if err != nil {
			break
		}
	}
	return records, nil
}

// AntigravitySessionTurns reads the turns of a log, for usage and Stats.
func AntigravitySessionTurns(records []AntigravityLogRecord) []AntigravitySessionTurn {
	var turns []AntigravitySessionTurn
	model := ""
	for _, record := range records {
		at, _ := time.Parse(time.RFC3339Nano, record.At)
		switch record.T {
		case "session":
			model = firstNonEmpty(record.Model, model)
		case "user":
			turns = append(turns, AntigravitySessionTurn{ID: record.Turn, StartedAt: at, Model: model})
		case "turn_end":
			for index := len(turns) - 1; index >= 0; index-- {
				if turns[index].ID == record.Turn && turns[index].SettledAt.IsZero() {
					turns[index].SettledAt, turns[index].Stop, turns[index].Error = at, record.Stop, record.Error
					if len(record.Usage) > 0 {
						_ = json.Unmarshal(record.Usage, &turns[index].Usage)
					}
					break
				}
			}
		}
	}
	return turns
}

// ParseAntigravitySessionLog reads a Session's log as its conversation: every
// message and tool call with the time it happened, and the Activity of the
// newest turn from the turn's own start and end.
func ParseAntigravitySessionLog(path string) (CodexConversation, error) {
	records, err := ReadAntigravitySessionLog(path)
	if err != nil {
		return CodexConversation{}, err
	}
	return projectAntigravitySessionLog(path, records), nil
}

func projectAntigravitySessionLog(path string, records []AntigravityLogRecord) CodexConversation {
	var (
		updates      []antigravityACPUpdate
		times        []string
		conversation string
		users        int
		lastTurn     string
		lastStart    string
		lastEnd      *AntigravityLogRecord
		lastExit     *AntigravityLogRecord
	)
	boundary := func(at string) {
		updates = append(updates, antigravityACPUpdate{Kind: antigravityBoundary})
		times = append(times, at)
	}
	for index := range records {
		record := records[index]
		switch record.T {
		case "session":
			conversation = firstNonEmpty(record.Conversation, conversation)
		case "user":
			users++
			boundary(record.At)
			content, _ := json.Marshal(map[string]string{"type": "text", "text": record.Text})
			updates = append(updates, antigravityACPUpdate{Kind: "user_message_chunk", Content: content})
			times = append(times, record.At)
			boundary(record.At)
			lastTurn, lastStart, lastEnd, lastExit = record.Turn, record.At, nil, nil
		case "update":
			var update antigravityACPUpdate
			if json.Unmarshal(record.Update, &update) != nil {
				continue
			}
			updates = append(updates, update)
			times = append(times, record.At)
		case "turn_end":
			if record.Turn == lastTurn {
				copied := record
				lastEnd = &copied
			}
			boundary(record.At)
		case "agent_exit":
			copied := record
			lastExit = &copied
			boundary(record.At)
		}
	}
	id := firstNonEmpty(conversation, strings.TrimSuffix(filepathBase(path), ".jsonl"))
	conversation = id
	projected := projectAntigravityUpdates(id, "", path, updates, times)
	projected.Source = antigravitySessionSource
	for index := range projected.Events {
		projected.Events[index].Source = antigravitySessionSource
	}
	projected.SessionID = conversation
	if users > 0 && lastTurn != "" {
		activity := &ProviderActivity{ID: lastTurn, Status: ProviderActivityRunning, StartedAt: lastStart}
		switch {
		case lastEnd != nil:
			activity.SettledAt = lastEnd.At
			switch lastEnd.Stop {
			case "end_turn", "max_tokens", "max_turn_requests":
				activity.Status = ProviderActivityCompleted
			case "cancelled":
				activity.Status = ProviderActivityInterrupted
			case "refusal":
				activity.Status = ProviderActivityFailed
				activity.Error = &ProviderActivityError{Code: "refusal", Message: "agy refused the request"}
			default:
				activity.Status = ProviderActivityFailed
				activity.Error = &ProviderActivityError{Code: firstNonEmpty(lastEnd.Stop, "error"), Message: lastEnd.Error}
			}
		case lastExit != nil && lastExit.At >= lastStart:
			activity.Status, activity.SettledAt = ProviderActivityFailed, lastExit.At
			activity.Error = &ProviderActivityError{Code: "agent_exit", Message: lastExit.Error}
		}
		projected.Activity = activity
		if activity.Status == ProviderActivityRunning {
			if last := len(projected.Events) - 1; last >= 0 && projected.Events[last].Kind == "assistant_message" {
				projected.Events[last].Partial = true
			}
		}
	}
	if len(records) > 0 {
		if updated, err := time.Parse(time.RFC3339Nano, records[len(records)-1].At); err == nil {
			projected.Updated = &updated
		}
	}
	return projected
}

func filepathBase(path string) string {
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		return path[index+1:]
	}
	return path
}

// antigravitySessionProvider is the provider label of a Session command.
func antigravitySessionProvider(command string) string {
	if IsAntigravitySessionCommand(command) {
		return WorkerProviderAntigravity
	}
	return ""
}
