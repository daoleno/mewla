package server

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// A Stop that ended the commands its turn had started says so in one line of
// the Session's thread, under the interrupted turn (source commands_stopped).
// The lines are kept in memory: a daemon restart drops them.

const (
	stoppedCommandsSource = "commands_stopped"
	// stoppedCommandsKept bounds the lines kept across every Session.
	stoppedCommandsKept = 512
	// stoppedCommandShown bounds how much of one command the line shows.
	stoppedCommandShown = 80
)

// StoppedCommand is one command a Stop found its turn had started.
type StoppedCommand struct {
	Command string
	Ended   bool
	// Reason says why a command that did not end could not be stopped.
	Reason string
}

// StoppedCommands is what one Stop did to the commands its turn started.
type StoppedCommands struct {
	SessionID string
	TurnID    string
	At        time.Time
	Commands  []StoppedCommand
}

type stoppedCommandsLog struct {
	mu      sync.Mutex
	records []StoppedCommands
	version uint64
}

func (l *stoppedCommandsLog) add(record StoppedCommands) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, record)
	if len(l.records) > stoppedCommandsKept {
		l.records = append([]StoppedCommands(nil), l.records[len(l.records)-stoppedCommandsKept:]...)
	}
	l.version++
}

func (l *stoppedCommandsLog) of(sessionID string) []StoppedCommands {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []StoppedCommands
	for _, record := range l.records {
		if record.SessionID == sessionID {
			out = append(out, record)
		}
	}
	return out
}

func (l *stoppedCommandsLog) Version() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version
}

// RecordStoppedCommands keeps what a Stop did to its turn's commands, for the
// Session's thread. A Stop that found none is not a line.
func (s *Server) RecordStoppedCommands(record StoppedCommands) {
	record.SessionID = strings.TrimSpace(record.SessionID)
	if record.SessionID == "" || len(record.Commands) == 0 {
		return
	}
	if record.At.IsZero() {
		record.At = time.Now()
	}
	s.stopped.add(record)
}

// withStoppedCommands adds a Session's Stop lines to its thread.
func (s *Server) withStoppedCommands(sessionID string, conversation work.CodexConversation) work.CodexConversation {
	records := s.stopped.of(strings.TrimSpace(sessionID))
	if len(records) == 0 {
		return conversation
	}
	events := make([]work.CodexConversationEvent, 0, len(conversation.Events)+len(records))
	events = append(events, conversation.Events...)
	for _, record := range records {
		events = append(events, stoppedCommandsEvent(record))
	}
	conversation.Events = events
	return conversation
}

// stoppedCommandsEvent is a Stop's line: a status row whose title says it.
func stoppedCommandsEvent(record StoppedCommands) work.CodexConversationEvent {
	status := "stopped"
	for _, command := range record.Commands {
		if !command.Ended {
			status = "failed"
		}
	}
	return work.CodexConversationEvent{
		ID:        "stopped:" + record.SessionID + ":" + record.TurnID + ":" + strconv.FormatInt(record.At.UnixMilli(), 10),
		Timestamp: record.At.UTC().Format(time.RFC3339Nano),
		Kind:      "status",
		Role:      "system",
		Title:     StoppedCommandsLine(record.Commands),
		Status:    status,
		Source:    stoppedCommandsSource,
	}
}

// StoppedCommandsLine says in one line what a Stop did to the commands its
// turn had started: "Stopped 1 command this turn started: seq 1 30", and for
// any it could not end "1 command could not be stopped: … (why)".
func StoppedCommandsLine(commands []StoppedCommand) string {
	var ended, left []string
	for _, command := range commands {
		shown := shownCommand(command.Command)
		if command.Ended {
			ended = append(ended, shown)
			continue
		}
		if reason := strings.TrimSpace(command.Reason); reason != "" {
			shown += " (" + reason + ")"
		}
		left = append(left, shown)
	}
	var parts []string
	if len(ended) > 0 {
		parts = append(parts, fmt.Sprintf("Stopped %s this turn started: %s", countCommands(len(ended)), strings.Join(ended, ", ")))
	}
	if len(left) > 0 {
		parts = append(parts, fmt.Sprintf("%s could not be stopped: %s", countCommands(len(left)), strings.Join(left, ", ")))
	}
	return strings.Join(parts, " · ")
}

func countCommands(n int) string {
	if n == 1 {
		return "1 command"
	}
	return strconv.Itoa(n) + " commands"
}

// shownCommand is the command's first line, cut to stoppedCommandShown.
func shownCommand(command string) string {
	command = strings.TrimSpace(command)
	if line, _, multi := strings.Cut(command, "\n"); multi {
		command = strings.TrimSpace(line) + " …"
	}
	if runes := []rune(command); len(runes) > stoppedCommandShown {
		command = string(runes[:stoppedCommandShown-1]) + "…"
	}
	if command == "" {
		return "a command"
	}
	return command
}
