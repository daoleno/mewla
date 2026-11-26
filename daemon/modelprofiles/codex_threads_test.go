package modelprofiles

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedCodexThreads(t *testing.T, home string, rows map[string][3]any) string {
	t.Helper()
	path := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, model_provider TEXT NOT NULL, history_mode TEXT NOT NULL DEFAULT 'legacy', updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for id, row := range rows {
		if _, err := db.Exec(`INSERT INTO threads (id, model_provider, history_mode, updated_at) VALUES (?, ?, ?, ?)`, id, row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func codexThreadProviders(t *testing.T, path string) map[string]string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, model_provider FROM threads`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, provider string
		if err := rows.Scan(&id, &provider); err != nil {
			t.Fatal(err)
		}
		out[id] = provider
	}
	return out
}

// Codex lists and resumes a thread only on the provider its row names: the
// threads follow Codex onto the gateway and back to the user's provider.
func TestTakeoverThreadsFollowTheProvider(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	old := time.Now().Add(-time.Hour).Unix()
	db := seedCodexThreads(t, filepath.Dir(path), map[string][3]any{
		"mine":    {"freeride-xcode", "paginated", old},
		"running": {"freeride-xcode", "paginated", time.Now().Unix()},
		"legacy":  {"freeride-xcode", "legacy", old},
		"other":   {"packycode", "paginated", old},
	})
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	got := codexThreadProviders(t, db)
	want := map[string]string{"mine": GatewayProviderName, "running": "freeride-xcode", "legacy": "freeride-xcode", "other": "packycode"}
	for id, provider := range want {
		if got[id] != provider {
			t.Fatalf("after connect %s is on %q, want %q (%v)", id, got[id], provider, got)
		}
	}
	if _, err := takeover.Disable(); err != nil {
		t.Fatal(err)
	}
	got = codexThreadProviders(t, db)
	for id, provider := range map[string]string{"mine": "freeride-xcode", "running": "freeride-xcode", "legacy": "freeride-xcode", "other": "packycode"} {
		if got[id] != provider {
			t.Fatalf("after disconnect %s is on %q, want %q (%v)", id, got[id], provider, got)
		}
	}
}

// A Codex without a threads table (another version) is left alone.
func TestTakeoverThreadsLeaveAnUnknownDatabaseAlone(t *testing.T) {
	takeover, path, _ := coexistFixture(t)
	dbPath := filepath.Join(filepath.Dir(path), "state_9.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, _ := os.ReadFile(dbPath)
	if _, err := takeover.Enable(takeover.gateway.ActualAddr()); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(dbPath); string(after) != string(before) {
		t.Fatal("an unknown database was written")
	}
}
