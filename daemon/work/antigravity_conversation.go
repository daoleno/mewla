package work

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// antigravityConversationIDPattern is the id of an agy conversation, the value
// of its --conversation flag: a lower-case UUID. agy never accepts an id a
// caller made up (an unknown id is "not found" and starts a new conversation),
// so Mewla reads the id agy chose instead of assigning one.
var antigravityConversationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// AntigravityDataDir is agy's own directory for conversations, settings and
// logs: ~/.gemini/antigravity-cli. agy has no environment variable or flag
// that moves it, so an isolated agy is an isolated HOME.
func AntigravityDataDir(home string) string {
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// AntigravityConversation is one row of agy's conversation_summaries.db.
type AntigravityConversation struct {
	ID         string
	CWDs       []string
	ModifiedAt time.Time
	// Status is agy's run status for the conversation, for example
	// CASCADE_RUN_STATUS_IDLE when no turn is running.
	Status string
	// NotFullyIdle is agy's flag for a conversation that still has work going
	// after a turn (background tasks or sub-agents).
	NotFullyIdle bool
	Killed       bool
	Preview      string
}

// Idle reports that agy has no turn or background work running in the
// conversation.
func (c AntigravityConversation) Idle() bool {
	return !c.Killed && !c.NotFullyIdle && strings.HasSuffix(c.Status, "_IDLE")
}

// ListAntigravityConversations reads agy's conversation summaries, read only
// (agy owns the database and keeps it open), newest first. Sub-agent
// conversations (those with a parent) are left out. A missing database is no
// conversations, not an error: agy has not run for this person yet.
func ListAntigravityConversations(home string) ([]AntigravityConversation, error) {
	path := filepath.Join(AntigravityDataDir(home), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("open agy conversations: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query(`SELECT conversation_id, workspace_uris, last_modified_time, status, not_fully_idle, killed, preview
		FROM conversation_summaries WHERE parent_conversation_id = '' ORDER BY last_modified_time DESC`)
	if err != nil {
		return nil, fmt.Errorf("query agy conversations: %w", err)
	}
	defer rows.Close()
	var out []AntigravityConversation
	for rows.Next() {
		var id, uris, modified, status, preview sql.NullString
		var notIdle, killed sql.NullBool
		if err := rows.Scan(&id, &uris, &modified, &status, &notIdle, &killed, &preview); err != nil {
			return nil, fmt.Errorf("read agy conversation: %w", err)
		}
		if !antigravityConversationIDPattern.MatchString(id.String) {
			continue
		}
		out = append(out, AntigravityConversation{
			ID:           id.String,
			CWDs:         antigravityWorkspacePaths(uris.String),
			ModifiedAt:   parseAntigravityTime(modified.String),
			Status:       status.String,
			NotFullyIdle: notIdle.Bool,
			Killed:       killed.Bool,
			Preview:      preview.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query agy conversations: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModifiedAt.After(out[j].ModifiedAt) })
	return out, nil
}

// FindAntigravityConversation returns the conversation a Worker started in cwd
// at or after startedAt: the newest top-level conversation whose workspace is
// cwd and that was last written after the launch. A conversation older than the
// launch is never returned, so a new Worker cannot bind to the person's earlier
// one in the same folder.
func FindAntigravityConversation(home, cwd string, startedAt time.Time) (AntigravityConversation, bool, error) {
	conversations, err := ListAntigravityConversations(home)
	if err != nil {
		return AntigravityConversation{}, false, err
	}
	want := cleanAntigravityPath(cwd)
	for _, conversation := range conversations {
		if conversation.ModifiedAt.Before(startedAt) {
			continue
		}
		for _, dir := range conversation.CWDs {
			if dir == want {
				return conversation, true, nil
			}
		}
	}
	return AntigravityConversation{}, false, nil
}

func cleanAntigravityPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

func antigravityWorkspacePaths(raw string) []string {
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		return nil
	}
	var out []string
	for _, value := range uris {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "file" {
			continue
		}
		if path := cleanAntigravityPath(parsed.Path); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func parseAntigravityTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
