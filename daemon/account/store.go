package account

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
CREATE TABLE IF NOT EXISTS accounts (
	id TEXT PRIMARY KEY,
	provider TEXT NOT NULL,
	subject TEXT NOT NULL UNIQUE,
	login TEXT NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS machines (
	id TEXT PRIMARY KEY,
	account_id TEXT NOT NULL REFERENCES accounts(id),
	daemon_public_key TEXT NOT NULL UNIQUE,
	route_id TEXT NOT NULL UNIQUE,
	label TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS machines_account ON machines(account_id);
CREATE TABLE IF NOT EXISTS logins (
	id TEXT PRIMARY KEY,
	daemon_public_key TEXT NOT NULL,
	machine_name TEXT NOT NULL,
	route_id TEXT NOT NULL,
	provider TEXT NOT NULL,
	provider_handle TEXT NOT NULL,
	status TEXT NOT NULL,
	interval_ms INTEGER NOT NULL,
	next_poll_at_ms INTEGER NOT NULL,
	expires_at_ms INTEGER NOT NULL,
	subject TEXT NOT NULL DEFAULT '',
	account_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS logins_key ON logins(daemon_public_key);
`

// Login statuses stored in logins.status.
const (
	loginPending  = "pending"
	loginComplete = "complete"
	loginDenied   = "denied"
	loginExpired  = "expired"
	loginQuota    = "quota"
	loginConflict = "conflict"
)

var (
	errNotFound      = errors.New("not found")
	errRouteConflict = errors.New("route is bound to another machine")
	errQuota         = errors.New("machine quota exceeded")
	errDisabled      = errors.New("account disabled")
)

// Store is the account service's SQLite database.
type Store struct {
	db *sql.DB
}

type accountRecord struct {
	ID       string
	Provider string
	Subject  string
	Login    string
	Disabled bool
}

type machineRecord struct {
	ID              string
	AccountID       string
	DaemonPublicKey string
	RouteID         string
	Label           string
	Name            string
}

type loginRecord struct {
	ID              string
	DaemonPublicKey string
	MachineName     string
	RouteID         string
	Provider        string
	ProviderHandle  string
	Status          string
	IntervalMS      int64
	NextPollAtMS    int64
	ExpiresAtMS     int64
	AccountID       string
}

// OpenStore opens (and migrates) the SQLite database at path.
func OpenStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writers; the service's load is tiny.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate account database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SetAccountDisabled bans or unbans an account. A ban takes effect for a
// running daemon when its current route grant expires (at most the grant TTL).
func (s *Store) SetAccountDisabled(ctx context.Context, accountID string, disabled bool) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE accounts SET disabled = ? WHERE id = ?`,
		boolInt(disabled),
		accountID,
	)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return errNotFound
	}
	return nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getAccount(ctx context.Context, q queryer, query string, arg string) (accountRecord, error) {
	var record accountRecord
	var disabled int64
	err := q.QueryRowContext(
		ctx,
		`SELECT id, provider, subject, login, disabled FROM accounts WHERE `+query,
		arg,
	).Scan(&record.ID, &record.Provider, &record.Subject, &record.Login, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return accountRecord{}, errNotFound
	}
	record.Disabled = disabled != 0
	return record, err
}

func getMachine(ctx context.Context, q queryer, query string, arg string) (machineRecord, error) {
	var record machineRecord
	err := q.QueryRowContext(
		ctx,
		`SELECT id, account_id, daemon_public_key, route_id, label, name FROM machines WHERE `+query,
		arg,
	).Scan(
		&record.ID,
		&record.AccountID,
		&record.DaemonPublicKey,
		&record.RouteID,
		&record.Label,
		&record.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return machineRecord{}, errNotFound
	}
	return record, err
}

// machineForKey returns the machine and its account for a daemon key.
func (s *Store) machineForKey(ctx context.Context, key string) (machineRecord, accountRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machineRecord{}, accountRecord{}, err
	}
	defer tx.Rollback()
	machine, err := getMachine(ctx, tx, `daemon_public_key = ?`, key)
	if err != nil {
		return machineRecord{}, accountRecord{}, err
	}
	account, err := getAccount(ctx, tx, `id = ?`, machine.AccountID)
	if err != nil {
		return machineRecord{}, accountRecord{}, err
	}
	return machine, account, tx.Commit()
}

func (s *Store) touchMachine(ctx context.Context, id string, nowMS int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE machines SET last_seen_at = ? WHERE id = ?`, nowMS, id)
	return err
}

// deleteMachine removes the machine for key and its logins.
func (s *Store) deleteMachine(ctx context.Context, key string) (machineRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machineRecord{}, err
	}
	defer tx.Rollback()
	machine, err := getMachine(ctx, tx, `daemon_public_key = ?`, key)
	if err != nil {
		return machineRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM machines WHERE id = ?`, machine.ID); err != nil {
		return machineRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM logins WHERE daemon_public_key = ?`, key); err != nil {
		return machineRecord{}, err
	}
	return machine, tx.Commit()
}

// renameMachine replaces the label of the machine for key with a unique
// label built from name.
func (s *Store) renameMachine(ctx context.Context, key, name string, newLabel func(string) (string, error)) (machineRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machineRecord{}, err
	}
	defer tx.Rollback()
	machine, err := getMachine(ctx, tx, `daemon_public_key = ?`, key)
	if err != nil {
		return machineRecord{}, err
	}
	for attempt := 0; attempt < 8; attempt++ {
		label, err := newLabel(name)
		if err != nil {
			return machineRecord{}, err
		}
		var taken int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM machines WHERE label = ?`, label).Scan(&taken); err != nil {
			return machineRecord{}, err
		}
		if taken != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE machines SET label = ?, name = ? WHERE id = ?`, label, name, machine.ID); err != nil {
			return machineRecord{}, err
		}
		machine.Label, machine.Name = label, name
		return machine, tx.Commit()
	}
	return machineRecord{}, errors.New("could not allocate a unique hostname label")
}

// routeOwner returns the daemon key bound to routeID, or "" when unbound.
func (s *Store) routeOwner(ctx context.Context, routeID string) (string, error) {
	machine, err := getMachine(ctx, s.db, `route_id = ?`, routeID)
	if errors.Is(err, errNotFound) {
		return "", nil
	}
	return machine.DaemonPublicKey, err
}

// createLogin prunes stale logins and inserts login unless key already has
// maxPending unexpired pending logins.
func (s *Store) createLogin(ctx context.Context, login loginRecord, nowMS int64, maxPending int) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM logins WHERE expires_at_ms < ?`,
		nowMS-staleLoginRetention.Milliseconds(),
	); err != nil {
		return false, err
	}
	var pending int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM logins WHERE daemon_public_key = ? AND status = ? AND expires_at_ms > ?`,
		login.DaemonPublicKey,
		loginPending,
		nowMS,
	).Scan(&pending); err != nil {
		return false, err
	}
	if pending >= maxPending {
		return false, nil
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO logins (id, daemon_public_key, machine_name, route_id, provider,
			provider_handle, status, interval_ms, next_poll_at_ms, expires_at_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		login.ID,
		login.DaemonPublicKey,
		login.MachineName,
		login.RouteID,
		login.Provider,
		login.ProviderHandle,
		loginPending,
		login.IntervalMS,
		login.NextPollAtMS,
		login.ExpiresAtMS,
		nowMS,
	); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) getLogin(ctx context.Context, id string) (loginRecord, error) {
	var record loginRecord
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, daemon_public_key, machine_name, route_id, provider, provider_handle,
			status, interval_ms, next_poll_at_ms, expires_at_ms, account_id
		FROM logins WHERE id = ?`,
		id,
	).Scan(
		&record.ID,
		&record.DaemonPublicKey,
		&record.MachineName,
		&record.RouteID,
		&record.Provider,
		&record.ProviderHandle,
		&record.Status,
		&record.IntervalMS,
		&record.NextPollAtMS,
		&record.ExpiresAtMS,
		&record.AccountID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return loginRecord{}, errNotFound
	}
	return record, err
}

// claimPoll reserves the next identity provider poll for a pending login. It
// returns false when the interval has not elapsed or another request won.
func (s *Store) claimPoll(ctx context.Context, login loginRecord, nowMS int64) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE logins SET next_poll_at_ms = ?
		WHERE id = ? AND status = ? AND next_poll_at_ms = ? AND next_poll_at_ms <= ?`,
		nowMS+login.IntervalMS,
		login.ID,
		loginPending,
		login.NextPollAtMS,
		nowMS,
	)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) slowDown(ctx context.Context, id string, intervalMS, nextPollAtMS int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE logins SET interval_ms = ?, next_poll_at_ms = ? WHERE id = ? AND status = ?`,
		intervalMS,
		nextPollAtMS,
		id,
		loginPending,
	)
	return err
}

func (s *Store) finishLogin(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE logins SET status = ?, provider_handle = '' WHERE id = ? AND status = ?`,
		status,
		id,
		loginPending,
	)
	return err
}

// completeLoginParams describes a successful identity provider login.
type completeLoginParams struct {
	Login       loginRecord
	Provider    string
	Subject     Subject
	MaxMachines int
	NowMS       int64
	NewID       func() (string, error)
	NewLabel    func(name string) (string, error)
}

// completeLogin upserts the account, binds the login's daemon key to one
// machine and marks the login complete, atomically. created reports whether
// a new machine row was inserted.
func (s *Store) completeLogin(ctx context.Context, p completeLoginParams) (machineRecord, accountRecord, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return machineRecord{}, accountRecord{}, false, err
	}
	defer tx.Rollback()

	subject := p.Provider + ":" + p.Subject.ID
	login := strings.TrimSpace(p.Subject.Login)
	if login == "" {
		login = p.Subject.ID
	}
	account, err := getAccount(ctx, tx, `subject = ?`, subject)
	switch {
	case errors.Is(err, errNotFound):
		id, idErr := p.NewID()
		if idErr != nil {
			return machineRecord{}, accountRecord{}, false, idErr
		}
		account = accountRecord{ID: id, Provider: p.Provider, Subject: subject, Login: login}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO accounts (id, provider, subject, login, disabled, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			account.ID,
			account.Provider,
			account.Subject,
			account.Login,
			p.NowMS,
		); err != nil {
			return machineRecord{}, accountRecord{}, false, err
		}
	case err != nil:
		return machineRecord{}, accountRecord{}, false, err
	default:
		if account.Login != login {
			account.Login = login
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET login = ? WHERE id = ?`, login, account.ID); err != nil {
				return machineRecord{}, accountRecord{}, false, err
			}
		}
	}
	if account.Disabled {
		return s.failLogin(ctx, tx, p.Login.ID, loginDenied, errDisabled)
	}

	key := p.Login.DaemonPublicKey
	routeMachine, err := getMachine(ctx, tx, `route_id = ?`, p.Login.RouteID)
	if err == nil && routeMachine.DaemonPublicKey != key {
		return s.failLogin(ctx, tx, p.Login.ID, loginConflict, errRouteConflict)
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return machineRecord{}, accountRecord{}, false, err
	}

	machine, err := getMachine(ctx, tx, `daemon_public_key = ?`, key)
	created := false
	switch {
	case errors.Is(err, errNotFound):
		if quotaErr := checkQuota(ctx, tx, account.ID, p.MaxMachines); quotaErr != nil {
			if errors.Is(quotaErr, errQuota) {
				return s.failLogin(ctx, tx, p.Login.ID, loginQuota, errQuota)
			}
			return machineRecord{}, accountRecord{}, false, quotaErr
		}
		machine, err = insertMachine(ctx, tx, p, account.ID)
		if err != nil {
			return machineRecord{}, accountRecord{}, false, err
		}
		created = true
	case err != nil:
		return machineRecord{}, accountRecord{}, false, err
	default:
		if machine.AccountID != account.ID {
			if quotaErr := checkQuota(ctx, tx, account.ID, p.MaxMachines); quotaErr != nil {
				if errors.Is(quotaErr, errQuota) {
					return s.failLogin(ctx, tx, p.Login.ID, loginQuota, errQuota)
				}
				return machineRecord{}, accountRecord{}, false, quotaErr
			}
		}
		machine.AccountID = account.ID
		machine.RouteID = p.Login.RouteID
		machine.Name = p.Login.MachineName
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE machines SET account_id = ?, route_id = ?, name = ?, last_seen_at = ? WHERE id = ?`,
			machine.AccountID,
			machine.RouteID,
			machine.Name,
			p.NowMS,
			machine.ID,
		); err != nil {
			return machineRecord{}, accountRecord{}, false, err
		}
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE logins SET status = ?, provider_handle = '', subject = ?, account_id = ? WHERE id = ?`,
		loginComplete,
		subject,
		account.ID,
		p.Login.ID,
	); err != nil {
		return machineRecord{}, accountRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return machineRecord{}, accountRecord{}, false, err
	}
	return machine, account, created, nil
}

// failLogin records a terminal login failure in tx and commits it, so the
// outcome is stable for later polls, then returns cause.
func (s *Store) failLogin(
	ctx context.Context,
	tx *sql.Tx,
	loginID string,
	status string,
	cause error,
) (machineRecord, accountRecord, bool, error) {
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE logins SET status = ?, provider_handle = '' WHERE id = ?`,
		status,
		loginID,
	); err != nil {
		return machineRecord{}, accountRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return machineRecord{}, accountRecord{}, false, err
	}
	return machineRecord{}, accountRecord{}, false, cause
}

func checkQuota(ctx context.Context, tx *sql.Tx, accountID string, maxMachines int) error {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM machines WHERE account_id = ?`,
		accountID,
	).Scan(&count); err != nil {
		return err
	}
	if count >= maxMachines {
		return errQuota
	}
	return nil
}

func insertMachine(ctx context.Context, tx *sql.Tx, p completeLoginParams, accountID string) (machineRecord, error) {
	id, err := p.NewID()
	if err != nil {
		return machineRecord{}, err
	}
	for attempt := 0; attempt < 8; attempt++ {
		label, err := p.NewLabel(p.Login.MachineName)
		if err != nil {
			return machineRecord{}, err
		}
		var taken int
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM machines WHERE label = ?`,
			label,
		).Scan(&taken); err != nil {
			return machineRecord{}, err
		}
		if taken != 0 {
			continue
		}
		machine := machineRecord{
			ID:              id,
			AccountID:       accountID,
			DaemonPublicKey: p.Login.DaemonPublicKey,
			RouteID:         p.Login.RouteID,
			Label:           label,
			Name:            p.Login.MachineName,
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO machines (id, account_id, daemon_public_key, route_id, label, name, created_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			machine.ID,
			machine.AccountID,
			machine.DaemonPublicKey,
			machine.RouteID,
			machine.Label,
			machine.Name,
			p.NowMS,
			p.NowMS,
		); err != nil {
			return machineRecord{}, err
		}
		return machine, nil
	}
	return machineRecord{}, errors.New("could not allocate a unique hostname label")
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
