package work

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// A Claude transcript only grows, and a Worker that is working appends to it
// several times a second. Parsing the whole file for every append read a
// 12 MB transcript twice (once for the rewind branches, once for the events)
// per poll and a 60 MB one through a 4 MB window and a recount of its lines:
// some 5k read syscalls and 100 MB a second for two Workers.
//
// parseClaudeConversationIncremental keeps, per transcript, the builder that
// consumed it and the byte offset it stopped at. A file that only grew costs
// the bytes it grew by. It answers exactly what parseClaudeConversationFull
// answers, or it starts over from the file (the tail parse or the full parse):
//
//   - the file is the same one, did not shrink, and the bytes just before the
//     offset are the ones that were there;
//   - no appended human prompt forks the conversation: a prompt whose parent
//     already has a human child marks the older prompt and everything under it
//     as abandoned, which would change events the builder has already
//     consumed. Such a prompt, and one whose parent is not among the records
//     the state remembers, makes the state start over;
//   - only complete lines are consumed; a last line without its newline waits
//     for it unless it already is a whole JSON object.

// claudeIncrementalMaxStates bounds the transcripts whose parse state is kept.
const claudeIncrementalMaxStates = 8

// claudeIncrementalRecent is how many of the latest records' ids the state
// remembers, to tell what a new prompt hangs off.
const claudeIncrementalRecent = 8192

// claudeIncrementalMaxAppend is the most one advance reads; a transcript that
// grew by more (a long idle gap, a bulk write) is parsed from the file.
const claudeIncrementalMaxAppend = 32 << 20

// claudeGuardBytes is how much of the file before the offset must still be
// there for the file to count as the one that was parsed.
const claudeGuardBytes = 256

// claudeIncrementalObserve, when set by a test, receives every advance: how
// many bytes it read and whether the state started over instead.
var claudeIncrementalObserve func(readBytes int64, reseeded bool)

type claudeIncrementalState struct {
	mu   sync.Mutex
	used time.Time

	info       os.FileInfo
	modTime    time.Time
	offset     int64
	lineNumber int
	guard      []byte
	builder    *claudeConversationBuilder

	// abandoned is builder.abandoned: the rewound records, grown with the
	// descendants of an abandoned record that the transcript keeps appending.
	abandoned map[string]struct{}
	// recent maps the ids of the latest records to whether a human prompt
	// already hangs off them; ring is the order they came in.
	recent    map[string]bool
	ring      []string
	ringNext  int
	rootHuman bool
}

var claudeIncrementalStates = struct {
	mu      sync.Mutex
	entries map[string]*claudeIncrementalState
}{entries: map[string]*claudeIncrementalState{}}

func claudeIncrementalStateFor(path string) *claudeIncrementalState {
	claudeIncrementalStates.mu.Lock()
	defer claudeIncrementalStates.mu.Unlock()
	state := claudeIncrementalStates.entries[path]
	if state == nil {
		state = &claudeIncrementalState{}
		claudeIncrementalStates.entries[path] = state
		for len(claudeIncrementalStates.entries) > claudeIncrementalMaxStates {
			var oldestKey string
			var oldest time.Time
			for key, candidate := range claudeIncrementalStates.entries {
				if candidate == state {
					continue
				}
				if oldestKey == "" || candidate.used.Before(oldest) {
					oldestKey, oldest = key, candidate.used
				}
			}
			delete(claudeIncrementalStates.entries, oldestKey)
		}
	}
	state.used = time.Now()
	return state
}

func parseClaudeConversationIncremental(path string) (CodexConversation, error) {
	state := claudeIncrementalStateFor(path)
	state.mu.Lock()
	defer state.mu.Unlock()
	conversation, ok, err := state.advance(path)
	if err != nil {
		state.reset()
		return CodexConversation{}, err
	}
	if ok {
		return conversation, nil
	}
	state.reset()
	conversation, err = state.seed(path)
	if err != nil {
		state.reset()
		return CodexConversation{}, err
	}
	return conversation, nil
}

func (s *claudeIncrementalState) reset() {
	s.info, s.modTime, s.offset, s.lineNumber, s.guard, s.builder = nil, time.Time{}, 0, 0, nil, nil
	s.abandoned, s.recent, s.ring, s.ringNext, s.rootHuman = nil, nil, nil, 0, false
}

// advance feeds the bytes the transcript grew by to the builder. ok is false
// when the state cannot answer and the file must be parsed again.
func (s *claudeIncrementalState) advance(path string) (CodexConversation, bool, error) {
	if s.builder == nil {
		return CodexConversation{}, false, nil
	}
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
	if !sameProviderSourceFile(s.info, info) || size < s.offset || size-s.offset > claudeIncrementalMaxAppend {
		return CodexConversation{}, false, nil
	}
	if size == s.offset && !info.ModTime().Equal(s.modTime) {
		// Rewritten in place without growing: not an append.
		return CodexConversation{}, false, nil
	}
	if len(s.guard) > 0 {
		before := make([]byte, len(s.guard))
		if n, err := file.ReadAt(before, s.offset-int64(len(before))); n != len(before) || (err != nil && err != io.EOF) || !bytes.Equal(before, s.guard) {
			return CodexConversation{}, false, nil
		}
	}
	readBytes := size - s.offset
	if readBytes > 0 {
		data := make([]byte, readBytes)
		n, err := file.ReadAt(data, s.offset)
		if err != nil && err != io.EOF {
			return CodexConversation{}, false, err
		}
		data = data[:n]
		consumed, ok := s.feed(data)
		if !ok {
			if claudeIncrementalObserve != nil {
				claudeIncrementalObserve(readBytes, true)
			}
			return CodexConversation{}, false, nil
		}
		s.offset += int64(consumed)
		if err := s.rememberGuard(file); err != nil {
			return CodexConversation{}, false, err
		}
		s.builder.compactEvents(2 * maxCodexConversationEvents)
	}
	s.info, s.modTime = info, info.ModTime()
	if claudeIncrementalObserve != nil {
		claudeIncrementalObserve(readBytes, false)
	}
	if s.builder.supportedRecords == 0 {
		return CodexConversation{}, false, errClaudeNoSupportedRecords
	}
	return s.builder.snapshot(), true, nil
}

var errClaudeNoSupportedRecords = errors.New("claude transcript has no supported records")

// feed consumes the complete lines of data, and a last line that is a whole
// record already; it returns how many bytes that was. ok is false when a line
// forks the conversation, which the builder cannot take back.
func (s *claudeIncrementalState) feed(data []byte) (consumed int, ok bool) {
	for consumed < len(data) {
		rest := data[consumed:]
		end := bytes.IndexByte(rest, '\n')
		line := rest
		if end >= 0 {
			line = rest[:end+1]
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
			return consumed, true
		}
		consumed += len(line)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		s.lineNumber++
		if !s.noteRecord(line) {
			return consumed, false
		}
		s.builder.consumeLine(s.lineNumber, line)
	}
	return consumed, true
}

// noteRecord keeps what claudeBranchScan would learn from the line, in step
// with the builder. It returns false when the line makes records the builder
// already consumed abandoned.
func (s *claudeIncrementalState) noteRecord(line []byte) bool {
	var envelope claudePromptEnvelope
	if json.Unmarshal(line, &envelope) != nil || envelope.UUID == "" {
		return true
	}
	if envelope.IsSidechain {
		s.remember(envelope.UUID)
		return true
	}
	if _, left := s.abandoned[envelope.ParentUUID]; left {
		s.abandoned[envelope.UUID] = struct{}{}
	}
	if envelope.humanPrompt() {
		if envelope.ParentUUID == "" {
			if s.rootHuman {
				return false
			}
			s.rootHuman = true
		} else {
			hasPrompt, known := s.recent[envelope.ParentUUID]
			if !known || hasPrompt {
				return false
			}
			s.recent[envelope.ParentUUID] = true
		}
	}
	s.remember(envelope.UUID)
	return true
}

func (s *claudeIncrementalState) remember(uuid string) {
	if _, known := s.recent[uuid]; known {
		return
	}
	if len(s.ring) < claudeIncrementalRecent {
		s.ring = append(s.ring, uuid)
	} else {
		delete(s.recent, s.ring[s.ringNext])
		s.ring[s.ringNext] = uuid
		s.ringNext = (s.ringNext + 1) % claudeIncrementalRecent
	}
	s.recent[uuid] = false
}

func (s *claudeIncrementalState) rememberGuard(file *os.File) error {
	size := int64(claudeGuardBytes)
	if s.offset < size {
		size = s.offset
	}
	guard := make([]byte, size)
	if n, err := file.ReadAt(guard, s.offset-size); n != len(guard) || (err != nil && err != io.EOF) {
		return fmt.Errorf("read transcript guard: %v", err)
	}
	s.guard = guard
	return nil
}

// seed parses the transcript from the file, by its tail where that is
// provable and in full otherwise, and keeps the state to go on from.
func (s *claudeIncrementalState) seed(path string) (CodexConversation, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexConversation{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return CodexConversation{}, err
	}
	tail, ok, err := claudeTailSeed(file, info, path)
	if err != nil {
		return CodexConversation{}, err
	}
	var scan *claudeBranchScan
	if ok {
		s.builder, scan, s.offset, s.lineNumber = tail.builder, tail.scan, tail.offset, tail.lineNumber
	} else {
		if scan, err = s.seedFull(file, info, path); err != nil {
			return CodexConversation{}, err
		}
	}
	s.abandoned = scan.abandoned()
	if s.abandoned == nil {
		s.abandoned = map[string]struct{}{}
	}
	s.builder.abandoned = s.abandoned
	s.recent = make(map[string]bool, claudeIncrementalRecent)
	s.ring = make([]string, 0, claudeIncrementalRecent)
	first := 0
	if len(scan.records) > claudeIncrementalRecent {
		first = len(scan.records) - claudeIncrementalRecent
	}
	for _, record := range scan.records[first:] {
		s.remember(record.uuid)
		if len(scan.humanChildren[record.uuid]) > 0 {
			s.recent[record.uuid] = true
		}
	}
	s.rootHuman = len(scan.humanChildren[""]) > 0
	s.info, s.modTime = info, info.ModTime()
	if err := s.rememberGuard(file); err != nil {
		return CodexConversation{}, err
	}
	s.builder.compactEvents(2 * maxCodexConversationEvents)
	if claudeIncrementalObserve != nil {
		claudeIncrementalObserve(info.Size(), true)
	}
	if s.builder.supportedRecords == 0 {
		return CodexConversation{}, errClaudeNoSupportedRecords
	}
	return s.builder.snapshot(), nil
}

// seedFull is the full parse: one pass for the rewind branches, one for the
// events, both over the bytes the file had when it was statted.
func (s *claudeIncrementalState) seedFull(file *os.File, info os.FileInfo, path string) (*claudeBranchScan, error) {
	size := info.Size()
	scan := &claudeBranchScan{}
	if _, err := consumeClaudeRange(file, size, func(_ int, line []byte) { scan.add(0, line) }); err != nil {
		return nil, err
	}
	builder := newClaudeConversationBuilder(strings.TrimSuffix(pathBase(path), ".jsonl"))
	builder.abandoned = scan.abandoned()
	offset, err := consumeClaudeRange(file, size, func(number int, line []byte) {
		s.lineNumber = number
		builder.consumeLine(number, line)
	})
	if err != nil {
		return nil, err
	}
	s.builder, s.offset = builder, offset
	return scan, nil
}

// consumeClaudeRange feeds the non-blank lines of the first size bytes of the
// file to consume, numbered like consumeClaudeJSONL, and returns where the
// consumed lines end: after the last newline, or after a last line that is a
// whole record already.
func consumeClaudeRange(file *os.File, size int64, consume func(int, []byte)) (int64, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(file, 0, size), claudeReadBuffer)
	lineNumber := 0
	var offset int64
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return 0, err
		}
		complete := err == nil
		if !complete {
			if trimmed := bytes.TrimSpace(line); len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
				return offset, nil
			}
		}
		offset += int64(len(line))
		if len(bytes.TrimSpace(line)) > 0 {
			lineNumber++
			consume(lineNumber, line)
		}
		if !complete {
			return offset, nil
		}
	}
}

// claudeUsableLines is the part of data the builder may consume: up to the
// last newline, plus a last line that is a whole record already.
func claudeUsableLines(data []byte) []byte {
	end := bytes.LastIndexByte(data, '\n') + 1
	if end == len(data) {
		return data
	}
	if trimmed := bytes.TrimSpace(data[end:]); len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed) {
		return data
	}
	return data[:end]
}

// compactEvents drops all but the newest keep events of a builder that goes
// on consuming a growing transcript, once it holds maxCodexConversationEvents
// more than that, and moves the call index with them. The conversation shows
// only the newest maxCodexConversationEvents, so an event older than keep can
// matter to nothing: not to the answer, which the full parse cuts the same
// way, and not to a late tool result, which would change an event the answer
// no longer shows.
func (b *claudeConversationBuilder) compactEvents(keep int) {
	if len(b.events) <= keep+maxCodexConversationEvents {
		return
	}
	drop := len(b.events) - keep
	b.events = append([]CodexConversationEvent(nil), b.events[drop:]...)
	b.evicted += drop
	for call, index := range b.eventByCall {
		if index < drop {
			delete(b.eventByCall, call)
		} else {
			b.eventByCall[call] = index - drop
		}
	}
}

// snapshot is conversation() for a builder that keeps consuming: it copies the
// newest events instead of cutting the builder's own, whose positions the call
// index depends on.
func (b *claudeConversationBuilder) snapshot() CodexConversation {
	events := b.events
	if len(events) > maxCodexConversationEvents {
		events = events[len(events)-maxCodexConversationEvents:]
	}
	own := make([]CodexConversationEvent, len(events))
	copy(own, events)
	for index := range own {
		if own[index].Seq <= 0 {
			own[index].Seq = index + 1
		}
	}
	return conversationWithActivity(CodexConversation{
		Available: true,
		Source:    claudeConversationSource,
		SessionID: firstNonEmpty(b.sessionID, b.sourceID),
		CWD:       b.cwd,
		Events:    own,
	}, &b.activityLifecycle)
}
