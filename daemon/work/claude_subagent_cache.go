package work

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// claudeSubagentCache keeps, per subagent transcript, what the last read made
// of it: the answer, and the builder with the offset it stopped at. A file that
// did not change costs a stat and a read of its small meta file; one that only
// grew costs the bytes it grew by. Anything else is read again from the start,
// which is also what the answer is held to (readClaudeSubagentFull).
var claudeSubagentCache = &claudeSubagentStore{entries: map[string]*claudeSubagentEntry{}}

const (
	// claudeSubagentCacheMax bounds the subagent transcripts whose state is kept.
	claudeSubagentCacheMax = 48
	// claudeSubagentKeepBuilderMax is the largest transcript whose builder is
	// kept to go on from; a bigger one keeps its answer only and is read again
	// when it changes.
	claudeSubagentKeepBuilderMax = 64 << 20
	// claudeSubagentMaxAppend is the most one advance reads; more is read from
	// the file.
	claudeSubagentMaxAppend = 32 << 20
)

// claudeSubagentObserve, when set by a test, receives every read of a subagent
// transcript: how many bytes it read from the file.
var claudeSubagentObserve func(path string, readBytes int64)

type claudeSubagentStore struct {
	mu      sync.Mutex
	entries map[string]*claudeSubagentEntry
}

type claudeSubagentEntry struct {
	mu   sync.Mutex
	used time.Time

	info       os.FileInfo
	size       int64
	modTime    time.Time
	offset     int64
	lineNumber int
	guard      []byte
	builder    *claudeConversationBuilder
	metaRaw    []byte
	result     *claudeSubagent
}

func (c *claudeSubagentStore) entry(path string) *claudeSubagentEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[path]
	if entry == nil {
		entry = &claudeSubagentEntry{}
		c.entries[path] = entry
		for len(c.entries) > claudeSubagentCacheMax {
			var oldestKey string
			var oldest time.Time
			for key, candidate := range c.entries {
				if candidate == entry {
					continue
				}
				if oldestKey == "" || candidate.used.Before(oldest) {
					oldestKey, oldest = key, candidate.used
				}
			}
			delete(c.entries, oldestKey)
		}
	}
	entry.used = time.Now()
	return entry
}

func (c *claudeSubagentStore) forget(path string) {
	c.mu.Lock()
	delete(c.entries, path)
	c.mu.Unlock()
}

func (c *claudeSubagentStore) read(path string) (*claudeSubagent, bool) {
	rawMeta, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json")
	if err != nil {
		return nil, false
	}
	var meta claudeSubagentMeta
	if json.Unmarshal(rawMeta, &meta) != nil || strings.TrimSpace(meta.ToolUseID) == "" {
		return nil, false
	}
	entry := c.entry(path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	file, err := os.Open(path)
	if err != nil {
		entry.reset()
		c.forget(path)
		return nil, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		entry.reset()
		return nil, false
	}
	if entry.result != nil && entry.unchanged(info) {
		if bytes.Equal(entry.metaRaw, rawMeta) {
			return entry.result, true
		}
		if entry.builder != nil {
			entry.metaRaw = rawMeta
			entry.result = claudeSubagentOf(path, meta, entry.builder)
			return entry.result, true
		}
	}
	read, advanced, err := entry.advance(file, info)
	if err != nil {
		entry.reset()
		return nil, false
	}
	if !advanced {
		entry.reset()
		if read, err = entry.seed(file, info); err != nil {
			entry.reset()
			return nil, false
		}
	}
	if claudeSubagentObserve != nil {
		claudeSubagentObserve(path, read)
	}
	entry.metaRaw = rawMeta
	entry.result = claudeSubagentOf(path, meta, entry.builder)
	if info.Size() > claudeSubagentKeepBuilderMax {
		// Too big to hold the builder: keep the answer, read again on change.
		entry.builder, entry.guard = nil, nil
	}
	return entry.result, true
}

func (e *claudeSubagentEntry) reset() {
	e.info, e.size, e.modTime, e.offset, e.lineNumber = nil, 0, time.Time{}, 0, 0
	e.guard, e.builder, e.metaRaw, e.result = nil, nil, nil, nil
}

func (e *claudeSubagentEntry) unchanged(info os.FileInfo) bool {
	return sameProviderSourceFile(e.info, info) && e.size == info.Size() && e.modTime.Equal(info.ModTime())
}

// advance feeds the builder the bytes the file grew by. advanced is false when
// the entry cannot answer from what it holds and the file must be read again.
func (e *claudeSubagentEntry) advance(file *os.File, info os.FileInfo) (read int64, advanced bool, err error) {
	size := info.Size()
	if e.builder == nil || !sameProviderSourceFile(e.info, info) || size < e.offset || size-e.offset > claudeSubagentMaxAppend {
		return 0, false, nil
	}
	if size == e.size && !info.ModTime().Equal(e.modTime) {
		// Rewritten in place without growing: not an append.
		return 0, false, nil
	}
	if len(e.guard) > 0 {
		before := make([]byte, len(e.guard))
		if n, err := file.ReadAt(before, e.offset-int64(len(before))); n != len(before) || (err != nil && err != io.EOF) || !bytes.Equal(before, e.guard) {
			return 0, false, nil
		}
	}
	if size > e.offset {
		data := make([]byte, size-e.offset)
		n, err := file.ReadAt(data, e.offset)
		if err != nil && err != io.EOF {
			return 0, false, err
		}
		data = data[:n]
		e.offset += int64(e.feed(data))
		read = int64(n)
		if err := e.rememberGuard(file); err != nil {
			return 0, false, err
		}
	}
	e.info, e.size, e.modTime = info, size, info.ModTime()
	return read, true, nil
}

// seed reads the file from its start, in blocks, and keeps the state to go on from.
func (e *claudeSubagentEntry) seed(file *os.File, info os.FileInfo) (int64, error) {
	e.builder = newClaudeConversationBuilder(strings.TrimSuffix(filepath.Base(file.Name()), ".jsonl"))
	e.builder.sidechain = true
	e.offset, e.lineNumber = 0, 0
	size := info.Size()
	const block = 4 << 20
	var carry []byte
	var read int64
	for e.offset+int64(len(carry)) < size {
		chunk := make([]byte, min(int64(block), size-e.offset-int64(len(carry))))
		n, err := file.ReadAt(chunk, e.offset+int64(len(carry)))
		if err != nil && err != io.EOF {
			return read, err
		}
		read += int64(n)
		data := append(carry, chunk[:n]...)
		consumed := e.feed(data)
		e.offset += int64(consumed)
		carry = append([]byte(nil), data[consumed:]...)
		if n == 0 {
			break
		}
	}
	if err := e.rememberGuard(file); err != nil {
		return read, err
	}
	e.info, e.size, e.modTime = info, size, info.ModTime()
	return read, nil
}

// feed consumes the complete lines of data, and a last line that is a whole
// record already; it returns how many bytes that was.
func (e *claudeSubagentEntry) feed(data []byte) int {
	consumed := 0
	for consumed < len(data) {
		rest := data[consumed:]
		end := bytes.IndexByte(rest, '\n')
		line := rest
		if end >= 0 {
			line = rest[:end+1]
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
			return consumed
		}
		consumed += len(line)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		e.lineNumber++
		e.builder.consumeLine(e.lineNumber, line)
	}
	return consumed
}

func (e *claudeSubagentEntry) rememberGuard(file *os.File) error {
	size := int64(claudeGuardBytes)
	if e.offset < size {
		size = e.offset
	}
	guard := make([]byte, size)
	if size > 0 {
		if n, err := file.ReadAt(guard, e.offset-size); n != len(guard) || (err != nil && err != io.EOF) {
			return err
		}
	}
	e.guard = guard
	return nil
}

// readClaudeSubagentFull is the definition of the answer: the file read from
// its start, and the meta beside it. The cache reaches the same answer from
// less, and the tests hold it to this one.
func readClaudeSubagentFull(path string) (*claudeSubagent, bool) {
	rawMeta, err := os.ReadFile(strings.TrimSuffix(path, ".jsonl") + ".meta.json")
	if err != nil {
		return nil, false
	}
	var meta claudeSubagentMeta
	if json.Unmarshal(rawMeta, &meta) != nil || strings.TrimSpace(meta.ToolUseID) == "" {
		return nil, false
	}
	builder := newClaudeConversationBuilder(strings.TrimSuffix(filepath.Base(path), ".jsonl"))
	builder.sidechain = true
	if err := consumeClaudeJSONL(path, builder.consumeLine); err != nil {
		return nil, false
	}
	return claudeSubagentOf(path, meta, builder), true
}
