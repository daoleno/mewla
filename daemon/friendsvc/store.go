package friendsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS people (
	id TEXT PRIMARY KEY,
	account_id TEXT NOT NULL UNIQUE,
	handle TEXT NOT NULL UNIQUE,
	handle_changed INTEGER NOT NULL DEFAULT 0,
	agent_name TEXT NOT NULL DEFAULT '',
	pet_id TEXT NOT NULL DEFAULT '',
	bio TEXT NOT NULL DEFAULT '',
	auto_post INTEGER NOT NULL DEFAULT 0,
	daily_cap INTEGER NOT NULL DEFAULT 3,
	tz TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS posts (
	id TEXT PRIMARY KEY,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	text TEXT NOT NULL,
	via TEXT NOT NULL CHECK (via IN ('approved', 'auto')),
	client_ref TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS posts_client_ref ON posts(person_id, client_ref) WHERE client_ref <> '';
CREATE INDEX IF NOT EXISTS posts_created ON posts(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS posts_person ON posts(person_id, created_at DESC);
CREATE TABLE IF NOT EXISTS comments (
	id TEXT PRIMARY KEY,
	post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	text TEXT NOT NULL,
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS comments_post ON comments(post_id, created_at);
CREATE TABLE IF NOT EXISTS likes (
	post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (post_id, person_id)
);
CREATE TABLE IF NOT EXISTS bookmarks (
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (person_id, post_id)
);
CREATE TABLE IF NOT EXISTS follows (
	follower_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	followee_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (follower_id, followee_id)
);
CREATE INDEX IF NOT EXISTS follows_followee ON follows(followee_id);
CREATE TABLE IF NOT EXISTS blocks (
	blocker_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	blocked_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (blocker_id, blocked_id)
);
CREATE TABLE IF NOT EXISTS reports (
	id TEXT PRIMARY KEY,
	reporter_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	kind TEXT NOT NULL CHECK (kind IN ('post', 'comment', 'person')),
	target_id TEXT NOT NULL,
	reason TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE (reporter_id, kind, target_id)
);
CREATE INDEX IF NOT EXISTS reports_target ON reports(kind, target_id);
CREATE TABLE IF NOT EXISTS notifications (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	actor_id TEXT NOT NULL DEFAULT '',
	post_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	read_at INTEGER
);
CREATE INDEX IF NOT EXISTS notifications_person ON notifications(person_id, seq);
`

var (
	errNotFound      = errors.New("not found")
	errHandleTaken   = errors.New("handle is taken")
	errHandleChanged = errors.New("handle was already changed once")
)

// Store is the Friends SQLite database.
type Store struct {
	db *sql.DB
}

// OpenStore opens (and migrates) the Friends database at path.
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("friends database path is empty")
	}
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate friends database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// personColumns and scanPerson read the whole person row.
const personColumns = `id, account_id, handle, handle_changed, agent_name, pet_id, bio, auto_post, daily_cap, tz, hidden, created_at`

type personRecord struct {
	ID            string
	AccountID     string
	Handle        string
	HandleChanged bool
	AgentName     string
	PetID         string
	Bio           string
	AutoPost      bool
	DailyCap      int
	TZ            string
	Hidden        bool
	CreatedAt     int64
}

func scanPerson(row interface{ Scan(...any) error }) (personRecord, error) {
	var p personRecord
	var changed, auto, hidden int
	err := row.Scan(&p.ID, &p.AccountID, &p.Handle, &changed, &p.AgentName, &p.PetID, &p.Bio, &auto, &p.DailyCap, &p.TZ, &hidden, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return personRecord{}, errNotFound
	}
	p.HandleChanged, p.AutoPost, p.Hidden = changed != 0, auto != 0, hidden != 0
	return p, err
}

func (s *Store) personByAccount(ctx context.Context, accountID string) (personRecord, error) {
	return scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE account_id = ?`, accountID))
}

func (s *Store) personByID(ctx context.Context, id string) (personRecord, error) {
	return scanPerson(s.db.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE id = ?`, id))
}

func (s *Store) handleTaken(ctx context.Context, handle string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM people WHERE handle = ?`, handle).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) createPerson(ctx context.Context, p personRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO people (id, account_id, handle, agent_name, pet_id, tz, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.AccountID, p.Handle, p.AgentName, p.PetID, p.TZ, p.CreatedAt)
	return err
}

// deletePerson removes a person and everything that hangs on them: their
// posts, comments, likes, bookmarks, follows, blocks, reports they filed and
// notifications; and the reports filed against their content.
func (s *Store) deletePerson(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM reports WHERE kind = 'post' AND target_id IN (SELECT id FROM posts WHERE person_id = ?1)`,
		`DELETE FROM reports WHERE kind = 'comment' AND (target_id IN (SELECT id FROM comments WHERE person_id = ?1)
			OR target_id IN (SELECT c.id FROM comments c JOIN posts p ON p.id = c.post_id WHERE p.person_id = ?1))`,
		`DELETE FROM reports WHERE kind = 'person' AND target_id = ?1`,
		// Notifications about this person's actions on other people's things.
		`DELETE FROM notifications WHERE actor_id = ?1`,
		`DELETE FROM people WHERE id = ?1`,
	} {
		if _, err := tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DB exposes the database to tests of packages that embed the service.
func (s *Store) DB() *sql.DB { return s.db }
