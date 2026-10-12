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

// Problem statuses. Step 1 only creates "new"; later steps move them.
const (
	StatusNew       = "new"
	StatusConfirmed = "confirmed"
	StatusFixing    = "fixing"
	StatusWatching  = "watching"
	StatusFixed     = "fixed"
	StatusRecurred  = "recurred"
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
}

// Linear holds suggested team mappings and the one the user confirmed.
type Linear struct {
	Suggested []LinearTeam `json:"suggested,omitempty"`
	Confirmed *LinearTeam  `json:"confirmed,omitempty"`
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
}

// Problem is a provisional record raised by a rule. Its ID is stable for the
// rule and subject, so re-evaluation updates the same record.
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
