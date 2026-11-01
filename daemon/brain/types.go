package brain

import (
	"time"

	"github.com/daoleno/mewla/daemon/work"
)

// ExternalInputDisposition is the channel-neutral result of one exact Brain
// admission attempt. Channels persist this value before acknowledging their
// own transport receipt and must never infer provider acceptance themselves.
type ExternalInputDisposition string

const (
	ExternalInputAccepted     ExternalInputDisposition = "accepted"
	ExternalInputNotSubmitted ExternalInputDisposition = "not_submitted"
	ExternalInputUncertain    ExternalInputDisposition = "uncertain"
	ExternalInputPending      ExternalInputDisposition = "pending"
)

type WorkerRef struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Summary   string     `json:"summary,omitempty"`
	Phase     string     `json:"phase,omitempty"`
	Cwd       string     `json:"cwd,omitempty"`
	Command   string     `json:"command,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	ProcessID int        `json:"process_id,omitempty"`
	Updated   time.Time  `json:"updated_at"`
	Hidden    bool       `json:"hidden,omitempty"`
	Delegated bool       `json:"delegated,omitempty"`
}

// HostProblem says why Brain has no running host, so a client shows the
// actual problem instead of waiting for a host that is not coming.
type HostProblem struct {
	// Code is not_installed, unsupported, not_signed_in, not_started or not_ready.
	Code     string `json:"code"`
	Executor string `json:"executor"`
	Detail   string `json:"detail"`
	// SuggestExecutor is an installed agent Mewla verified as signed in, when
	// the host's own is not one: `mewla brain use <id>` moves Brain to it.
	SuggestExecutor string `json:"suggest_executor,omitempty"`
	// InstalledExecutors are the other configured agents installed on this
	// computer, signed in or not. A client offers to switch Brain's executor
	// only when there is one to switch to.
	InstalledExecutors []string `json:"installed_executors,omitempty"`
}

// ExecutorState is what a client needs to offer an executor as a choice: only
// an installed agent is one, and a signed-in one is the better one.
type ExecutorState struct {
	Installed bool `json:"installed"`
	// SignedIn is true only when Mewla verified the sign-in; unknown is false.
	SignedIn bool `json:"signed_in,omitempty"`
}

type Snapshot struct {
	Personality  string                `json:"personality"`
	CurrentWork  []CurrentWork         `json:"current_work"`
	OpenWork     []OpenWork            `json:"open_work"`
	WorkBacklog  WorkBacklog           `json:"work_backlog"`
	Objective    *Objective            `json:"objective,omitempty"`
	Workers      []WorkerRef           `json:"workers"`
	HostWorker   *WorkerRef            `json:"host_worker,omitempty"`
	HostExecutor *work.WorkerExecutor  `json:"host_executor,omitempty"`
	HostProblem  *HostProblem          `json:"host_problem,omitempty"`
	Executors    []work.WorkerExecutor `json:"executors"`
	// ExecutorStates is each executor's install and sign-in state, by id.
	ExecutorStates map[string]ExecutorState `json:"executor_states,omitempty"`
	ChatThreadID   string                   `json:"chat_thread_id,omitempty"`
	Workspace      string                   `json:"workspace,omitempty"`
	WorklogPath    string                   `json:"worklog_path,omitempty"`
	GeneratedAt    time.Time                `json:"generated_at"`
}

type BrainContext struct {
	ThreadID     string                `json:"thread_id,omitempty"`
	Workspace    string                `json:"workspace,omitempty"`
	WorklogPath  string                `json:"worklog_path,omitempty"`
	Notes        []WorkspaceNote       `json:"notes"`
	Personality  string                `json:"personality,omitempty"`
	CurrentWork  []CurrentWork         `json:"current_work"`
	WorkBacklog  WorkBacklog           `json:"work_backlog"`
	Objective    *Objective            `json:"objective,omitempty"`
	Playbooks    []PlaybookEntry       `json:"playbooks,omitempty"`
	HostWorker   *WorkerRef            `json:"host_worker,omitempty"`
	HostExecutor *work.WorkerExecutor  `json:"host_executor,omitempty"`
	Executors    []work.WorkerExecutor `json:"executors"`
	Workers      []WorkerRef           `json:"workers"`
	GeneratedAt  time.Time             `json:"generated_at"`
}

// WorkspaceNote locates a private Brain note without inlining it; Brain reads
// the file when it is relevant. Over-budget notes need compaction.
type WorkspaceNote struct {
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	BudgetBytes int64  `json:"budget_bytes"`
	OverBudget  bool   `json:"over_budget,omitempty"`
}

type HousekeepingReport struct {
	Workspace            string          `json:"workspace,omitempty"`
	CurrentPath          string          `json:"current_path"`
	Notes                []WorkspaceNote `json:"notes"`
	UnmanagedPaths       []string        `json:"unmanaged_paths,omitempty"`
	PolicyPaths          []string        `json:"policy_paths"`
	PlaybookPaths        []string        `json:"playbook_paths"`
	WorklogPath          string          `json:"worklog_path"`
	OpenDelegatedWorkers []WorkerRef     `json:"open_delegated_workers"`
	ChangedPaths         []string        `json:"changed_paths"`
	RecommendedNextSteps []string        `json:"recommended_next_steps,omitempty"`
	GeneratedAt          time.Time       `json:"generated_at"`
}

type WorkspaceTree struct {
	Workspace   string           `json:"workspace,omitempty"`
	Path        string           `json:"path,omitempty"`
	Entries     []WorkspaceEntry `json:"entries"`
	GeneratedAt time.Time        `json:"generated_at"`
}

type WorkspaceEntry struct {
	Name       string           `json:"name"`
	Path       string           `json:"path"`
	Kind       string           `json:"kind"`
	Size       int64            `json:"size,omitempty"`
	ModifiedAt time.Time        `json:"modified_at,omitempty"`
	Children   []WorkspaceEntry `json:"children,omitempty"`
}

type WorkspaceFile struct {
	DataURL    string    `json:"data_url,omitempty"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	Language   string    `json:"language"`
	Content    string    `json:"content"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modified_at,omitempty"`
}
