package memorysearch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ftsMinRunes is the shortest term the trigram tokenizer can match; shorter
// terms fall back to LIKE.
const ftsMinRunes = 3

const (
	snippetBefore = 60
	snippetAfter  = 100
)

// Hit is one search result. Duplicates counts older messages with the same
// text from the same side that were collapsed into this newest one.
type Hit struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Who        string    `json:"who"`
	Snippet    string    `json:"snippet"`
	WorkID     string    `json:"work_id,omitempty"`
	Duplicates int       `json:"duplicates,omitempty"`
}

// Message is one message returned by Show. Match marks the requested one.
type Message struct {
	ID     string    `json:"id"`
	At     time.Time `json:"at"`
	Who    string    `json:"who"`
	Text   string    `json:"text"`
	WorkID string    `json:"work_id,omitempty"`
	Match  bool      `json:"match,omitempty"`
}

// SearchOptions filters a search. From is "user", "brain", "work" or
// "all"/"" (everything). Zero Since/Until are open-ended.
type SearchOptions struct {
	Query string
	From  string
	Since time.Time
	Until time.Time
	Limit int
}

// Search returns hits for opts.Query: exact-phrase matches first, then
// messages containing every whitespace-separated term, newest first within
// each group, identical texts collapsed.
func (ix *Index) Search(ctx context.Context, opts SearchOptions) ([]Hit, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errors.New("search query required")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	var filters []string
	var filterArgs []any
	switch from := strings.ToLower(strings.TrimSpace(opts.From)); from {
	case "", "all":
	case WhoUser, WhoBrain, WhoWork:
		filters = append(filters, "m.who = ?")
		filterArgs = append(filterArgs, from)
	default:
		return nil, fmt.Errorf("--from must be user, brain or all, got %q", opts.From)
	}
	if !opts.Since.IsZero() {
		filters = append(filters, "m.at >= ?")
		filterArgs = append(filterArgs, opts.Since.UnixNano())
	}
	if !opts.Until.IsZero() {
		filters = append(filters, "m.at <= ?")
		filterArgs = append(filterArgs, opts.Until.UnixNano())
	}

	tiers := [][]string{{query}}
	if terms := strings.Fields(query); len(terms) > 1 {
		tiers = append(tiers, terms)
	}

	var hits []Hit
	seenID := map[string]bool{}
	seenText := map[string]int{} // dedupe key -> index in hits
	for _, terms := range tiers {
		if len(hits) >= limit {
			break
		}
		rows, err := ix.candidates(ctx, terms, filters, filterArgs)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if seenID[row.id] {
				continue
			}
			seenID[row.id] = true
			if at, ok := seenText[row.dedupe]; ok {
				hits[at].Duplicates++
				continue
			}
			if len(hits) >= limit {
				continue // keep counting duplicates of listed hits only
			}
			seenText[row.dedupe] = len(hits)
			hits = append(hits, Hit{
				ID: row.id, At: time.Unix(0, row.at).UTC(), Who: row.who,
				Snippet: snippet(row.body, terms), WorkID: row.workID,
			})
		}
	}
	return hits, nil
}

type candidate struct {
	id, who, workID, body, dedupe string
	at                            int64
}

// candidates returns messages containing every term, newest first. A single
// term is the exact phrase.
func (ix *Index) candidates(ctx context.Context, terms []string, filters []string, filterArgs []any) ([]candidate, error) {
	var conds []string
	var args []any
	var match []string
	for _, term := range terms {
		if utf8.RuneCountInString(term) >= ftsMinRunes {
			match = append(match, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
		} else {
			conds = append(conds, `m.body LIKE ? ESCAPE '\'`)
			args = append(args, "%"+likeEscape(term)+"%")
		}
	}
	if len(match) > 0 {
		conds = append(conds, `m.seq IN (SELECT rowid FROM msg_fts WHERE msg_fts MATCH ?)`)
		args = append(args, strings.Join(match, " AND "))
	}
	conds = append(conds, filters...)
	args = append(args, filterArgs...)
	args = append(args, maxCandidates)
	rows, err := ix.db.QueryContext(ctx,
		`SELECT m.id, m.at, m.who, m.work_id, m.body, m.dedupe FROM msg m
		 WHERE `+strings.Join(conds, " AND ")+`
		 ORDER BY m.at DESC, m.seq DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.at, &c.who, &c.workID, &c.body, &c.dedupe); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// snippet returns a one-line window of body around the first matching term.
func snippet(body string, terms []string) string {
	runes := []rune(body)
	lower := []rune(strings.ToLower(body))
	at := -1
	for _, term := range terms {
		if i := indexRunes(lower, []rune(strings.ToLower(term))); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	start, end := 0, min(len(runes), snippetBefore+snippetAfter)
	if at >= 0 {
		start = max(at-snippetBefore, 0)
		end = min(at+snippetAfter, len(runes))
	}
	text := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		text = "…" + text
	}
	if end < len(runes) {
		text += "…"
	}
	return text
}

func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j, r := range needle {
			if haystack[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// maxAround bounds how many neighbours Show returns on each side.
const maxAround = 50

// Show returns the message with the given id (or a unique id prefix) with up
// to around messages before and after it in chat order.
func (ix *Index) Show(ctx context.Context, id string, around int) ([]Message, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("message id required")
	}
	around = min(max(around, 0), maxAround)

	var seq, at int64
	err := ix.db.QueryRowContext(ctx, `SELECT seq, at FROM msg WHERE id = ?`, id).Scan(&seq, &at)
	if errors.Is(err, sql.ErrNoRows) {
		matches, qerr := ix.idsWithPrefix(ctx, id)
		if qerr != nil {
			return nil, qerr
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		case 1:
			return ix.Show(ctx, matches[0], around)
		default:
			return nil, fmt.Errorf("id prefix %q is ambiguous (%d messages); use the full id", id, len(matches))
		}
	}
	if err != nil {
		return nil, err
	}

	before, err := ix.neighbours(ctx, `(at, seq) < (?, ?) ORDER BY at DESC, seq DESC`, at, seq, around)
	if err != nil {
		return nil, err
	}
	self, err := ix.neighbours(ctx, `seq = ? AND at = ? ORDER BY seq`, seq, at, 1)
	if err != nil {
		return nil, err
	}
	after, err := ix.neighbours(ctx, `(at, seq) > (?, ?) ORDER BY at ASC, seq ASC`, at, seq, around)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(before)-1; i < j; i, j = i+1, j-1 {
		before[i], before[j] = before[j], before[i]
	}
	if len(self) == 1 {
		self[0].Match = true
	}
	return append(append(before, self...), after...), nil
}

func (ix *Index) idsWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	if utf8.RuneCountInString(prefix) < 4 {
		return nil, nil
	}
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id FROM msg WHERE id LIKE ? ESCAPE '\' LIMIT 3`, likeEscape(prefix)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (ix *Index) neighbours(ctx context.Context, where string, a, b int64, limit int) ([]Message, error) {
	if limit == 0 {
		return nil, nil
	}
	rows, err := ix.db.QueryContext(ctx,
		`SELECT id, at, who, work_id, body FROM msg WHERE `+where+` LIMIT ?`, a, b, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var at int64
		if err := rows.Scan(&m.ID, &at, &m.Who, &m.WorkID, &m.Text); err != nil {
			return nil, err
		}
		m.At = time.Unix(0, at).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}
