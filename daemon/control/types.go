package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/enrollment"
	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/memorysearch"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/screen"
	"github.com/daoleno/mewla/daemon/statedir"
	telegramchannel "github.com/daoleno/mewla/daemon/telegram"
	"github.com/daoleno/mewla/daemon/watch"
	"github.com/daoleno/mewla/daemon/watcher"
)

const SocketName = "control.sock"

type Request struct {
	BrowserID            string                                 `json:"browser_id,omitempty"`
	BrowserRequest       *browser.Request                       `json:"browser_request,omitempty"`
	ConnectionRequest    *connections.Request                   `json:"connection_request,omitempty"`
	WatchRequest         *watch.Request                         `json:"watch_request,omitempty"`
	ComputersRequest     *computers.Request                     `json:"computers_request,omitempty"`
	ComputerAction       *screen.AgentAction                    `json:"computer_action,omitempty"`
	ProcessID            int                                    `json:"process_id,omitempty"`
	ProcessStart         string                                 `json:"process_start,omitempty"`
	Type                 string                                 `json:"type"`
	Name                 string                                 `json:"name,omitempty"`
	Executor             string                                 `json:"executor,omitempty"`
	ExecutorID           string                                 `json:"executor_id,omitempty"`
	Command              string                                 `json:"command,omitempty"`
	Cwd                  string                                 `json:"cwd,omitempty"`
	Prompt               string                                 `json:"prompt,omitempty"`
	PromptFile           string                                 `json:"prompt_file,omitempty"`
	Profile              string                                 `json:"profile,omitempty"`
	ProfileID            string                                 `json:"profile_id,omitempty"`
	ConnectionID         string                                 `json:"connection_id,omitempty"`
	ModelID              string                                 `json:"model_id,omitempty"`
	ReasoningEffort      string                                 `json:"reasoning_effort,omitempty"`
	ModelIDs             []string                               `json:"model_ids,omitempty"`
	ProviderConnection   *modelprofiles.ProviderConnectionInput `json:"provider_connection,omitempty"`
	Runtime              *modelprofiles.ThreadRuntimeChoice     `json:"runtime,omitempty"`
	Generation           int64                                  `json:"generation,omitempty"` // internal CAS only; ignored on Provider activate
	Operation            string                                 `json:"operation,omitempty"`
	WorkerID             string                                 `json:"worker_id,omitempty"`
	Client               string                                 `json:"client,omitempty"`
	Credential           string                                 `json:"credential,omitempty"`
	Status               string                                 `json:"status,omitempty"`
	Phase                string                                 `json:"phase,omitempty"`
	Attention            string                                 `json:"attention,omitempty"`
	Summary              string                                 `json:"summary,omitempty"`
	TaskClass            string                                 `json:"task_class,omitempty"`
	EventKind            string                                 `json:"event_kind,omitempty"`
	DetailsJSON          string                                 `json:"details_json,omitempty"`
	LeaseSeconds         int                                    `json:"lease_seconds,omitempty"`
	ProgressEventID      string                                 `json:"progress_event_id,omitempty"`
	TurnID               string                                 `json:"turn_id,omitempty"`
	EventID              string                                 `json:"event_id,omitempty"`
	HandlingID           string                                 `json:"handling_id,omitempty"`
	ProviderTurnID       string                                 `json:"provider_turn_id,omitempty"`
	Text                 string                                 `json:"text,omitempty"`
	QuickReplies         [][]string                             `json:"quick_replies,omitempty"`
	Submit               bool                                   `json:"submit,omitempty"`
	Hidden               bool                                   `json:"hidden,omitempty"`
	Force                bool                                   `json:"force,omitempty"`
	ID                   string                                 `json:"id,omitempty"`
	Revision             int64                                  `json:"revision,omitempty"`
	Actor                string                                 `json:"actor,omitempty"`
	Reason               string                                 `json:"reason,omitempty"`
	EnrollmentID         string                                 `json:"enrollment_id,omitempty"`
	VerificationNumber   string                                 `json:"verification_number,omitempty"`
	Approve              *bool                                  `json:"approve,omitempty"`
	CalendarItem         *calendar.Item                         `json:"calendar_item,omitempty"`
	BrainWork            *brain.Work                            `json:"brain_work,omitempty"`
	BrainWorkEvent       *brain.WorkEvent                       `json:"brain_work_event,omitempty"`
	BrainWorkDisposition *brain.WorkReviewDispositionRequest    `json:"brain_work_disposition,omitempty"`
	WorkID               string                                 `json:"work_id,omitempty"`
	WorkFields           []string                               `json:"work_fields,omitempty"`
	Service              *watcher.ManagedServiceDescriptor      `json:"service,omitempty"`
	ServiceID            string                                 `json:"service_id,omitempty"`
	ServiceGeneration    string                                 `json:"service_generation,omitempty"`
	TunnelAction         string                                 `json:"tunnel_action,omitempty"`
	ServiceUnit          string                                 `json:"service_unit,omitempty"`
	Memory               *MemoryRequest                         `json:"memory,omitempty"`
	AgentEvent           *AgentEventRequest                     `json:"agent_event,omitempty"`
	ApprovalID           string                                 `json:"approval_id,omitempty"`
	OptionID             string                                 `json:"option_id,omitempty"`
}

// AgentEventRequest carries one agent hook call to the daemon. Payload is the
// agent's own JSON, read from the hook's stdin and passed through untouched.
type AgentEventRequest struct {
	// Source names the agent whose hook fired.
	Source string `json:"source"`
	// WorkerID is the Worker the hook ran under (MEWLA_WORKER_ID), when the
	// environment carries it.
	WorkerID string          `json:"worker_id,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

// MemoryRequest is a memory_search (Query, From, Since, Until, Limit) or
// memory_show (ID, Around) call over Brain chat.
type MemoryRequest struct {
	Query  string    `json:"query,omitempty"`
	From   string    `json:"from,omitempty"`
	Since  time.Time `json:"since,omitempty"`
	Until  time.Time `json:"until,omitempty"`
	Limit  int       `json:"limit,omitempty"`
	ID     string    `json:"id,omitempty"`
	Around int       `json:"around,omitempty"`
}

type PersistenceOutcome string

const (
	PersistenceApplied        PersistenceOutcome = "applied"
	PersistenceVerifiedAbsent PersistenceOutcome = "verified_absent"
	PersistenceUnknown        PersistenceOutcome = "unknown"
)

type PairingInfo struct {
	Token           string    `json:"token"`
	ExpiresAt       time.Time `json:"expires_at"`
	DaemonID        string    `json:"daemon_id"`
	DaemonPublicKey string    `json:"daemon_public_key"`
	// ShortLinks says the daemon serves /p/<token> for this computer's browser.
	ShortLinks bool `json:"short_links,omitempty"`
}

type Response struct {
	Browser              *browser.Response                        `json:"browser,omitempty"`
	Connections          *connections.Response                    `json:"connections,omitempty"`
	Watch                *watch.Response                          `json:"watch,omitempty"`
	Computers            *computers.Result                        `json:"computers,omitempty"`
	AgentUse             *agentuse.Status                         `json:"agent_use,omitempty"`
	ComputerResult       *screen.AgentResult                      `json:"computer_result,omitempty"`
	ResourceTelemetry    *watcher.MachineResourceSnapshot         `json:"resource_telemetry,omitempty"`
	OK                   bool                                     `json:"ok"`
	HookOutput           string                                   `json:"hook_output,omitempty"`
	Approvals            []Approval                               `json:"approvals,omitempty"`
	Error                *Error                                   `json:"error,omitempty"`
	Worker               *Worker                                  `json:"worker,omitempty"`
	Workers              []Worker                                 `json:"workers,omitempty"`
	WorkerReceipt        *WorkerReceipt                           `json:"worker_receipt,omitempty"`
	Executor             *Executor                                `json:"executor,omitempty"`
	Executors            []Executor                               `json:"executors,omitempty"`
	Context              any                                      `json:"context,omitempty"`
	ScreenCapability     *screen.Capability                       `json:"screen_capability,omitempty"`
	Housekeeping         any                                      `json:"housekeeping,omitempty"`
	Playbooks            any                                      `json:"playbooks,omitempty"`
	Text                 string                                   `json:"text,omitempty"`
	Workspace            string                                   `json:"workspace,omitempty"`
	CalendarItem         *calendar.Item                           `json:"calendar_item,omitempty"`
	CalendarItems        []calendar.Item                          `json:"calendar_items,omitempty"`
	Devices              []auth.DeviceInfo                        `json:"devices,omitempty"`
	Enrollments          []enrollment.Request                     `json:"enrollments,omitempty"`
	Pairing              *PairingInfo                             `json:"pairing,omitempty"`
	PersistenceOutcome   PersistenceOutcome                       `json:"persistence_outcome,omitempty"`
	PersistenceDurable   *bool                                    `json:"persistence_durable,omitempty"`
	Confirmation         string                                   `json:"confirmation,omitempty"`
	TurnID               string                                   `json:"turn_id,omitempty"`
	BrainWork            *brain.Work                              `json:"brain_work,omitempty"`
	BrainWorks           []brain.Work                             `json:"brain_work_items,omitempty"`
	BrainWorkEvent       *brain.WorkEvent                         `json:"brain_work_event,omitempty"`
	BrainWorkEvents      []brain.WorkEvent                        `json:"brain_work_events,omitempty"`
	BrainWorkTranscripts []brain.WorkTranscriptLink               `json:"brain_work_transcripts,omitempty"`
	Providers            *modelprofiles.ProviderCatalogProjection `json:"providers,omitempty"`
	ThreadRuntime        *modelprofiles.ThreadRuntimeSelection    `json:"thread_runtime,omitempty"`
	SessionRoute         *modelprofiles.WireSessionSnapshot       `json:"session_route,omitempty"`
	Binding              *modelprofiles.WireBinding               `json:"binding,omitempty"`
	TelegramStatus       *telegramchannel.Status                  `json:"telegram_status,omitempty"`
	TelegramBinding      *telegramchannel.BindingChallenge        `json:"telegram_binding,omitempty"`
	ServiceTunnel        *watcher.ServiceTunnel                   `json:"service_tunnel,omitempty"`
	ServiceSnapshot      *watcher.SessionServiceSnapshot          `json:"service_snapshot,omitempty"`
	Service              *watcher.ManagedServiceDescriptor        `json:"service,omitempty"`
	MemoryHits           []memorysearch.Hit                       `json:"memory_hits,omitempty"`
	MemoryMessages       []memorysearch.Message                   `json:"memory_messages,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type WorkerReceipt struct {
	WorkID      lifecycle.WorkID         `json:"work_id"`
	Admission   lifecycle.AdmissionState `json:"admission"`
	OwnsAttempt bool                     `json:"owns_attempt"`
}

type Worker struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Status              string     `json:"status"`
	Summary             string     `json:"summary,omitempty"`
	Phase               string     `json:"phase,omitempty"`
	Attention           string     `json:"attention,omitempty"`
	TaskClass           string     `json:"task_class,omitempty"`
	EventKind           string     `json:"event_kind,omitempty"`
	DetailsJSON         string     `json:"details_json,omitempty"`
	NeedsAttention      bool       `json:"needs_attention,omitempty"`
	LastProgressAt      *time.Time `json:"last_progress_at,omitempty"`
	ExpectedNextCheckAt *time.Time `json:"expected_next_check_at,omitempty"`
	LeaseSeconds        int        `json:"lease_seconds,omitempty"`
	Cwd                 string     `json:"cwd,omitempty"`
	Command             string     `json:"command,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at,omitempty"`
	LastSeenAt          *time.Time `json:"last_seen_at,omitempty"`
	Hidden              bool       `json:"hidden,omitempty"`
	Delegated           bool       `json:"delegated,omitempty"`
}

// Approval is a decision a Worker's agent is waiting on. Each option carries
// the agent's own id; a decision names one of them.
type Approval struct {
	ID       string           `json:"id"`
	WorkerID string           `json:"worker_id"`
	Kind     string           `json:"kind,omitempty"`
	Tool     string           `json:"tool,omitempty"`
	Summary  string           `json:"summary,omitempty"`
	Options  []ApprovalOption `json:"options"`
}

// ApprovalOption is one answer the agent offers.
type ApprovalOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope,omitempty"`
}

type ExecutorCapabilities struct {
	InteractiveTTY   bool `json:"interactive_tty"`
	StructuredEvents bool `json:"structured_events"`
}

type Executor struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Provider     string               `json:"provider"`
	Command      string               `json:"command,omitempty"`
	Runtime      string               `json:"runtime"`
	Capabilities ExecutorCapabilities `json:"capabilities"`
	Host         bool                 `json:"host,omitempty"`
}

type Handler interface {
	HandleControlRequest(Request) Response
}

func DefaultSocketPath(stateDir string) (string, error) {
	if stateDir == "" {
		// A Session started by a daemon with its own state directory talks to
		// that daemon, not to the one in the home directory.
		stateDir = strings.TrimSpace(os.Getenv("MEWLA_STATE_DIR"))
	}
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateDir = statedir.Default(home)
	}
	return filepath.Join(stateDir, "run", SocketName), nil
}

func ErrorResponse(code, message string) Response {
	return Response{
		OK: false,
		Error: &Error{
			Code:    code,
			Message: message,
		},
	}
}
