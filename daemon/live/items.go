package live

import (
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// The live surfaces (the phone's live update, Dynamic Island, widgets) show
// what the app's header and tray show (soft-final SPEC §1.2): the requests
// that wait for the person, the Works and Sessions that run, and the queued
// and waiting Work, counted the same way. Items are the rows a surface draws.

// Item opens.
const (
	OpenWork    = "work"
	OpenSession = "session"
	OpenBrain   = "brain"
)

// Needs labels, the tray row's tag.
const (
	NeedsAnswer  = "answer"
	NeedsApprove = "approve"
)

// AgentBrain is Brain's mark: Brain itself, or a Work with several Workers or none.
const AgentBrain = "brain"

const maxItems = 3

// Ring is a Work's judged progress as the app draws it (client
// core/work/judgedProgress.ts): moving or back with a value rounded to 5 and
// capped at 95, or the last stretch. Never a number for the person to read.
type Ring struct {
	Stage   string `json:"stage"`
	Percent int    `json:"percent,omitempty"`
}

// Item is one row of a live surface.
type Item struct {
	// Key is stable while the row is the same thing: "work:<id>",
	// "session:<id>" or "brain".
	Key string `json:"key"`
	// Open is what a tap opens (OpenWork, OpenSession, OpenBrain) and ID its id.
	Open  string `json:"open"`
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
	// Line is what it does, or, needing the person, what it asks.
	Line string `json:"line,omitempty"`
	// Agent is the Session's provider (claude, codex, opencode, …), AgentBrain,
	// or empty for a plain terminal.
	Agent string `json:"agent,omitempty"`
	// Needs is NeedsAnswer or NeedsApprove while it waits for the person.
	Needs string `json:"needs,omitempty"`
	Ring  *Ring  `json:"ring,omitempty"`
	// StartedAt (unix ms) anchors a running row's clock; it never makes a new state.
	StartedAt int64 `json:"started_at,omitempty"`

	since time.Time
}

// Session is one visible Session as the server sees it (never Brain's own).
type Session struct {
	ID        string
	Title     string
	Provider  string
	Line      string
	Delegated bool
	Running   bool
	StartedAt time.Time
	// Asks is NeedsAnswer or NeedsApprove when the Session waits for the
	// person (server asksOwner: a delegated Worker's own report is Brain's).
	Asks string
	// Ask is what it asks; Requests how many requests it holds (its approvals, else one).
	Ask      string
	Requests int
	AskSince time.Time
}

// Input is everything the live state is projected from.
type Input struct {
	Snapshot brain.Snapshot
	Sessions []Session
	// HostAsk is the prompt Brain's own Session is blocked on, "" for none.
	HostAsk      string
	HostAskSince time.Time
}

type workGroup int

const (
	groupNone workGroup = iota
	groupNeeds
	groupRunning
	groupWaiting
	groupQueued
)

// groupOf is the tray's grouping of a current Work (client core/work/surface.ts).
func groupOf(work brain.CurrentWork, now time.Time) workGroup {
	if work.Status == brain.WorkDone || work.Status == brain.WorkCancelled {
		return groupNone
	}
	snoozed := work.SnoozedUntil != nil && work.SnoozedUntil.After(now)
	if strings.TrimSpace(work.Question) != "" || work.Wake != nil && work.Wake.Kind == brain.WorkWakeUserInput {
		if snoozed {
			return groupWaiting
		}
		return groupNeeds
	}
	switch {
	case work.AttentionState == brain.WorkAttentionReviewing, work.Status == brain.WorkNeedsInput:
		return groupRunning
	case work.AttentionState == brain.WorkAttentionQueued, work.AttentionState == "reserved", work.ProgressMode == brain.WorkProgressReady:
		// A result back for Brain to read: not on the live surfaces.
		return groupNone
	case work.Status == brain.WorkWaiting, work.ProgressMode == brain.WorkProgressWaiting:
		return groupWaiting
	}
	return groupRunning
}

// ringOf is the ring a Work's row carries (client workProgressMark), or nil.
func ringOf(work brain.CurrentWork) *Ring {
	progress := work.Progress
	if progress == nil {
		return nil
	}
	waiting := strings.TrimSpace(work.Question) != "" || work.Status == brain.WorkNeedsInput ||
		work.Wake != nil && work.Wake.Kind == brain.WorkWakeUserInput ||
		work.Status == brain.WorkWaiting && strings.TrimSpace(work.WaitFor) != ""
	if waiting || work.Status == brain.WorkDone || work.Status == brain.WorkCancelled {
		return nil
	}
	switch progress.Stage {
	case brain.ProgressLast:
		return &Ring{Stage: brain.ProgressLast}
	case brain.ProgressMoving, brain.ProgressBack:
		percent := (progress.Percent + 2) / 5 * 5
		if percent < 0 {
			percent = 0
		}
		if percent > 95 {
			percent = 95
		}
		return &Ring{Stage: progress.Stage, Percent: percent}
	}
	return nil
}

type surface struct {
	needs   []Item
	running []Item
	queued  int
	waiting int
	// requests counts what waits for the person (a Session's approvals count one each).
	requests int
}

func project(in Input, now time.Time) surface {
	var out surface
	workers := map[string]brain.WorkerRef{}
	for _, ref := range in.Snapshot.Workers {
		workers[ref.ID] = ref
	}
	sessions := map[string]Session{}
	for _, session := range in.Sessions {
		sessions[session.ID] = session
	}

	if in.HostAsk != "" {
		out.requests++
		out.needs = append(out.needs, Item{Key: "brain", Open: OpenBrain, Title: "Brain", Line: clip(oneLine(in.HostAsk), maxQuestionText), Agent: AgentBrain, Needs: NeedsAnswer, since: in.HostAskSince})
	}

	folded := map[string]bool{}
	for _, work := range in.Snapshot.CurrentWork {
		group := groupOf(work, now)
		if group == groupNone {
			continue
		}
		live := liveSessions(work)
		for _, id := range live {
			folded[id] = true
		}
		if work.AttemptSessionID != "" {
			folded[work.AttemptSessionID] = true
		}
		item := workItem(work, live, workers, sessions)
		switch group {
		case groupNeeds:
			out.requests++
			item.Needs = NeedsAnswer
			item.Ring = nil
			item.StartedAt = 0
			if question := strings.TrimSpace(work.Question); question != "" {
				item.Line = clip(oneLine(question), maxQuestionText)
			} else if wait := oneLine(work.WaitFor); strings.Contains(wait, " ") {
				item.Line = clip(wait, maxTextRunes)
			}
			item.since = work.UpdatedAt
			if work.AttentionSince != nil {
				item.since = *work.AttentionSince
			}
			out.needs = append(out.needs, item)
		case groupRunning:
			if asking(live, sessions) {
				// Its Session asks: that request is its row until it is answered.
				continue
			}
			out.running = append(out.running, item)
		case groupWaiting:
			out.waiting++
		case groupQueued:
			out.queued++
		}
	}
	listed := map[string]bool{}
	for _, work := range in.Snapshot.CurrentWork {
		listed[work.ID] = true
	}
	for _, work := range in.Snapshot.OpenWork {
		if listed[work.ID] || work.Status == brain.WorkDone || work.Status == brain.WorkCancelled {
			continue
		}
		if work.ProgressMode == brain.WorkProgressWaiting || work.Status == brain.WorkWaiting || work.Status == brain.WorkNeedsInput {
			out.waiting++
		} else {
			out.queued++
		}
	}

	for _, session := range in.Sessions {
		if session.Asks != "" {
			requests := session.Requests
			if requests < 1 {
				requests = 1
			}
			out.requests += requests
			out.needs = append(out.needs, Item{
				Key: "session:" + session.ID, Open: OpenSession, ID: session.ID,
				Title: clip(session.Title, maxTextRunes), Line: clip(oneLine(session.Ask), maxQuestionText),
				Agent: session.Provider, Needs: session.Asks, since: session.AskSince,
			})
			continue
		}
		if !session.Running || folded[session.ID] {
			continue
		}
		out.running = append(out.running, Item{
			Key: "session:" + session.ID, Open: OpenSession, ID: session.ID,
			Title: clip(session.Title, maxTextRunes), Line: clip(oneLine(session.Line), maxTextRunes),
			Agent: session.Provider, StartedAt: millis(session.StartedAt), since: session.StartedAt,
		})
	}

	// Needs: the newest first (what just arrived is what a glance is for).
	// Running: the one that started last first.
	sort.SliceStable(out.needs, func(i, j int) bool { return out.needs[i].since.After(out.needs[j].since) })
	sort.SliceStable(out.running, func(i, j int) bool { return out.running[i].since.After(out.running[j].since) })
	return out
}

func (s surface) items() []Item {
	items := make([]Item, 0, maxItems)
	for _, list := range [][]Item{s.needs, s.running} {
		for _, item := range list {
			if len(items) == maxItems {
				return items
			}
			items = append(items, item)
		}
	}
	return items
}

// workItem is a Work's row: its one Session's logo and target, or Brain's
// mark and Work detail with several Workers or none.
func workItem(work brain.CurrentWork, live []string, workers map[string]brain.WorkerRef, sessions map[string]Session) Item {
	item := Item{Key: "work:" + work.ID, Open: OpenWork, ID: work.ID, Title: clip(strings.TrimSpace(work.Title), maxTextRunes), Agent: AgentBrain, Ring: ringOf(work)}
	joined := work.WorkerSessions
	sessionID := ""
	switch {
	case len(joined) == 1:
		sessionID = joined[0].SessionID
	case len(joined) == 0:
		sessionID = work.AttemptSessionID
	}
	var lead *brain.WorkWorkerSession
	for index := range joined {
		if joined[index].Live {
			lead = &joined[index]
			break
		}
	}
	if lead == nil && len(joined) == 1 {
		lead = &joined[0]
	}
	if sessionID != "" {
		item.Open, item.ID = OpenSession, sessionID
		if session, ok := sessions[sessionID]; ok {
			item.Agent = session.Provider
			item.Line = session.Line
			item.StartedAt = millis(session.StartedAt)
		} else if ref, ok := workers[sessionID]; ok {
			item.Agent = ref.Provider
			item.Line = firstNonEmpty(ref.Summary, ref.Phase)
			if ref.StartedAt != nil {
				item.StartedAt = millis(*ref.StartedAt)
			}
		} else if lead != nil {
			item.Agent = lead.Provider
		}
	}
	if lead != nil {
		if item.Line == "" {
			item.Line = firstNonEmpty(lead.Summary, lead.Phase)
		}
		if !lead.JoinedAt.IsZero() {
			item.StartedAt = millis(lead.JoinedAt)
		}
	}
	if len(joined) > 1 && item.Line == "" {
		item.Line = itoa(int64(len(joined))) + " Workers"
	}
	item.Line = clip(oneLine(item.Line), maxTextRunes)
	if item.StartedAt > 0 {
		item.since = time.UnixMilli(item.StartedAt)
	} else {
		item.since = work.UpdatedAt
	}
	return item
}

func liveSessions(work brain.CurrentWork) []string {
	var out []string
	for _, joined := range work.WorkerSessions {
		if joined.Live && joined.SessionID != "" {
			out = append(out, joined.SessionID)
		}
	}
	return out
}

func asking(ids []string, sessions map[string]Session) bool {
	for _, id := range ids {
		if sessions[id].Asks != "" {
			return true
		}
	}
	return false
}

func oneLine(value string) string { return strings.Join(strings.Fields(value), " ") }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func millis(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}
