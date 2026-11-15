package work

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// queryCodexThreadRows reads thread rows from Codex's state database
// (state_5.sqlite) in this process, read only. Codex keeps the database open
// in WAL mode, so the read sees its latest commits, and a write in progress is
// waited for briefly instead of failing the poll. Nothing is forked: Windows
// has no sqlite3 program, and when this ran one, no Codex thread was ever
// found there, so a Codex Brain's replies never reached Chat.
func queryCodexThreadRows(dbPath, query string, args ...any) ([]codexThreadRow, error) {
	codexLookupCost.Queries.Add(1)
	db, err := sql.Open("sqlite", codexStateDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open codex state: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query codex threads: %w", err)
	}
	defer rows.Close()
	var out []codexThreadRow
	for rows.Next() {
		var id, rollout sql.NullString
		var created, createdMS sql.NullInt64
		if err := rows.Scan(&id, &rollout, &created, &createdMS); err != nil {
			return nil, fmt.Errorf("read codex thread: %w", err)
		}
		out = append(out, codexThreadRow{ID: id.String, RolloutPath: rollout.String, CreatedAt: created.Int64, CreatedAtMS: createdMS.Int64})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query codex threads: %w", err)
	}
	return out, nil
}

// codexStateDSN opens path read only. It never writes, migrates or changes
// the journal mode of a database another program owns.
func codexStateDSN(path string) string {
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?mode=ro&_pragma=busy_timeout(2000)"
}

// codexRecordedCWDs are the spellings Codex may have recorded for cwd in its
// threads table. On Windows Codex records the extended-length form
// (\\?\C:\Users\..., \\?\UNC\server\share\...) there, while each rollout's
// session_meta keeps the plain path.
func codexRecordedCWDs(cwd string) []string {
	out := []string{cwd}
	if filepath.VolumeName(cwd) == "" || strings.HasPrefix(cwd, `\\?\`) {
		return out
	}
	if strings.HasPrefix(cwd, `\\`) {
		return append(out, `\\?\UNC\`+strings.TrimPrefix(cwd, `\\`))
	}
	return append(out, `\\?\`+cwd)
}
