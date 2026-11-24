package gc

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Where a file system records no reads (this machine's / is mounted
// noatime), an old unmarked t/ item holding files cannot be proven unread, so
// the sweep neither takes it nor, as it did, keeps it for ever without a word:
// it goes to review. The collector asks the agent that owns t/ (Brain) about
// the new ones, at most once a day; the answers are the verbs that already
// exist, `mewla gc keep` and `mewla gc release`. Nothing here moves anything.

// ReviewAskEvery is the least time between two askings.
const ReviewAskEvery = 24 * time.Hour

// Review answers.
const (
	AnswerKeep    = "keep"    // Brain keeps it until a date
	AnswerRelease = "release" // it went to the trash
	AnswerGone    = "gone"    // it is not there any more
	AnswerUsed    = "used"    // it changed, was kept or was used since
)

// Unproven is an unmarked t/ item the sweep could not prove unread.
type Unproven struct {
	Path      string
	Bytes     int64
	LastWrite time.Time
	Creator   string // the Session that made it, when its marker says so
	Looks     string // what it looks like
	PreRule   bool   // it was in t/ before the sweep rule came
}

// Review is a row of the review table.
type Review struct {
	Path       string    `json:"path"`
	Bytes      int64     `json:"bytes"`
	LastWrite  time.Time `json:"last_write"`
	Creator    string    `json:"creator,omitempty"`
	Looks      string    `json:"looks,omitempty"`
	PreRule    bool      `json:"pre_rule,omitempty"`
	FirstSeen  time.Time `json:"first_seen"`
	AskedAt    time.Time `json:"asked_at,omitempty"`
	Answer     string    `json:"answer,omitempty"`
	AnsweredAt time.Time `json:"answered_at,omitempty"`
}

func (r Review) unproven() Unproven {
	return Unproven{Path: r.Path, Bytes: r.Bytes, LastWrite: r.LastWrite, Creator: r.Creator, Looks: r.Looks, PreRule: r.PreRule}
}

// tally is what one walk of an item counted.
type tally struct {
	bytes  int64
	newest time.Time
	exts   map[string]int
}

func (t tally) unproven(stateRoot, path string, info os.FileInfo, preRule bool) Unproven {
	creator, looks := describe(stateRoot, path, info, t)
	return Unproven{Path: path, Bytes: t.bytes, LastWrite: t.newest, Creator: creator, Looks: looks, PreRule: preRule}
}

// describe reads a few small files to say who made an item and what it looks
// like: nothing is run and nothing is read beyond four bytes of a file.
func describe(stateRoot, path string, info os.FileInfo, t tally) (creator, looks string) {
	for _, marker := range []string{scratchMarker, ".zen-agent-unit"} {
		raw, err := os.ReadFile(filepath.Join(path, marker))
		if err != nil {
			continue
		}
		unit := strings.TrimSpace(string(raw))
		if unit == "" {
			continue
		}
		if s, ok, _ := SessionOfUnit(stateRoot, unit); ok {
			creator = "Session " + s.SessionID
		} else {
			creator = "unit " + unit
		}
		break
	}
	switch {
	case info.Mode().IsRegular():
		if isELF(path) {
			looks = "a binary"
		}
	case exists(filepath.Join(path, "CACHEDIR.TAG")):
		looks = "a cache"
	case exists(filepath.Join(path, ".git")):
		looks = "a repository"
	}
	if looks == "" {
		looks = commonExtensions(t.exts, 3)
	}
	return creator, looks
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func isELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := f.Read(head)
	return n == 4 && string(head) == "\x7fELF"
}

// commonExtensions names the n commonest extensions, like ".o×62401 .d×3".
func commonExtensions(exts map[string]int, n int) string {
	type count struct {
		ext string
		n   int
	}
	var all []count
	for ext, c := range exts {
		if ext == "" {
			ext = "(none)"
		}
		all = append(all, count{ext, c})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].ext < all[j].ext
	})
	if len(all) > n {
		all = all[:n]
	}
	parts := make([]string, 0, len(all))
	for _, c := range all {
		parts = append(parts, c.ext+"×"+strconv.Itoa(c.n))
	}
	return strings.Join(parts, " ")
}

// knownReviews are the review rows by path, empty when there is no ledger.
func knownReviews(stateRoot string) map[string]Review {
	rows, _ := listReviews(stateRoot, false)
	known := make(map[string]Review, len(rows))
	for _, r := range rows {
		known[r.Path] = r
	}
	return known
}

const reviewColumns = `path, bytes, last_write, creator, looks, pre_rule, first_seen, asked_at, answer, answered_at`

func listReviews(stateRoot string, openOnly bool) ([]Review, error) {
	var out []Review
	err := readLedger(stateRoot, func(db *sql.DB) error {
		query := `SELECT ` + reviewColumns + ` FROM review`
		if openOnly {
			query += ` WHERE answer = ''`
		}
		rows, err := db.Query(query + ` ORDER BY bytes DESC, path`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r Review
			var lastWrite, firstSeen, askedAt, answeredAt string
			var pre int
			if err := rows.Scan(&r.Path, &r.Bytes, &lastWrite, &r.Creator, &r.Looks, &pre, &firstSeen, &askedAt, &r.Answer, &answeredAt); err != nil {
				return err
			}
			r.LastWrite, r.PreRule, r.FirstSeen = unstamp(lastWrite), pre != 0, unstamp(firstSeen)
			r.AskedAt, r.AnsweredAt = unstamp(askedAt), unstamp(answeredAt)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// OpenReviews are the items waiting for an answer, the biggest first.
func OpenReviews(stateRoot string) ([]Review, error) { return listReviews(stateRoot, true) }

// ReviewOf is the review row of path, answered or not.
func ReviewOf(stateRoot, path string) (Review, bool) {
	r, ok := knownReviews(stateRoot)[path]
	return r, ok
}

// SyncReviews makes the review table say what the sweep found: unproven is
// the whole set it found this pass. A new item is added (once: an answered
// item is added again only if it comes back); an open item that is no longer
// found is answered from the records that exist, with no new hook in keep or
// release: Brain's keep is a keep, the trash holds what went there, and what
// is not there is gone. A failure is reported, never returned.
func SyncReviews(stateRoot string, unproven []Unproven, now time.Time) {
	current := make(map[string]bool, len(unproven))
	for _, u := range unproven {
		current[u.Path] = true
	}
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		for _, u := range unproven {
			pre := 0
			if u.PreRule {
				pre = 1
			}
			// An answered row that comes back is a new question.
			if _, err := db.Exec(`INSERT INTO review(path, bytes, last_write, creator, looks, pre_rule, first_seen) VALUES(?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(path) DO UPDATE SET bytes = excluded.bytes, last_write = excluded.last_write, creator = excluded.creator, looks = excluded.looks,
					first_seen = CASE WHEN review.answer != '' THEN excluded.first_seen ELSE review.first_seen END,
					asked_at = CASE WHEN review.answer != '' THEN '' ELSE review.asked_at END,
					answered_at = CASE WHEN review.answer != '' THEN '' ELSE review.answered_at END,
					answer = ''`,
				u.Path, u.Bytes, stamp(u.LastWrite), u.Creator, u.Looks, pre, stamp(now)); err != nil {
				return err
			}
		}
		defer recordReviewSize(db, now)
		open, err := db.Query(`SELECT path, bytes FROM review WHERE answer = ''`)
		if err != nil {
			return err
		}
		type gone struct {
			path  string
			bytes int64
		}
		var left []gone
		for open.Next() {
			var g gone
			if err := open.Scan(&g.path, &g.bytes); err != nil {
				open.Close()
				return err
			}
			if !current[g.path] {
				left = append(left, g)
			}
		}
		open.Close()
		if len(left) == 0 {
			return nil
		}
		keeps, _ := ListKeeps(stateRoot)
		trashed, _ := List(stateRoot)
		for _, g := range left {
			answer := answerOf(g.path, keeps, trashed)
			if _, err := db.Exec(`UPDATE review SET answer = ?, answered_at = ? WHERE path = ?`, answer, stamp(now), g.path); err != nil {
				return err
			}
			if _, err := db.Exec(`INSERT INTO event(at, action, kind, subject, bytes, detail) VALUES(?, ?, ?, ?, ?, ?)`,
				stamp(now), ActionReviewed, KindUnmarked, g.path, g.bytes, "review answered: "+answer); err != nil {
				return err
			}
		}
		return nil
	}))
}

// recordReviewSize makes today's size sample of what waits for review say
// what waits now (no sample when nothing does), for Resources.
func recordReviewSize(db *sql.DB, now time.Time) {
	day := now.Local().Format("2006-01-02")
	var count int
	var bytes int64
	var oldest string
	if err := db.QueryRow(`SELECT count(*), coalesce(sum(bytes), 0), coalesce(min(last_write), '') FROM review WHERE answer = ''`).Scan(&count, &bytes, &oldest); err != nil {
		noteLedger(err)
		return
	}
	if count == 0 {
		_, err := db.Exec(`DELETE FROM size WHERE day = ? AND kind = ?`, day, SizeReview)
		noteLedger(err)
		return
	}
	_, err := db.Exec(`INSERT INTO size(day, kind, count, bytes, oldest) VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(day, kind) DO UPDATE SET count = excluded.count, bytes = excluded.bytes, oldest = excluded.oldest`,
		day, SizeReview, count, bytes, oldest)
	noteLedger(err)
}

func answerOf(path string, keeps []Keep, trashed []Item) string {
	if keptIn(keeps, path) {
		return AnswerKeep
	}
	for _, item := range trashed {
		if item.Original == path {
			return AnswerRelease
		}
	}
	if !exists(path) {
		return AnswerGone
	}
	return AnswerUsed
}

// ReviewsDue returns the open reviews and whether Brain should be asked now:
// some item has not been asked about, and nothing was asked in the last
// ReviewAskEvery. The same set is never asked about twice.
func ReviewsDue(stateRoot string, now time.Time) (open []Review, due bool) {
	all, err := listReviews(stateRoot, false)
	if err != nil {
		return nil, false
	}
	var lastAsked time.Time
	fresh := false
	for _, r := range all {
		if r.AskedAt.After(lastAsked) {
			lastAsked = r.AskedAt
		}
		if r.Answer == "" {
			open = append(open, r)
			if r.AskedAt.IsZero() {
				fresh = true
			}
		}
	}
	return open, fresh && (lastAsked.IsZero() || now.Sub(lastAsked) >= ReviewAskEvery)
}

// MarkReviewsAsked records that the items at paths were put to Brain at now.
// Only those: an item the collector added while the question was being
// written was not in it and is still to be asked about.
func MarkReviewsAsked(stateRoot string, paths []string, now time.Time) {
	noteLedger(withLedger(stateRoot, func(db *sql.DB) error {
		for _, path := range paths {
			if _, err := db.Exec(`UPDATE review SET asked_at = ? WHERE path = ? AND answer = '' AND asked_at = ''`, stamp(now), path); err != nil {
				return err
			}
		}
		return nil
	}))
}

// ReviewWhy is the sentence `gc why` says of an item in review.
func ReviewWhy(r Review) string {
	if r.Answer != "" {
		return "was in review since " + r.FirstSeen.Local().Format("2006-01-02") + "; answered: " + r.Answer
	}
	if r.AskedAt.IsZero() {
		return "in review since " + r.FirstSeen.Local().Format("2006-01-02") + ": reads leave no trace on this file system, so it cannot be proven unread; Brain is asked once a day about new ones"
	}
	return "asked about since " + r.AskedAt.Local().Format("2006-01-02") + "; waits for Brain to keep or release it"
}
