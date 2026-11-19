package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// WorkShown is how a Work reads in the person's list: a short name for the
// outcome, one line on where it stands, what comes next and the project it
// belongs to. The progress judge writes it in the same call that judges
// progress; the person may correct the title and the project, and a field the
// person set is never written by the judge again. Like Progress it is
// presentation state: setting it never moves the Work's Revision.
type WorkShown struct {
	Title   string `json:"title,omitempty"`
	Line    string `json:"line,omitempty"`
	Next    string `json:"next,omitempty"`
	Project string `json:"project,omitempty"`
	// JudgedAt is when the judge last wrote it; ChangedAt when what a person
	// sees (title, line or project) last changed, by the judge or the person.
	JudgedAt  time.Time `json:"judged_at,omitempty"`
	ChangedAt time.Time `json:"changed_at,omitempty"`
	// ByPerson names the fields the person set ("title", "project").
	ByPerson []string `json:"by_person,omitempty"`
	// Status is where the Work stood when the judge last wrote the line
	// ("active", "waiting" or "done"), so a list can tell a line written
	// while it ran from one that says how it ended.
	Status string `json:"status,omitempty"`
}

func shownStatus(status WorkStatus) string {
	switch status {
	case WorkDone, WorkCancelled:
		return "done"
	case WorkWaiting:
		return "waiting"
	}
	return "active"
}

// Limits on what the judge writes, in characters: a row has one line for the
// title and one for the state.
const (
	// shownMinRunes is the least a title, line or next step says: fewer
	// characters are not a reading of the Work, whatever the language.
	shownMinRunes   = 4
	shownTitleMax   = 60
	shownLineMax    = 110
	shownNextMax    = 200
	shownProjectMax = 24
)

func cloneWorkShown(s *WorkShown) *WorkShown {
	if s == nil {
		return nil
	}
	out := *s
	out.ByPerson = append([]string(nil), s.ByPerson...)
	return &out
}

// view is the shown text as clients read it, or nil when there is none.
func (s *WorkShown) view() *WorkShown {
	if s == nil || (s.Title == "" && s.Line == "" && s.Project == "") {
		return nil
	}
	return cloneWorkShown(s)
}

func (s *WorkShown) setByPerson(field string) bool {
	for _, f := range s.ByPerson {
		if f == field {
			return true
		}
	}
	return false
}

// shownVerdict is the judge's answer about how the Work reads.
type shownVerdict struct {
	Title   string
	Line    string
	Next    string
	Project string
}

// applyShownVerdict folds the judge's answer into what is shown. An empty
// answer keeps the last good text (a judge that could not say leaves the row
// as it was), and a field the person set is kept. changed says whether what
// a person sees moved.
func applyShownVerdict(prior *WorkShown, verdict shownVerdict, now time.Time) (WorkShown, bool) {
	next := WorkShown{}
	if prior != nil {
		next = *cloneWorkShown(prior)
	}
	changed := false
	set := func(field string, dst *string, value string, max int) {
		value = clipShown(value, max)
		// A word or two is not a reading ("矫正", 11-19 22:00): the last good text stays.
		if field != "project" && utf8.RuneCountInString(value) < shownMinRunes {
			value = ""
		}
		if value == "" || next.setByPerson(field) || *dst == value {
			return
		}
		*dst = value
		if field != "next" {
			changed = true
		}
	}
	set("title", &next.Title, verdict.Title, shownTitleMax)
	set("line", &next.Line, verdict.Line, shownLineMax)
	set("next", &next.Next, verdict.Next, shownNextMax)
	set("project", &next.Project, verdict.Project, shownProjectMax)
	next.JudgedAt = now
	if changed {
		next.ChangedAt = now
	}
	return next, changed
}

// clipShown makes one line of at most max characters.
func clipShown(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

// ShownField is a field of WorkShown the person may set.
type ShownField string

const (
	ShownTitle   ShownField = "title"
	ShownProject ShownField = "project"
)

var ErrShownField = errors.New("unknown shown field")

// SetWorkShown records the person's own title or project for a Work. Their
// value sticks: the judge never writes that field again. An empty value hands
// the field back to the judge, which fills it at its next judgment.
func (s *Store) SetWorkShown(workID string, field ShownField, value string, now time.Time) (WorkShown, error) {
	max := 0
	switch field {
	case ShownTitle:
		max = shownTitleMax
	case ShownProject:
		max = shownProjectMax
	default:
		return WorkShown{}, fmt.Errorf("%w: %q", ErrShownField, field)
	}
	value = clipShown(value, max)
	var out WorkShown
	err := s.updateWorkProgressLocked(workID, func(item *Work) (*WorkEvent, bool) {
		shown := WorkShown{}
		if item.Shown != nil {
			shown = *cloneWorkShown(item.Shown)
		}
		kept := shown.ByPerson[:0]
		for _, f := range shown.ByPerson {
			if f != string(field) {
				kept = append(kept, f)
			}
		}
		shown.ByPerson = kept
		if value != "" {
			shown.ByPerson = append(shown.ByPerson, string(field))
			sort.Strings(shown.ByPerson)
			if field == ShownTitle {
				shown.Title = value
			} else {
				shown.Project = value
			}
			shown.ChangedAt = now
		}
		item.Shown = &shown
		out = *cloneWorkShown(&shown)
		return nil, true
	})
	return out, err
}

// projectsInUse lists the projects open Work is shown under, most used first,
// so the judge keeps one name per project.
func projectsInUse(items []Work, except string) []string {
	count := map[string]int{}
	for _, item := range items {
		if item.ID == except || item.Shown == nil || item.Shown.Project == "" {
			continue
		}
		if item.Status == WorkDone || item.Status == WorkCancelled {
			continue
		}
		count[item.Shown.Project]++
	}
	names := make([]string, 0, len(count))
	for name := range count {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if count[names[i]] != count[names[j]] {
			return count[names[i]] > count[names[j]]
		}
		return names[i] < names[j]
	})
	if len(names) > 12 {
		names = names[:12]
	}
	return names
}

// applyWorkShown folds the judge's answer about how the Work reads into the
// Work, and tells the clients when what a person sees changed. A done Work
// keeps its last text.
func (s *Store) applyWorkShown(workID string, verdict shownVerdict, now time.Time) error {
	return s.updateWorkProgressLocked(workID, func(item *Work) (*WorkEvent, bool) {
		if item.Status == WorkCancelled {
			return nil, false
		}
		next, changed := applyShownVerdict(item.Shown, verdict, now)
		next.Status = shownStatus(item.Status)
		item.Shown = &next
		return nil, changed
	})
}

// personMessage is one message the person typed to Brain.
type personMessage struct {
	At   time.Time
	Text string
}

// Which of the person's messages the judge reads for a Work: those from a
// while before it began (the ask that started it) and the newest since (a
// follow-up about it), clipped. They set the language of the title and line
// and the person's own words for the thing.
const (
	personLookBack   = 3 * time.Hour
	personBeforeMax  = 8
	personAfterMax   = 6
	personMessageMax = 300
	personScanRows   = 600
)

// personMessagesFor reads the person's messages around a Work from Brain's
// chat timeline. Messages a computer, Watch or a tool wrote into the chat are
// not the person's and are left out.
func (s *Store) personMessagesFor(item Work, now time.Time) []personMessage {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT body FROM timeline WHERE json_extract(body, '$.role') = 'user' ORDER BY seq DESC LIMIT ?`, personScanRows)
	if err != nil {
		return nil
	}
	defer rows.Close()
	from := item.CreatedAt.Add(-personLookBack)
	start := item.CreatedAt.Add(10 * time.Minute)
	var before, after []personMessage
	for rows.Next() {
		var body string
		if rows.Scan(&body) != nil {
			continue
		}
		message, ok := personMessageOf([]byte(body))
		if !ok || message.At.After(now) {
			continue
		}
		if message.At.Before(from) {
			break
		}
		if message.At.After(start) {
			if len(after) < personAfterMax {
				after = append(after, message)
			}
		} else if len(before) < personBeforeMax {
			before = append(before, message)
		}
	}
	out := make([]personMessage, 0, len(before)+len(after))
	for i := len(before) - 1; i >= 0; i-- {
		out = append(out, before[i])
	}
	for i := len(after) - 1; i >= 0; i-- {
		out = append(out, after[i])
	}
	return out
}

// personRecent reads the person's newest messages to Brain, whatever Work
// they are about: the language they write in is theirs, not a Work's, and a
// Work started by Brain (a review, a Trigger) may have none of its own near it.
func (s *Store) personRecent(n int, now time.Time) []personMessage {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT body FROM timeline WHERE json_extract(body, '$.role') = 'user' ORDER BY seq DESC LIMIT ?`, personScanRows)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []personMessage
	for rows.Next() && len(out) < n {
		var body string
		if rows.Scan(&body) != nil {
			continue
		}
		if message, ok := personMessageOf([]byte(body)); ok && !message.At.After(now) {
			out = append(out, message)
		}
	}
	return out
}

// personLanguageRecent is how many of the person's newest messages set the
// language the judge writes in.
const personLanguageRecent = 30

var personMarkup = regexp.MustCompile(`(?s)<mewla_attachments>.*?</mewla_attachments>`)

func personMessageOf(body []byte) (personMessage, bool) {
	var row struct {
		Role      string `json:"role"`
		Kind      string `json:"kind"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(body, &row) != nil {
		return personMessage{}, false
	}
	if row.Kind != "user_message" && !(row.Kind == "" && row.Role == "user") {
		return personMessage{}, false
	}
	text := strings.TrimSpace(personMarkup.ReplaceAllString(row.Body, ""))
	if text == "" || strings.HasPrefix(text, "<mewla_") || strings.HasPrefix(text, "Execute:") {
		return personMessage{}, false
	}
	at := conversationTime(row.CreatedAt)
	if at.IsZero() {
		return personMessage{}, false
	}
	return personMessage{At: at.UTC(), Text: judgeClip(text, personMessageMax)}, true
}

// buildShownContext is what the judge reads besides the Work to say how it
// reads: the person's messages around it and the projects in use.
func buildShownContext(item Work, messages []personMessage, projects []string, now time.Time, language string) string {
	var b strings.Builder
	b.WriteString(workStanding(item, now))
	b.WriteString("\n<<<PERSON\n## The person's own messages to Brain around this Work (oldest first; many are about other Work)\n")
	if len(messages) == 0 {
		b.WriteString("(none)\n")
	}
	for _, message := range messages {
		offset := message.At.Sub(item.CreatedAt)
		sign := "+"
		if offset < 0 {
			sign, offset = "-", -offset
		}
		fmt.Fprintf(&b, "- %s%s: %s\n", sign, progressDuration(offset), message.Text)
	}
	b.WriteString("\n## Projects already in use\n")
	if len(projects) == 0 {
		b.WriteString("(none yet)\n")
	} else {
		b.WriteString(strings.Join(projects, ", ") + "\n")
	}
	if item.Shown != nil && (item.Shown.Title != "" || item.Shown.Line != "") {
		fmt.Fprintf(&b, "\n## Shown so far (keep the title and project unless wrong; write the line anew from the evidence above, never from this one)\ntitle: %s\nline: %s\nproject: %s\n", item.Shown.Title, item.Shown.Line, item.Shown.Project)
		if len(item.Shown.ByPerson) > 0 {
			fmt.Fprintf(&b, "The person set: %s (kept as they wrote it).\n", strings.Join(item.Shown.ByPerson, ", "))
		}
	}
	b.WriteString("PERSON>>>\n")
	// Last, so it is the rule the judge reads nearest its answer: a brief, reports and notes in another
	// language pull the words toward theirs.
	if language != "" {
		fmt.Fprintf(&b, "\nThe person writes in %s: write title, line and next in %s.\n", language, language)
	} else {
		b.WriteString("\nWrite title, line and next in the language of the person's own messages above.\n")
	}
	return b.String()
}

// personLanguage names the language the person writes in when their script
// says it: most of their messages carry Han, kana or Hangul (a Chinese message
// is often half English words and ids, so it is counted by message, not by
// letter). Else "": the judge reads the messages for any other language.
func personLanguage(messages []personMessage) string {
	counts := map[string]int{}
	for _, message := range messages {
		var han, kana, hangul int
		for _, r := range message.Text {
			switch {
			case unicode.In(r, unicode.Hiragana, unicode.Katakana):
				kana++
			case unicode.Is(unicode.Hangul, r):
				hangul++
			case unicode.Is(unicode.Han, r):
				han++
			}
		}
		switch {
		case kana > 0:
			counts["Japanese"]++
		case hangul >= 2:
			counts["Korean"]++
		case han >= 2:
			counts["Chinese"]++
		}
	}
	for language, n := range counts {
		if n*2 > len(messages) {
			return language
		}
	}
	return ""
}

// workStanding tells the judge where Brain has put the Work for the person:
// whether anything runs on it, whether Brain asks the person anything (only a
// question does), and Brain's own notes, which are shorthand for Brain and
// never words for the person. And the person's local time, so a time in the
// line is one they read without converting.
func workStanding(item Work, now time.Time) string {
	var b strings.Builder
	b.WriteString("\n<<<STANDING\n## Where the Work stands\n")
	switch item.Status {
	case WorkDone, WorkCancelled:
		b.WriteString("Closed by Brain: nothing more runs on it.\n")
	case WorkWaiting:
		b.WriteString("Parked: nothing runs on it now; it waits for something before it goes on.\n")
	case WorkNeedsInput:
		b.WriteString("Brain is reviewing what its Worker sent.\n")
	case WorkRunning:
		b.WriteString("A Worker is on it.\n")
	default:
		b.WriteString("Not started yet: it starts when Brain sends it or its trigger fires.\n")
	}
	if question := strings.TrimSpace(item.Question); question != "" {
		fmt.Fprintf(&b, "Brain asks the person: %s\n", judgeClip(question, 600))
	} else {
		b.WriteString("Brain asks the person nothing.\n")
	}
	if item.Wake != nil {
		switch item.Wake.Kind {
		case WorkWakeSessionTerminal:
			b.WriteString("It goes on when its Worker's turn ends.\n")
		case WorkWakeCalendarResult:
			b.WriteString("It goes on when a scheduled run reports.\n")
		case WorkWakeNamed:
			b.WriteString("It waits on something Brain named, not on the person.\n")
		}
	}
	if wait := strings.TrimSpace(item.WaitFor); wait != "" {
		fmt.Fprintf(&b, "Brain's own note on what it waits for (Brain's shorthand, never words for the person): %s\n", judgeClip(wait, 600))
	}
	if next := strings.TrimSpace(item.NextAction); next != "" {
		fmt.Fprintf(&b, "Brain's own note on its next step (Brain's shorthand, never words for the person): %s\n", judgeClip(next, 600))
	}
	local := now.In(time.Local)
	fmt.Fprintf(&b, "The person's local time now: %s (UTC%s)\n", local.Format("2006-01-02 15:04 Mon"), local.Format("-07:00"))
	b.WriteString("STANDING>>>\n")
	return b.String()
}

// sameProject reads a project name the judge wrote as one already in use when
// they differ only in case, spaces or punctuation, or one is the other with a
// word added ("0G" and "0G app"), so the person's chips do not split one
// project in two. Otherwise the judge's name stands.
func sameProject(name string, inUse []string) string {
	key := projectKey(name)
	if key == "" {
		return name
	}
	for _, used := range inUse {
		other := projectKey(used)
		if other == "" {
			continue
		}
		if key == other || (len(other) >= 2 && strings.HasPrefix(key, other)) || (len(key) >= 2 && strings.HasPrefix(other, key)) {
			return used
		}
	}
	return name
}

func projectKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
