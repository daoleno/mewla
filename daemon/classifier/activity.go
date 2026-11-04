package classifier

import (
	"strings"
	"time"
)

// ActivitySignal is a provider adapter observation from visible pane or process
// evidence. It upgrades Unknown after progress/classification merge; it never
// reads provider transcripts or invents Running from raw pane churn alone.
type ActivitySignal struct {
	State    WorkerState
	Summary  string
	Source   string // stable machine tag, e.g. "cursor_pane_stop_marker"
	Provider string // adapter name, e.g. "cursor", "codex"
	// Prompt is the structured prompt behind a Blocked signal, when the
	// adapter could read it; nil when the pane proves a prompt but not its
	// shape.
	Prompt *Prompt
}

// ActivityInput is the provider-neutral observation bundle passed to adapters.
// Watcher fills shared fields; adapters ignore hints they do not use.
type ActivityInput struct {
	Worker          Worker
	PaneContent     string
	ToolChildActive bool // optional process-tree hint (non-idle worker child)
	// Capture says how PaneContent was captured. A dialog that asks to run
	// something is answerable only from a capture that says what it can prove;
	// without one the dialog is reported blocked, with no prompt id.
	Capture *CaptureInfo
}

// CaptureInfo is what the capture of a pane proves about its text.
type CaptureInfo struct {
	// Exact: the capture keeps the spaces a program wrote at the end of a line
	// and joins lines the terminal wrapped (tmux capture-pane -J), so a line's
	// text is what was displayed, trailing blanks included. It cannot show what
	// the program did before drawing: a tab, a trimmed or re-wrapped line.
	Exact bool
	// Origin is the marker line Mewla made the pane print as its first output
	// when it launched it (OriginMarkerLine of the nonce stored in the pane's
	// own @mewla_origin option); empty for a pane Mewla did not launch or one
	// launched before the marker existed. tmux evicts the oldest lines first,
	// so a line that is still in the capture proves that every line written
	// after it is too: the dialog's first lines cannot have been cut off if
	// the marker is above them. How much history the pane holds says nothing
	// about this (tmux trims in batches, so a pane below its limit may have
	// lost lines).
	Origin string
}

// OriginMarkerLine is the line a Worker pane prints as its first output; the
// nonce is private to the pane and kept in its @mewla_origin option.
func OriginMarkerLine(nonce string) string { return "mewla-origin " + nonce }

// ActivityAdapter is a provider-specific pane/process activity detector.
// Adapters must not rely on package init registration.
type ActivityAdapter interface {
	// Name returns a stable provider id used in ActivitySignal.Provider.
	Name() string
	// Match reports whether this adapter should interpret the session.
	Match(in ActivityInput) bool
	// Infer returns an activity signal. Empty State means "no opinion".
	// StateUnknown with a Source means "provider idle".
	Infer(in ActivityInput) ActivitySignal
}

// ActivityProbe is the watcher-facing contract for activity inference.
type ActivityProbe interface {
	Infer(in ActivityInput) ActivitySignal
}

// MultiActivityProbe dispatches to the first matching ActivityAdapter.
type MultiActivityProbe struct {
	adapters []ActivityAdapter
}

// NewActivityProbe builds an explicit, ordered adapter chain.
func NewActivityProbe(adapters ...ActivityAdapter) *MultiActivityProbe {
	out := make([]ActivityAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter != nil {
			out = append(out, adapter)
		}
	}
	return &MultiActivityProbe{adapters: out}
}

// DefaultActivityProbe returns the shipped pane/process adapter chain. A
// provider with no such evidence has no adapter and stays honestly Unknown.
// Watcher wires this explicitly via SetActivityProbe.
func DefaultActivityProbe() *MultiActivityProbe {
	return NewActivityProbe(
		NewCursorActivityAdapter(),
		NewCodexActivityAdapter(),
		NewClaudeActivityAdapter(),
	)
}

// Infer implements ActivityProbe.
func (p *MultiActivityProbe) Infer(in ActivityInput) ActivitySignal {
	if p == nil {
		return ActivitySignal{}
	}
	for _, adapter := range p.adapters {
		if !adapter.Match(in) {
			continue
		}
		signal := adapter.Infer(in)
		if signal.Provider == "" {
			signal.Provider = adapter.Name()
		}
		return signal
	}
	return ActivitySignal{}
}

// MergeActivitySignal applies a provider activity signal to an already-resolved
// status. Progress leases, sticky done/failed/blocked, and pane blocked/failed
// always win: activity only fills Unknown.
func MergeActivitySignal(base WorkerState, baseSummary string, signal ActivitySignal) (WorkerState, string) {
	if signal.State == "" || signal.State == StateUnknown {
		return base, baseSummary
	}
	if base != StateUnknown {
		return base, baseSummary
	}
	switch signal.State {
	case StateRunning, StateBlocked, StateFailed, StateDone:
		return signal.State, firstNonEmpty(signal.Summary, baseSummary)
	default:
		return base, baseSummary
	}
}

// ResolveSessionStatus is the shared status pipeline:
//
//	Classify → MergeProgressAndClassification → MergeActivitySignal
//
// Brain progress leases remain highest priority for Running among durable
// progress signals; provider adapters only resolve remaining Unknown panes.
func ResolveSessionStatus(worker *Worker, classified WorkerState, classifiedSummary string, now time.Time, activity ActivitySignal) (WorkerState, string) {
	state, summary := MergeProgressAndClassification(worker, classified, classifiedSummary, now)
	return MergeActivitySignal(state, summary, activity)
}

func commandBaseName(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(filepathBase(fields[0]))
}

func filepathBase(path string) string {
	path = strings.Trim(path, `"'`)
	if path == "" {
		return ""
	}
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}
