package work

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/providerpaths"
)

// TranscriptSource is one provider-native conversation on this computer,
// found for offline collection rather than for a live Session subscription.
type TranscriptSource struct {
	Provider  string
	Path      string
	SessionID string
	CWD       string
	Size      int64
	ModTime   time.Time
}

// TranscriptMessage is one user or assistant message read from a transcript.
// ID is stable across scans: it never depends on where a scan started.
type TranscriptMessage struct {
	ID        string
	Role      string
	Text      string
	Timestamp time.Time
	// AdmissionSHA256 is the provider-native digest of a user message's exact
	// bytes; a match against a lifecycle admission means Mewla delivered it.
	AdmissionSHA256          string
	AdmissionUnwrappedSHA256 string
}

// TranscriptScan reports where a scan stopped and what it learned about the
// source. Offset is the next unread byte for streamed sources and the parsed
// size for whole-parse sources.
type TranscriptScan struct {
	Offset    int64
	SessionID string
	CWD       string
}

// TranscriptStreamed reports whether a provider's transcript is read
// incrementally from a byte offset. Other providers are parsed whole whenever
// they change, and callers deduplicate by message ID.
func TranscriptStreamed(provider string) bool {
	return provider == WorkerProviderClaude || provider == WorkerProviderCodex
}

// DiscoverTranscripts lists provider transcripts modified at or after since.
// OpenCode keeps its sessions in one database; each session is one source
// whose Path is "<db>#<session id>".
func DiscoverTranscripts(home string, since time.Time) ([]TranscriptSource, error) {
	var out []TranscriptSource
	add := func(provider, path, sessionID, cwd string) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.ModTime().Before(since) {
			return
		}
		out = append(out, TranscriptSource{
			Provider: provider, Path: path, SessionID: sessionID, CWD: cwd,
			Size: info.Size(), ModTime: info.ModTime(),
		})
	}

	claudeFiles, _ := filepath.Glob(filepath.Join(providerpaths.ClaudeConfigDir(home), "projects", "*", "*.jsonl"))
	for _, path := range claudeFiles {
		add(WorkerProviderClaude, path, strings.TrimSuffix(filepath.Base(path), ".jsonl"), "")
	}

	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	codexFiles, _ := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	for _, path := range codexFiles {
		add(WorkerProviderCodex, path, "", "")
	}

	piFiles, _ := filepath.Glob(filepath.Join(providerpaths.PiAgentDir(home), "sessions", "*", "*.jsonl"))
	for _, path := range piFiles {
		add(WorkerProviderPi, path, "", "")
	}

	cursorProjects, _ := filepath.Glob(filepath.Join(home, cursorProjectDirPrefix, "*"))
	for _, projectDir := range cursorProjects {
		cwd, _ := readCursorWorkspaceTrustedPath(filepath.Join(projectDir, cursorWorkspaceTrustedMarker))
		files, _ := filepath.Glob(filepath.Join(projectDir, cursorTranscriptDir, "*", "*.jsonl"))
		for _, path := range files {
			add(WorkerProviderCursor, path, cursorTranscriptIDFromPath(path), cwd)
		}
	}

	grokSessions, _ := filepath.Glob(filepath.Join(home, ".grok", "sessions", "*", "*"))
	for _, sessionDir := range grokSessions {
		summary, _ := readGrokSummary(sessionDir)
		add(WorkerProviderGrok, filepath.Join(sessionDir, grokChatHistoryFile),
			firstNonEmpty(summary.Info.ID, filepath.Base(sessionDir)), summary.Info.CWD)
	}

	dshFiles, _ := filepath.Glob(filepath.Join(DSHRoot(), "logs", "*", "*", "session.jsonl.zstd"))
	for _, path := range dshFiles {
		add(WorkerProviderDSH, path, filepath.Base(filepath.Dir(path)), "")
	}

	sessions, err := discoverOpenCodeTranscripts(home, since)
	if err != nil {
		return out, err
	}
	return append(out, sessions...), nil
}

func discoverOpenCodeTranscripts(home string, since time.Time) ([]TranscriptSource, error) {
	dbPath := providerpaths.OpenCodeDB(home)
	info, err := os.Stat(dbPath)
	if err != nil || info.Size() == 0 {
		return nil, nil
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, nil
	}
	rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, fmt.Sprintf(
		`SELECT id, directory, parent_id, time_created, time_updated FROM session WHERE time_updated >= %d AND (parent_id IS NULL OR parent_id = '') ORDER BY time_updated;`,
		since.UnixMilli(),
	))
	if err != nil {
		return nil, err
	}
	out := make([]TranscriptSource, 0, len(rows))
	for _, row := range rows {
		out = append(out, TranscriptSource{
			Provider: WorkerProviderOpenCode, Path: dbPath + "#" + row.ID,
			SessionID: row.ID, CWD: row.Directory,
			// The update clock stands in for size: it changes whenever the
			// session gains a message.
			Size: row.TimeUpdated, ModTime: time.UnixMilli(row.TimeUpdated).UTC(),
		})
	}
	return out, nil
}

// ScanTranscript reads messages from source starting at offset and calls visit
// for each, in transcript order. Streamed sources stop at the last complete
// line, so a growing file is never read mid-record; checkpoint runs every
// checkpointBytes with the offset reached so far.
func ScanTranscript(source TranscriptSource, offset int64, visit func(TranscriptMessage), checkpoint func(TranscriptScan) error) (TranscriptScan, error) {
	switch source.Provider {
	case WorkerProviderClaude:
		builder := newClaudeConversationBuilder(source.SessionID)
		drain := func() []CodexConversationEvent {
			events := builder.events
			builder.events = nil
			clear(builder.eventByCall)
			return events
		}
		scan, err := streamTranscript(source.Path, offset, builder.consumeLine, drain, visit, checkpoint, func(scan *TranscriptScan) {
			scan.SessionID = firstNonEmpty(builder.sessionID, source.SessionID)
			scan.CWD = builder.cwd
		})
		return scan, err
	case WorkerProviderCodex:
		meta, _ := readCodexMeta(source.Path)
		builder := newCodexConversationBuilder(filepath.Base(source.Path))
		builder.sessionID = strings.TrimSpace(meta.ID)
		builder.cwd = strings.TrimSpace(meta.CWD)
		drain := func() []CodexConversationEvent {
			events := builder.events
			builder.events = nil
			clear(builder.eventByCall)
			return events
		}
		finish := func(scan *TranscriptScan) {
			scan.SessionID = builder.sessionID
			scan.CWD = builder.cwd
		}
		return streamTranscript(source.Path, offset, builder.consumeLine, drain, visit, checkpoint, finish)
	default:
		return parseWholeTranscript(source, visit)
	}
}

const transcriptCheckpointBytes = 16 << 20

func streamTranscript(
	path string,
	offset int64,
	consume func(int, []byte),
	drain func() []CodexConversationEvent,
	visit func(TranscriptMessage),
	checkpoint func(TranscriptScan) error,
	finish func(*TranscriptScan),
) (TranscriptScan, error) {
	file, err := os.Open(path)
	if err != nil {
		return TranscriptScan{Offset: offset}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return TranscriptScan{Offset: offset}, err
	}
	if offset > info.Size() {
		// The file was replaced or truncated; read it again from the start.
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return TranscriptScan{Offset: offset}, err
	}
	reader := bufio.NewReaderSize(file, 1<<20)
	position := offset
	lastCheckpoint := offset
	scan := TranscriptScan{Offset: offset}
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return scan, readErr
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			// A trailing line without its newline is still being written.
			break
		}
		start := position
		position += int64(len(line))
		if len(bytes.TrimSpace(line)) > 0 {
			// The absolute byte offset is the line marker, so event IDs do not
			// depend on where this scan started.
			consume(int(start)+1, line)
			emitTranscriptMessages(drain(), visit)
		}
		scan.Offset = position
		if checkpoint != nil && position-lastCheckpoint >= transcriptCheckpointBytes {
			finish(&scan)
			if err := checkpoint(scan); err != nil {
				return scan, err
			}
			lastCheckpoint = position
		}
		if readErr != nil {
			break
		}
	}
	finish(&scan)
	return scan, nil
}

func parseWholeTranscript(source TranscriptSource, visit func(TranscriptMessage)) (TranscriptScan, error) {
	var (
		events []CodexConversationEvent
		scan   = TranscriptScan{Offset: source.Size, SessionID: source.SessionID, CWD: source.CWD}
	)
	switch source.Provider {
	case WorkerProviderPi:
		header, _ := readPiSessionHeader(source.Path)
		scan.SessionID = firstNonEmpty(header.ID, scan.SessionID)
		scan.CWD = firstNonEmpty(header.CWD, scan.CWD)
		builder := newPiConversationBuilder(filepath.Base(source.Path))
		if err := scanJSONLLines(source.Path, builder.consumeLine); err != nil {
			return scan, err
		}
		events = builder.result().Events
	case WorkerProviderCursor:
		builder := newCursorConversationBuilder(source.SessionID)
		if err := consumeCursorJSONL(source.Path, builder.consumeLine); err != nil {
			return scan, err
		}
		events = builder.events
	case WorkerProviderGrok:
		builder := newGrokConversationBuilder(source.SessionID)
		if err := consumeGrokJSONL(source.Path, builder.consumeChatHistoryLine); err != nil {
			return scan, err
		}
		events = builder.events
	case WorkerProviderDSH:
		conversation, err := parseDSHConversation(source.Path)
		if err != nil {
			return scan, err
		}
		scan.SessionID = firstNonEmpty(conversation.SessionID, scan.SessionID)
		scan.CWD = firstNonEmpty(conversation.CWD, scan.CWD)
		events = conversation.Events
	case WorkerProviderOpenCode:
		dbPath, sessionID, _ := strings.Cut(source.Path, "#")
		conversation, err := parseOpenCodeConversation(dbPath, sessionID)
		if err != nil {
			return scan, err
		}
		events = conversation.Events
	default:
		return scan, fmt.Errorf("unsupported transcript provider %q", source.Provider)
	}
	fallback := source.ModTime
	for index := range events {
		if events[index].Timestamp == "" && !fallback.IsZero() {
			events[index].Timestamp = fallback.UTC().Format(time.RFC3339Nano)
		}
	}
	emitTranscriptMessages(events, visit)
	return scan, nil
}

func emitTranscriptMessages(events []CodexConversationEvent, visit func(TranscriptMessage)) {
	for _, event := range events {
		if event.Kind != "user_message" && event.Kind != "assistant_message" {
			continue
		}
		role := event.Role
		if role == "" {
			role = strings.TrimSuffix(event.Kind, "_message")
		}
		at, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.Timestamp))
		visit(TranscriptMessage{
			ID:                       event.ID,
			Role:                     role,
			Text:                     event.Body,
			Timestamp:                at,
			AdmissionSHA256:          event.AdmissionSHA256,
			AdmissionUnwrappedSHA256: event.AdmissionUnwrappedSHA256,
		})
	}
}
