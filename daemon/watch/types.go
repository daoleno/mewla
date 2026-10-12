// Package watch collects signals about the user's projects and flags the few
// that need attention. Collection is local, deterministic and free: it reads
// transcripts, the Work store, the daemon log and connected Plugins, and
// never wakes Brain. See docs/watch.md.
package watch

import "time"

// Signal sources.
const (
	SourceHuman    = "human"
	SourceProcess  = "process"
	SourceRuntime  = "runtime"
	SourceExternal = "external"
)

// Problem statuses. Rules create "new"; judgment marks problems fixed or
// recurred; the user's label confirms or dismisses them.
const (
	StatusNew       = "new"
	StatusConfirmed = "confirmed"
	StatusFixing    = "fixing"
	StatusWatching  = "watching"
	StatusFixed     = "fixed"
	StatusRecurred  = "recurred"
	StatusDismissed = "dismissed"
)

// User labels on a problem.
const (
	LabelReal  = "real"
	LabelNot   = "not"
	LabelLater = "later"
)

// Judged authors of an ambiguous user message in a Worker session.
const (
	AuthorHuman   = "human"
	AuthorBrain   = "brain_followup"
	AuthorRoutine = "routine"
)

// How an unplaced signal got its project after collection.
const (
	PlacedByIndex    = "index"
	PlacedByJudgment = "judgment"
)

// Project is one repository root, inferred from Work and Session working
// directories. ID derives from Root and never changes; Name is user-editable.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Root      string    `json:"root,omitempty"`
	Remotes   []Remote  `json:"remotes,omitempty"`
	Builtin   bool      `json:"builtin,omitempty"`
	Renamed   bool      `json:"renamed,omitempty"`
	GitHub    *GitHub   `json:"github,omitempty"`
	Linear    *Linear   `json:"linear,omitempty"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Remote is one git remote with any credentials removed from its URL.
type Remote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// GitHub records which connected account can read the project's repository
// and what each read endpoint last answered.
type GitHub struct {
	Owner     string            `json:"owner"`
	Repo      string            `json:"repo"`
	AccountID string            `json:"account_id,omitempty"`
	Branch    string            `json:"default_branch,omitempty"`
	Endpoints map[string]string `json:"endpoints,omitempty"` // endpoint -> "ok" or the failure
	CheckedAt time.Time         `json:"checked_at,omitzero"`
	// Coverage says whether Watch collects from the repository (collecting,
	// not_connected, disabled, not_allowed, unreachable); Reason says why not.
	Coverage string `json:"coverage,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Linear holds suggested team mappings and the one the user confirmed.
type Linear struct {
	Suggested []LinearTeam `json:"suggested,omitempty"`
	Confirmed *LinearTeam  `json:"confirmed,omitempty"`
	// Coverage and Reason report the confirmed team's collection, as for GitHub.
	Coverage string `json:"coverage,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// LinearTeam is one Linear team reachable through a connected account.
type LinearTeam struct {
	AccountID string `json:"account_id"`
	TeamID    string `json:"team_id,omitempty"`
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Signal is one normalized raw record from a source. Text fields are redacted
// before they are stored.
type Signal struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	ProjectID   string            `json:"project_id,omitempty"`
	Time        time.Time         `json:"time"`
	Kind        string            `json:"kind"`
	Excerpt     string            `json:"excerpt"`
	Refs        map[string]string `json:"refs,omitempty"`
	Pointer     string            `json:"pointer,omitempty"`
	CollectedAt time.Time         `json:"collected_at"`

	// Set from the annotation overlay, never written to the signal log.
	PlacedBy     string `json:"placed_by,omitempty"`
	PlaceReason  string `json:"place_reason,omitempty"`
	Author       string `json:"judged_author,omitempty"`
	AuthorReason string `json:"author_reason,omitempty"`
}

// Annotation is what Watch learned about a signal after collecting it: a
// project for an unplaced signal, a judged author, and whether judgment has
// seen it. Annotations live beside the append-only signal log.
type Annotation struct {
	ProjectID    string `json:"project_id,omitempty"`
	PlacedBy     string `json:"placed_by,omitempty"`
	PlaceReason  string `json:"place_reason,omitempty"`
	Author       string `json:"author,omitempty"`
	AuthorReason string `json:"author_reason,omitempty"`
	// Run is the judgment run that saw the signal; empty until judged.
	Run string `json:"run,omitempty"`
}

// Problem is raised by a rule or by judgment. A rule problem's ID is stable
// for the rule and subject, so re-evaluation updates the same record; a
// judged problem keeps its ID as later runs add evidence.
type Problem struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id,omitempty"`
	Rule      string    `json:"rule"`
	Subject   string    `json:"subject"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail,omitempty"`
	Status    string    `json:"status"`
	Severe    bool      `json:"severe,omitempty"`
	Active    bool      `json:"active"`
	Count     int       `json:"count"`
	Evidence  []string  `json:"evidence"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	UpdatedAt time.Time `json:"updated_at"`

	// Judgment fields.
	Reason      string   `json:"reason,omitempty"`
	Cost        *Cost    `json:"cost,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	MergedFrom  []string `json:"merged_from,omitempty"`
	MergedInto  string   `json:"merged_into,omitempty"`
	FixedReason string   `json:"fixed_reason,omitempty"`
	Run         string   `json:"run,omitempty"`
	Label       *Label   `json:"label,omitempty"`
}

// Cost estimates what a problem costs: the user's attention (corrections,
// waiting, chasing) or impact on the project. Score ranks problems, 1–100.
type Cost struct {
	Score    int    `json:"score"`
	Kind     string `json:"kind"`
	Estimate string `json:"estimate"`
}

// Label is the user's verdict on a problem.
type Label struct {
	Value string    `json:"value"`
	Note  string    `json:"note,omitempty"`
	At    time.Time `json:"at"`
}

// Config is the user's Watch setting. Collection runs regardless; Enabled
// gates the agent pass that later steps add.
type Config struct {
	Enabled bool `json:"enabled"`
}

// Filter selects signals or problems.
type Filter struct {
	ProjectID string    `json:"project_id,omitempty"`
	Source    string    `json:"source,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Since     time.Time `json:"since,omitzero"`
	Limit     int       `json:"limit,omitempty"`
}

// PassResult reports what one collection pass added per source.
type PassResult struct {
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	Added      map[string]int    `json:"added"`
	Errors     map[string]string `json:"errors,omitempty"`
	Problems   int               `json:"problems"`
}
