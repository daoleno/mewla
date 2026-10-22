package work

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
)

// A Claude transcript only grows, and a Brain host's reaches hundreds of MB,
// while the conversation shows its last maxCodexConversationEvents. Parsing the
// whole file to show the end of it made the first snapshot after a daemon
// restart take as long as the file is large. parseClaudeConversationTail reads
// only the end, and answers exactly what the full parse would, or reports that
// it cannot prove that so the caller parses everything:
//
//   - the window starts at a human prompt, so no tool call or turn is open at
//     its first line;
//   - the lines before it are counted, not parsed, so every event keeps the
//     Seq the full parse gives it;
//   - the session id and cwd come from the head of the file, as the full parse
//     takes them from its first records;
//   - no branch a rewind left behind starts before the window: all its records
//     hang off at most one parent outside it;
//   - the window holds a full conversation's worth of events.

// claudeTailMinBytes is the transcript size from which the end is read alone.
var claudeTailMinBytes int64 = 16 << 20

// claudeTailInitialWindow is the first guess at how much of the end holds the
// last events; the window doubles until it proves complete.
var claudeTailInitialWindow int64 = 4 << 20

// claudeTailMaxWindow bounds what one parse holds in memory; past it the whole
// file is streamed instead.
const claudeTailMaxWindow int64 = 128 << 20

// claudeHeadMaxLines bounds the search of the file's head for its session id
// and cwd.
const claudeHeadMaxLines = 2000

// claudeTailObserve, when set by a test, receives the number of lines the
// builder consumed for a tail parse.
var claudeTailObserve func(lines int)

func parseClaudeConversationTail(path string) (CodexConversation, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexConversation{}, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return CodexConversation{}, false, err
	}
	size := info.Size()
	if size < claudeTailMinBytes {
		return CodexConversation{}, false, nil
	}
	sourceID := strings.TrimSuffix(pathBase(path), ".jsonl")
	sessionID, cwd, ok, err := readClaudeHead(file, sourceID)
	if err != nil || !ok {
		return CodexConversation{}, false, err
	}

	for window := claudeTailInitialWindow; window <= claudeTailMaxWindow && window < size; window *= 2 {
		data, start, found, err := readClaudeTailWindow(file, size, window)
		if err != nil {
			return CodexConversation{}, false, err
		}
		if !found {
			continue
		}
		var scan claudeBranchScan
		consumeClaudeLines(data, 1, scan.add)
		if scan.openParents() > 1 {
			continue
		}
		prefixLines, err := claudeLinesBefore(file, info, path, start)
		if err != nil {
			return CodexConversation{}, false, err
		}
		builder := newClaudeConversationBuilder(sourceID)
		builder.sessionID = sessionID
		builder.cwd = cwd
		builder.abandoned = scan.abandoned()
		parsed := 0
		consumeClaudeLines(data, prefixLines+1, func(lineNumber int, line []byte) {
			parsed++
			builder.consumeLine(lineNumber, line)
		})
		if builder.supportedRecords == 0 || len(builder.events) < maxCodexConversationEvents {
			continue
		}
		if claudeTailObserve != nil {
			claudeTailObserve(parsed)
		}
		return builder.conversation(), true, nil
	}
	return CodexConversation{}, false, nil
}

func pathBase(path string) string {
	if index := strings.LastIndexAny(path, `/\`); index >= 0 {
		return path[index+1:]
	}
	return path
}

// readClaudeHead returns what the full parse takes from the first records of
// the file: the session id of the first supported record and the first cwd. ok
// is false when the head does not settle them within claudeHeadMaxLines.
func readClaudeHead(file *os.File, sourceID string) (sessionID, cwd string, ok bool, err error) {
	for limit := int64(256 << 10); limit <= 16<<20; limit *= 4 {
		data := make([]byte, limit)
		read, readErr := file.ReadAt(data, 0)
		if readErr != nil && readErr != io.EOF {
			return "", "", false, readErr
		}
		data = data[:read]
		if read == int(limit) {
			// The last line may be cut short; drop it.
			if newline := bytes.LastIndexByte(data, '\n'); newline >= 0 {
				data = data[:newline+1]
			}
		}
		probe := newClaudeConversationBuilder(sourceID)
		lines := 0
		consumeClaudeLines(data, 1, func(_ int, line []byte) {
			if lines >= claudeHeadMaxLines || (probe.sessionID != "" && probe.cwd != "") {
				return
			}
			lines++
			var envelope struct {
				Type       string `json:"type"`
				SessionID  string `json:"sessionId"`
				SessionID2 string `json:"session_id"`
				CWD        string `json:"cwd"`
			}
			if json.Unmarshal(line, &envelope) != nil || !isSupportedClaudeRecordType(envelope.Type) {
				return
			}
			if probe.sessionID == "" {
				probe.sessionID = firstNonEmpty(envelope.SessionID, envelope.SessionID2, sourceID)
			}
			if probe.cwd == "" {
				probe.cwd = strings.TrimSpace(envelope.CWD)
			}
		})
		if probe.sessionID != "" && probe.cwd != "" {
			return probe.sessionID, probe.cwd, true, nil
		}
		if read < int(limit) || lines >= claudeHeadMaxLines {
			break
		}
	}
	return "", "", false, nil
}

// readClaudeTailWindow reads about window bytes from the end and returns them
// from the first human prompt in them, with that prompt's byte offset in the
// file. found is false when the window holds no prompt.
func readClaudeTailWindow(file *os.File, size, window int64) (data []byte, start int64, found bool, err error) {
	from := size - window
	if from < 0 {
		from = 0
	}
	raw := make([]byte, size-from)
	if _, err = file.ReadAt(raw, from); err != nil && err != io.EOF {
		return nil, 0, false, err
	}
	offset := 0
	if from > 0 {
		// The window starts inside a line; the next line starts at a boundary.
		newline := bytes.IndexByte(raw, '\n')
		if newline < 0 {
			return nil, 0, false, nil
		}
		offset = newline + 1
	}
	for offset < len(raw) {
		end := bytes.IndexByte(raw[offset:], '\n')
		line := raw[offset:]
		next := len(raw)
		if end >= 0 {
			line, next = raw[offset:offset+end+1], offset+end+1
		}
		var envelope claudePromptEnvelope
		if bytes.Contains(line, []byte(`"type":"user"`)) &&
			json.Unmarshal(line, &envelope) == nil && envelope.UUID != "" && !envelope.IsSidechain && envelope.humanPrompt() {
			return raw[offset:], from + int64(offset), true, nil
		}
		offset = next
	}
	return nil, 0, false, nil
}

// claudeLineCounts remembers how many non-blank lines precede an offset of a
// transcript, so the next parse of a file that only grew counts the new bytes
// alone.
var claudeLineCounts = struct {
	mu      sync.Mutex
	entries map[string]claudeLineCount
}{entries: map[string]claudeLineCount{}}

type claudeLineCount struct {
	info     os.FileInfo
	offset   int64
	lines    int
	afterEOL bool
}

const maxClaudeLineCounts = 8

// claudeLinesBefore counts the non-blank lines of the file before offset,
// which is a line boundary.
func claudeLinesBefore(file *os.File, info os.FileInfo, path string, offset int64) (int, error) {
	claudeLineCounts.mu.Lock()
	cached, ok := claudeLineCounts.entries[path]
	claudeLineCounts.mu.Unlock()
	if !ok || cached.offset > offset || !sameProviderSourceFile(cached.info, info) || cached.info.Size() > info.Size() {
		cached = claudeLineCount{afterEOL: true}
	}
	cached.info = info

	buffer := make([]byte, 4<<20)
	for cached.offset < offset {
		want := int64(len(buffer))
		if remaining := offset - cached.offset; remaining < want {
			want = remaining
		}
		read, err := file.ReadAt(buffer[:want], cached.offset)
		if read > 0 {
			cached.lines += countNonBlankLines(buffer[:read], cached.afterEOL)
			cached.afterEOL = buffer[read-1] == '\n'
			cached.offset += int64(read)
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		if read == 0 {
			break
		}
	}

	claudeLineCounts.mu.Lock()
	claudeLineCounts.entries[path] = cached
	for len(claudeLineCounts.entries) > maxClaudeLineCounts {
		for key := range claudeLineCounts.entries {
			if key != path {
				delete(claudeLineCounts.entries, key)
				break
			}
		}
	}
	claudeLineCounts.mu.Unlock()
	return cached.lines, nil
}

// countNonBlankLines counts the '\n'-ended lines of chunk that hold anything
// but whitespace, with the same rule consumeClaudeJSONL numbers lines by.
// atLineStart says chunk begins at the start of a line.
func countNonBlankLines(chunk []byte, atLineStart bool) int {
	lines := bytes.Count(chunk, []byte{'\n'})
	if !bytes.Contains(chunk, []byte("\n\n")) && !(atLineStart && chunk[0] == '\n') {
		return lines
	}
	// A blank line is rare in a transcript; count the exact way when one shows.
	lines = 0
	blank := atLineStart
	for _, c := range chunk {
		if c == '\n' {
			if !blank {
				lines++
			}
			blank = true
			continue
		}
		if c != ' ' && c != '\t' && c != '\r' {
			blank = false
		}
	}
	return lines
}
