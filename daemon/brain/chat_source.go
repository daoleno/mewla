package brain

import (
	"fmt"

	"github.com/daoleno/mewla/daemon/memorysearch"
)

// The chat log as memory search reads it: the archive of rows earlier repairs
// removed from the live timeline first, then the live timeline. Each source
// grows only at its end, so a reader keeps one position per source.
const (
	chatSourceArchive = "archive"
	chatSourceLive    = "live"
)

var _ memorysearch.Source = (*Store)(nil)

// ChatSources names the sources of the chat log in reading order.
func (s *Store) ChatSources() []string {
	return []string{chatSourceArchive, chatSourceLive}
}

// ChatRows returns up to limit rows of a chat source whose position is after
// the given one, oldest first.
func (s *Store) ChatRows(source string, after int64, limit int) ([]memorysearch.ChatRow, error) {
	var query string
	switch source {
	case chatSourceArchive:
		query = `SELECT seq, body FROM timeline_archive WHERE seq > ? ORDER BY seq LIMIT ?`
	case chatSourceLive:
		query = `SELECT seq, body FROM timeline WHERE seq > ? ORDER BY seq LIMIT ?`
	default:
		return nil, fmt.Errorf("unknown chat source %q", source)
	}
	rows, err := s.db.Query(query, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memorysearch.ChatRow
	for rows.Next() {
		var row memorysearch.ChatRow
		var body string
		if err := rows.Scan(&row.Seq, &body); err != nil {
			return nil, err
		}
		row.Body = []byte(body)
		out = append(out, row)
	}
	return out, rows.Err()
}
