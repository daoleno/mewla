// Package memorysearch indexes Brain's visible chat (messages.jsonl and its
// archives) into a SQLite FTS5 file so Brain can look up what the user said or
// decided without guessing. The chat log stays the source of truth: the index
// is rebuilt from it whenever the schema or redaction rules change.
//
// Only visible text is indexed: the user's messages, Brain's replies and Work
// result cards. Tool output and thinking never enter the log, so they never
// enter the index. Text is redacted before it is stored.
package memorysearch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion is stored in PRAGMA user_version. Bump it when the schema or
// Redact changes so the next open discards the index and rebuilds it.
const schemaVersion = 2

// cursorProbe is how many bytes before the cursor are hashed to detect that a
// file was rewritten or replaced rather than appended to.
const cursorProbe = 256

// maxCandidates bounds the rows one search tier reads before deduplication.
const maxCandidates = 5000

var ErrNotFound = errors.New("message not found")

// Who labels of indexed messages.
const (
	WhoUser  = "user"
	WhoBrain = "brain"
	WhoWork  = "work"
	// WhoContext marks user-role rows Brain's Claude transcript fed back into
	// the chat that the user did not type: compaction summaries, forwarded
	// Mewla events and notifications. They are searchable but never "user".
	WhoContext = "context"
)

var sourceName = regexp.MustCompile(`^messages(-[0-9]+)?\.jsonl$`)

// Index is one open index file. It is not safe for concurrent use; callers
// serialize Refresh and queries.
type Index struct {
	db *sql.DB
}

// Open opens (creating, mode 0600) the index at path. An index written by a
// different schemaVersion is deleted and recreated empty.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	ix, err := openFile(path)
	if err != nil {
		return nil, err
	}
	var version int
	if err := ix.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		_ = ix.Close()
		return nil, err
	}
	if version != 0 && version != schemaVersion {
		_ = ix.Close()
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		if ix, err = openFile(path); err != nil {
			return nil, err
		}
		version = 0
	}
	if version == 0 {
		if err := ix.createSchema(); err != nil {
			_ = ix.Close()
			return nil, err
		}
	}
	return ix, nil
}

func openFile(path string) (*Index, error) {
	// Create the file 0600 first: SQLite gives -wal and -shm the same mode.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Index{db: db}, nil
}

func (ix *Index) createSchema() error {
	stmts := []string{
		`CREATE TABLE files(
			path TEXT PRIMARY KEY, off INTEGER NOT NULL, head TEXT NOT NULL, tail TEXT NOT NULL)`,
		`CREATE TABLE msg(
			seq INTEGER PRIMARY KEY, id TEXT NOT NULL UNIQUE, at INTEGER NOT NULL,
			who TEXT NOT NULL, work_id TEXT NOT NULL DEFAULT '', body TEXT NOT NULL,
			dedupe TEXT NOT NULL, backfilled INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX msg_at ON msg(at, seq)`,
		`CREATE VIRTUAL TABLE msg_fts USING fts5(
			body, content='msg', content_rowid='seq', tokenize='trigram')`,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	}
	for _, stmt := range stmts {
		if _, err := ix.db.Exec(stmt); err != nil {
			return fmt.Errorf("create memory index: %w", err)
		}
	}
	return nil
}

func (ix *Index) Close() error {
	if ix == nil || ix.db == nil {
		return nil
	}
	return ix.db.Close()
}

// RefreshStats reports what one Refresh did.
type RefreshStats struct {
	Files    int
	Rescans  int
	Messages int
}

// Refresh reads whatever was appended to the chat log files in stateDir since
// the last call. Archives are read before messages.jsonl; ids are unique, so
// a message that appears in both is stored once.
func (ix *Index) Refresh(ctx context.Context, stateDir string) (RefreshStats, error) {
	var stats RefreshStats
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, err
	}
	var archives []string
	live := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !sourceName.MatchString(name) {
			continue
		}
		if name == "messages.jsonl" {
			live = name
		} else {
			archives = append(archives, name)
		}
	}
	sort.Strings(archives)
	if live != "" {
		archives = append(archives, live)
	}
	for _, name := range archives {
		stats.Files++
		rescan, added, err := ix.refreshFile(ctx, filepath.Join(stateDir, name))
		if err != nil {
			return stats, fmt.Errorf("index %s: %w", name, err)
		}
		if rescan {
			stats.Rescans++
		}
		stats.Messages += added
	}
	return stats, nil
}

type timelineRow struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	WorkID    string `json:"work_id"`
	// BrainAdmission marks rows written when the user sent a message through
	// Mewla, as opposed to rows backfilled from Brain's provider transcript.
	BrainAdmission bool `json:"brain_admission"`
}

func (ix *Index) refreshFile(ctx context.Context, path string) (rescan bool, added int, err error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, nil
		}
		return false, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, 0, err
	}

	var off int64
	var head, tail string
	row := ix.db.QueryRowContext(ctx, `SELECT off, head, tail FROM files WHERE path = ?`, path)
	switch err := row.Scan(&off, &head, &tail); {
	case errors.Is(err, sql.ErrNoRows):
		off = 0
	case err != nil:
		return false, 0, err
	default:
		if off > info.Size() || !cursorMatches(file, off, head, tail) {
			// Shrunk or rewritten: read it all again. Stored ids are kept.
			off, rescan = 0, true
		}
	}
	if off == info.Size() {
		return rescan, 0, nil
	}

	if _, err := file.Seek(off, io.SeekStart); err != nil {
		return rescan, 0, err
	}
	data, err := io.ReadAll(io.LimitReader(file, info.Size()-off))
	if err != nil {
		return rescan, 0, err
	}
	// Only whole lines count: a writer may be mid-append.
	end := strings.LastIndexByte(string(data), '\n') + 1
	if end == 0 {
		return rescan, 0, nil
	}
	data = data[:end]

	conn, err := ix.db.Conn(ctx)
	if err != nil {
		return rescan, 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return rescan, 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var item timelineRow
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}
		who, ok := whoOf(item)
		if !ok {
			continue
		}
		backfilled := isBackfilled(item)
		if backfilled && who == WhoUser && isContextBody(item.Body) {
			who = WhoContext
		}
		at, perr := time.Parse(time.RFC3339Nano, item.CreatedAt)
		body := strings.TrimSpace(Redact(item.Body))
		if perr != nil || body == "" || strings.TrimSpace(item.ID) == "" {
			continue
		}
		res, err := conn.ExecContext(ctx,
			`INSERT INTO msg(id, at, who, work_id, body, dedupe, backfilled) VALUES(?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO NOTHING`,
			item.ID, at.UnixNano(), who, strings.TrimSpace(item.WorkID), body, dedupeKey(who, body), backfilled)
		if err != nil {
			return rescan, added, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		seq, err := res.LastInsertId()
		if err != nil {
			return rescan, added, err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO msg_fts(rowid, body) VALUES(?,?)`, seq, body); err != nil {
			return rescan, added, err
		}
		added++
	}

	newOff := off + int64(end)
	newHead, newTail, err := cursorHashes(file, newOff)
	if err != nil {
		return rescan, added, err
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO files(path, off, head, tail) VALUES(?,?,?,?)
		 ON CONFLICT(path) DO UPDATE SET off=excluded.off, head=excluded.head, tail=excluded.tail`,
		path, newOff, newHead, newTail); err != nil {
		return rescan, added, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return rescan, added, err
	}
	committed = true
	return rescan, added, nil
}

// isBackfilled reports a user row Brain's provider transcript wrote into the
// chat (id "<session>:<turn>:user"), as opposed to one Mewla admitted when the
// user sent it.
func isBackfilled(item timelineRow) bool {
	return item.Role == "user" && !item.BrainAdmission && strings.HasSuffix(item.ID, ":user") && strings.Count(item.ID, ":") >= 2
}

var (
	pastedWrapper = regexp.MustCompile(`^<pasted_content[^>]*>\s*`)
	markupStart   = regexp.MustCompile(`^<[A-Za-z][\w:-]*[\s>]`)
)

// isContextBody reports text the user did not type: Claude's compaction
// summary, an interruption marker, or a message that is only markup (task
// notifications, forwarded Mewla events). A paste wrapper is looked through;
// the user's own paste (a log, a document) stays theirs. Typed text followed by
// markup does not start with it and also stays theirs.
func isContextBody(body string) bool {
	body = strings.TrimSpace(body)
	for _, prefix := range []string{"This session is being continued", "[Request interrupted", "Caveat:"} {
		if strings.HasPrefix(body, prefix) {
			return true
		}
	}
	if loc := pastedWrapper.FindStringIndex(body); loc != nil {
		body = body[loc[1]:]
	}
	return markupStart.MatchString(body)
}

func whoOf(item timelineRow) (string, bool) {
	switch strings.TrimSpace(item.Kind) {
	case "user_message":
		return WhoUser, true
	case "assistant_message":
		return WhoBrain, true
	case "work_card":
		return WhoWork, true
	case "":
		switch item.Role {
		case "user":
			return WhoUser, true
		case "assistant":
			return WhoBrain, true
		}
	}
	return "", false
}

// dedupeKey identifies a message by side and normalized text. User and context
// rows share a side: a backfilled copy of what the user typed collapses into
// the real message.
func dedupeKey(who, body string) string {
	if who == WhoContext {
		who = WhoUser
	}
	sum := sha256.Sum256([]byte(who + "\x00" + strings.Join(strings.Fields(body), " ")))
	return hex.EncodeToString(sum[:12])
}

// cursorHashes fingerprints the start of the file and the bytes just before
// off. A file replaced or rewritten in place almost surely changes one.
func cursorHashes(file *os.File, off int64) (head, tail string, err error) {
	if off <= 0 {
		return "", "", nil
	}
	if head, err = hashRange(file, 0, min(off, cursorProbe)); err != nil {
		return "", "", err
	}
	start := max(off-cursorProbe, 0)
	tail, err = hashRange(file, start, off-start)
	return head, tail, err
}

func cursorMatches(file *os.File, off int64, head, tail string) bool {
	if off == 0 {
		return true
	}
	gotHead, gotTail, err := cursorHashes(file, off)
	return err == nil && gotHead == head && gotTail == tail
}

func hashRange(file *os.File, start, length int64) (string, error) {
	buf := make([]byte, length)
	if _, err := file.ReadAt(buf, start); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:8]), nil
}
