// Package trigger watches something for the person and wakes Brain with the
// evidence when it changes. A Trigger runs a check command on an interval;
// a check that prints nothing, or prints what the previous check printed, does
// nothing at all: no Work event, no model call. Output that differs from the
// previous check fires once, as an actionable event on the Trigger's Work.
//
// The daemon owns every Trigger, so a Trigger keeps running across Brain
// Session replacement, daemon restarts and reboots. A Trigger whose checks
// keep failing, that paused itself for waking Brain too often, or whose firing
// nobody handled says so: watching that stopped is never silent.
package trigger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinInterval     = time.Minute
	MaxInterval     = 24 * time.Hour
	DefaultInterval = 10 * time.Minute
	DefaultTimeout  = time.Minute
	MaxTimeout      = 10 * time.Minute

	// MaxOutputBytes caps what one check's stdout keeps; MaxStderrBytes is
	// the tail of stderr a failure keeps.
	MaxOutputBytes = 64 * 1024
	MaxStderrBytes = 4 * 1024
	// MaxPayloadBytes caps one firing's payload file, however many check
	// outputs it gathers.
	MaxPayloadBytes = 256 * 1024

	// KeepFirings is how many firings (and payload files) a Trigger keeps.
	KeepFirings = 50

	// A Trigger fails after FailAfterChecks failed checks in a row that span
	// at least FailAfter; it reminds once every FailReminder while failing.
	FailAfterChecks = 3
	FailAfter       = 30 * time.Minute
	FailReminder    = 24 * time.Hour

	// WakesPerHour is the most a Trigger wakes Brain in an hour; the next
	// firing pauses it instead (runaway).
	WakesPerHour = 6

	// NotHandledAfter is how long a delivered firing may wait for Brain before
	// the Trigger needs the person.
	NotHandledAfter = 3 * time.Hour

	// A check that comes GapFactor intervals late records a gap.
	GapFactor = 3

	MaxTitleRunes       = 120
	MaxCheckBytes       = 16 * 1024
	MaxInstructionBytes = 16 * 1024
)

// Event kinds a Trigger appends to its Work.
const (
	KindFired     = "trigger.fired"
	KindFailing   = "trigger.failing"
	KindRecovered = "trigger.recovered"
	KindPaused    = "trigger.paused"
)

const PauseRunaway = "runaway"

var (
	ErrNotFound = errors.New("trigger not found")
	ErrInvalid  = errors.New("invalid trigger")
)

// Trigger is one durable watch. The first block is what the person (through
// Brain) asked for; the rest is the daemon's record of watching it.
type Trigger struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Check           string `json:"check"`
	Cwd             string `json:"cwd,omitempty"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	// Instruction is what Brain does when the Trigger fires.
	Instruction    string `json:"instruction,omitempty"`
	SourceThreadID string `json:"source_thread_id,omitempty"`
	// WorkID is the Work the Trigger's events land on. OwnsWork says the
	// Trigger created it (and closes it when deleted) rather than adopting
	// one the caller named.
	WorkID      string    `json:"work_id"`
	OwnsWork    bool      `json:"owns_work,omitempty"`
	Paused      bool      `json:"paused,omitempty"`
	PauseReason string    `json:"pause_reason,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Revision    int64     `json:"revision"`

	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	ResumedAt     *time.Time `json:"resumed_at,omitempty"`
	// LastOutputHash is the previous successful check's output (empty when
	// it printed nothing): the edge a firing needs.
	LastOutputHash string `json:"last_output_hash,omitempty"`
	// Failures counts failed checks in a row since FailingSince. Failing is
	// set once the person and Brain were told.
	Failures          int        `json:"failures,omitempty"`
	FailingSince      *time.Time `json:"failing_since,omitempty"`
	LastFailure       string     `json:"last_failure,omitempty"`
	Failing           bool       `json:"failing,omitempty"`
	FailingNotifiedAt *time.Time `json:"failing_notified_at,omitempty"`
	// GapFrom..GapTo is the latest stretch without checks (the daemon was
	// down, the computer slept).
	GapFrom *time.Time `json:"gap_from,omitempty"`
	GapTo   *time.Time `json:"gap_to,omitempty"`

	FireSeq int      `json:"fire_seq,omitempty"`
	Firings []Firing `json:"firings,omitempty"`
	// Pending gathers outputs that changed while the latest firing was with
	// Brain but not handled yet; it fires once that one is handled.
	Pending *Pending `json:"pending,omitempty"`
	// Wakes are the times this Trigger woke Brain within the last hour.
	Wakes []time.Time `json:"wakes,omitempty"`
	// NoticeSeq numbers the failing and paused notices (their dedupe keys).
	NoticeSeq int `json:"notice_seq,omitempty"`
	// HeldNotice is a failing or paused notice the Work could not take as
	// its Review yet (a firing was still with Brain); each check retries it.
	HeldNotice *Notice `json:"held_notice,omitempty"`
}

// Notice is a failing or paused notice waiting to reach Brain.
type Notice struct {
	Kind    string `json:"kind"`
	Seq     int    `json:"seq"`
	Tries   int    `json:"tries"`
	Payload string `json:"payload,omitempty"`
	Summary string `json:"summary"`
}

// Firing is one wake: the outputs it carries are in Payload.
type Firing struct {
	Seq     int       `json:"seq"`
	At      time.Time `json:"at"`
	Outputs int       `json:"outputs"`
	Payload string    `json:"payload"`
	// EventID is the actionable event that woke Brain; empty while the
	// firing is held (the Work could not take a wake yet) and retried.
	EventID   string     `json:"event_id,omitempty"`
	Tries     int        `json:"tries,omitempty"`
	HandledAt *time.Time `json:"handled_at,omitempty"`
	// Lost says the event is gone from the Work, so it can never be handled.
	Lost bool `json:"lost,omitempty"`
}

// Pending is the next firing's gathered outputs.
type Pending struct {
	Since   time.Time `json:"since"`
	Outputs []Output  `json:"outputs"`
}

type Output struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

func (t Trigger) Interval() time.Duration {
	return time.Duration(t.IntervalSeconds) * time.Second
}

func (t Trigger) Timeout() time.Duration {
	return time.Duration(t.TimeoutSeconds) * time.Second
}

// LatestFiring is the newest firing, or nil.
func (t Trigger) LatestFiring() *Firing {
	if len(t.Firings) == 0 {
		return nil
	}
	return &t.Firings[len(t.Firings)-1]
}

// open reports whether f still waits for Brain.
func (f *Firing) open() bool {
	return f != nil && f.HandledAt == nil && !f.Lost
}

// NextCheckAt is when the scheduler checks next; zero when paused.
func (t Trigger) NextCheckAt() time.Time {
	if t.Paused {
		return time.Time{}
	}
	if t.LastCheckedAt == nil {
		return t.CreatedAt
	}
	next := t.LastCheckedAt.Add(t.Interval())
	if t.ResumedAt != nil && t.ResumedAt.After(*t.LastCheckedAt) {
		return *t.ResumedAt
	}
	return next
}

// Normalize fills defaults and validates what a caller asked for.
func (t Trigger) Normalize() (Trigger, error) {
	t.Title = strings.TrimSpace(t.Title)
	t.Check = strings.TrimSpace(t.Check)
	t.Cwd = strings.TrimSpace(t.Cwd)
	t.Instruction = strings.TrimSpace(t.Instruction)
	t.SourceThreadID = strings.TrimSpace(t.SourceThreadID)
	t.WorkID = strings.TrimSpace(t.WorkID)
	invalid := func(format string, args ...any) (Trigger, error) {
		return Trigger{}, fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case t.Title == "":
		return invalid("a title is required")
	case utf8.RuneCountInString(t.Title) > MaxTitleRunes || strings.ContainsAny(t.Title, "\r\n"):
		return invalid("the title must be one line of at most %d characters", MaxTitleRunes)
	case t.Check == "":
		return invalid("a check command is required")
	case len(t.Check) > MaxCheckBytes:
		return invalid("the check command must be at most %d bytes", MaxCheckBytes)
	case len(t.Instruction) > MaxInstructionBytes:
		return invalid("the instruction must be at most %d bytes", MaxInstructionBytes)
	case t.WorkID == "" && t.Instruction == "":
		return invalid("an instruction (what to do when it fires) is required unless the Trigger uses existing Work")
	case t.WorkID == "" && t.SourceThreadID == "":
		return invalid("a source thread is required unless the Trigger uses existing Work")
	}
	if t.IntervalSeconds == 0 {
		t.IntervalSeconds = int(DefaultInterval / time.Second)
	}
	interval := t.Interval()
	if interval < MinInterval || interval > MaxInterval {
		return invalid("the interval must be between %s and %s", MinInterval, MaxInterval)
	}
	if t.TimeoutSeconds == 0 {
		t.TimeoutSeconds = int(min(DefaultTimeout, interval/2) / time.Second)
	}
	timeout := t.Timeout()
	if timeout < time.Second || timeout > MaxTimeout || timeout >= interval {
		return invalid("the timeout must be at least 1s, at most %s and shorter than the interval", MaxTimeout)
	}
	if t.Cwd != "" {
		if !filepath.IsAbs(t.Cwd) {
			return invalid("the working directory must be an absolute path")
		}
		info, err := os.Stat(t.Cwd)
		if err != nil || !info.IsDir() {
			return invalid("the working directory %s is not a directory", t.Cwd)
		}
	}
	return t, nil
}

// State is what the Trigger is doing, for the person.
type State string

const (
	StateWatching State = "watching"
	StatePaused   State = "paused"
	StateFailing  State = "failing"
)

// Attention says why a Trigger needs the person; empty when it does not.
type Attention string

const (
	AttentionNone       Attention = ""
	AttentionFailing    Attention = "failing"
	AttentionRunaway    Attention = "runaway"
	AttentionNotHandled Attention = "not_handled"
)

// View is a Trigger as clients and the CLI see it.
type View struct {
	Trigger
	State       State      `json:"state"`
	Attention   Attention  `json:"attention,omitempty"`
	NextCheckAt *time.Time `json:"next_check_at,omitempty"`
	// LastFiredAt is the newest firing; Handled says whether Brain handled
	// it, nil when there is none.
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	Handled     *bool      `json:"handled,omitempty"`
	// Line is the row in words: "Every 10 min · checked 2 min ago · fired
	// 3 h ago · not handled".
	Line string `json:"line"`
}

// ViewOf derives the person-facing state at now.
func ViewOf(t Trigger, now time.Time) View {
	v := View{Trigger: t, State: StateWatching}
	switch {
	case t.Paused:
		v.State = StatePaused
	case t.Failing:
		v.State = StateFailing
	}
	if next := t.NextCheckAt(); !next.IsZero() {
		v.NextCheckAt = &next
	}
	latest := t.LatestFiring()
	if latest != nil {
		at := latest.At
		v.LastFiredAt = &at
		handled := !latest.open()
		v.Handled = &handled
	}
	switch {
	case t.Paused && t.PauseReason == PauseRunaway:
		v.Attention = AttentionRunaway
	case t.Failing:
		v.Attention = AttentionFailing
	case latest.open() && now.Sub(latest.At) >= NotHandledAfter:
		v.Attention = AttentionNotHandled
	}
	v.Line = line(v, now)
	return v
}

func line(v View, now time.Time) string {
	parts := []string{"Every " + words(v.Interval())}
	switch {
	case v.Paused && v.PauseReason == PauseRunaway:
		parts = append(parts, fmt.Sprintf("paused: woke Brain %d times in an hour", WakesPerHour))
	case v.Paused:
		parts = append(parts, "paused")
	case v.Failing:
		parts = append(parts, "failing: "+failureLine(v.LastFailure))
	}
	if v.LastCheckedAt != nil {
		parts = append(parts, "checked "+ago(now.Sub(*v.LastCheckedAt)))
	} else if !v.Paused {
		parts = append(parts, "not checked yet")
	}
	if v.GapFrom != nil && v.GapTo != nil && now.Sub(*v.GapTo) < 24*time.Hour {
		parts = append(parts, "not checked for "+words(v.GapTo.Sub(*v.GapFrom))+": Mewla was not running")
	}
	if latest := v.LatestFiring(); latest != nil {
		fired := "fired " + ago(now.Sub(latest.At))
		switch {
		case latest.Lost:
			fired += " · not handled"
		case latest.HandledAt != nil:
			fired += " · handled"
		case latest.EventID == "":
			fired += " · waiting to reach Brain"
		case now.Sub(latest.At) >= NotHandledAfter:
			fired += " · not handled"
		default:
			fired += " · with Brain"
		}
		parts = append(parts, fired)
	}
	return strings.Join(parts, " · ")
}

func ago(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return words(d) + " ago"
}

// Words is a duration as the person reads it: "10 min", "3 h".
func Words(d time.Duration) string { return words(d) }

func words(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	}
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	if utf8.RuneCountInString(text) > 80 {
		runes := []rune(text)
		text = string(runes[:80]) + "…"
	}
	return text
}

// failureLine is a failure on one line: the reason, then the last line the
// check wrote to stderr.
func failureLine(failure string) string {
	lines := strings.Split(strings.TrimSpace(failure), "\n")
	text := strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
			text += ": " + last
		}
	}
	return firstLine(text)
}
