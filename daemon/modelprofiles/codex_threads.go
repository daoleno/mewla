package modelprofiles

import (
	"database/sql"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Codex lists and resumes a thread only on the provider its row names ("This
// chat uses mewla-gateway, but your current provider is freeride-xcode"), so
// when the takeover changes Codex's provider the threads follow it, as Magpie
// lends them (github.com/yetone/magpie, MIT: internal/sessions/codex_provider.go
// RetagPagedCodexThreads). Only paginated threads move, by their row in
// Codex's state database: Codex keeps places in a paginated rollout by byte
// and reads its provider from the row. A thread written to in the last minute
// may be running and waits for the next move (Heal tries again).

// codexThreadQuiet is how long a thread must be untouched before it moves.
const codexThreadQuiet = time.Minute

func (t *Takeover) moveThreads(from, to string) {
	if t == nil || t.configPath == "" {
		return
	}
	from, to = providerOrOpenAI(from), providerOrOpenAI(to)
	if from == to {
		return
	}
	moved, err := moveCodexThreads(filepath.Dir(t.configPath), from, to, time.Now().Add(-codexThreadQuiet))
	if err != nil {
		log.Printf("WARN codex threads: moving %s's threads to %s: %v", from, to, err)
	}
	if moved > 0 {
		log.Printf("codex threads: %d thread(s) on %s moved to %s, where Codex lists and resumes them", moved, from, to)
	}
}

// codexStateDBs are Codex's state databases in its home (CODEX_SQLITE_HOME
// when Codex is told to keep them elsewhere).
func codexStateDBs(home string) []string {
	dir := home
	if d := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); d != "" && filepath.Clean(home) == filepath.Dir(DefaultCodexConfigPath()) {
		dir = d
	}
	out, _ := filepath.Glob(filepath.Join(dir, "state_*.sqlite"))
	return out
}

// moveCodexThreads sets model_provider from -> to on the paginated threads
// last touched before quietBefore, in every state database of the Codex home.
// A database without those columns (another Codex version) is left alone.
func moveCodexThreads(home, from, to string, quietBefore time.Time) (int64, error) {
	var total int64
	var first error
	for _, path := range codexStateDBs(home) {
		n, err := moveCodexThreadsIn(path, from, to, quietBefore)
		total += n
		if err != nil && first == nil {
			first = err
		}
	}
	return total, first
}

func moveCodexThreadsIn(path, from, to string, quietBefore time.Time) (int64, error) {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('threads') WHERE name IN ('model_provider','history_mode','updated_at')`).Scan(&n); err != nil || n != 3 {
		return 0, nil
	}
	res, err := db.Exec(`UPDATE threads SET model_provider = ? WHERE model_provider = ? AND history_mode = 'paginated' AND updated_at < ?`, to, from, quietBefore.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
