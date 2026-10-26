package friendsvc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS people (
	id TEXT PRIMARY KEY,
	github_id TEXT NOT NULL UNIQUE,
	github_login TEXT NOT NULL,
	handle TEXT NOT NULL UNIQUE,
	handle_changed INTEGER NOT NULL DEFAULT 0,
	github_created_at INTEGER NOT NULL,
	auto_post INTEGER NOT NULL DEFAULT 0,
	daily_cap INTEGER NOT NULL DEFAULT 3,
	tz TEXT NOT NULL DEFAULT '',
	disabled INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS human_keys (
	pubkey TEXT PRIMARY KEY,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	device_label TEXT NOT NULL DEFAULT '',
	push_token TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS human_keys_person ON human_keys(person_id);
CREATE TABLE IF NOT EXISTS agent_keys (
	pubkey TEXT PRIMARY KEY,
	person_id TEXT NOT NULL UNIQUE REFERENCES people(id) ON DELETE CASCADE,
	name TEXT NOT NULL DEFAULT '',
	pet_id TEXT NOT NULL DEFAULT '',
	bio TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS link_codes (
	hash TEXT PRIMARY KEY,
	person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	used_at INTEGER
);
CREATE INDEX IF NOT EXISTS link_codes_person ON link_codes(person_id);
CREATE TABLE IF NOT EXISTS enroll_sessions (
	state TEXT PRIMARY KEY,
	human_pubkey TEXT NOT NULL,
	return_url TEXT NOT NULL,
	ticket TEXT UNIQUE,
	challenge TEXT NOT NULL DEFAULT '',
	github_id TEXT NOT NULL DEFAULT '',
	github_login TEXT NOT NULL DEFAULT '',
	github_created_at INTEGER NOT NULL DEFAULT 0,
	expires_at INTEGER NOT NULL,
	used INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
`

var (
	errNotFound      = errors.New("not found")
	errHandleTaken   = errors.New("handle is taken")
	errHandleChanged = errors.New("handle was already changed once")
	errKeyInUse      = errors.New("key is registered as another credential")
	errAlreadyLinked = errors.New("github account already has friends")
	errDeviceLimit   = errors.New("device limit reached")
)

const maxDevicesPerPerson = 5

// Store is the Friends SQLite database.
type Store struct {
	db *sql.DB
}

type personRecord struct {
	ID              string
	GitHubID        string
	GitHubLogin     string
	Handle          string
	HandleChanged   bool
	GitHubCreatedAt int64
	AutoPost        bool
	DailyCap        int
	Disabled        bool
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

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

const personColumns = `id, github_id, github_login, handle, handle_changed, github_created_at, auto_post, daily_cap, disabled`

func scanPerson(row interface{ Scan(...any) error }) (personRecord, error) {
	var p personRecord
	var changed, auto, disabled int
	err := row.Scan(&p.ID, &p.GitHubID, &p.GitHubLogin, &p.Handle, &changed, &p.GitHubCreatedAt, &auto, &p.DailyCap, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return personRecord{}, errNotFound
	}
	p.HandleChanged, p.AutoPost, p.Disabled = changed != 0, auto != 0, disabled != 0
	return p, err
}

// humanPerson returns the person who owns a registered human key.
func (s *Store) humanPerson(ctx context.Context, key string, nowMS int64) (personRecord, error) {
	p, err := scanPerson(s.db.QueryRowContext(ctx,
		`SELECT `+personColumns+` FROM people WHERE id = (SELECT person_id FROM human_keys WHERE pubkey = ?)`, key))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE human_keys SET last_seen_at = ? WHERE pubkey = ?`, nowMS, key)
	}
	return p, err
}

// agentPerson returns the person who owns a registered agent key.
func (s *Store) agentPerson(ctx context.Context, key string, nowMS int64) (personRecord, error) {
	p, err := scanPerson(s.db.QueryRowContext(ctx,
		`SELECT `+personColumns+` FROM people WHERE id = (SELECT person_id FROM agent_keys WHERE pubkey = ?)`, key))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE agent_keys SET last_seen_at = ? WHERE pubkey = ?`, nowMS, key)
	}
	return p, err
}

func (s *Store) isHumanKey(ctx context.Context, key string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM human_keys WHERE pubkey = ?`, key)
}

func (s *Store) isAgentKey(ctx context.Context, key string) (bool, error) {
	return s.exists(ctx, `SELECT 1 FROM agent_keys WHERE pubkey = ?`, key)
}

func (s *Store) exists(ctx context.Context, query string, args ...any) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

type enrollSession struct {
	State           string
	HumanKey        string
	ReturnURL       string
	Ticket          string
	Challenge       string
	GitHubID        string
	GitHubLogin     string
	GitHubCreatedAt int64
	ExpiresAtMS     int64
	Used            bool
}

func (s *Store) createEnrollSession(ctx context.Context, state, humanKey, returnURL string, nowMS, expiresMS int64) error {
	// Sessions that were never finished are not kept.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM enroll_sessions WHERE expires_at < ?`, nowMS); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enroll_sessions (state, human_pubkey, return_url, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		state, humanKey, returnURL, expiresMS, nowMS)
	return err
}

func (s *Store) enrollByState(ctx context.Context, state string) (enrollSession, error) {
	return s.scanEnroll(s.db.QueryRowContext(ctx,
		`SELECT state, human_pubkey, return_url, COALESCE(ticket, ''), challenge, github_id, github_login, github_created_at, expires_at, used
		 FROM enroll_sessions WHERE state = ?`, state))
}

func (s *Store) enrollByTicket(ctx context.Context, ticket string) (enrollSession, error) {
	return s.scanEnroll(s.db.QueryRowContext(ctx,
		`SELECT state, human_pubkey, return_url, COALESCE(ticket, ''), challenge, github_id, github_login, github_created_at, expires_at, used
		 FROM enroll_sessions WHERE ticket = ?`, ticket))
}

func (s *Store) scanEnroll(row *sql.Row) (enrollSession, error) {
	var e enrollSession
	var used int
	err := row.Scan(&e.State, &e.HumanKey, &e.ReturnURL, &e.Ticket, &e.Challenge, &e.GitHubID, &e.GitHubLogin, &e.GitHubCreatedAt, &e.ExpiresAtMS, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return enrollSession{}, errNotFound
	}
	e.Used = used != 0
	return e, err
}

// completeEnrollSession records the GitHub identity, once, and the ticket and
// challenge the phone must answer.
func (s *Store) completeEnrollSession(ctx context.Context, state, ticket, challenge string, identity Identity) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE enroll_sessions SET ticket = ?, challenge = ?, github_id = ?, github_login = ?, github_created_at = ?
		 WHERE state = ? AND ticket IS NULL AND used = 0`,
		ticket, challenge, identity.ID, identity.Login, identity.CreatedAt.UnixMilli(), state)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// finishEnroll creates the person (or recognizes the same device) and
// registers the human key, in one transaction.
func (s *Store) finishEnroll(ctx context.Context, e enrollSession, personID, handle, deviceLabel string, nowMS int64) (personRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return personRecord{}, err
	}
	defer tx.Rollback()

	var agent int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_keys WHERE pubkey = ?`, e.HumanKey).Scan(&agent); err != nil {
		return personRecord{}, err
	}
	if agent > 0 {
		return personRecord{}, errKeyInUse
	}
	person, err := scanPerson(tx.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE github_id = ?`, e.GitHubID))
	switch {
	case err == nil:
		var mine int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_keys WHERE pubkey = ? AND person_id = ?`, e.HumanKey, person.ID).Scan(&mine); err != nil {
			return personRecord{}, err
		}
		if mine == 0 {
			// A second device needs approval from an existing one; that
			// approval is not built, so a second sign-in is refused.
			return personRecord{}, errAlreadyLinked
		}
	case errors.Is(err, errNotFound):
		taken, err := s.handleTaken(ctx, tx, handle)
		if err != nil {
			return personRecord{}, err
		}
		if taken {
			return personRecord{}, errHandleTaken
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO people (id, github_id, github_login, handle, github_created_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			personID, e.GitHubID, e.GitHubLogin, handle, e.GitHubCreatedAt, nowMS); err != nil {
			return personRecord{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO human_keys (pubkey, person_id, device_label, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
			e.HumanKey, personID, deviceLabel, nowMS, nowMS); err != nil {
			return personRecord{}, err
		}
		person, err = scanPerson(tx.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE id = ?`, personID))
		if err != nil {
			return personRecord{}, err
		}
	default:
		return personRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE enroll_sessions SET used = 1 WHERE state = ?`, e.State); err != nil {
		return personRecord{}, err
	}
	return person, tx.Commit()
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Store) handleTaken(ctx context.Context, q queryer, handle string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM people WHERE handle = ?`, handle).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) handleIsTaken(ctx context.Context, handle string) (bool, error) {
	return s.handleTaken(ctx, s.db, handle)
}

func (s *Store) changeHandle(ctx context.Context, personID, handle string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var changed int
	if err := tx.QueryRowContext(ctx, `SELECT handle_changed FROM people WHERE id = ?`, personID).Scan(&changed); err != nil {
		return err
	}
	if changed != 0 {
		return errHandleChanged
	}
	taken, err := s.handleTaken(ctx, tx, handle)
	if err != nil {
		return err
	}
	if taken {
		return errHandleTaken
	}
	if _, err := tx.ExecContext(ctx, `UPDATE people SET handle = ?, handle_changed = 1 WHERE id = ?`, handle, personID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) deviceCount(ctx context.Context, personID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_keys WHERE person_id = ?`, personID).Scan(&n)
	return n, err
}

func (s *Store) agentName(ctx context.Context, personID string) (string, bool, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM agent_keys WHERE person_id = ?`, personID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return name, err == nil, err
}

func (s *Store) createLinkCode(ctx context.Context, personID, code string, nowMS, expiresMS int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM link_codes WHERE expires_at < ? OR used_at IS NOT NULL`, nowMS); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO link_codes (hash, person_id, expires_at) VALUES (?, ?, ?)`, hashCode(code), personID, expiresMS)
	return err
}

// consumeLinkCode spends a code and registers key as its person's agent,
// replacing any earlier agent key. It reports whether a key was replaced.
func (s *Store) consumeLinkCode(ctx context.Context, code, key, name string, nowMS int64) (personRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return personRecord{}, false, err
	}
	defer tx.Rollback()
	var personID string
	err = tx.QueryRowContext(ctx,
		`SELECT person_id FROM link_codes WHERE hash = ? AND used_at IS NULL AND expires_at >= ?`, hashCode(code), nowMS).Scan(&personID)
	if errors.Is(err, sql.ErrNoRows) {
		return personRecord{}, false, errNotFound
	}
	if err != nil {
		return personRecord{}, false, err
	}
	var human int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM human_keys WHERE pubkey = ?`, key).Scan(&human); err != nil {
		return personRecord{}, false, err
	}
	if human > 0 {
		return personRecord{}, false, errKeyInUse
	}
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT person_id FROM agent_keys WHERE pubkey = ?`, key).Scan(&owner)
	if err == nil && owner != personID {
		return personRecord{}, false, errKeyInUse
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return personRecord{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE link_codes SET used_at = ? WHERE hash = ?`, nowMS, hashCode(code)); err != nil {
		return personRecord{}, false, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM agent_keys WHERE person_id = ? AND pubkey <> ?`, personID, key)
	if err != nil {
		return personRecord{}, false, err
	}
	replaced := false
	if n, _ := result.RowsAffected(); n > 0 {
		replaced = true
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO agent_keys (pubkey, person_id, name, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(pubkey) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		key, personID, name, nowMS, nowMS); err != nil {
		return personRecord{}, false, err
	}
	person, err := scanPerson(tx.QueryRowContext(ctx, `SELECT `+personColumns+` FROM people WHERE id = ?`, personID))
	if err != nil {
		return personRecord{}, false, err
	}
	return person, replaced, tx.Commit()
}
