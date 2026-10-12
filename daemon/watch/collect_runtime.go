package watch

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/logfile"
)

// Runtime signals are the daemon's own problem lines from its log file. Each
// carries a signature with variable parts removed, so the repeated-error rule
// can count one failure however its IDs and numbers vary.

const (
	kindLogError = "log_error"
	kindLogWarn  = "log_warn"
	kindLogLine  = "log_line"
)

type runtimeCursor struct {
	Head   string `json:"head"`
	Offset int64  `json:"offset"`
}

const logHeadBytes = 256

func (c *Collector) collectRuntime(ctx context.Context) (int, error) {
	if c.logPath == "" {
		return 0, nil
	}
	var cursor runtimeCursor
	c.store.Cursor("runtime:daemon_log", &cursor)
	currentHead, size, err := logHead(c.logPath)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	projectID := c.store.RuntimeProject()
	total := 0
	if cursor.Head != "" && cursor.Head != currentHead {
		// The live file rotated since the last read: finish the rotated copy
		// if it is the file we were reading, then read the new one whole.
		rotated := logfile.RotatedPath(c.logPath)
		if rotatedHead, _, headErr := logHead(rotated); headErr == nil && rotatedHead == cursor.Head {
			added, _, readErr := c.readLog(rotated, cursor.Head, cursor.Offset, projectID)
			total += added
			if readErr != nil {
				return total, readErr
			}
		}
		cursor = runtimeCursor{}
	}
	if cursor.Head == "" || size < cursor.Offset {
		cursor = runtimeCursor{Head: currentHead}
	}
	if currentHead == "" {
		return total, nil
	}
	added, offset, err := c.readLog(c.logPath, currentHead, cursor.Offset, projectID)
	total += added
	if err != nil {
		return total, err
	}
	if err := c.store.SetCursor("runtime:daemon_log", runtimeCursor{Head: currentHead, Offset: offset}); err != nil {
		return total, err
	}
	return total, ctx.Err()
}

// logHead identifies a log file by its first bytes; rotation starts a new
// file with a different head.
func logHead(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	head := make([]byte, logHeadBytes)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", 0, err
	}
	// The first line (timestamped) identifies the file; it never changes as
	// the file grows. Without a complete first line the head is not final.
	if end := strings.IndexByte(string(head[:n]), '\n'); end >= 0 {
		n = end + 1
	} else if n < logHeadBytes {
		return "", info.Size(), nil
	}
	sum := sha256.Sum256(head[:n])
	return hex.EncodeToString(sum[:8]), info.Size(), nil
}

func (c *Collector) readLog(path, head string, offset int64, projectID string) (int, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, offset, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return 0, offset, err
	}
	reader := bufio.NewReader(file)
	since := c.horizon()
	var signals []Signal
	for {
		line, readErr := reader.ReadString('\n')
		if !strings.HasSuffix(line, "\n") {
			break
		}
		start := offset
		offset += int64(len(line))
		if signal, ok := runtimeSignal(strings.TrimRight(line, "\r\n"), head, start); ok && !signal.Time.Before(since) {
			signal.ProjectID = projectID
			signal.Pointer = path + ":" + strconv.FormatInt(start, 10)
			signals = append(signals, signal)
		}
		if readErr != nil {
			break
		}
	}
	added, err := c.store.Append(signals)
	return added, offset, err
}

var logTimestamp = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})(?:\.\d+)? `)

func runtimeSignal(line, head string, offset int64) (Signal, bool) {
	match := logTimestamp.FindStringSubmatch(line)
	if match == nil {
		return Signal{}, false
	}
	at, err := time.ParseInLocation("2006/01/02 15:04:05", match[1], time.Local)
	if err != nil {
		return Signal{}, false
	}
	message := strings.TrimSpace(line[len(match[0]):])
	if message == "" {
		return Signal{}, false
	}
	kind := kindLogLine
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "panic") || strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		kind = kindLogError
	case strings.HasPrefix(message, "WARN") || strings.Contains(lower, "warn"):
		kind = kindLogWarn
	}
	signature := LogSignature(message)
	return Signal{
		ID:      signalID(SourceRuntime, head+"|"+strconv.FormatInt(offset, 10)),
		Source:  SourceRuntime,
		Time:    at,
		Kind:    kind,
		Excerpt: excerpt(message, 500),
		Refs:    map[string]string{"signature": signatureID(signature), "pattern": excerpt(signature, 200)},
	}, true
}

var signatureRules = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`"[^"]*"|'[^']*'`), `"…"`},
	{regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`), "<id>"},
	{regexp.MustCompile(`(?:/[^\s/:]+)+/?`), "<path>"},
	{regexp.MustCompile(`%\d+|@\d+`), "<session>"},
	{regexp.MustCompile(`\b[0-9a-f]{7,}\b`), "<hex>"},
	{regexp.MustCompile(`\d+(?:\.\d+)?(?:ms|s|m|h|µs|ns)?`), "<n>"},
}

// LogSignature removes the variable parts of a log message.
func LogSignature(message string) string {
	for _, rule := range signatureRules {
		message = rule.re.ReplaceAllString(message, rule.with)
	}
	return strings.Join(strings.Fields(message), " ")
}

func signatureID(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:6])
}
