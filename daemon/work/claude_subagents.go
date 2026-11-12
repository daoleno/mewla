package work

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Claude Code keeps a subagent's own transcript beside the conversation:
//
//	<projects>/<cwd>/<sessionId>.jsonl                      the conversation
//	<projects>/<cwd>/<sessionId>/subagents/agent-<id>.jsonl  one per subagent
//	<projects>/<cwd>/<sessionId>/subagents/agent-<id>.meta.json
//
// Every record of a subagent's transcript is marked isSidechain, which is why
// the conversation leaves them out. The meta file names the tool call that
// started it (toolUseId), what it was asked (description) and its kind, and
// says finished once it has stopped. Attaching the steps to that call lets the
// apps show a subagent's work under the row that started it.

// maxClaudeSubagentSteps bounds the steps one subagent carries to the client.
const maxClaudeSubagentSteps = 200

// maxClaudeSubagentDepth bounds how far a subagent's own subagents are followed.
const maxClaudeSubagentDepth = 3

type claudeSubagentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
	Finished    bool   `json:"finished"`
}

type claudeSubagent struct {
	meta     claudeSubagentMeta
	agentID  string
	path     string
	subagent *CodexConversationSubagent
}

func claudeSubagentsDir(transcriptPath string) string {
	stem := strings.TrimSuffix(transcriptPath, filepath.Ext(transcriptPath))
	return filepath.Join(stem, "subagents")
}

// claudeSubagentFiles lists the subagent transcripts of a conversation.
func claudeSubagentFiles(transcriptPath string) []string {
	files, err := filepath.Glob(filepath.Join(claudeSubagentsDir(transcriptPath), "agent-*.jsonl"))
	if err != nil {
		return nil
	}
	return files
}

// claudeTranscriptStat is os.Stat for a conversation, with the subagent
// transcripts folded in: a subagent works while the conversation file is quiet,
// so a cached parse must go stale when only they change.
func claudeTranscriptStat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	files := claudeSubagentFiles(path)
	if len(files) == 0 {
		return info, nil
	}
	size, modTime := info.Size(), info.ModTime()
	for _, file := range files {
		sub, err := os.Stat(file)
		if err != nil {
			continue
		}
		size += sub.Size()
		if sub.ModTime().After(modTime) {
			modTime = sub.ModTime()
		}
	}
	for _, file := range files {
		if meta, err := os.Stat(strings.TrimSuffix(file, ".jsonl") + ".meta.json"); err == nil {
			size += meta.Size()
			if meta.ModTime().After(modTime) {
				modTime = meta.ModTime()
			}
		}
	}
	return fingerprintedFileInfo{FileInfo: info, size: size, modTime: modTime}, nil
}

// fingerprintedFileInfo is a transcript's file info whose size and time cover
// the files that belong to it.
type fingerprintedFileInfo struct {
	os.FileInfo
	size    int64
	modTime time.Time
}

func (f fingerprintedFileInfo) Size() int64        { return f.size }
func (f fingerprintedFileInfo) ModTime() time.Time { return f.modTime }

// underlying is the file itself, which is what identifies it.
func (f fingerprintedFileInfo) underlying() os.FileInfo { return f.FileInfo }

// attachClaudeSubagents puts each subagent's steps on the tool call that
// started it. A conversation with no subagents, or one whose transcripts cannot
// be read, is left as it is.
func attachClaudeSubagents(conversation *CodexConversation, transcriptPath string) {
	files := claudeSubagentFiles(transcriptPath)
	if len(files) == 0 || len(conversation.Events) == 0 {
		return
	}
	byCall := map[string]*claudeSubagent{}
	for _, file := range files {
		agent, ok := readClaudeSubagent(file)
		if ok {
			byCall[agent.meta.ToolUseID] = agent
		}
	}
	if len(byCall) == 0 {
		return
	}
	attachSubagentsToEvents(conversation.Events, byCall, map[string]bool{}, 0)
}

func attachSubagentsToEvents(
	events []CodexConversationEvent,
	byCall map[string]*claudeSubagent,
	used map[string]bool,
	depth int,
) {
	for index := range events {
		callID := events[index].CallID
		agent := byCall[callID]
		if callID == "" || agent == nil || used[callID] {
			continue
		}
		used[callID] = true
		subagent := *agent.subagent
		subagent.Steps = append([]CodexConversationEvent(nil), subagent.Steps...)
		if depth < maxClaudeSubagentDepth {
			attachSubagentsToEvents(subagent.Steps, byCall, used, depth+1)
		}
		events[index].Subagent = &subagent
	}
}

// readClaudeSubagent parses one subagent transcript and its meta file. A
// subagent file is read once and then only for the bytes it grew by: the
// conversation is parsed again whenever any file of it changes (a subagent
// works while the conversation is quiet), and re-reading every subagent each
// time was most of the daemon's CPU on a conversation with a few dozen of them
// (tens of MB each). See claude_subagent_cache.go.
func readClaudeSubagent(path string) (*claudeSubagent, bool) {
	return claudeSubagentCache.read(path)
}

// claudeSubagentOf is what a subagent's transcript, as the builder consumed
// it so far, and its meta file say.
func claudeSubagentOf(path string, meta claudeSubagentMeta, builder *claudeConversationBuilder) *claudeSubagent {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	agentID := strings.TrimPrefix(base, "agent-")
	finished := meta.Finished || claudeSubagentEndedOnAnswer(builder.events)
	steps := make([]CodexConversationEvent, 0, min(len(builder.events), maxClaudeSubagentSteps))
	for index := len(builder.events) - 1; index >= 0 && len(steps) < maxClaudeSubagentSteps; index-- {
		if claudeSubagentStep(builder.events[index], meta.ToolUseID) {
			steps = append(steps, builder.events[index])
		}
	}
	for left, right := 0, len(steps)-1; left < right; left, right = left+1, right-1 {
		steps[left], steps[right] = steps[right], steps[left]
	}
	return &claudeSubagent{
		meta:    meta,
		agentID: agentID,
		path:    path,
		subagent: &CodexConversationSubagent{
			AgentID:     agentID,
			Type:        strings.TrimSpace(meta.AgentType),
			Description: strings.TrimSpace(meta.Description),
			Finished:    finished,
			StartedAt:   builder.firstTimestamp,
			EndedAt:     builder.lastTimestamp,
			Steps:       steps,
		},
	}
}

// claudeSubagentStep keeps a subagent's tool calls. Its words are the answer
// the delegating call already carries, and the call that started a forked
// subagent is repeated at the head of its own transcript.
func claudeSubagentStep(event CodexConversationEvent, startedBy string) bool {
	switch event.Kind {
	case "command", "tool", "patch":
	default:
		return false
	}
	return startedBy == "" || event.CallID != startedBy
}

// claudeSubagentEndedOnAnswer is true when a transcript's last record is the
// subagent's reply and no call of its own is still running.
func claudeSubagentEndedOnAnswer(events []CodexConversationEvent) bool {
	if len(events) == 0 {
		return false
	}
	last := events[len(events)-1]
	if last.Kind != "assistant_message" || last.Partial {
		return false
	}
	for _, event := range events {
		if event.Status == "running" {
			return false
		}
	}
	return true
}
