package server

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/daoleno/mewla/daemon/answered"
	"hash/fnv"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/agentuse"
	"github.com/daoleno/mewla/daemon/attachment"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/browser"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/debuglog"
	"github.com/daoleno/mewla/daemon/diag"
	"github.com/daoleno/mewla/daemon/enrollment"
	"github.com/daoleno/mewla/daemon/modelprofiles"
	"github.com/daoleno/mewla/daemon/outsideagents"
	"github.com/daoleno/mewla/daemon/pairlink"
	"github.com/daoleno/mewla/daemon/phone"
	"github.com/daoleno/mewla/daemon/push"
	"github.com/daoleno/mewla/daemon/screen"
	skillmgmt "github.com/daoleno/mewla/daemon/skills"
	"github.com/daoleno/mewla/daemon/stats"
	telegramchannel "github.com/daoleno/mewla/daemon/telegram"
	"github.com/daoleno/mewla/daemon/terminal"
	"github.com/daoleno/mewla/daemon/vdisplay"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
	"github.com/oklog/ulid/v2"
)

const maxCodexAssetBytes = 6 << 20
const codexConversationSubscriptionInterval = 220 * time.Millisecond
const maxUploadFileBytes int64 = 2 << 30
const maxUploadStoreBytes int64 = 8 << 30
const uploadNameHeader = "X-Mewla-Upload-Name"
const maxUploadNameBytes = 1024
const maxUploadNameHeaderBytes = maxUploadNameBytes * 3
const uploadRetention = 7 * 24 * time.Hour

type uploadLimits struct {
	fileBytes  int64
	storeBytes int64
	retention  time.Duration
}

func productionUploadLimits() uploadLimits {
	return uploadLimits{
		fileBytes:  maxUploadFileBytes,
		storeBytes: maxUploadStoreBytes,
		retention:  uploadRetention,
	}
}

// upgrader negotiates permessage-deflate when the client offers it: snapshots
// and transcripts are JSON and markdown, which shrink several times over a
// phone or proxy link.
var upgrader = websocket.Upgrader{
	CheckOrigin:       func(r *http.Request) bool { return true },
	EnableCompression: true,
}

type notificationPusher interface {
	SetRegistration(deviceID, token, serverRef string)
	ForgetDevice(deviceID string)
	NotifyWorker(kind string, notice push.WorkerNotice) error
	NotifyScheduledResult(title, status, threadID, resultID string) error
}

// deviceInfoPusher is a pusher that keeps what a device says about itself:
// its platform and whether it wants strong calendar alerts.
type deviceInfoPusher interface {
	SetDevice(deviceID, platform string, strongAlerts bool)
}

// notificationRecorder is how the server tells Watch what the phone did with
// notifications, and which ones it held back. It is optional.
type notificationRecorder interface {
	NotificationSuppressed(kind, workerID, title, body, reason string)
	NotificationResponse(id, action string, at time.Time)
	NotificationEngaged(action, workerID string)
}

// Server handles WebSocket connections from the Mewla mobile app.
type Server struct {
	browsers                   *browser.Manager
	displays                   *vdisplay.Service
	screen                     *screen.Service
	phone                      *phone.Manager
	phonePing                  time.Duration // the phone's ping interval; zero is the default
	connections                *connections.Manager
	auth                       *auth.Manager
	watcher                    *watcher.Watcher
	terminal                   *terminal.Manager
	hostTerminal               *terminal.HostBackend // opens session host Sessions; SetVersion names the daemon to the host
	terminalTarget             func(id string) (watcher.TerminalTarget, error)
	pusher                     notificationPusher
	deviceHook                 func(timeZone, locale string)
	notifications              notificationRecorder
	appStates                  map[*websocket.Conn]string
	doneNotified               map[string]bool        // Work ID -> its done push already went out this run
	workerPushed               map[string]*workerPush // Worker -> its last blocked or failed push; see worker_push_dedupe.go
	workerPushPath             string
	workerPushNow              func() time.Time
	stats                      *stats.Collector
	work                       *work.Store
	execs                      *work.ExecutorConfig
	brain                      *brain.Service
	resourceSampler            *watcher.ResourceSampler
	profiles                   *modelprofiles.Owner
	calendar                   *calendar.Store
	calendarScheduler          *calendar.Scheduler
	telegram                   *telegramchannel.Manager
	computers                  *computers.Manager
	openRequests               openRequestsCache
	outsideAgents              *outsideagents.Manager
	outsideAgentsDoor          http.Handler
	computerName               string
	agentUse                   *agentuse.Service
	daemonUpdater              DaemonUpdater
	version                    string
	providerConversationLoader func(reader *work.ProviderConversationReader, workerID string) (work.CodexConversation, error)
	// brainTickObserve, when set by a test, hears what each Brain-scoped
	// subscription tick did; see the brainTick constants.
	brainTickObserve             func(brainTick)
	promptPaneOverride           PanePrompts
	answerClaudeChoiceOverride   func(workerID string, questions []watcher.ClaudeChoiceQuestion, answers []watcher.ClaudeChoiceAnswer) error
	rewindMessageOverride        func(workerID string, request watcher.MessageRewindRequest) error
	rewound                      rewoundMessages
	sendInputOverride            func(workerID, text string) error
	sendInputWithReceiptOverride func(workerID, text, receipt string) error
	sendActionOverride           func(workerID, action string) error
	killSessionOverride          func(workerID string) error
	probeSessionOverride         func(workerID string) (watcher.SessionPresence, error)
	getWorkerOverride            func(workerID string) *classifier.Worker
	// skillsMutationExecuteOverride / pluginMutationExecuteOverride replace the
	// production executor in handler tests so ownership and cancellation can be
	// proven without touching real Plugin or Skill state.
	skillsMutationExecuteOverride func(ctx context.Context, command skillmgmt.MutationCommand, options skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error)
	pluginMutationExecuteOverride func(ctx context.Context, command skillmgmt.PluginMutationCommand, options skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error)
	// codexLiveDial opens the live native Codex app-server control surface for
	// a Session's control socket. Nil (tests, legacy embedded sessions) keeps
	// SetThreadRuntime on the route-only path. Production New installs the
	// codexctl client.
	codexLiveDial              func(ctx context.Context, socketPath string) (codexctl.LiveControl, error)
	brainSnapshotBroadcastHook func(payload map[string]any)
	live                       liveHub // live state, see live_state.go
	uploadDir                  string
	uploadStore                *attachment.Store
	// firstMessageSend submits a create_session's first message once the agent is ready (tests replace it).
	firstMessageSend           func(workerID, command, text string) error
	sessionFileWorkerLoader    func(workerID string) *classifier.Worker
	sessionFileCapabilityClock func() time.Time
	authRevocationUnsubscribe  func()
	runtimeClosing             bool
	terminalCleanup            terminalCleanupOwner
	tlsConfig                  *tls.Config
	addresses                  *addressbook.Store
	directEndpoints            func() []DirectEndpoint
	private                    *privateListeners
	enrollments                *enrollment.Manager
	enrollmentObserver         func(enrollment.Request)
	webUIFiles                 fs.FS
	webUIDir                   string
	webUIOverrideSeen          atomic.Bool
	// webPort is the port this daemon listens on (0 until it binds): a
	// loopback web UI is this daemon's only on it.
	webPort atomic.Int32

	workSubID                   int
	workSub                     <-chan work.Event
	calendarSubID               int
	calendarSub                 <-chan calendar.Event
	brainWorkSubID              int
	brainWorkSub                <-chan brain.WorkChange
	brainHostRecovery           chan struct{}
	signalSystemStartupComplete bool
	// brainHostReconcileFailed: the last Host continuity pass failed, so the
	// pass that ends the failure is broadcast too.
	brainHostReconcileFailed atomic.Bool
	workerFeed               workerFeed
	codexThread              func(sessionID string) string

	// hostBlockSignature is the blocked state and prompt of the Brain host that
	// the last brain_snapshot carried, so only a change in it is broadcast.
	// handleWatcherEvent runs on one goroutine and is its only user.
	hostBlockSignature string

	clients            map[*websocket.Conn]*authenticatedClient
	active             map[*websocket.Conn]string
	outboxes           map[*websocket.Conn]*clientOutbox
	inputLanes         sessionInputLanes
	codexSubs          map[*websocket.Conn]map[string]codexConversationSubscription
	skillsInventories  map[*websocket.Conn]skillsInventoryRequest
	skillsMutations    map[*websocket.Conn]skillsMutationRequest
	skillsInspects     map[*websocket.Conn]skillsInspectRequest
	pluginsInventories map[*websocket.Conn]pluginsInventoryRequest
	pluginsMutations   map[*websocket.Conn]pluginsMutationRequest
	pluginRuntime      skillmgmt.PluginRuntime
	mu                 sync.Mutex
	enrollmentRateMu   sync.Mutex
	refusals           refusalLog
	enrollmentRates    map[string][]time.Time
	agentLevelsFn      func(*classifier.Worker) map[string]string
	agentActivityFn    func(workerID string) AgentActivity
	agentApprovals     AgentApprovals
	// answered is the answered-request ledger (answered_requests.go);
	// prompts tracks open screen prompts for it.
	answered *answered.Ledger
	prompts  promptWatch
	// afterPromptLeft runs a prompt's check once it has stayed off the
	// screen for the grace; nil is time.AfterFunc. Tests run it themselves.
	afterPromptLeft func(grace time.Duration, check func())
	codexStatus     func(ctx context.Context, workerID string) (any, error)
}

// SetTLSConfig enables identity-bound TLS alongside local HTTP.
func (s *Server) SetTLSConfig(config *tls.Config) {
	s.tlsConfig = config
}

func (s *Server) SetCalendar(store *calendar.Store, scheduler *calendar.Scheduler) {
	s.calendar, s.calendarScheduler = store, scheduler
	if store != nil {
		s.calendarSubID, s.calendarSub = store.Subscribe()
	}
}

func (s *Server) SetTelegram(manager *telegramchannel.Manager) {
	s.telegram = manager
}

func (s *Server) closeEventSubscriptions() {
	if s.work != nil && s.workSub != nil {
		s.work.Unsubscribe(s.workSubID)
	}
	if s.calendar != nil && s.calendarSub != nil {
		s.calendar.Unsubscribe(s.calendarSubID)
	}
	if s.brain != nil && s.brainWorkSub != nil {
		s.brain.UnsubscribeWork(s.brainWorkSubID)
	}
}

type codexConversationSubscription struct {
	cancel     context.CancelFunc
	generation string
}

type authenticatedClient struct {
	deviceID string
	revoked  atomic.Bool
}

type terminalCleanupState uint8

const (
	terminalCleanupOpen terminalCleanupState = iota
	terminalCleanupDraining
	terminalCleanupClosed
)

type terminalCleanupOwner struct {
	mu      sync.Mutex
	state   terminalCleanupState
	running int
	drained chan struct{}
	execute func(cleanup func())
}

func (o *terminalCleanupOwner) Submit(cleanup func()) {
	if cleanup == nil {
		return
	}
	o.mu.Lock()
	if o.state != terminalCleanupOpen {
		o.mu.Unlock()
		panic("terminal cleanup submitted after draining began")
	}
	if o.drained == nil {
		o.drained = make(chan struct{})
	}
	o.running++
	execute := o.execute
	o.mu.Unlock()
	go func() {
		defer o.finish()
		if execute != nil {
			execute(cleanup)
			return
		}
		cleanup()
	}()
}

func (o *terminalCleanupOwner) finish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.running--
	if o.state == terminalCleanupDraining && o.running == 0 {
		o.state = terminalCleanupClosed
		close(o.drained)
	}
}

func (o *terminalCleanupOwner) Drain() {
	o.mu.Lock()
	if o.drained == nil {
		o.drained = make(chan struct{})
	}
	if o.state == terminalCleanupOpen {
		o.state = terminalCleanupDraining
		if o.running == 0 {
			o.state = terminalCleanupClosed
			close(o.drained)
		}
	}
	drained := o.drained
	o.mu.Unlock()
	<-drained
}

type clientDetachWork struct {
	conn *websocket.Conn
}

// New creates a WebSocket server.
func New(authManager *auth.Manager, w *watcher.Watcher, pusher *push.Client, sc *stats.Collector, workStore *work.Store, execs *work.ExecutorConfig, brainService *brain.Service) *Server {
	uploadDir := ""
	if authManager != nil {
		uploadDir = filepath.Join(authManager.StorageDir(), "uploads")
	}
	hostTerminal := &terminal.HostBackend{}
	srv := &Server{
		auth:               authManager,
		watcher:            w,
		hostTerminal:       hostTerminal,
		terminal:           terminal.NewManager(&terminal.TmuxBackend{}, hostTerminal),
		stats:              sc,
		work:               workStore,
		execs:              execs,
		brain:              brainService,
		brainHostRecovery:  make(chan struct{}, 1),
		resourceSampler:    watcher.NewResourceSampler(),
		uploadDir:          uploadDir,
		workerPushPath:     workerPushPath(authManager),
		uploadStore:        &attachment.Store{Dir: uploadDir},
		clients:            make(map[*websocket.Conn]*authenticatedClient),
		active:             make(map[*websocket.Conn]string),
		appStates:          make(map[*websocket.Conn]string),
		outboxes:           make(map[*websocket.Conn]*clientOutbox),
		codexSubs:          make(map[*websocket.Conn]map[string]codexConversationSubscription),
		skillsInventories:  make(map[*websocket.Conn]skillsInventoryRequest),
		skillsMutations:    make(map[*websocket.Conn]skillsMutationRequest),
		skillsInspects:     make(map[*websocket.Conn]skillsInspectRequest),
		pluginsInventories: make(map[*websocket.Conn]pluginsInventoryRequest),
		pluginsMutations:   make(map[*websocket.Conn]pluginsMutationRequest),
		pluginRuntime:      skillmgmt.NewPluginRuntime(),
		enrollmentRates:    make(map[string][]time.Time),
	}
	if brainService != nil {
		srv.brainWorkSubID, srv.brainWorkSub = brainService.SubscribeWork()
	}
	srv.codexLiveDial = func(ctx context.Context, socketPath string) (codexctl.LiveControl, error) {
		return codexctl.Open(ctx, socketPath, codexctl.DialOptions{})
	}
	if workStore != nil {
		srv.workSubID, srv.workSub = workStore.Subscribe()
	}
	if authManager != nil {
		srv.authRevocationUnsubscribe = authManager.SubscribeRevocations(
			srv.revokeAuthenticatedDevice,
		)
	}
	if pusher != nil {
		// A nil *push.Client in the interface would pass every nil check.
		srv.pusher = pusher
	}
	srv.terminal.SetCleanupSubmitter(srv.terminalCleanup.Submit)
	srv.terminalTarget = srv.openableTerminal
	return srv
}

type clientMessage struct {
	ConnectionRequest    *connections.Request    `json:"connection_request"`
	DSHAnswer            json.RawMessage         `json:"dsh_answer"`
	DSHAfterRev          *int64                  `json:"dsh_after_rev"`
	ChoiceAnswer         json.RawMessage         `json:"choice_answer"`
	PromptAnswer         json.RawMessage         `json:"prompt_answer"`
	MessageRewind        json.RawMessage         `json:"message_rewind"`
	ServiceID            string                  `json:"service_id"`
	ServiceGeneration    string                  `json:"service_generation"`
	TunnelAction         string                  `json:"tunnel_action"`
	Type                 string                  `json:"type"`
	RequestID            string                  `json:"request_id"`
	WorkerID             string                  `json:"worker_id"`
	TargetID             string                  `json:"target_id"`
	Cwd                  string                  `json:"cwd"`
	Command              string                  `json:"command"`
	Name                 string                  `json:"name"`
	StartedAt            json.RawMessage         `json:"started_at"`
	Backend              string                  `json:"backend"`
	SessionID            string                  `json:"session_id"`
	Text                 string                  `json:"text"`
	SnoozeUntil          *time.Time              `json:"snooze_until,omitempty"`
	SnoozeSeconds        int64                   `json:"snooze_seconds,omitempty"`
	Key                  string                  `json:"key"`
	OptionID             string                  `json:"option_id"`
	Data                 string                  `json:"data"`
	Body                 string                  `json:"body"`
	Action               string                  `json:"action"`
	PushToken            string                  `json:"push_token"`
	Platform             string                  `json:"platform"`
	StrongAlerts         *bool                   `json:"strong_alerts"`
	SnoozeMinutes        int                     `json:"snooze_minutes"`
	NotificationID       string                  `json:"notification_id"`
	AppState             string                  `json:"app_state"`
	OccurredAtMS         int64                   `json:"occurred_at_ms"`
	ServerRef            string                  `json:"server_ref"`
	TimeZone             string                  `json:"time_zone"`
	Locale               string                  `json:"locale"`
	Cols                 int                     `json:"cols"`
	Rows                 int                     `json:"rows"`
	Col                  int                     `json:"col"`
	Row                  int                     `json:"row"`
	Lines                int                     `json:"lines"`
	Scroll               bool                    `json:"scroll,omitempty"`
	Since                *terminal.HistoryCursor `json:"since,omitempty"`
	ProcessID            int                     `json:"process_id"`
	Path                 string                  `json:"path"`
	FileGeneration       string                  `json:"file_generation"`
	ID                   string                  `json:"id"`
	Project              string                  `json:"project"`
	Frontmatter          map[string]interface{}  `json:"frontmatter"`
	BaseMtime            string                  `json:"base_mtime"`
	Prompt               string                  `json:"prompt"`
	Executor             string                  `json:"executor"`
	ExecutorID           string                  `json:"executor_id"`
	Client               string                  `json:"client"`
	AdapterID            string                  `json:"adapter_id"`
	Personality          string                  `json:"personality"`
	Done                 bool                    `json:"done"`
	CalendarItem         *calendar.Item          `json:"calendar_item"`
	Revision             int64                   `json:"revision"`
	ConversationScopeKey string                  `json:"conversation_scope_key"`
	// FirstEvents, on codex_conversation_subscribe, asks for the thread's first
	// snapshot in two parts: the newest FirstEvents events, then the rest as
	// the next revision's delta (codexConversationFirstPage).
	FirstEvents int `json:"first_events,omitempty"`
	// MaxEdge asks codex_asset for a picture no longer than this on its long
	// side (attachment.FitImage): a thread's row, not the full-size viewer.
	MaxEdge            int                                    `json:"max_edge,omitempty"`
	Attachments        []attachment.File                      `json:"attachments"`
	Generation         int64                                  `json:"generation"`
	Limit              int                                    `json:"limit"`
	Operation          string                                 `json:"operation"`
	Scope              string                                 `json:"scope"`
	SkillAgents        []string                               `json:"agents"`
	SkillID            string                                 `json:"skill_id"`
	SkillRoot          string                                 `json:"root_path"`
	SkillCanonical     string                                 `json:"canonical_path"`
	SkillAllowedRoot   string                                 `json:"allowed_root"`
	Source             string                                 `json:"source"`
	SkillName          string                                 `json:"skill_name"`
	Ref                string                                 `json:"ref"`
	PluginID           string                                 `json:"plugin_id"`
	PluginCopyID       string                                 `json:"plugin_copy_id"`
	PluginName         string                                 `json:"plugin_name"`
	PluginHost         string                                 `json:"plugin_host"`
	PluginSource       string                                 `json:"plugin_source"`
	PluginVersion      string                                 `json:"plugin_version"`
	PluginRevision     string                                 `json:"plugin_revision"`
	ProfileID          string                                 `json:"profile_id"`
	ConnectionID       string                                 `json:"connection_id"`
	ModelID            string                                 `json:"model_id"`
	ModelIDs           []string                               `json:"model_ids"`
	ReasoningEffort    string                                 `json:"reasoning_effort"`
	Runtime            *modelprofiles.ThreadRuntimeChoice     `json:"runtime"`
	Credential         string                                 `json:"credential"`
	ProviderConnection *modelprofiles.ProviderConnectionInput `json:"provider_connection"`
	GitRepoRoot        string                                 `json:"repo_root"`
	GitPaths           []string                               `json:"paths"`
	GitPatch           string                                 `json:"patch"`
	GitMessage         string                                 `json:"message"`
	GitSHA             string                                 `json:"sha"`
	GitBranch          string                                 `json:"branch"`
	GitCreate          bool                                   `json:"create"`
	GitSkip            int                                    `json:"skip"`
	GitUndo            []gitDiscardUndo                       `json:"undo"`
}

// Handler exposes the daemon's complete origin as one transport boundary.
// LAN, tunnels, reverse proxies, and Mewla Relay all use this same handler so
// /ws and the HTTP streaming routes cannot drift between transports.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	webUI := s.webUIHandler()
	mux.HandleFunc("/ws", s.handleWS)
	mux.Handle("/browser", s.appRoute(webUI, http.HandlerFunc(s.handleBrowser)))
	mux.Handle("/screen", s.appRoute(webUI, http.HandlerFunc(s.handleScreen)))
	mux.HandleFunc("/screen/viewer", s.handleScreenViewer)
	mux.HandleFunc("/display", s.handleDisplay)
	mux.HandleFunc("/display/viewer", s.handleDisplayViewer)
	mux.HandleFunc("/plugins/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if s.connections == nil {
			http.Error(w, "Plugins unavailable", http.StatusServiceUnavailable)
			return
		}
		s.connections.BrowserOAuthCallback(w, r, s.webUIAdmitted(r))
	})
	mux.Handle("/resources", s.appRoute(webUI, http.HandlerFunc(s.handleResourceTelemetryHTTP)))
	mux.HandleFunc(widgetSnapshotPath, s.handleWidgetSnapshot) // live: read-only widget snapshot
	mux.HandleFunc("/pair", s.handlePair)
	mux.HandleFunc("/p/", s.handleShortPairLink)
	mux.HandleFunc("/enrollment/request", s.handleEnrollmentRequest)
	mux.HandleFunc("/enrollment/status", s.handleEnrollmentStatus)
	mux.HandleFunc("/enrollment/pending", s.handleEnrollmentPending)
	mux.HandleFunc("/enrollment/decision", s.handleEnrollmentDecision)
	mux.HandleFunc("/auth-check", s.handleAuthCheck)
	mux.HandleFunc("/devices", s.handleDevices)
	mux.HandleFunc("/computer", s.handleComputer)
	mux.HandleFunc("/computers/invite", s.handleComputersInvite)
	mux.HandleFunc("/computers", s.handleComputers)
	mux.HandleFunc("/agents", s.handleAgents)
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/agent-use", s.handleAgentUse)
	mux.HandleFunc("/daemon-update", s.handleDaemonUpdate)
	mux.HandleFunc("/phone", s.handlePhone)
	mux.HandleFunc("/iphone", s.handleIPhone)
	mux.HandleFunc("/phone/device", s.handlePhoneDevice)
	mux.Handle("/friends", s.appRoute(webUI, http.HandlerFunc(s.handleFriends)))
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/session-file-capability", s.handleSessionFileCapability)
	mux.HandleFunc("/session-file", s.handleSessionFileBinary)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/", webUI)
	return withCORS(mux)
}

// RunWithReady starts the HTTP server and calls onReady only after the TCP
// listener has been acquired successfully.
func (s *Server) RunWithReady(ctx context.Context, addr string, onReady func()) error {
	defer s.closeEventSubscriptions()
	tcp, err := net.Listen("tcp", addr)
	if err != nil {
		s.shutdownAuthenticatedClients()
		s.terminalCleanup.Drain()
		return err
	}
	if s.connections != nil {
		if bound, ok := tcp.Addr().(*net.TCPAddr); ok {
			s.connections.SetLoopbackPort(bound.Port)
		}
	}
	if bound, ok := tcp.Addr().(*net.TCPAddr); ok {
		s.webPort.Store(int32(bound.Port))
	}
	var listener net.Listener = tcp
	if s.tlsConfig != nil {
		listener = &tlsHTTPListener{Listener: tcp, config: s.tlsConfig.Clone()}
	}
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.startLive(runtimeCtx) // live state
	srv := &http.Server{Handler: s.Handler()}

	var runtime sync.WaitGroup
	runtime.Add(6)
	go func() { defer runtime.Done(); s.runResourceTelemetry(runtimeCtx) }()
	go func() { defer runtime.Done(); s.runBrainHostContinuity(runtimeCtx) }()
	go func() { defer runtime.Done(); s.runPrivateListeners(runtimeCtx) }()
	go func() {
		defer runtime.Done()
		s.broadcastEvents(runtimeCtx)
	}()
	go func() {
		defer runtime.Done()
		s.runBrainSessionReconcile(runtimeCtx)
	}()
	go func() { defer runtime.Done(); s.runWorkerFeedReconcile(runtimeCtx) }()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-runtimeCtx.Done()
		s.shutdownAuthenticatedClients()
		_ = srv.Shutdown(context.Background())
	}()

	if onReady != nil {
		onReady()
	} else {
		log.Printf("mewla listening on %s", listener.Addr())
	}
	serveErr := srv.Serve(listener)
	cancel()
	<-shutdownDone
	runtime.Wait()
	s.terminalCleanup.Drain()
	return serveErr
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		if _, ok := s.authenticateRequest(w, r, "mewla-probe"); !ok {
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, "mewla-probe", map[string]any{
			"ok":                true,
			"daemon_id":         s.auth.DaemonID(),
			"daemon_public_key": s.auth.PublicKeyHex(),
		})
		return
	}
	device, ok := s.authenticateRequest(w, r, "mewla-connect")
	if !ok {
		return
	}

	conn, err := upgrader.Upgrade(w, r, websocketAuthResponseHeader(r))
	if err != nil {
		log.Printf("websocket: upgrade failed remote=%s device=%s: %v", r.RemoteAddr, device.ID, err)
		return
	}
	keepBacklogInOutbox(conn.UnderlyingConn())
	owner := &authenticatedClient{
		deviceID: device.ID,
	}
	if !s.bindAuthenticatedClient(conn, owner) {
		_ = conn.Close()
		return
	}
	defer s.terminal.ForgetOwner(clientID(conn))
	defer s.detachAuthenticatedClient(conn, owner)

	debuglog.Printf("websocket: client connected remote=%s device=%s clients=%d", r.RemoteAddr, device.ID, s.clientCount())
	connectedAt := time.Now()
	link := keepWebSocketAlive(conn)
	link.deflate = clientOffersDeflate(r)
	defer link.stop()
	if outbox := s.outboxFor(conn); outbox != nil {
		if clientOffersFrameDeflate(r) {
			outbox.useFrameDeflate()
		}
		if clientOffersTailless(r) {
			outbox.useTailless()
		}
	}
	// Brain first: an app opens on Brain, and subscribes to its thread once it
	// knows the thread. Work and Calendar go in the bulk lane (isBulkMessage),
	// behind whatever thread the app opens meanwhile.
	s.sendBrainSnapshot(conn, "")
	s.sendWorkerSessionList(conn)
	if s.work != nil {
		s.sendJSON(conn, map[string]any{
			"type":       "work_items_snapshot",
			"work_items": s.workItemsForClients(),
		})
	}
	if s.calendar != nil {
		s.sendCalendarSnapshot(conn, "")
	}
	s.sendDirectEndpoints(conn)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			out := s.outboxStats(conn)
			if cause := link.endCause(err, out); cause != "peer-closed" || !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || out.sent >= wsQuietCloseLogBytes {
				// Without this line a phone that lost its connection leaves no
				// trace here: connect and disconnect are verbose-only. The
				// cause says which end hung up. Only a close frame that says
				// goodbye is quiet: a reset or an abrupt close is the phone
				// vanishing, which is what this line is for. So is a goodbye
				// after megabytes: types= says what the link carried.
				log.Printf("websocket: client connection lost remote=%s device=%s age=%s cause=%s %s: %v", r.RemoteAddr, device.ID, time.Since(connectedAt).Round(time.Second), cause, link.describe(out), err)
			}
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsReadIdleTimeout))
		if owner.revoked.Load() ||
			s.auth == nil ||
			!s.auth.IsDeviceTrusted(owner.deviceID) {
			break
		}
		s.handleClientMessage(conn, msg)
	}

	debuglog.Printf("websocket: client disconnected remote=%s device=%s clients=%d", r.RemoteAddr, device.ID, s.clientCount())
}

func (s *Server) bindAuthenticatedClient(
	conn *websocket.Conn,
	owner *authenticatedClient,
) bool {
	if conn == nil || owner == nil || strings.TrimSpace(owner.deviceID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeClosing ||
		s.auth == nil ||
		!s.auth.IsDeviceTrusted(owner.deviceID) {
		owner.revoked.Store(true)
		return false
	}
	s.clients[conn] = owner
	s.active[conn] = ""
	s.outboxes[conn] = newClientOutbox(conn)
	s.codexSubs[conn] = map[string]codexConversationSubscription{}
	return true
}

func (s *Server) detachAuthenticatedClient(
	conn *websocket.Conn,
	owner *authenticatedClient,
) {
	s.mu.Lock()
	var work clientDetachWork
	if s.clients[conn] == owner {
		work = s.removeClientLocked(conn)
	}
	s.mu.Unlock()
	s.completeClientDetach(work)
}

// deviceIDOf is the paired device behind an authenticated connection.
func (s *Server) deviceIDOf(conn *websocket.Conn) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner := s.clients[conn]; owner != nil {
		return owner.deviceID
	}
	return ""
}

func (s *Server) revokeAuthenticatedDevice(deviceID string) {
	normalizedID := strings.TrimSpace(deviceID)
	if normalizedID == "" {
		return
	}
	if s.pusher != nil {
		// A revoked phone must stop hearing what its agents say.
		s.pusher.ForgetDevice(normalizedID)
	}
	var revoked []clientDetachWork
	s.mu.Lock()
	for conn, owner := range s.clients {
		if owner == nil || owner.deviceID != normalizedID {
			continue
		}
		revoked = append(revoked, s.removeClientLocked(conn))
	}
	s.mu.Unlock()

	for _, work := range revoked {
		s.completeClientDetach(work)
	}
	s.forgetUnpairedScreenDevices()
}

func (s *Server) shutdownAuthenticatedClients() {
	var closing []clientDetachWork
	s.mu.Lock()
	if s.runtimeClosing {
		s.mu.Unlock()
		return
	}
	s.runtimeClosing = true
	for conn := range s.clients {
		closing = append(closing, s.removeClientLocked(conn))
	}
	unsubscribe := s.authRevocationUnsubscribe
	s.authRevocationUnsubscribe = nil
	s.mu.Unlock()

	for _, work := range closing {
		s.completeClientDetach(work)
	}
	if unsubscribe != nil {
		unsubscribe()
	}
}

func (s *Server) removeClientLocked(
	conn *websocket.Conn,
) clientDetachWork {
	owner, exists := s.clients[conn]
	if !exists {
		return clientDetachWork{}
	}
	if owner != nil {
		owner.revoked.Store(true)
	}
	s.cancelCodexSubscriptionsLocked(conn)
	s.cancelSkillsRequestsLocked(conn)
	terminalCleanup := s.terminal.DetachAll(clientID(conn))
	s.terminalCleanup.Submit(terminalCleanup)
	delete(s.clients, conn)
	delete(s.active, conn)
	delete(s.appStates, conn)
	if outbox := s.outboxes[conn]; outbox != nil {
		outbox.close()
	}
	delete(s.outboxes, conn)
	delete(s.codexSubs, conn)
	return clientDetachWork{conn: conn}
}

func (s *Server) completeClientDetach(work clientDetachWork) {
	if work.conn != nil {
		_ = work.conn.Close()
	}
}

func (s *Server) clientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *Server) cancelSkillsRequestsLocked(conn *websocket.Conn) {
	if inventory, ok := s.skillsInventories[conn]; ok {
		inventory.cancel()
		delete(s.skillsInventories, conn)
	}
	if plugins, ok := s.pluginsInventories[conn]; ok {
		plugins.cancel()
		delete(s.pluginsInventories, conn)
	}
	if mutation, ok := s.skillsMutations[conn]; ok {
		mutation.cancel()
		delete(s.skillsMutations, conn)
	}
	if inspect, ok := s.skillsInspects[conn]; ok {
		inspect.cancel()
		delete(s.skillsInspects, conn)
	}
	if mutation, ok := s.pluginsMutations[conn]; ok {
		mutation.cancel()
		delete(s.pluginsMutations, conn)
	}
}

// handleShortPairLink turns http://127.0.0.1:9876/p/<token> into the browser
// pairing page for this origin. The token is the secret either way; only
// loopback requests to a loopback host are served, so the origin in the
// connect link is this computer.
func (s *Server) handleShortPairLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	host, _, err := net.SplitHostPort(r.Host)
	remote, _, remoteErr := net.SplitHostPort(r.RemoteAddr)
	if err != nil || remoteErr != nil || !loopbackHost(host) || !loopbackHost(remote) {
		http.NotFound(w, r)
		return
	}
	token, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(r.URL.Path, "/p/"))
	if err != nil || len(token) != 32 {
		http.NotFound(w, r)
		return
	}
	link, err := pairlink.ConnectLink("ws://"+r.Host, s.auth.PublicKeyHex(), hex.EncodeToString(token))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fragment := url.Values{}
	fragment.Set("pair", link)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/#"+fragment.Encode(), http.StatusFound)
}

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// pairErrorStatus maps a pairing failure to its HTTP status: conflicts name an
// identity that cannot be paired as asked; everything else is unauthorized.
func pairErrorStatus(err error) int {
	if errors.Is(err, auth.ErrWrongDaemon) || errors.Is(err, auth.ErrDeviceKeyMismatch) {
		return http.StatusConflict
	}
	return http.StatusUnauthorized
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var raw struct {
		EnrollmentToken   string `json:"enrollment_token"`
		ExpectedDaemonID  string `json:"expected_daemon_id"`
		ExpectedPublicKey string `json:"expected_daemon_public_key"`
		DeviceID          string `json:"device_id"`
		DeviceName        string `json:"device_name"`
		DevicePublicKey   string `json:"device_public_key"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&raw) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	device, err := s.auth.EnrollDevice(
		raw.EnrollmentToken,
		raw.ExpectedDaemonID,
		raw.ExpectedPublicKey,
		raw.DeviceID,
		raw.DeviceName,
		raw.DevicePublicKey,
	)
	if err != nil {
		http.Error(w, err.Error(), pairErrorStatus(err))
		return
	}

	s.writeJSONWithAssertion(w, http.StatusOK, "mewla-pair", map[string]any{
		"ok":                true,
		"daemon_id":         s.auth.DaemonID(),
		"daemon_public_key": s.auth.PublicKeyHex(),
		"device_id":         device.ID,
		"device_name":       device.Name,
	})
}

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authenticateRequest(w, r, "mewla-probe")
	if !ok {
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "mewla-probe", map[string]any{
		"ok":                true,
		"device_id":         device.ID,
		"daemon_id":         s.auth.DaemonID(),
		"daemon_public_key": s.auth.PublicKeyHex(),
		"name":              s.computerName,
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, auth.DeviceListPurpose); !ok {
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, auth.DeviceListPurpose, map[string]any{
			"devices": s.auth.ListDevices(),
		})
	case http.MethodDelete:
		var raw struct {
			DeviceID string `json:"device_id"`
		}
		decodeErr := json.NewDecoder(
			io.LimitReader(r.Body, 4<<10),
		).Decode(&raw)
		deviceID := strings.TrimSpace(raw.DeviceID)
		purpose := auth.DeviceRevokePurpose(deviceID)
		if _, ok := s.authenticateRequest(w, r, purpose); !ok {
			return
		}
		if decodeErr != nil || deviceID == "" {
			writeDeviceAPIError(
				w,
				http.StatusBadRequest,
				"invalid_device",
				"A valid device_id is required.",
			)
			return
		}
		persistence, err := s.auth.RevokeDevice(deviceID)
		if !persistence.Applied {
			if errors.Is(err, auth.ErrUnknownDevice) {
				writeDeviceAPIError(
					w,
					http.StatusNotFound,
					"device_not_found",
					"The trusted device was not found.",
				)
				return
			}
			log.Printf("revoke trusted device %q: %v", deviceID, err)
			writeDeviceAPIError(
				w,
				http.StatusInternalServerError,
				"device_revoke_failed",
				"The device could not be revoked.",
			)
			return
		}
		if err != nil {
			log.Printf(
				"revoked trusted device %q but directory durability is uncertain: %v",
				deviceID,
				err,
			)
		}
		s.writeJSONWithAssertion(w, http.StatusOK, purpose, map[string]any{
			"ok":                  true,
			"device_id":           deviceID,
			"persistence_applied": persistence.Applied,
			"persistence_durable": persistence.Durable,
		})
	default:
		writeDeviceAPIError(
			w,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Method not allowed.",
		)
	}
}

func writeDeviceAPIError(
	w http.ResponseWriter,
	status int,
	code string,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func (s *Server) handleClientMessage(conn *websocket.Conn, msg []byte) {
	var raw clientMessage
	if err := json.Unmarshal(msg, &raw); err != nil {
		log.Printf("websocket: rejected malformed message remote=%s bytes=%d: %v", conn.RemoteAddr(), len(msg), err)
		return
	}

	if s.handleLiveMessage(conn, msg, raw) { // live state
		return
	}

	if s.handleModelProfileMessage(conn, raw) {
		return
	}

	switch raw.Type {
	case "connections":
		s.handleConnections(conn, raw)
	case "list_worker_sessions", "set_active_worker", "create_session", "kill_worker":
		s.handleSessionLifecycleMessage(conn, raw)
	case "telegram_connection_status", "telegram_connection_configure", "telegram_connection_bind", "telegram_connection_enable", "telegram_connection_disable", "telegram_connection_revoke", "telegram_connection_remove":
		s.handleTelegramMessage(conn, raw)
	case "client_diagnostic":
		s.handleClientDiagnostic(conn, raw)
	case "send_input":
		s.handleQueuedSessionInput(conn, raw)
	case "send_key", "send_action":
		s.handleSessionInputMessage(conn, raw)
	case "approval_decision":
		s.handleApprovalDecision(conn, raw)
	case "answer_choice":
		go s.handleAnswerChoice(conn, raw)
	case "answer_prompt":
		go s.handleAnswerPrompt(conn, raw)
	case "rewind_message":
		go s.handleRewindMessage(conn, raw)
	case "git_diff_status", "git_diff_patch", "git_diff_file_content", "git_repo_paths", "git_repo_file_content", "list_dir":
		s.handleRepositoryMessage(conn, raw)
	case "git_stage", "git_unstage", "git_discard", "git_discard_undo", "git_hunk", "git_commit", "git_push", "git_pull", "git_fetch", "git_switch", "git_branches", "git_log", "git_show":
		// Mutations can wait on the network (push, pull), so they never block the read loop.
		go s.handleGitMessage(conn, raw)
	case "service_tunnel":
		s.handleServiceTunnel(conn, raw)
	case "list_session_services":
		s.handleListSessionServices(conn, raw)
	case "list_work_items":
		s.handleListWorkItems(conn, raw)
	case "write_work_item":
		s.handleWriteWorkItem(conn, raw)
	case "delete_work_item":
		s.handleDeleteWorkItem(conn, raw)
	case "list_calendar_items":
		s.sendCalendarSnapshot(conn, raw.RequestID)
	case "get_calendar_item":
		s.handleGetCalendarItem(conn, raw)
	case "create_calendar_item":
		s.handleCreateCalendarItem(conn, raw)
	case "update_calendar_item":
		s.handleUpdateCalendarItem(conn, raw)
	case "cancel_calendar_item":
		s.handleCancelCalendarItem(conn, raw)
	case "run_calendar_item":
		s.handleRunCalendarItem(conn, raw)
	case "snooze_calendar_item":
		s.handleSnoozeCalendarItem(conn, raw)
	case "done_calendar_item":
		s.handleDoneCalendarItem(conn, raw)
	case "computers":
		s.handleComputersList(conn, raw)
	case "brain_snapshot":
		s.sendBrainSnapshot(conn, raw.RequestID)
	case "brain_context":
		s.handleBrainContext(conn, raw)
	case "brain_gc":
		s.handleBrainGC(conn, raw)
	case "brain_work_read":
		s.handleBrainWorkRead(conn, raw)
	case "brain_work_detail":
		s.handleBrainWorkDetail(conn, raw)
	case "answered_requests":
		s.handleAnsweredRequests(conn, raw)
	case "brain_work_action":
		// A reply waits on the Brain host's input admission; keep reading.
		go s.handleBrainWorkAction(conn, raw)
	case "brain_set_executor":
		s.handleBrainSetExecutor(conn, raw)
	case "brain_chat_new":
		s.handleBrainChatNew(conn, raw)
	case "brain_workspace_tree":
		s.handleBrainWorkspaceTree(conn, raw)
	case "brain_workspace_file":
		s.handleBrainWorkspaceFile(conn, raw)
	case "register_push":
		if raw.PushToken != "" && s.pusher != nil {
			s.pusher.SetRegistration(s.deviceIDOf(conn), raw.PushToken, raw.ServerRef)
			if pusher, ok := s.pusher.(deviceInfoPusher); ok {
				pusher.SetDevice(s.deviceIDOf(conn), raw.Platform, raw.StrongAlerts == nil || *raw.StrongAlerts)
			}
			if s.deviceHook != nil {
				s.deviceHook(strings.TrimSpace(raw.TimeZone), strings.TrimSpace(raw.Locale))
			}
			s.sendJSON(conn, map[string]any{"type": "push_registered", "ok": true})
		}
	case "notification_response":
		s.handleNotificationResponse(raw)
	case "app_state":
		s.handleAppState(conn, raw)
	case "codex_conversation_subscribe":
		s.handleCodexConversationSubscribe(conn, raw)
	case "codex_conversation_unsubscribe":
		s.handleCodexConversationUnsubscribe(conn, raw)
	case "codex_slash_commands":
		s.handleCodexSlashCommands(conn, raw)
	case "codex_skills":
		s.handleCodexSkills(conn, raw)
	case "skills_inventory":
		s.handleSkillsInventory(conn, raw)
	case "skills_command":
		s.handleSkillsCommand(conn, raw)
	case "skills_mutation":
		s.handleSkillsMutation(conn, raw)
	case "skills_inspect":
		s.handleSkillsInspect(conn, raw)
	case "plugins_inventory":
		s.handlePluginsInventory(conn, raw)
	case "plugin_command":
		s.handlePluginCommand(conn, raw)
	case "plugin_mutation":
		s.handlePluginMutation(conn, raw)
	case "codex_status":
		go s.handleCodexStatus(conn, raw)
	case "terminal_snapshot":
		s.handleTerminalMessage(conn, raw)
	case "codex_asset":
		s.handleCodexAsset(conn, raw)
	case "session_file_metadata":
		s.handleSessionFileMetadata(conn, raw)
	case "session_file_text":
		s.handleSessionFileText(conn, raw)
	case "terminal_history", "terminal_open", "terminal_input", "terminal_resize", "terminal_scroll", "terminal_scroll_cancel", "terminal_focus_pane", "terminal_close":
		s.handleTerminalMessage(conn, raw)
	case "get_stats":
		if resp := s.stats.Stats(); resp != nil {
			payload := map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     resp.Ranges,
				"pricing":    resp.Pricing,
			}
			// Official subscription usage is only meaningful for the direct
			// ChatGPT/Codex login. When the effective Codex connection is a
			// routed Provider/API-key connection, suppress the subscription
			// even if stale official usage is still cached on this host.
			if resp.CodexSubscription != nil && resp.CodexSubscription.AuthKind == "official" &&
				(s.profiles == nil || !s.profiles.CodexRoutedDefault()) {
				payload["codexSubscription"] = resp.CodexSubscription
			}
			s.sendJSON(conn, payload)
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "stats_data",
				"request_id": raw.RequestID,
				"ranges":     map[string]any{},
			})
		}
	case "dsh_interaction":
		// A snapshot request that waits for a change must not hold the read loop.
		go s.handleDSHInteraction(conn, raw)
	case "session_image":
		s.handleDSHImage(conn, raw)
	case "get_resource_telemetry":
		s.handleGetResourceTelemetry(conn, raw)
	default:
		log.Printf("websocket: rejected unknown message type=%q remote=%s request=%s", raw.Type, conn.RemoteAddr(), raw.RequestID)
		if raw.RequestID != "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "unknown_message_type", fmt.Sprintf("Unknown message type: %s", raw.Type))
		}
	}
}

func (s *Server) handleSessionLifecycleMessage(conn *websocket.Conn, raw clientMessage) {
	switch raw.Type {
	case "list_worker_sessions":
		s.sendWorkerSessionList(conn)
	case "set_active_worker":
		s.mu.Lock()
		s.active[conn] = raw.WorkerID
		recorder := s.notifications
		s.mu.Unlock()
		if recorder != nil && strings.TrimSpace(raw.WorkerID) != "" {
			recorder.NotificationEngaged(notificationViewed, strings.TrimSpace(raw.WorkerID))
		}
	case "create_session":
		// An app that predates the bypass default still sends its Claude preset
		// in Auto mode; Mewla has no Auto mode, so that Session starts in full
		// authority like every other.
		command := work.MigrateClaudeAutoMode(strings.TrimSpace(raw.Command))
		if work.InferWorkerProvider(command) == work.WorkerProviderDSH {
			ensured, err := work.EnsureDSHSessionLaunchCommand(command)
			if err != nil {
				s.sendErrorWithRequestID(conn, raw.RequestID, "create_session_failed", err.Error())
				return
			}
			command = ensured
		}
		if work.InferWorkerProvider(command) == work.WorkerProviderPi {
			ensured, err := work.EnsurePiSessionLaunchCommand(command)
			if err != nil {
				s.sendJSON(conn, map[string]any{
					"type":       "error",
					"code":       "create_session_failed",
					"message":    err.Error(),
					"request_id": raw.RequestID,
				})
				return
			}
			command = ensured
		}
		firstMessage, firstErr := s.prepareFirstMessage(command, raw.Text, raw.Attachments)
		if firstErr != nil {
			code := "create_session_failed"
			var refused *firstMessageError
			if errors.As(firstErr, &refused) {
				code = refused.code
			}
			s.sendErrorWithRequestID(conn, raw.RequestID, code, firstErr.Error())
			return
		}
		connectionID := strings.TrimSpace(raw.ConnectionID)
		if connectionID == "" {
			connectionID = strings.TrimSpace(raw.ProfileID)
		}
		workerID, routeSnap, persist, err := s.createSessionWithProfiles(raw.TargetID, watcher.CreateSessionOptions{
			Cwd:     raw.Cwd,
			Command: command,
			Name:    raw.Name,
		}, connectionID, raw.ModelID)
		if err != nil && (!persist.Applied || strings.TrimSpace(workerID) == "") {
			code := "create_session_failed"
			if mapped := modelprofiles.ControlErrorCode(err); mapped != "" && mapped != modelprofiles.CodeProfilesUnavailable {
				code = mapped
			}
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"code":       code,
				"message":    err.Error(),
				"request_id": raw.RequestID,
			})
			return
		}
		response := map[string]any{
			"type":       "session_created",
			"request_id": raw.RequestID,
			"worker_id":  workerID,
		}
		launched := command
		if worker := s.watcher.GetWorker(workerID); worker != nil {
			response["worker_session"] = s.workerSessionWire(worker)
			if strings.TrimSpace(worker.Command) != "" {
				launched = worker.Command
			}
		}
		if firstMessage != nil {
			response["first_message"] = "pending"
		}
		if routeSnap != nil {
			response["session_route"] = routeSnap
			if outcome, durable := modelprofiles.WirePersistFields(persist); outcome != "" {
				response["persistence_outcome"] = outcome
				response["persistence_durable"] = durable
			}
			if err != nil {
				response["persistence_warning"] = err.Error()
			}
		}
		s.sendJSON(conn, response)
		if firstMessage != nil {
			go s.deliverFirstMessage(workerID, launched, raw.RequestID, firstMessage)
		}
	case "kill_worker":
		teardown := s.teardownWorkerSession(raw.WorkerID)
		if teardown.Err != nil {
			log.Printf("kill_worker failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, teardown.Err)
			code := modelprofiles.ControlErrorCode(teardown.Err)
			if code == "" || code == modelprofiles.CodeProfilesUnavailable || code == "close_failed" {
				code = "kill_failed"
			}
			payload := map[string]any{
				"type":    "error",
				"code":    code,
				"message": teardown.Err.Error(),
			}
			if raw.RequestID != "" {
				payload["request_id"] = raw.RequestID
			}
			if teardown.Persist.Applied {
				outcome, durable := modelprofiles.WirePersistFields(teardown.Persist)
				if outcome != "" {
					payload["persistence_outcome"] = outcome
					payload["persistence_durable"] = durable
				}
			}
			s.sendJSON(conn, payload)
			return
		}
		// The Session is confirmed gone; this reply, not its absence from some
		// later list, is what tells the client the terminate succeeded.
		s.sendJSON(conn, map[string]any{
			"type":       "worker_killed",
			"request_id": raw.RequestID,
			"worker_id":  raw.WorkerID,
		})
	}
}

func (s *Server) handleTelegramMessage(conn *websocket.Conn, raw clientMessage) {
	switch raw.Type {
	case "telegram_connection_status":
		s.sendTelegramStatus(conn, raw.RequestID)
	case "telegram_connection_configure":
		if s.telegram == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_unavailable", "Telegram connection is unavailable.")
			break
		}
		configureCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, err := s.telegram.Configure(configureCtx, raw.Credential)
		cancel()
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_configure_failed", err.Error())
			break
		}
		s.sendTelegramStatus(conn, raw.RequestID)
	case "telegram_connection_bind":
		if s.telegram == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_unavailable", "Telegram connection is unavailable.")
			break
		}
		challenge, err := s.telegram.BeginBinding()
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_bind_failed", err.Error())
			break
		}
		s.sendJSON(conn, map[string]any{"type": "telegram_binding_challenge", "request_id": raw.RequestID, "challenge": challenge})
	case "telegram_connection_enable", "telegram_connection_disable", "telegram_connection_revoke", "telegram_connection_remove":
		if s.telegram == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_unavailable", "Telegram connection is unavailable.")
			break
		}
		var err error
		switch raw.Type {
		case "telegram_connection_enable":
			err = s.telegram.Enable()
		case "telegram_connection_disable":
			err = s.telegram.Disable()
		case "telegram_connection_revoke":
			err = s.telegram.RevokeOwner()
		case "telegram_connection_remove":
			err = s.telegram.Remove()
		}
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "telegram_mutation_failed", "Telegram connection could not be updated.")
			break
		}
		s.sendTelegramStatus(conn, raw.RequestID)
	}
}

func (s *Server) handleSessionInputMessage(conn *websocket.Conn, raw clientMessage) {
	switch raw.Type {
	case "send_input":
		// The phone sends the person's text and the staged files as typed
		// fields; this is the one place they become what a provider receives.
		// The final newline is the transport's submit delimiter.
		// The phone names its attachments by path; only files the upload store
		// holds may reach a provider, which can read whatever the path says.
		files, confineErr := s.uploadStore.ConfineAll(raw.Attachments)
		if confineErr != nil {
			s.sendJSON(conn, map[string]any{
				"type": "input_failed", "request_id": raw.RequestID,
				"code": "attachment_not_uploaded", "message": confineErr.Error(),
			})
			break
		}
		message := attachment.Message{Text: raw.Text, Files: files}.Render()
		if message == "" {
			s.sendJSON(conn, map[string]any{
				"type": "input_failed", "request_id": raw.RequestID,
				"code": "send_input_failed", "message": "Message is empty.",
			})
			break
		}
		brainSteering := false
		if s.brain != nil {
			var steeringErr error
			brainSteering, steeringErr = s.brain.NoteUserSteering(raw.WorkerID)
			if steeringErr != nil {
				s.sendJSON(conn, map[string]any{
					"type": "input_failed", "request_id": raw.RequestID,
					"code": "send_input_failed", "message": steeringErr.Error(),
				})
				break
			}
		}
		var brainAdmission brain.BrainInputAdmission
		if brainSteering {
			prepared, created, prepareErr := s.brain.PrepareHostUserInput(
				raw.WorkerID,
				raw.RequestID,
				message,
				raw.ConversationScopeKey,
			)
			if prepareErr != nil {
				s.brain.CancelUserSteering(raw.WorkerID)
				log.Printf("brain prepare user input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, prepareErr)
				s.sendJSON(conn, map[string]any{
					"type":       "input_failed",
					"request_id": raw.RequestID,
					"code":       "send_input_failed",
					"message":    prepareErr.Error(),
				})
				break
			}
			brainAdmission = prepared
			if !created {
				// Accepted is an idempotent duplicate that may still need
				// timeline projection. Terminal non-admission states never cross
				// the provider mutation boundary again.
				s.brain.CancelUserSteering(raw.WorkerID)
				if prepared.State == brain.BrainInputAdmissionAccepted {
					if admitErr := s.brain.AdmitHostUserInput(prepared); admitErr == nil {
						s.sendJSON(conn, map[string]any{"type": "input_sent", "request_id": raw.RequestID})
						break
					}
				}
				if prepared.State == brain.BrainInputAdmissionNotSubmitted {
					s.sendJSON(conn, map[string]any{
						"type":       "input_failed",
						"request_id": raw.RequestID,
						"code":       "input_not_submitted",
						"message":    "This request was not submitted. Retry with a new request id.",
					})
					break
				}
				if prepared.State == brain.BrainInputAdmissionUncertain {
					s.sendJSON(conn, map[string]any{
						"type":       "input_pending",
						"request_id": raw.RequestID,
						"code":       "input_uncertain",
						"message":    "Delivery could not be proven and was not replayed.",
					})
					break
				}
				s.sendJSON(conn, map[string]any{"type": "input_pending", "request_id": raw.RequestID})
				break
			}
		}
		err := s.sendInputWithReceipt(raw.WorkerID, message+"\n", raw.RequestID)
		if err != nil {
			inputOutcome := watcher.InputOutcomeFromError(err)
			if watcher.IsStaleReceiptMismatch(err) {
				// The receipt is durably bound to a different payload: a
				// legacy/stale binding detected strictly before provider
				// mutation. The daemon keeps the fail-closed guard; the app
				// invalidates this stale identity and transparently resubmits
				// the same logical input once with a fresh identity.
				if brainSteering {
					if abortErr := s.brain.AbortHostUserInput(brainAdmission.RequestID, brainAdmission.ThreadID); abortErr != nil {
						log.Printf("brain abort stale user input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, abortErr)
						s.brain.CancelUserSteering(raw.WorkerID)
						s.sendJSON(conn, map[string]any{
							"type":       "input_pending",
							"request_id": raw.RequestID,
						})
						break
					}
					s.brain.CancelUserSteering(raw.WorkerID)
				}
				s.sendJSON(conn, map[string]any{
					"type":       "input_failed",
					"request_id": raw.RequestID,
					"code":       "stale_receipt_invalidated",
					"message":    err.Error(),
				})
				break
			}
			if inputOutcome == watcher.InputAmbiguous {
				if brainSteering {
					if releaseErr := s.brain.ReleaseHostUserInputAttempt(brainAdmission.RequestID, brainAdmission.ThreadID); releaseErr != nil {
						log.Printf("brain release ambiguous user input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, releaseErr)
					}
				}
				s.sendJSON(conn, map[string]any{
					"type":       "input_pending",
					"request_id": raw.RequestID,
					"code":       "input_uncertain",
					"message":    "Delivery could not be proven and was not replayed.",
				})
				break
			}
			if brainSteering {
				if abortErr := s.brain.AbortHostUserInput(brainAdmission.RequestID, brainAdmission.ThreadID); abortErr != nil {
					// The provider proved non-submission, but failure to durably
					// remove the intent must remain a no-replay pending outcome.
					log.Printf("brain abort user input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, abortErr)
					s.brain.CancelUserSteering(raw.WorkerID)
					s.sendJSON(conn, map[string]any{"type": "input_pending", "request_id": raw.RequestID})
					break
				}
				s.brain.CancelUserSteering(raw.WorkerID)
				s.sendJSON(conn, map[string]any{
					"type":       "input_failed",
					"request_id": raw.RequestID,
					"code":       "input_not_submitted",
					"message":    "This request was not submitted. Retry with a new request id.",
				})
				break
			}
			log.Printf("send_input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, err)
			code := "send_input_failed"
			if errors.Is(err, watcher.ErrPromptOpen) {
				code = "prompt_open"
			}
			s.sendJSON(conn, map[string]any{
				"type":       "input_failed",
				"request_id": raw.RequestID,
				"code":       code,
				"message":    err.Error(),
			})
		} else {
			if brainSteering {
				if admitErr := s.brain.AdmitHostUserInput(brainAdmission); admitErr != nil {
					// Provider already accepted. Canonical Brain persistence failed,
					// so do not falsely ack input_sent. Pending preserves the local
					// row for transcript recovery without encouraging a duplicate retry.
					log.Printf("brain admit user input failed worker=%s request=%s: %v", raw.WorkerID, raw.RequestID, admitErr)
					s.sendJSON(conn, map[string]any{
						"type":       "input_pending",
						"request_id": raw.RequestID,
					})
					break
				}
			}
			s.sendJSON(conn, map[string]any{
				"type":       "input_sent",
				"request_id": raw.RequestID,
			})
		}
	case "send_key":
		if err := s.watcher.SendKey(raw.WorkerID, raw.Key); err != nil {
			log.Printf("send_key failed worker=%s request=%s key=%q: %v", raw.WorkerID, raw.RequestID, raw.Key, err)
			s.sendErrorWithRequestID(conn, raw.RequestID, "send_key_failed", err.Error())
		} else if raw.RequestID != "" {
			s.sendJSON(conn, map[string]any{
				"type":       "key_sent",
				"request_id": raw.RequestID,
				"worker_id":  raw.WorkerID,
				"key":        raw.Key,
			})
		}
	case "send_action":
		err := s.sendAction(raw.WorkerID, raw.Action)
		if err != nil {
			log.Printf("send_action failed worker=%s request=%s action=%q: %v", raw.WorkerID, raw.RequestID, raw.Action, err)
			s.sendJSON(conn, map[string]any{
				"type":       "action_failed",
				"request_id": raw.RequestID,
				"code":       "send_action_failed",
				"message":    err.Error(),
			})
		} else {
			s.sendJSON(conn, map[string]any{
				"type":       "action_sent",
				"request_id": raw.RequestID,
			})
		}
	}
}

func (s *Server) handleRepositoryMessage(conn *websocket.Conn, raw clientMessage) {
	switch raw.Type {
	case "git_diff_status":
		payload, err := s.buildGitDiffStatus(raw.TargetID, raw.Cwd)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_status_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_status",
			"request_id": raw.RequestID,
			"status":     payload,
		})
	case "git_diff_patch":
		payload, err := s.buildGitDiffPatch(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_patch_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_patch",
			"request_id": raw.RequestID,
			"patch":      payload,
		})
	case "git_diff_file_content":
		payload, err := s.buildGitDiffFileContent(raw.TargetID, raw.Cwd, raw.Path, raw.Scope)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_diff_file_content_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_diff_file_content",
			"request_id": raw.RequestID,
			"content":    payload,
		})
	case "git_repo_paths":
		payload, err := s.buildGitRepoPaths(raw.TargetID, raw.Cwd)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_repo_paths_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_repo_paths",
			"request_id": raw.RequestID,
			"browser":    payload,
		})
	case "git_repo_file_content":
		payload, err := s.buildGitRepoFileContent(raw.TargetID, raw.Cwd, raw.Path)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "git_repo_file_content_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "git_repo_file_content",
			"request_id": raw.RequestID,
			"content":    payload,
		})
	case "list_dir":
		dirPath := raw.Cwd
		if dirPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				s.sendJSON(conn, map[string]any{
					"type":       "error",
					"code":       "list_dir_failed",
					"message":    err.Error(),
					"request_id": raw.RequestID,
				})
				return
			}
			dirPath = home
		}
		dirPath = filepath.Clean(dirPath)
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"code":       "list_dir_failed",
				"message":    err.Error(),
				"request_id": raw.RequestID,
			})
			return
		}
		dirs := make([]map[string]string, 0)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if len(name) > 0 && name[0] == '.' {
				continue
			}
			dirs = append(dirs, map[string]string{
				"name": name,
				"path": filepath.Join(dirPath, name),
			})
		}
		s.sendJSON(conn, map[string]any{
			"type":       "dir_list",
			"request_id": raw.RequestID,
			"path":       dirPath,
			"entries":    dirs,
		})
	}
}

// handleTerminalMessage owns the terminal protocol family. The outer client
// dispatcher only selects the family, keeping terminal ownership and error
// shaping together.
func (s *Server) handleTerminalMessage(conn *websocket.Conn, raw clientMessage) {
	switch raw.Type {
	case "terminal_history":
		// A cursor asks for the scrollback after it. A Session without
		// numbered scrollback (tmux) answers with its snapshot instead.
		if raw.Since != nil {
			delta, ok, err := s.terminal.HistoryFrom(clientID(conn), raw.SessionID, *raw.Since)
			if err != nil {
				s.sendErrorWithRequestID(conn, raw.RequestID, "terminal_history_failed", err.Error())
				return
			}
			if ok {
				s.sendJSON(conn, map[string]any{"type": "terminal_history", "request_id": raw.RequestID, "session_id": raw.SessionID, "history": delta})
				return
			}
		}
		history, err := s.terminal.History(clientID(conn), raw.SessionID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "terminal_history_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "terminal_history", "request_id": raw.RequestID, "session_id": raw.SessionID, "history": history})

	case "terminal_snapshot":
		text, err := s.watcher.CapturePaneContent(raw.TargetID)
		if err != nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "terminal_snapshot_failed", err.Error())
			return
		}
		s.sendJSON(conn, map[string]any{"type": "terminal_snapshot", "request_id": raw.RequestID, "target_id": raw.TargetID, "text": text})
	case "terminal_open":
		targetID := raw.TargetID
		if targetID == "" {
			targetID = raw.WorkerID
		}
		target, err := s.terminalTarget(targetID)
		if err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "code": "open_failed", "message": err.Error()})
			return
		}
		_, err = s.terminal.Open(clientID(conn), target.Backend, targetID, terminal.OpenOptions{Cols: raw.Cols, Rows: raw.Rows, Socket: target.Socket}, func(v any) { s.sendJSONWhenRoom(conn, v) })
		if err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "code": "open_failed", "message": err.Error()})
		}
	case "terminal_input":
		// scroll: what a scroll drag made (wheel presses, cursor keys) is reading, not typing: it never takes
		// the Session's size from another viewer.
		input := s.terminal.Input
		if raw.Scroll {
			input = s.terminal.ScrollInput
		}
		if err := input(clientID(conn), raw.SessionID, raw.Data); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "input_failed", "message": err.Error()})
		}
	case "terminal_resize":
		if err := s.terminal.Resize(clientID(conn), raw.SessionID, raw.Cols, raw.Rows); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "resize_failed", "message": err.Error()})
		}
	case "terminal_scroll":
		if err := s.terminal.Scroll(clientID(conn), raw.SessionID, raw.Lines); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "scroll_failed", "message": err.Error()})
		}
	case "terminal_scroll_cancel":
		if err := s.terminal.ScrollCancel(clientID(conn), raw.SessionID); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "scroll_cancel_failed", "message": err.Error()})
		}
	case "terminal_focus_pane":
		if err := s.terminal.FocusPane(clientID(conn), raw.SessionID, raw.Col, raw.Row); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "focus_pane_failed", "message": err.Error()})
		}
	case "terminal_close":
		if err := s.terminal.Close(clientID(conn), raw.SessionID); err != nil {
			s.sendJSON(conn, map[string]any{"type": "terminal_error", "session_id": raw.SessionID, "code": "close_failed", "message": err.Error()})
		}
	}
}

// openableTerminal resolves where a Terminal view opens: only a present
// Session this daemon owns, through the backend the watcher says runs it.
// The client's backend field does not choose.
func (s *Server) openableTerminal(id string) (watcher.TerminalTarget, error) {
	unavailable := fmt.Errorf("target is not an available Mewla-owned Session")
	target, owned := s.watcher.TerminalTarget(id)
	if !owned {
		return watcher.TerminalTarget{}, unavailable
	}
	presence, err := s.watcher.ProbeSession(id)
	if err != nil {
		return watcher.TerminalTarget{}, err
	}
	if presence != watcher.SessionPresencePresent {
		return watcher.TerminalTarget{}, unavailable
	}
	return target, nil
}

func (s *Server) sendTelegramStatus(conn *websocket.Conn, requestID string) {
	if s.telegram == nil {
		s.sendErrorWithRequestID(conn, requestID, "telegram_unavailable", "Telegram connection is unavailable.")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "telegram_connection_status",
		"request_id": requestID,
		"connection": s.telegram.Status(),
	})
}

// sendWorkerSessionList sends the full list, current as of the seq it carries.
// It is sent once when a client connects and again only when the client asks
// after noticing a gap in the sequenced changes.
func (s *Server) sendWorkerSessionList(conn *websocket.Conn) {
	s.workerFeed.snapshot(s.visibleWorkerSessionWires, func(data []byte) {
		var lite []byte
		if outbox := s.outboxFor(conn); outbox != nil && outbox.isTailless() {
			lite = withoutTerminalTail(data)
		}
		if err := s.queueFeedMessage(conn, data, lite); err != nil {
			if errors.Is(err, errOutboxClosed) {
				debuglog.Printf("websocket: send to closed client remote=%s: %v", conn.RemoteAddr(), err)
				return
			}
			log.Printf("websocket: send failed remote=%s: %v", conn.RemoteAddr(), err)
		}
	})
}

type resolvedCodexConversationWorker struct {
	targetID    string
	worker      classifier.Worker
	provider    string
	fromWatcher bool
	ready       bool
	reason      string
}

func (s *Server) resolveCodexConversationWorker(raw clientMessage) resolvedCodexConversationWorker {
	targetID := strings.TrimSpace(raw.TargetID)
	if targetID == "" {
		targetID = strings.TrimSpace(raw.WorkerID)
	}

	var worker classifier.Worker
	workerFromWatcher := false
	if targetID != "" {
		if snapshot := s.watcher.GetWorker(targetID); snapshot != nil {
			worker = *snapshot
			workerFromWatcher = true
		}
	}
	startedAt := clientStartedAt(raw.StartedAt)
	if worker.ID == "" {
		if targetID != "" && startedAt.IsZero() {
			return resolvedCodexConversationWorker{
				targetID: targetID,
				ready:    false,
				reason:   "session_not_ready",
			}
		}
		worker = classifier.Worker{
			ID:        targetID,
			Name:      raw.Name,
			Cwd:       raw.Cwd,
			Command:   raw.Command,
			StartedAt: startedAt,
			ProcessID: raw.ProcessID,
		}
	}
	if raw.ProcessID > 0 && worker.ProcessID == 0 {
		worker.ProcessID = raw.ProcessID
	}
	if !startedAt.IsZero() && (!workerFromWatcher || worker.StartedAt.IsZero()) {
		worker.StartedAt = startedAt
	}
	if worker.ID == "" && strings.TrimSpace(worker.Cwd) == "" {
		return resolvedCodexConversationWorker{
			targetID: targetID,
			ready:    false,
			reason:   "worker_not_found",
		}
	}
	return resolvedCodexConversationWorker{
		targetID:    targetID,
		worker:      worker,
		provider:    s.structuredProviderForWorker(&worker),
		fromWatcher: workerFromWatcher,
		ready:       true,
	}
}

func (s *Server) sendInputWithReceipt(workerID, text, receipt string) error {
	if s != nil && s.sendInputWithReceiptOverride != nil {
		return s.sendInputWithReceiptOverride(strings.TrimSpace(workerID), text, strings.TrimSpace(receipt))
	}
	if s != nil && s.sendInputOverride != nil {
		return s.sendInputOverride(strings.TrimSpace(workerID), text)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("executor watcher unavailable")
	}
	if strings.TrimSpace(receipt) == "" {
		return s.watcher.SendInput(strings.TrimSpace(workerID), text)
	}
	return s.watcher.SendInputWithReceipt(strings.TrimSpace(workerID), text, strings.TrimSpace(receipt))
}

func (s *Server) sendAction(workerID, action string) error {
	if s != nil && s.sendActionOverride != nil {
		return s.sendActionOverride(strings.TrimSpace(workerID), action)
	}
	if s == nil || s.watcher == nil {
		return fmt.Errorf("executor watcher unavailable")
	}
	return s.watcher.SendAction(strings.TrimSpace(workerID), action)
}

func (s *Server) loadProviderConversationSnapshot(
	reader *work.ProviderConversationReader,
	resolved resolvedCodexConversationWorker,
	now time.Time,
) (work.CodexConversation, error) {
	conversation, err := s.loadProviderConversation(reader, resolved, now)
	if err != nil {
		return work.CodexConversation{}, err
	}
	return work.SanitizeConversationProjection(conversation), nil
}

// loadProviderConversation loads the provider conversation without applying
// the user-facing sanitize projection. The subscription loop runs the version
// skip before sanitize so an unchanged poll performs no history work at all;
// every sent payload is still sanitized afterwards.
func (s *Server) loadProviderConversation(
	reader *work.ProviderConversationReader,
	resolved resolvedCodexConversationWorker,
	now time.Time,
) (work.CodexConversation, error) {
	var conversation work.CodexConversation
	var err error
	if s.providerConversationLoader != nil {
		conversation, err = s.providerConversationLoader(reader, strings.TrimSpace(resolved.targetID))
	} else {
		conversation, err = reader.Load(resolved.worker, resolved.provider, now)
	}
	if err != nil {
		return work.CodexConversation{}, err
	}
	return s.rewound.apply(strings.TrimSpace(resolved.targetID), conversation), nil
}

func (s *Server) handleListSessionServices(conn *websocket.Conn, raw clientMessage) {
	payload, err := s.watcher.DiscoverSessionServices()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_session_services_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         "session_service_list",
		"request_id":   raw.RequestID,
		"generated_at": payload.GeneratedAt,
		"interfaces":   payload.Interfaces,
		"services":     payload.Services,
	})
}

func (s *Server) handleCodexConversationSubscribe(conn *websocket.Conn, raw clientMessage) {
	subscriptionID := strings.TrimSpace(raw.RequestID)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.TargetID)
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.WorkerID)
	}
	if subscriptionID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_conversation_subscribe_failed", "missing subscription id")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	generation := uuid.NewString()
	s.mu.Lock()
	if s.codexSubs[conn] == nil {
		s.codexSubs[conn] = map[string]codexConversationSubscription{}
	}
	if previous := s.codexSubs[conn][subscriptionID]; previous.cancel != nil {
		previous.cancel()
	}
	s.codexSubs[conn][subscriptionID] = codexConversationSubscription{
		cancel:     cancel,
		generation: generation,
	}
	s.mu.Unlock()

	go s.runCodexConversationSubscription(ctx, conn, raw, subscriptionID, generation)
}

func (s *Server) handleCodexConversationUnsubscribe(conn *websocket.Conn, raw clientMessage) {
	subscriptionID := strings.TrimSpace(raw.RequestID)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.TargetID)
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(raw.WorkerID)
	}
	if subscriptionID == "" {
		return
	}
	s.mu.Lock()
	if sub := s.codexSubs[conn][subscriptionID]; sub.cancel != nil {
		sub.cancel()
		delete(s.codexSubs[conn], subscriptionID)
	}
	s.mu.Unlock()
}

func (s *Server) cancelCodexSubscriptionsLocked(conn *websocket.Conn) {
	for id, sub := range s.codexSubs[conn] {
		if sub.cancel != nil {
			sub.cancel()
		}
		delete(s.codexSubs[conn], id)
	}
}

// brainTick says how much of a Brain-scoped conversation a tick had to project.
type brainTick string

const (
	// brainTickIdle: nothing the conversation is made from had moved.
	brainTickIdle brainTick = "idle"
	// brainTickSame: something had moved and the projection that followed was
	// the answer already sent.
	brainTickSame brainTick = "same"
	// brainTickBrain: only the Brain side had moved; the provider conversation
	// was not read again.
	brainTickBrain brainTick = "brain"
	// brainTickProvider: the provider conversation had moved.
	brainTickProvider brainTick = "provider"
)

func (s *Server) observeBrainTick(tick brainTick) {
	if s.brainTickObserve != nil {
		s.brainTickObserve(tick)
	}
}

type codexConversationSubscriptionSnapshot struct {
	conversation        work.CodexConversation
	fingerprint         string
	providerFingerprint string
	eventsByID          map[string]work.CodexConversationEvent
	// A Brain-scoped snapshot also remembers what the last projection that ran
	// was made from, whether or not it changed the answer: the provider
	// conversation it read (sanitized, as the Brain overlay takes it) with the
	// key that names it, and the key of the Brain side. A tick whose keys equal
	// these has nothing to project.
	providerConversation work.CodexConversation
	providerKey          providerProjectionKey
	brainKey             brainProjectionKey
	// eventFingerprints memoizes per-event content fingerprints so unchanged
	// history is never re-hashed on later polls or deltas.
	eventFingerprints map[string]string
	revision          int64
	// readerVersion is the provider reader content version of this snapshot.
	// 0 means unknown (test loader or non-cached provider); never skip then.
	readerVersion int64
	// overlayVersion is the version of the thread's own overlays (rewinds,
	// answered requests) this snapshot was projected with.
	overlayVersion uint64
	attached       bool
}

func (s *Server) runCodexConversationSubscription(
	ctx context.Context,
	conn *websocket.Conn,
	raw clientMessage,
	subscriptionID string,
	generation string,
) {
	reader := work.NewProviderConversationReader()
	ticker := time.NewTicker(codexConversationSubscriptionInterval)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		if current, ok := s.codexSubs[conn][subscriptionID]; ok && current.generation == generation {
			delete(s.codexSubs[conn], subscriptionID)
		}
		s.mu.Unlock()
	}()

	var previous *codexConversationSubscriptionSnapshot
	for {
		s.publishCodexConversationSubscription(ctx, conn, raw, subscriptionID, generation, reader, &previous)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) publishCodexConversationSubscription(
	ctx context.Context,
	conn *websocket.Conn,
	raw clientMessage,
	subscriptionID string,
	generation string,
	reader *work.ProviderConversationReader,
	previous **codexConversationSubscriptionSnapshot,
) {
	if ctx.Err() != nil || !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	resolved := s.resolveCodexConversationWorker(raw)
	if !resolved.ready {
		conversation := work.CodexConversation{
			Available: false,
			Reason:    resolved.reason,
			Events:    []work.CodexConversationEvent{},
		}
		fingerprint := fmt.Sprintf(
			"sync:%s:%s:%s",
			resolved.targetID,
			resolved.reason,
			codexConversationSubscriptionFingerprint(conversation),
		)
		if (*previous) != nil && (*previous).fingerprint == fingerprint {
			return
		}
		revision := int64(1)
		if (*previous) != nil {
			revision = (*previous).revision + 1
		}
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, codexConversationSyncStatusPayload(
			subscriptionID,
			generation,
			resolved.targetID,
			revision,
			resolved.reason,
		))
		*previous = &codexConversationSubscriptionSnapshot{
			conversation: conversation,
			fingerprint:  fingerprint,
			eventsByID:   map[string]work.CodexConversationEvent{},
			revision:     revision,
		}
		return
	}

	now := time.Now()
	// The thread's overlays (a rewind, answered requests) move apart from
	// the provider transcript; either moving reprojects it.
	rewoundVersion := s.rewound.version() + s.answeredLedger().Version()
	conversation, err := s.loadProviderConversation(reader, resolved, now)
	if took := time.Since(now); took >= slowSessionInputLog {
		// A snapshot cannot arrive before this load returns; the phone only
		// sees its own "no snapshot after 8000ms".
		log.Printf(
			"websocket: slow conversation load target=%s scope=%q events=%d took=%s",
			resolved.targetID, raw.ConversationScopeKey, len(conversation.Events), took.Round(time.Millisecond),
		)
	}
	if err != nil {
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		s.sendJSON(conn, map[string]any{
			"type":       "error",
			"request_id": subscriptionID,
			"generation": generation,
			"code":       "codex_conversation_subscribe_failed",
			"message":    err.Error(),
		})
		return
	}
	readerVersion := reader.ConversationVersion()
	changedIDs := reader.ChangedEventIDs()
	brainScoped := strings.HasPrefix(strings.TrimSpace(raw.ConversationScopeKey), "brain-thread:")

	// Content-version fast path: the reader proves nothing changed, so the
	// whole poll is O(1) — no sanitize, fingerprint, eventsByID, or delta.
	// This requires a content-versioned provider (OpenCode cache), a stable
	// conversation identity and attachment state, and no Brain overlay (which
	// changes independently of the provider source). The check runs before
	// sanitize so the unchanged-poll claim is truthful end to end.
	if readerVersion != 0 && (*previous) != nil &&
		(*previous).readerVersion == readerVersion &&
		(*previous).overlayVersion == rewoundVersion &&
		(*previous).attached == resolved.fromWatcher &&
		codexConversationIdentity((*previous).conversation) == codexConversationIdentity(conversation) &&
		!brainScoped {
		return
	}

	projectionStart := time.Now()
	var (
		providerKey         providerProjectionKey
		brainKey            brainProjectionKey
		providerFingerprint string
		reusedProvider      bool
	)
	if brainScoped {
		// The keys are read before anything is projected: a write that lands
		// while the projection runs is then newer than its key and the next tick
		// projects again, where a key read afterwards would call it seen.
		providerKey = newProviderProjectionKey(reader.ConversationStamp(), resolved.fromWatcher, rewoundVersion, conversation)
		brainKey = s.brainProjectionKey(raw.ConversationScopeKey)
		if last := *previous; last != nil && providerKey.stamp != 0 && last.providerKey == providerKey {
			if last.brainKey == brainKey {
				s.observeBrainTick(brainTickIdle)
				return
			}
			// Only the Brain side moved: the provider conversation is the one
			// the last projection sanitized.
			conversation = last.providerConversation
			providerFingerprint = last.providerFingerprint
			reusedProvider = true
		}
	}
	if !reusedProvider {
		conversation = work.SanitizeConversationProjection(conversation)
		conversation = conversationForProviderAttachment(conversation, resolved.fromWatcher)
		// A Brain-scoped provider conversation with no parse stamp can only be
		// told unchanged by its content; one with a stamp that moved is projected
		// whole, and the answer's own fingerprint below decides what is sent.
		if brainScoped && providerKey.stamp == 0 {
			providerFingerprint = codexConversationSubscriptionFingerprint(conversation)
		}
		if last := *previous; brainScoped && providerKey.stamp == 0 && last != nil &&
			last.providerFingerprint == providerFingerprint &&
			last.brainKey == brainKey && last.attached == resolved.fromWatcher {
			last.providerKey = providerKey
			s.observeBrainTick(brainTickSame)
			return
		}
	}
	providerConversation := conversation
	if !brainScoped {
		// Answered requests join the Session's thread after its projection;
		// Brain's thread takes its own in brainScopedConversation.
		conversation = s.withAnsweredRequests(resolved.targetID, conversation)
	}
	if brainScoped {
		conversation = s.brainScopedConversation(raw.ConversationScopeKey, conversation, now)
		if reusedProvider {
			s.observeBrainTick(brainTickBrain)
		} else {
			s.observeBrainTick(brainTickProvider)
		}
	}
	if took := time.Since(projectionStart); took >= slowSessionInputLog {
		// The other half of a slow first snapshot: the load above is logged
		// on its own, and this is the Brain overlay and sanitize after it.
		log.Printf(
			"websocket: slow conversation projection target=%s scope=%q events=%d took=%s",
			resolved.targetID, raw.ConversationScopeKey, len(conversation.Events), took.Round(time.Millisecond),
		)
	}

	// Never-erase guard: an unavailable empty load (transcript miss, malformed
	// partial write, mtime flap) must never replace an authoritative non-empty
	// history — neither as a snapshot (empty identity is treated as a new
	// conversation) nor as a delete-everything delta. Hold the last-known-good
	// serving state instead; the next poll either restores the same source via
	// delta or binds a genuinely new conversation via snapshot. This is a
	// monotonicity invariant, not a retry heuristic: nothing is re-sent and no
	// timer is involved.
	if (*previous) != nil &&
		(*previous).conversation.Available &&
		len((*previous).conversation.Events) > 0 &&
		!conversation.Available &&
		len(conversation.Events) == 0 {
		if identity := codexConversationIdentity(conversation); identity == "" ||
			identity == codexConversationIdentity((*previous).conversation) {
			return
		}
	}

	// Memoized fingerprints/deltas are only sound when the provider reader
	// proves an authoritative nonzero content version AND the conversation is
	// not a Brain-thread overlay (which appends independently of the provider
	// source). Codex, Claude, Cursor, Grok, and Pi return version 0 and must
	// retain the full pre-existing fingerprint and delta behavior: their
	// stable-ID in-place updates change event content without any changed-id
	// report, and a memoized fingerprint would suppress the delta forever.
	useMemoized := readerVersion != 0 && !brainScoped
	var fingerprint codexConversationFingerprintMemo
	if useMemoized {
		fingerprint = codexConversationSubscriptionFingerprintMemoized(
			conversation,
			(*previous),
			changedIDs,
		)
	} else {
		fingerprint = codexConversationFingerprintMemo{
			fingerprint: codexConversationSubscriptionFingerprint(conversation),
		}
	}
	if (*previous) != nil && (*previous).fingerprint == fingerprint.fingerprint {
		if brainScoped {
			// The answer is the one already sent, and what it was made from has
			// moved on: remember that, or every tick would find the keys behind
			// and project again until the answer next changed.
			last := *previous
			last.providerKey, last.brainKey = providerKey, brainKey
			last.providerFingerprint, last.providerConversation = providerFingerprint, providerConversation
			last.attached = resolved.fromWatcher
		}
		return
	}
	next := codexConversationSubscriptionSnapshot{
		conversation:         conversation,
		fingerprint:          fingerprint.fingerprint,
		providerFingerprint:  providerFingerprint,
		providerConversation: providerConversation,
		providerKey:          providerKey,
		brainKey:             brainKey,
		eventsByID:           codexConversationEventsByID(conversation.Events),
		eventFingerprints:    fingerprint.eventFingerprints,
		revision:             1,
		readerVersion:        readerVersion,
		overlayVersion:       rewoundVersion,
		attached:             resolved.fromWatcher,
	}
	if (*previous) != nil {
		next.revision = (*previous).revision + 1
	}

	if (*previous) == nil || !(*previous).conversation.Available ||
		codexConversationIdentity((*previous).conversation) != codexConversationIdentity(conversation) {
		if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
			return
		}
		first, older := codexConversationFirstPage(conversation, raw.FirstEvents)
		s.sendJSON(conn, codexConversationSnapshotPayload(
			subscriptionID,
			generation,
			resolved.targetID,
			next.revision,
			first,
		))
		if len(older) > 0 {
			// The rest of the thread follows as the next revision, so what
			// the client holds after it is the whole conversation, as later
			// deltas expect.
			page := codexConversationSubscriptionSnapshot{conversation: first, revision: next.revision}
			next.revision++
			s.sendJSON(conn, codexConversationDeltaPayload(
				subscriptionID,
				generation,
				resolved.targetID,
				page,
				next,
				older,
				nil,
			))
		}
		*previous = &next
		return
	}

	var upserts []work.CodexConversationEvent
	var deletes []string
	if useMemoized {
		upserts, deletes = codexConversationDeltaMemoized(
			(*previous).eventsByID,
			(*previous).eventFingerprints,
			conversation.Events,
			next.eventFingerprints,
		)
	} else {
		upserts, deletes = codexConversationDelta(
			(*previous).eventsByID,
			conversation.Events,
		)
	}
	if !s.isCurrentCodexSubscription(conn, subscriptionID, generation) {
		return
	}
	delta := codexConversationDeltaPayload(
		subscriptionID,
		generation,
		resolved.targetID,
		**previous,
		next,
		upserts,
		deletes,
	)
	s.sendJSON(conn, delta)
	*previous = &next
}

// brainProjectionKey names the Brain side of a Brain-scoped conversation: what
// the overlay reads besides the provider conversation. Each part is a counter
// of the thing that owns the data, so comparing keys reads none of it. The
// Store's moves with every write that can change the timeline, the Work and
// Event rows, the chat state and the Host binding; the calendar's with every
// committed change to a scheduled result; the computers' with the handoffs of
// the thread and the peers that name them.
type brainProjectionKey struct {
	store     uint64
	calendar  uint64
	computers uint64
	answered  uint64
}

func (s *Server) brainProjectionKey(scopeKey string) brainProjectionKey {
	const prefix = "brain-thread:"
	if s.brain == nil || !strings.HasPrefix(strings.TrimSpace(scopeKey), prefix) {
		return brainProjectionKey{}
	}
	threadID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scopeKey), prefix))
	key := brainProjectionKey{store: s.brain.ConversationProjectionVersion(), answered: s.answeredLedger().Version()}
	if s.calendar != nil {
		key.calendar = s.calendar.Generation()
	}
	if s.computers != nil {
		key.computers = s.computers.ProjectionToken(threadID)
	}
	return key
}

// providerProjectionKey names a loaded provider conversation by what is cheap
// to read of it: the parse its events came from (zero when the load was not a
// retained parse, which proves nothing), the facts the load adds around the
// events, the overlay that was applied to them and whether the Session is
// attached. Equal keys with a nonzero stamp are the same conversation. The
// file's modification time is not part of it: the Brain overlay replaces
// Updated, and a transcript that is still being written moves its time faster
// than a large one parses.
type providerProjectionKey struct {
	stamp     uint64
	attached  bool
	rewound   uint64
	available bool
	path      string
	sessionID string
	cwd       string
	source    string
	reason    string
	events    int
	activity  providerActivityKey
}

type providerActivityKey struct {
	present   bool
	id        string
	status    work.ProviderActivityStatus
	startedAt string
	settledAt string
}

func newProviderProjectionKey(stamp uint64, attached bool, rewound uint64, conversation work.CodexConversation) providerProjectionKey {
	key := providerProjectionKey{
		stamp: stamp, attached: attached, rewound: rewound,
		available: conversation.Available, path: conversation.Path, sessionID: conversation.SessionID,
		cwd: conversation.CWD, source: conversation.Source, reason: conversation.Reason,
		events: len(conversation.Events),
	}
	if activity := conversation.Activity; activity != nil {
		key.activity = providerActivityKey{
			present: true, id: activity.ID, status: activity.Status,
			startedAt: activity.StartedAt, settledAt: activity.SettledAt,
		}
	}
	return key
}

func conversationForProviderAttachment(conversation work.CodexConversation, attached bool) work.CodexConversation {
	if !attached {
		// Detached transcripts remain valid history but cannot claim a current
		// executor Activity.
		conversation.Activity = nil
	}
	return conversation
}

func codexConversationSyncStatusPayload(
	subscriptionID string,
	generation string,
	targetID string,
	revision int64,
	reason string,
) map[string]any {
	return map[string]any{
		"type":            "codex_conversation_sync_status",
		"request_id":      subscriptionID,
		"generation":      generation,
		"worker_id":       targetID,
		"conversation_id": "",
		"revision":        revision,
		"state":           "syncing",
		"reason":          reason,
	}
}

// maxConversationFirstEvents bounds first_events; a thread is 240 events.
const maxConversationFirstEvents = 240

// codexConversationFirstPage splits a thread's first snapshot for a client that
// asked for its newest firstEvents events first: the conversation with only
// those, and the older events, which follow as a delta. A phone behind a slow
// link paints the end of the thread from some 20 KB (deflated) instead of
// waiting for all of it (250 KB for a Brain thread): the rest arrives while
// the person reads. firstEvents <= 0, or a thread no longer than that, is one
// snapshot as before.
func codexConversationFirstPage(conversation work.CodexConversation, firstEvents int) (work.CodexConversation, []work.CodexConversationEvent) {
	if firstEvents <= 0 || firstEvents > maxConversationFirstEvents || len(conversation.Events) <= firstEvents {
		return conversation, nil
	}
	split := len(conversation.Events) - firstEvents
	first := conversation
	first.Events = conversation.Events[split:]
	return first, conversation.Events[:split]
}

func codexConversationSnapshotPayload(
	subscriptionID string,
	generation string,
	targetID string,
	revision int64,
	conversation work.CodexConversation,
) map[string]any {
	return map[string]any{
		"type":            "codex_conversation_snapshot",
		"request_id":      subscriptionID,
		"generation":      generation,
		"worker_id":       targetID,
		"conversation_id": codexConversationIdentity(conversation),
		"revision":        revision,
		"conversation":    conversation,
	}
}

func codexConversationDeltaPayload(
	subscriptionID string,
	generation string,
	targetID string,
	previous codexConversationSubscriptionSnapshot,
	next codexConversationSubscriptionSnapshot,
	upserts []work.CodexConversationEvent,
	deletes []string,
) map[string]any {
	conversation := next.conversation
	delta := map[string]any{
		"type":            "codex_conversation_delta",
		"request_id":      subscriptionID,
		"generation":      generation,
		"worker_id":       targetID,
		"conversation_id": codexConversationIdentity(conversation),
		"base_revision":   previous.revision,
		"revision":        next.revision,
		"available":       conversation.Available,
		"reason":          conversation.Reason,
		"source":          conversation.Source,
		"path":            conversation.Path,
		"session_id":      conversation.SessionID,
		"cwd":             conversation.CWD,
		"updated_at":      conversation.Updated,
		"upserts":         upserts,
		"deletes":         deletes,
	}
	if !providerActivitiesEqual(previous.conversation.Activity, conversation.Activity) {
		// The key is intentionally present with nil when a prior Activity clears.
		delta["activity"] = conversation.Activity
	}
	return delta
}

func (s *Server) isCurrentCodexSubscription(conn *websocket.Conn, subscriptionID, generation string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscriptions := s.codexSubs[conn]
	current, ok := subscriptions[subscriptionID]
	return ok && current.generation == generation
}

func (s *Server) brainScopedConversation(scopeKey string, conversation work.CodexConversation, now time.Time) work.CodexConversation {
	const prefix = "brain-thread:"
	if s.brain == nil || !strings.HasPrefix(strings.TrimSpace(scopeKey), prefix) {
		return conversation
	}
	threadID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scopeKey), prefix))
	if threadID == "" {
		return conversation
	}
	known, err := s.brain.HasChatThread(threadID)
	if err != nil {
		conversation.Available = false
		conversation.Reason = "brain_threads_unavailable"
		return conversation
	}
	if !known {
		conversation.Available = false
		conversation.Reason = "brain_thread_unknown"
		conversation.Events = []work.CodexConversationEvent{}
		conversation.Activity = nil
		return conversation
	}
	currentThreadID, currentThreadErr := s.brain.ChatThreadID()
	if currentThreadErr != nil {
		conversation.Available = false
		conversation.Reason = "brain_threads_unavailable"
		return conversation
	}
	isCurrentThread := currentThreadErr == nil && strings.TrimSpace(currentThreadID) != "" &&
		threadID == strings.TrimSpace(currentThreadID)

	liveActivity := conversation.Activity
	providerEvents := []work.CodexConversationEvent(nil)
	if isCurrentThread {
		// Durable finals come from the Host Executor Session transcript
		// identity. Live cwd matching is never the recurring authority.
		bound, boundErr := s.brain.HostBoundProviderConversation()
		if boundErr == nil && bound.Available {
			conversation = work.PreferHostBoundConversation(conversation, bound)
			_ = s.brain.MaterializeProviderConversation(threadID, bound)
		}
		_ = s.brain.MaterializeProviderConversation(threadID, conversation)
		providerEvents = conversation.Events
	} else {
		liveActivity = nil
	}

	timelineItems, timelineErr := s.brain.ThreadTimelineView(threadID, 0)
	if timelineErr != nil {
		conversation.Available = false
		conversation.Reason = "brain_timeline_unavailable"
		return conversation
	}
	eventsByID := make(map[string]int, len(timelineItems)+len(providerEvents)+8)
	uniqueEvents := make([]work.CodexConversationEvent, 0, len(timelineItems)+len(providerEvents)+8)
	appendUnique := func(event work.CodexConversationEvent) {
		id := strings.TrimSpace(event.ID)
		if id != "" {
			if index, ok := eventsByID[id]; ok {
				uniqueEvents[index] = event
				return
			}
			eventsByID[id] = len(uniqueEvents)
		}
		uniqueEvents = append(uniqueEvents, event)
	}
	for _, event := range timelineItemsToConversationEventsForServer(timelineItems) {
		appendUnique(event)
	}
	// Current-host provider events overlay the durable timeline so tools,
	// streaming partials, and live Activity remain visible. An empty provider
	// snapshot leaves the durable timeline intact. Durable calendar/work_result
	// rows win over provider duplicates with the same ID. User rows come only
	// from the durable timeline: materialization above already decided which
	// provider inputs echo a Brain admission and which are independent Terminal
	// inputs, so the overlay never makes a second, live-only decision.
	for _, event := range providerEvents {
		if strings.TrimSpace(event.Kind) == "user_message" {
			continue
		}
		id := strings.TrimSpace(event.ID)
		if id != "" {
			if index, ok := eventsByID[id]; ok {
				existing := uniqueEvents[index]
				if existing.Source == "calendar_result" || existing.Source == "work_result" {
					continue
				}
				uniqueEvents[index] = event
				continue
			}
			eventsByID[id] = len(uniqueEvents)
		}
		uniqueEvents = append(uniqueEvents, event)
	}
	results := []calendar.ScheduledResult{}
	if s.calendar != nil {
		results = s.calendar.ScheduledResults(threadID, 0)
	}
	for _, result := range results {
		appendUnique(calendarResultConversationEvent(result))
	}
	s.addComputerMessages(threadID, uniqueEvents, appendUnique)
	for _, event := range s.brainThreadAnsweredRequests(threadID) {
		appendUnique(event)
	}
	uniqueEvents = withoutWatchEvents(uniqueEvents)
	if lifecycleErr := s.brain.AnnotateWorkResultEvents(uniqueEvents); lifecycleErr != nil {
		conversation.Available = false
		conversation.Reason = "brain_work_lifecycle_unavailable"
		return conversation
	}

	sort.SliceStable(uniqueEvents, func(left, right int) bool {
		return brainConversationEventLess(uniqueEvents[left], uniqueEvents[right])
	})
	conversation.Events = uniqueEvents
	conversation.Activity = liveActivity
	conversation.Available = true
	conversation.Reason = ""
	conversation.Source = "brain_chat"
	conversation.SessionID = prefix + threadID
	conversation.Updated = &now
	return conversation
}

// timelineItemsToConversationEventsForServer adapts brain timeline rows without
// importing unexported helpers across packages.
func timelineItemsToConversationEventsForServer(items []brain.TimelineItem) []work.CodexConversationEvent {
	return brain.TimelineItemsToConversationEvents(items)
}

func calendarResultConversationEvent(result calendar.ScheduledResult) work.CodexConversationEvent {
	title := strings.TrimSpace(result.Title)
	status := strings.TrimSpace(string(result.Status))
	if title != "" && status != "" {
		title += " " + status
	}
	body := strings.TrimSpace(result.Body)
	if heading := strings.TrimSpace(result.Title + " " + status); heading != "" {
		body = strings.TrimSpace(strings.TrimPrefix(body, "**"+heading+"**"))
	}
	return work.CodexConversationEvent{
		ID:        result.ID,
		Timestamp: result.CreatedAt.Format(time.RFC3339Nano),
		Kind:      "status",
		Title:     title,
		Body:      body,
		Status:    status,
		Source:    "calendar_result",
	}
}

func brainConversationEventLess(left, right work.CodexConversationEvent) bool {
	leftTime, leftOK := parseConversationEventTime(left.Timestamp)
	rightTime, rightOK := parseConversationEventTime(right.Timestamp)
	if leftOK != rightOK {
		// Structured events normally carry timestamps. Legacy undated events sort
		// first so they cannot pin a later scheduled result at the footer.
		return !leftOK
	}
	if leftOK && !leftTime.Equal(rightTime) {
		return leftTime.Before(rightTime)
	}
	if left.Seq != right.Seq {
		return left.Seq < right.Seq
	}
	return left.ID < right.ID
}

func parseConversationEventTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed, err == nil
}

func codexConversationIdentity(conversation work.CodexConversation) string {
	if strings.TrimSpace(conversation.SessionID) != "" {
		return conversation.SessionID
	}
	if strings.TrimSpace(conversation.Path) != "" {
		return conversation.Path
	}
	return conversation.CWD
}

type codexConversationFingerprintMemo struct {
	fingerprint       string
	eventFingerprints map[string]string
}

// codexConversationSubscriptionFingerprintMemoized computes the conversation
// fingerprint while memoizing per-event fingerprints. Only events listed in
// changedIDs (or missing from the previous memo) are re-hashed; the rest of
// the history is never re-read for hashing. O(events) small-string work per
// poll instead of O(total conversation text).
func codexConversationSubscriptionFingerprintMemoized(
	conversation work.CodexConversation,
	previous *codexConversationSubscriptionSnapshot,
	changedIDs []string,
) codexConversationFingerprintMemo {
	hash := fnv.New64a()
	writeFingerprintString(hash, codexConversationIdentity(conversation))
	writeFingerprintString(hash, conversation.Path)
	writeFingerprintString(hash, conversation.SessionID)
	writeFingerprintBool(hash, conversation.Available)
	writeFingerprintString(hash, conversation.Reason)
	writeFingerprintString(hash, conversation.Source)
	writeFingerprintString(hash, conversation.CWD)
	writeProviderActivityFingerprint(hash, conversation.Activity)

	eventFingerprints := map[string]string{}
	if previous != nil {
		for id, fingerprint := range previous.eventFingerprints {
			eventFingerprints[id] = fingerprint
		}
	}
	changed := make(map[string]struct{}, len(changedIDs)+1)
	for _, id := range changedIDs {
		changed[id] = struct{}{}
	}
	writeFingerprintInt(hash, len(conversation.Events))
	for _, event := range conversation.Events {
		id := strings.TrimSpace(event.ID)
		if _, ok := eventFingerprints[id]; !ok || event.Source == answeredConversationKind {
			// New or unknown event: its fingerprint must be computed. An
			// answered request is an overlay the reader does not version.
			changed[id] = struct{}{}
		}
		if _, needs := changed[id]; needs {
			eventFingerprints[id] = codexConversationEventFingerprint(event)
		}
		writeFingerprintString(hash, id)
		writeFingerprintString(hash, eventFingerprints[id])
	}
	return codexConversationFingerprintMemo{
		fingerprint:       fmt.Sprintf("%016x", hash.Sum64()),
		eventFingerprints: eventFingerprints,
	}
}

// codexConversationDeltaMemoized computes the upsert/delete sets against
// memoized per-event fingerprints so unchanged events are compared by a short
// hash string instead of re-hashing their full text.
func codexConversationDeltaMemoized(
	previous map[string]work.CodexConversationEvent,
	previousFingerprints map[string]string,
	next []work.CodexConversationEvent,
	nextFingerprints map[string]string,
) ([]work.CodexConversationEvent, []string) {
	var upserts []work.CodexConversationEvent
	nextIDs := make(map[string]struct{}, len(next))
	for _, event := range next {
		id := strings.TrimSpace(event.ID)
		if id == "" {
			upserts = append(upserts, event)
			continue
		}
		nextIDs[id] = struct{}{}
		if _, ok := previous[id]; !ok ||
			previousFingerprints[id] != nextFingerprints[id] {
			upserts = append(upserts, event)
		}
	}
	var deletes []string
	for id := range previous {
		if _, ok := nextIDs[id]; !ok {
			deletes = append(deletes, id)
		}
	}
	return upserts, deletes
}

func codexConversationSubscriptionFingerprint(conversation work.CodexConversation) string {
	hash := fnv.New64a()
	writeFingerprintString(hash, codexConversationIdentity(conversation))
	writeFingerprintString(hash, conversation.Path)
	writeFingerprintString(hash, conversation.SessionID)
	writeFingerprintBool(hash, conversation.Available)
	writeFingerprintString(hash, conversation.Reason)
	writeFingerprintString(hash, conversation.Source)
	writeFingerprintString(hash, conversation.CWD)
	writeProviderActivityFingerprint(hash, conversation.Activity)
	writeCodexConversationEventsFingerprint(hash, conversation.Events)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func writeProviderActivityFingerprint(w io.Writer, activity *work.ProviderActivity) {
	if activity == nil {
		writeFingerprintString(w, "")
		return
	}
	writeFingerprintString(w, activity.ID)
	writeFingerprintString(w, string(activity.Status))
	writeFingerprintString(w, activity.StartedAt)
	writeFingerprintString(w, activity.SettledAt)
}

func providerActivitiesEqual(left, right *work.ProviderActivity) bool {
	return left == right || left != nil && right != nil &&
		left.ID == right.ID &&
		left.Status == right.Status &&
		left.StartedAt == right.StartedAt &&
		left.SettledAt == right.SettledAt
}

func codexConversationEventsFingerprint(events []work.CodexConversationEvent) string {
	hash := fnv.New64a()
	writeCodexConversationEventsFingerprint(hash, events)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func codexConversationEventsByID(events []work.CodexConversationEvent) map[string]work.CodexConversationEvent {
	byID := make(map[string]work.CodexConversationEvent, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" {
			continue
		}
		byID[event.ID] = event
	}
	return byID
}

func codexConversationDelta(
	previous map[string]work.CodexConversationEvent,
	next []work.CodexConversationEvent,
) ([]work.CodexConversationEvent, []string) {
	var upserts []work.CodexConversationEvent
	nextIDs := make(map[string]struct{}, len(next))
	for _, event := range next {
		id := strings.TrimSpace(event.ID)
		if id == "" {
			upserts = append(upserts, event)
			continue
		}
		nextIDs[id] = struct{}{}
		if previousEvent, ok := previous[id]; !ok || codexConversationEventFingerprint(previousEvent) != codexConversationEventFingerprint(event) {
			upserts = append(upserts, event)
		}
	}
	var deletes []string
	for id := range previous {
		if _, ok := nextIDs[id]; !ok {
			deletes = append(deletes, id)
		}
	}
	return upserts, deletes
}

func codexConversationEventFingerprint(event work.CodexConversationEvent) string {
	hash := fnv.New64a()
	writeCodexConversationEventFingerprint(hash, event)
	return fmt.Sprintf("%016x", hash.Sum64())
}

func writeCodexConversationEventsFingerprint(w io.Writer, events []work.CodexConversationEvent) {
	writeFingerprintInt(w, len(events))
	for _, event := range events {
		writeCodexConversationEventFingerprint(w, event)
	}
}

func writeCodexConversationEventFingerprint(w io.Writer, event work.CodexConversationEvent) {
	writeFingerprintString(w, event.ID)
	writeFingerprintInt(w, event.Seq)
	writeFingerprintString(w, event.Timestamp)
	writeFingerprintString(w, event.Kind)
	writeFingerprintString(w, event.Role)
	writeFingerprintString(w, event.Title)
	writeFingerprintString(w, event.Body)
	writeFingerprintInt(w, len(event.Attachments))
	for _, file := range event.Attachments {
		writeFingerprintString(w, file.Name)
		writeFingerprintString(w, file.Path)
		writeFingerprintString(w, file.ContentType)
	}
	writeFingerprintString(w, event.Command)
	writeFingerprintString(w, event.ToolName)
	writeFingerprintString(w, event.Input)
	writeFingerprintString(w, event.Output)
	writeFingerprintString(w, event.CallID)
	if event.ExitCode == nil {
		writeFingerprintString(w, "")
	} else {
		writeFingerprintInt(w, *event.ExitCode)
	}
	writeFingerprintString(w, event.Status)
	writeFingerprintBool(w, event.Partial)
	writeFingerprintBool(w, event.Transient)
	writeFingerprintStrings(w, event.Files)
	writeFingerprintFileChanges(w, event.FileChanges)
	writeFingerprintString(w, event.Explanation)
	writeFingerprintPlan(w, event.Plan)
	writeFingerprintChoice(w, event.Choice)
	writeFingerprintString(w, event.Source)
	writeFingerprintString(w, event.Origin)
	writeFingerprintSubagent(w, event.Subagent)
	// The Work card and the other typed rows. A change in any of these alone
	// (a review settling, a Session closing, a card read) is a change to send.
	writeFingerprintString(w, event.WorkID)
	writeFingerprintString(w, event.WorkSession)
	writeFingerprintString(w, event.SessionName)
	writeFingerprintBool(w, event.Unread)
	writeFingerprintString(w, event.WorkReviewState)
	writeFingerprintString(w, event.WorkSessionState)
	writeFingerprintBool(w, event.WorkResultCurrent)
	writeFingerprintString(w, event.WorkPhase)
	writeFingerprintString(w, event.WorkAttention)
	writeFingerprintString(w, event.WorkEventKind)
	writeFingerprintString(w, event.WorkDetailsJSON)
	writeFingerprintString(w, event.WorkWaitFor)
	for _, action := range event.WorkActions {
		writeFingerprintString(w, action.Kind)
		writeFingerprintString(w, action.URL)
		writeFingerprintString(w, action.SessionID)
		writeFingerprintString(w, action.Cwd)
		writeFingerprintString(w, action.Path)
	}
	writeFingerprintInt(w, len(event.WorkActions))
	if request := event.Request; request == nil {
		writeFingerprintString(w, "")
	} else {
		encoded, _ := json.Marshal(request)
		writeFingerprintString(w, string(encoded))
	}
	writeFingerprintString(w, event.TaskID)
	for _, count := range []*int{event.TaskTokens, event.TaskToolUses, event.TaskDurationMS} {
		if count == nil {
			writeFingerprintString(w, "")
		} else {
			writeFingerprintInt(w, *count)
		}
	}
	if computer := event.Computer; computer == nil {
		writeFingerprintString(w, "")
	} else {
		for _, value := range []string{
			computer.Direction, computer.Computer, computer.DaemonID, computer.MessageID,
			computer.Kind, computer.ReplyTo, computer.UserSaid, computer.State,
		} {
			writeFingerprintString(w, value)
		}
	}
}

func writeFingerprintSubagent(w io.Writer, subagent *work.CodexConversationSubagent) {
	if subagent == nil {
		writeFingerprintString(w, "")
		return
	}
	writeFingerprintString(w, subagent.AgentID)
	writeFingerprintString(w, subagent.Type)
	writeFingerprintString(w, subagent.Description)
	writeFingerprintBool(w, subagent.Finished)
	writeFingerprintString(w, subagent.StartedAt)
	writeFingerprintString(w, subagent.EndedAt)
	writeCodexConversationEventsFingerprint(w, subagent.Steps)
}

func writeFingerprintChoice(w io.Writer, choice *work.ConversationChoice) {
	if choice == nil {
		writeFingerprintString(w, "")
		return
	}
	encoded, _ := json.Marshal(choice)
	writeFingerprintString(w, string(encoded))
}

func writeFingerprintFileChanges(w io.Writer, changes []work.CodexConversationFileChange) {
	writeFingerprintInt(w, len(changes))
	for _, change := range changes {
		writeFingerprintString(w, change.Path)
		writeFingerprintString(w, change.MovePath)
		writeFingerprintString(w, change.Operation)
		if change.Additions == nil {
			writeFingerprintString(w, "")
		} else {
			writeFingerprintInt(w, *change.Additions)
		}
		if change.Deletions == nil {
			writeFingerprintString(w, "")
		} else {
			writeFingerprintInt(w, *change.Deletions)
		}
	}
}

func writeFingerprintString(w io.Writer, value string) {
	var buffer [24]byte
	_, _ = w.Write(append(strconv.AppendInt(buffer[:0], int64(len(value)), 10), ':'))
	_, _ = io.WriteString(w, value)
	_, _ = io.WriteString(w, "\x00")
}

func writeFingerprintBool(w io.Writer, value bool) {
	if value {
		writeFingerprintString(w, "true")
		return
	}
	writeFingerprintString(w, "false")
}

func writeFingerprintInt(w io.Writer, value int) {
	_, _ = fmt.Fprintf(w, "%d\x00", value)
}

func writeFingerprintStrings(w io.Writer, values []string) {
	writeFingerprintInt(w, len(values))
	for _, value := range values {
		writeFingerprintString(w, value)
	}
}

func writeFingerprintPlan(w io.Writer, steps []work.CodexPlanStep) {
	writeFingerprintInt(w, len(steps))
	for _, step := range steps {
		writeFingerprintString(w, step.Step)
		writeFingerprintString(w, step.Status)
	}
}

func clientStartedAt(raw json.RawMessage) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil && numeric > 0 {
		seconds := int64(numeric)
		nanos := int64((numeric - float64(seconds)) * 1_000_000_000)
		if numeric > 10_000_000_000 {
			seconds = int64(numeric / 1000)
			nanos = int64(numeric-float64(seconds*1000)) * int64(time.Millisecond)
		}
		return time.Unix(seconds, nanos).UTC()
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return time.Time{}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed.UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func (s *Server) handleCodexSlashCommands(conn *websocket.Conn, raw clientMessage) {
	snapshot := bundledCodexSlashCommands()
	s.sendJSON(conn, map[string]any{
		"type":       "codex_slash_commands",
		"request_id": raw.RequestID,
		"source":     snapshot.Source,
		"commands":   snapshot.Commands,
	})
}

func (s *Server) handleCodexSkills(conn *websocket.Conn, raw clientMessage) {
	s.sendJSON(conn, map[string]any{
		"type":       "codex_skills",
		"request_id": raw.RequestID,
		"cwd":        raw.Cwd,
		"skills":     discoverCodexSkills(raw.Cwd),
	})
}

func (s *Server) handleCodexAsset(conn *websocket.Conn, raw clientMessage) {
	path := strings.TrimSpace(raw.Path)
	if path == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "missing asset path")
		return
	}
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(raw.Cwd) == "" {
			s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "relative asset path requires cwd")
			return
		}
		path = filepath.Join(raw.Cwd, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", err.Error())
		return
	}
	if info.IsDir() {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset path is a directory")
		return
	}
	if info.Size() > maxCodexAssetBytes {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset is too large to preview")
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", err.Error())
		return
	}
	contentType := codexAssetContentType(path, data)
	if !strings.HasPrefix(contentType, "image/") {
		s.sendErrorWithRequestID(conn, raw.RequestID, "codex_asset_failed", "asset is not a supported image")
		return
	}
	if raw.MaxEdge > 0 {
		data, contentType = attachment.FitImage(data, min(max(raw.MaxEdge, 64), 4096))
	}

	s.sendJSON(conn, map[string]any{
		"type":         "codex_asset",
		"request_id":   raw.RequestID,
		"path":         path,
		"content_type": contentType,
		"data_url":     "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data),
	})
}

func codexAssetContentType(path string, data []byte) string {
	contentType := http.DetectContentType(data)
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return contentType
}

// workItemsForClients is the Work read model clients load: calendar Work they
// can still reach, each with its full body.
func (s *Server) workItemsForClients() []*work.Item {
	linked := map[string]bool{}
	if s.calendar != nil {
		for _, item := range s.calendar.List() {
			if id := strings.TrimSpace(item.LinkedWorkID); id != "" {
				linked[id] = true
			}
		}
	}
	live := map[string]bool{}
	for _, session := range s.currentVisibleWorkerSessions() {
		live[session.ID] = true
	}
	return work.InUseCalendarWorkItems(s.work.List(), linked, live)
}

func (s *Server) handleListWorkItems(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "list_work_items_failed", "work store not configured")
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "work_items_snapshot",
		"request_id": raw.RequestID,
		"work_items": s.workItemsForClients(),
	})
}

func (s *Server) sendBrainSnapshot(conn *websocket.Conn, requestID string) {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.Snapshot()
	if err != nil {
		s.sendErrorWithRequestID(conn, requestID, "brain_snapshot_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, requestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": requestID,
		"brain":      payload,
	})
}

func (s *Server) handleBrainSetExecutor(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	executorID := strings.TrimSpace(raw.ExecutorID)
	if executorID == "" {
		executorID = strings.TrimSpace(raw.AdapterID)
	}
	// The choice is kept and answered at once; bringing the new Host up takes
	// as long as the agent takes to open, so the Host continuity owner does it
	// and clients follow the snapshots (and a host_problem if it fails).
	snapshot, err := s.brain.SelectHostExecutor(executorID)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_set_executor_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      payload,
	})
	select {
	case s.brainHostRecovery <- struct{}{}:
	default:
	}
	s.broadcastBrainHostCapabilityRefresh()
}

func (s *Server) handleBrainContext(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	context, err := s.brain.Context()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_context_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_context",
		"request_id": raw.RequestID,
		"context":    context,
	})
}

func (s *Server) handleBrainGC(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	report, err := s.brain.Housekeeping()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_gc_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":         "brain_gc",
		"request_id":   raw.RequestID,
		"housekeeping": report,
	})
}

func (s *Server) handleBrainWorkRead(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	if err := s.brain.MarkWorkRead(strings.TrimSpace(raw.ID)); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_work_read_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_work_read",
		"request_id": raw.RequestID,
		"work_id":    strings.TrimSpace(raw.ID),
	})
}

// handleBrainWorkDetail answers one Work's detail page, current or closed.
func (s *Server) handleBrainWorkDetail(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	detail, err := s.brain.WorkDetail(strings.TrimSpace(raw.ID))
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_work_detail_failed", brainWorkActionMessage(err))
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_work_detail",
		"request_id": raw.RequestID,
		"work":       detail,
	})
}

// handleBrainWorkAction applies one user action from a Work slip. The reply
// carries the Brain admission outcome so the slip never claims Brain heard an
// answer whose delivery is unknown.
func (s *Server) handleBrainWorkAction(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	// A reply names its files by path; only files the upload store holds may
	// reach Brain, which can read whatever the path says.
	files, confineErr := s.uploadStore.ConfineAll(raw.Attachments)
	if confineErr != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "attachment_not_uploaded", confineErr.Error())
		return
	}
	asked, _ := s.workQuestionBefore(raw.ID)
	result, err := s.brain.ActOnWork(brain.WorkUserActionRequest{
		WorkID: strings.TrimSpace(raw.ID), Kind: brain.WorkUserActionKind(strings.TrimSpace(raw.Action)),
		Text: raw.Text, SnoozeUntil: raw.SnoozeUntil, SnoozeFor: time.Duration(raw.SnoozeSeconds) * time.Second,
		RequestID: raw.RequestID, Attachments: files,
	})
	if err != nil && !errors.Is(err, brain.ErrWorkCleanupPending) {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_work_action_failed", brainWorkActionMessage(err))
		return
	}
	s.recordWorkReply(asked, result, s.answeredByConn(conn))
	s.sendJSON(conn, map[string]any{
		"type":       "brain_work_action",
		"request_id": raw.RequestID,
		"work_id":    result.Work.ID,
		"status":     result.Work.Status,
		"event_id":   result.Event.ID,
		"admission":  result.Admission,
	})
	s.broadcastBrainSnapshot()
}

func brainWorkActionMessage(err error) string {
	switch {
	case errors.Is(err, brain.ErrWorkCloseConflict):
		return "Brain is reading this Work right now. Try again in a moment."
	case errors.Is(err, brain.ErrWorkRevisionConflict):
		return "This Work just changed. Look again and retry."
	case errors.Is(err, brain.ErrWorkNotFound):
		return "This Work no longer exists."
	}
	return err.Error()
}

func (s *Server) handleBrainChatNew(conn *websocket.Conn, raw clientMessage) {
	if s.brain == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_unavailable", "Brain is not configured")
		return
	}
	snapshot, err := s.brain.NewChat()
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_chat_new_failed", err.Error())
		return
	}
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "brain_snapshot_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "brain_snapshot",
		"request_id": raw.RequestID,
		"brain":      payload,
	})
}

func (s *Server) brainSnapshotWire(snapshot brain.Snapshot) (any, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if _, ok := payload["host_adapter"]; !ok && snapshot.HostExecutor != nil {
		payload["host_adapter"] = snapshot.HostExecutor
	}
	if _, ok := payload["adapters"]; !ok && snapshot.Executors != nil {
		payload["adapters"] = snapshot.Executors
	}
	s.enrichBrainSnapshotHostWorkerCapabilities(payload, snapshot)
	return payload, nil
}

// enrichBrainSnapshotHostAgentCapabilities attaches the same flat capabilities
// object as worker_session wire onto brain_snapshot.host_worker. Derivation uses
// the hidden watcher Agent + Model Profiles route table only; missing agent or
// profile owner fail closed to false. The host stays Hidden and is never added
// to worker_session_list.
func (s *Server) enrichBrainSnapshotHostWorkerCapabilities(payload map[string]any, snapshot brain.Snapshot) {
	if payload == nil {
		return
	}
	hostRaw, ok := payload["host_worker"]
	if !ok || hostRaw == nil {
		return
	}
	hostMap, ok := hostRaw.(map[string]any)
	if !ok {
		return
	}
	sessionID := ""
	if snapshot.HostWorker != nil {
		sessionID = strings.TrimSpace(snapshot.HostWorker.ID)
	}
	if sessionID == "" {
		if id, _ := hostMap["id"].(string); strings.TrimSpace(id) != "" {
			sessionID = strings.TrimSpace(id)
		}
	}
	hostMap["capabilities"] = s.hostWorkerWireCapabilities(sessionID)
	if host := s.lookupWorker(sessionID); host != nil {
		// The blocking prompt the app answers through answer_prompt; only a
		// blocked host has one.
		if host.State == classifier.StateBlocked && host.Prompt != nil {
			hostMap["prompt"] = host.Prompt
		}
		executorID, provider := s.workerExecutor(host)
		if executorID != "" {
			hostMap["executor_id"] = executorID
		}
		if provider != "" {
			hostMap["provider"] = provider
		}
	}
	payload["host_worker"] = hostMap
}

func visibleWorkerSessions(workers []*classifier.Worker) []*classifier.Worker {
	if len(workers) == 0 {
		return nil
	}
	out := make([]*classifier.Worker, 0, len(workers))
	for _, worker := range workers {
		if worker == nil || worker.Hidden {
			continue
		}
		out = append(out, worker)
	}
	return out
}

func (s *Server) currentVisibleWorkerSessions() []*classifier.Worker {
	// The owned-VM fixture serves the full handler without a watcher. A chat
	// WebSocket must get an empty session list, not a system-process panic
	// that also breaks the shared native tunnel retries.
	if s == nil || s.watcher == nil {
		return nil
	}
	return visibleWorkerSessions(s.watcher.Workers())
}

func (s *Server) handleWriteWorkItem(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_work_item_failed", "work store not configured")
		return
	}

	now := time.Now().UTC()
	project := strings.TrimSpace(raw.Project)
	if project == "" {
		project = "inbox"
	}

	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = ulid.Make().String()
	}

	path := strings.TrimSpace(raw.Path)
	if path == "" {
		path = filepath.Join(s.work.Root, project, buildWorkFilename(now, raw.Body, id))
	}

	frontmatter := work.Frontmatter{
		ID:      id,
		Created: now,
	}
	if existing, ok := s.work.GetByID(id); ok {
		frontmatter = existing.Frontmatter
	}
	applyFrontmatterOverrides(&frontmatter, raw.Frontmatter)

	var baseMtime time.Time
	if strings.TrimSpace(raw.BaseMtime) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw.BaseMtime)
		if err == nil {
			baseMtime = parsed
		}
	}

	written, err := s.work.Write(&work.Item{
		ID:          frontmatter.ID,
		Path:        path,
		Project:     project,
		Body:        raw.Body,
		Frontmatter: frontmatter,
	}, baseMtime)
	if err != nil {
		if errors.Is(err, work.ErrConflict) {
			current, _ := s.work.GetByID(id)
			s.sendJSON(conn, map[string]any{
				"type":       "error",
				"request_id": raw.RequestID,
				"code":       "conflict",
				"message":    "work item changed on disk",
				"current":    current,
			})
			return
		}
		s.sendErrorWithRequestID(conn, raw.RequestID, "write_work_item_failed", err.Error())
		return
	}

	s.sendJSON(conn, map[string]any{
		"type":       "work_item_written",
		"request_id": raw.RequestID,
		"work_item":  written,
	})
}

func (s *Server) handleDeleteWorkItem(conn *websocket.Conn, raw clientMessage) {
	if s.work == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_work_item_failed", "work store not configured")
		return
	}
	if err := s.work.Delete(strings.TrimSpace(raw.ID)); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "delete_work_item_failed", err.Error())
		return
	}
	s.sendJSON(conn, map[string]any{
		"type":       "work_item_deleted_ack",
		"request_id": raw.RequestID,
		"id":         strings.TrimSpace(raw.ID),
	})
}

func (s *Server) sendJSON(conn *websocket.Conn, v any) {
	s.sendJSONQueued(conn, v, false)
}

// sendJSONWhenRoom is for a stream owned by this one client (terminal output):
// it waits for the client's queue to drain instead of dropping the client, so
// the stream slows to the client's pace.
func (s *Server) sendJSONWhenRoom(conn *websocket.Conn, v any) {
	s.sendJSONQueued(conn, v, true)
}

func (s *Server) sendJSONQueued(conn *websocket.Conn, v any, waitForRoom bool) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("sendJSON marshal error: %v", err)
		return
	}
	if err := s.queueMessage(conn, websocket.TextMessage, data, waitForRoom); err != nil {
		// A closed outbox is the normal end of a client; failLocked already
		// logged any abnormal drop with its remote address.
		if errors.Is(err, errOutboxClosed) {
			debuglog.Printf("websocket: send to closed client remote=%s: %v", conn.RemoteAddr(), err)
			return
		}
		log.Printf("websocket: send failed remote=%s: %v", conn.RemoteAddr(), err)
	}
}

func (s *Server) sendErrorWithRequestID(conn *websocket.Conn, requestID, code, message string) {
	s.sendJSON(conn, map[string]any{
		"type":       "error",
		"request_id": requestID,
		"code":       code,
		"message":    message,
	})
}

func (s *Server) broadcastEvents(ctx context.Context) {
	calendarSub := s.calendarSub
	brainWorkSub := s.brainWorkSub
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.watcher.Events():
			s.handleWatcherEvent(ev)
		case ie, ok := <-s.workSub:
			if !ok {
				s.workSub = nil
				continue
			}
			s.handleWorkEvent(ie)
		case event, ok := <-calendarSub:
			if !ok {
				calendarSub = nil
				continue
			}
			s.handleCalendarEvent(event)
		case _, ok := <-brainWorkSub:
			if !ok {
				brainWorkSub = nil
				continue
			}
			if err := s.brain.ReconcileWorkChange(); err != nil {
				log.Printf("brain durable Work change reconciliation failed: %v", err)
			}
			s.broadcastBrainSnapshot()
		}
	}
}

func (s *Server) broadcastBrainSnapshot() {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.Snapshot()
	if err != nil {
		log.Printf("brain snapshot broadcast failed: %v", err)
		return
	}
	s.emitBrainSnapshotBroadcast(snapshot)
}

// broadcastBrainHostCapabilityRefresh projects brain_snapshot for Hidden-host
// discovery/removal without ensureHostAgent. Continuity (create/resume/rebind)
// is owned by startup, Host-removal recovery, and NewChat, not projection.
func (s *Server) broadcastBrainHostCapabilityRefresh() {
	if s.brain == nil {
		return
	}
	snapshot, err := s.brain.ProjectionSnapshot()
	if err != nil {
		log.Printf("brain host capability refresh failed: %v", err)
		return
	}
	s.emitBrainSnapshotBroadcast(snapshot)
}

func (s *Server) emitBrainSnapshotBroadcast(snapshot brain.Snapshot) {
	s.observeLive(snapshot) // live state
	payload, err := s.brainSnapshotWire(snapshot)
	if err != nil {
		log.Printf("brain snapshot projection failed: %v", err)
		return
	}
	message := map[string]any{
		"type":  "brain_snapshot",
		"brain": payload,
	}
	if s.brainSnapshotBroadcastHook != nil {
		s.brainSnapshotBroadcastHook(message)
	}
	s.broadcastJSON(message)
}

func (s *Server) handleCalendarEvent(event calendar.Event) {
	s.broadcastJSON(map[string]any{"type": "calendar_item_changed", "calendar_item": calendarItemWire(event.Item)})
	if s.brain != nil {
		if _, err := s.brain.RouteCalendarEvent(event); err != nil {
			diag.Report("calendar.work_routing", diag.SeverityWarn, "", fmt.Sprintf("item %s: %v", event.Item.ID, err))
		}
	}
	if event.ScheduledResult == nil || s.pusher == nil {
		return
	}
	result := event.ScheduledResult
	if err := s.pusher.NotifyScheduledResult(result.Title, string(result.Status), result.ThreadID, result.ID); err != nil {
		logPushFailure("scheduled-result", "result="+result.ID, err)
	}
}

func (s *Server) handleWorkEvent(ev work.Event) {
	switch ev.Type {
	case work.EventChanged:
		if !work.IsCalendarWorkItem(ev.Item) {
			return
		}
		s.broadcastJSON(map[string]any{
			"type":      "work_item_changed",
			"path":      ev.Path,
			"id":        ev.ID,
			"work_item": ev.Item,
		})
	case work.EventDeleted:
		s.broadcastJSON(map[string]any{
			"type": "work_item_deleted",
			"path": ev.Path,
			"id":   ev.ID,
		})
	}
}

// signalSystemStartupOutcome decides whether the startup pass is finished. A
// failure is retried on the next tick unless nothing but refused cleanups of
// Sessions Mewla does not own remains: those never change by retrying.
func signalSystemStartupOutcome(complete bool, err error) (done, refused bool) {
	if err == nil {
		return complete, false
	}
	if complete && brain.OnlyCleanupRefusals(err) {
		return true, true
	}
	return false, false
}

// runBrainSessionReconcile lets Brain settle Sessions whose owner is missing,
// whose delegation lease expired or whose Event delivery was interrupted.
// Leases expire with time, so this keeps a timer; it broadcasts nothing.
func (s *Server) runBrainSessionReconcile(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.brain == nil || s.watcher == nil || !s.watcher.SnapshotReady() {
				continue
			}
			allWorkerSessions := s.watcher.Workers()
			if !s.signalSystemStartupComplete {
				complete, err := s.brain.ReconcileSignalSystemStartup(allWorkerSessions, 64)
				done, refused := signalSystemStartupOutcome(complete, err)
				switch {
				case refused:
					// Every Session left is one Mewla does not own. Retrying
					// every tick would only repeat the same refusal (and its
					// tmux probes) forever, so report it once and stop.
					log.Printf("brain signal-system startup reconciliation finished; left alone: %v", err)
				case err != nil:
					log.Printf("brain signal-system startup reconciliation failed: %v", err)
				case done:
					log.Printf("brain signal-system startup reconciliation complete")
				}
				s.signalSystemStartupComplete = done
			}
			s.brain.ReconcileDelegatedSessions(allWorkerSessions)
		}
	}
}

func (s *Server) handleWatcherEvent(ev watcher.SessionEvent) {
	if ev.Type == "worker_state_change" || ev.Type == "worker_removed" {
		// A launched Calendar action ends when its Worker does, even if its
		// Work file never changes.
		s.calendarScheduler.WorkerChanged()
	}
	if ev.Worker != nil && ev.Worker.Hidden {
		// A Session that was listed and has become hidden leaves the list now,
		// not at the next reconcile.
		s.workerFeed.retire(ev.Worker.ID, s.broadcastFeed)
		if s.brain != nil {
			if _, err := s.brain.ObserveHostSessionEvent(ev); err != nil {
				log.Printf("brain host event dispatch failed: %v", err)
			}
			// Hidden hosts stay off worker_session_list. Refresh brain_snapshot
			// only when the current Host appears or disappears so capabilities
			// converge after reconnect discovery, or when it becomes blocked,
			// stops being blocked or moves to another prompt so the Brain chat
			// can show it — never on output/turn noise,
			// without launching a provider from projection. Current-Host removal
			// separately wakes the continuity lifecycle owner.
			if s.shouldBroadcastHiddenHostBrainSnapshot(ev) {
				s.noteHostBlockSignature(ev)
				s.broadcastBrainHostCapabilityRefresh()
				if ev.Type == "worker_removed" {
					// Recovery can wait for provider readiness. Wake its lifecycle
					// owner without blocking consumption of watcher events.
					select {
					case s.brainHostRecovery <- struct{}{}:
					default:
					}
				}
			}
		}
		return
	}

	switch ev.Type {
	case "worker_discovered", "worker_state_change", "worker_metadata_change", "worker_removed":
		// A Session that starts or stops running or asking changes the live state.
		defer s.observeLiveSessions()
	}
	switch ev.Type {
	case "worker_discovered":
		if ev.Worker != nil {
			s.publishWorkerSession("worker_session_created", ev.Worker)
		}
	case "worker_output":
		if ev.Worker != nil {
			s.publishWorkerSessionOutput(ev.Worker)
		}
	case "worker_state_change":
		if ev.Worker != nil {
			s.publishWorkerSessionChange(ev.Worker)
		}
		s.observePromptLifecycle(ev)
		s.maybeNotifyForSessionEvent(ev)
		s.routeSessionEventToBrain(ev)
	case "worker_metadata_change":
		if ev.Worker != nil {
			s.publishWorkerSessionChange(ev.Worker)
		}
		s.observePromptLifecycle(ev)
		s.routeSessionEventToBrain(ev)
	case "worker_removed":
		s.observePromptLifecycle(ev)
		if ev.Worker != nil {
			s.publishWorkerSession("worker_session_archived", ev.Worker)
		}
		s.maybeNotifyForSessionEvent(ev)
		s.routeSessionEventToBrain(ev)
	}
}

// shouldBroadcastHiddenHostBrainSnapshot admits only current-Host discovery and
// removal for capability projection. Those refreshes must not call
// ensureHostAgent. Output chunks, turn state, and unrelated Hidden agents never
// churn brain_snapshot.
func (s *Server) shouldBroadcastHiddenHostBrainSnapshot(ev watcher.SessionEvent) bool {
	if s == nil || s.brain == nil || ev.Worker == nil || !ev.Worker.Hidden {
		return false
	}
	switch ev.Type {
	case "worker_discovered", "worker_removed":
	default:
		return s.hostBlockChanged(ev)
	}
	hostID := s.brain.CurrentHostSessionID()
	if hostID == "" {
		return false
	}
	return hostID == strings.TrimSpace(ev.Worker.ID)
}

// hostBlockSignature names what a phone can act on for the Brain host: it is
// empty unless the host is blocked, and then carries the prompt it is blocked on.
func hostBlockSignature(worker *classifier.Worker) string {
	if worker == nil || worker.State != classifier.StateBlocked {
		return ""
	}
	if worker.Prompt == nil {
		return "blocked"
	}
	return "blocked|" + worker.Prompt.ID
}

// noteHostBlockSignature records what the brain_snapshot about to be sent
// carries for the host.
func (s *Server) noteHostBlockSignature(ev watcher.SessionEvent) {
	if ev.Type == "worker_removed" {
		s.hostBlockSignature = ""
		return
	}
	s.hostBlockSignature = hostBlockSignature(ev.Worker)
}

// hostBlockChanged reports whether the current host became blocked, stopped
// being blocked, or moved to another prompt since the last brain_snapshot. The
// Brain chat answers the host's prompts, so it needs these changes as they
// happen; hidden hosts are not on worker_session_list.
func (s *Server) hostBlockChanged(ev watcher.SessionEvent) bool {
	if s == nil || s.brain == nil || ev.Worker == nil || !ev.Worker.Hidden {
		return false
	}
	switch ev.Type {
	case "worker_state_change", "worker_metadata_change":
	default:
		return false
	}
	hostID := s.brain.CurrentHostSessionID()
	if hostID == "" || hostID != strings.TrimSpace(ev.Worker.ID) {
		return false
	}
	return hostBlockSignature(ev.Worker) != s.hostBlockSignature
}

func (s *Server) routeSessionEventToBrain(ev watcher.SessionEvent) {
	if s.brain == nil {
		return
	}
	woke, err := s.brain.RouteSessionEvent(ev)
	if err != nil {
		log.Printf("brain Work event routing failed for %s: %v", ev.WorkerID, err)
		return
	}
	if woke {
		debuglog.Printf("brain Work event dispatched for %s", ev.WorkerID)
	}
}

// logPushFailure reports a push that could not be delivered. No registered
// device is the normal state until a phone enables notifications, so it only
// appears with MEWLA_LOG_VERBOSE.
func logPushFailure(kind, target string, err error) {
	if errors.Is(err, push.ErrNoRegistration) {
		debuglog.Printf("push: %s %s skipped: %v", kind, target, err)
		return
	}
	if errors.Is(err, push.ErrCredentials) {
		// The push client already logged this once for the device.
		debuglog.Printf("push: %s %s failed: %v", kind, target, err)
		return
	}
	log.Printf("push: %s %s failed: %v", kind, target, err)
}

func (s *Server) maybeNotifyForSessionEvent(ev watcher.SessionEvent) {
	if s.pusher == nil || ev.Worker == nil || ev.Worker.Hidden || !ev.Worker.Delegated {
		return
	}
	if ev.Type == "worker_removed" {
		return
	}
	// Push notifications are an actionable lifecycle projection, not a raw
	// classifier surface. Markerless delegated Sessions (empty TurnID) and
	// stale event projections from a superseded turn must never notify done,
	// failed, or blocked. The canonical Turn Ledger is the only owner.
	if s.brain == nil || strings.TrimSpace(ev.TurnID) == "" {
		s.noteSuppressedNotification(ev, "no_turn_marker")
		return
	}
	current, found, err := s.brain.Turn(ev.WorkerID)
	if err != nil || !found || current.TurnID != strings.TrimSpace(ev.TurnID) {
		return
	}
	if ev.Type != "worker_state_change" || ev.OldState == ev.NewState {
		return
	}
	kind, ok := workerNotificationKind(ev.NewState)
	if !ok {
		s.noteWorkerMoved(ev)
		return
	}
	// Blocked on its own report, the Worker asks Brain, not the owner: Brain
	// decides, and reaches the owner only with its Work's question.
	if kind == push.KindWorkerBlocked && !asksOwner(ev.Worker, len(s.pendingApprovals(ev.WorkerID))) {
		s.noteSuppressedNotification(ev, "brain_decides")
		return
	}
	if s.hasActiveViewer(ev.WorkerID) {
		s.noteSuppressedNotification(ev, "person_viewing_session")
		return
	}
	// A done push is a nicety: skip it while the app is open in front of the
	// person, and send only the first one a Work earns in a run. blocked and
	// failed need the person, so they go out, but once per question: the same
	// prompt flickering, re-rendering or surviving a restart is not a new one.
	if reason := s.doneSuppression(ev, kind); reason != "" {
		s.noteSuppressedNotification(ev, reason)
		return
	}
	if reason, first := s.workerPushSuppression(ev, kind); reason != "" {
		if first {
			s.noteSuppressedNotification(ev, reason)
		}
		return
	}
	if err := s.pusher.NotifyWorker(kind, workerNotice(ev)); err != nil {
		logPushFailure(strings.ReplaceAll(kind, "_", "-"), "worker="+ev.WorkerID, err)
		if errors.Is(err, push.ErrNoRegistration) {
			// Nobody was told: a phone that registers while the question is
			// still open must still hear it.
			s.forgetWorkerPush(ev.WorkerID)
		}
	}
}

func (s *Server) broadcast(data []byte) {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for _, conn := range conns {
		if err := s.queueMessage(conn, websocket.TextMessage, data, false); err != nil {
			s.mu.Lock()
			work := s.removeClientLocked(conn)
			s.mu.Unlock()
			s.completeClientDetach(work)
		}
	}
}

func (s *Server) broadcastJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.broadcast(data)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	s.handleUploadWithLimits(w, r, productionUploadLimits())
}

func (s *Server) handleUploadWithLimits(w http.ResponseWriter, r *http.Request, limits uploadLimits) {
	if _, ok := s.authenticateRequest(w, r, "mewla-upload"); !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	originalName, err := decodeUploadNameHeader(r.Header.Values(uploadNameHeader))
	if err != nil {
		logUploadRejected(r, "", http.StatusBadRequest, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(r.Header.Get("Content-Type")) == "" {
		logUploadRejected(r, originalName, http.StatusBadRequest, errors.New("missing Content-Type"))
		http.Error(w, "Content-Type header is required", http.StatusBadRequest)
		return
	}
	if r.ContentLength > limits.fileBytes {
		logUploadRejected(r, originalName, http.StatusRequestEntityTooLarge, fmt.Errorf("declared size exceeds %d byte limit", limits.fileBytes))
		writeUploadTooLarge(w, limits)
		return
	}
	if s.uploadDir == "" {
		logUploadRejected(r, originalName, http.StatusServiceUnavailable, errors.New("upload storage unavailable"))
		http.Error(w, "upload storage unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limits.fileBytes)
	started := time.Now()
	debuglog.Printf("upload start remote=%q content_length=%d transport=%q user_agent=%q name=%q",
		r.RemoteAddr, r.ContentLength, r.Header.Get("X-Mewla-Upload-Transport"), r.UserAgent(), originalName)
	file, err := s.uploadStore.Save(r.Context(), r.Body, attachment.File{
		Name: originalName, ContentType: r.Header.Get("Content-Type"), Size: r.ContentLength,
	}, attachment.Limits{FileBytes: limits.fileBytes, StoreBytes: limits.storeBytes, Retention: limits.retention})
	if err != nil {
		status := &uploadStatusWriter{ResponseWriter: w}
		w := http.ResponseWriter(status)
		defer func() {
			logUploadRejected(r, originalName, status.code, fmt.Errorf("save after %dms: %w", time.Since(started).Milliseconds(), err))
		}()
		switch {
		case errors.Is(err, attachment.ErrCapacity):
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
		case errors.Is(err, attachment.ErrTooLarge):
			writeUploadTooLarge(w, limits)
		default:
			var pathErr *os.PathError
			if errors.As(err, &pathErr) {
				http.Error(w, "upload storage unavailable", http.StatusInternalServerError)
				return
			}
			writeUploadReadError(w, err, limits)
		}
		return
	}
	duration := time.Since(started)
	rateMiB := 0.0
	if duration > 0 {
		rateMiB = float64(file.Size) / (1024 * 1024) / duration.Seconds()
	}
	debuglog.Printf("upload complete remote=%q bytes=%d duration_ms=%d rate_mib_s=%.2f transport=%q",
		r.RemoteAddr, file.Size, duration.Milliseconds(), rateMiB, r.Header.Get("X-Mewla-Upload-Transport"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": file.Path, "name": originalName})
}

// AttachmentStore is shared with verified channel adapters. Network handlers
// still authenticate before accessing it; it exposes no unauthenticated route.
func (s *Server) AttachmentStore() *attachment.Store { return s.uploadStore }

func cleanupUploadStore(dir string, now time.Time, limits uploadLimits) (int64, error) {
	return attachment.Inspect(dir, now, attachment.Limits{FileBytes: limits.fileBytes, StoreBytes: limits.storeBytes, Retention: limits.retention}, true)
}

func safeUploadExtension(name string) string { return attachment.SafeExtension(name) }

func decodeUploadNameHeader(values []string) (string, error) {
	if len(values) == 0 {
		return "", errors.New("upload filename header is required")
	}
	if len(values) != 1 {
		return "", errors.New("upload filename header must appear once")
	}
	encoded := values[0]
	if len(encoded) > maxUploadNameHeaderBytes {
		return "", errors.New("upload filename header is too long")
	}
	for index := range encoded {
		if encoded[index] < 0x21 || encoded[index] > 0x7e {
			return "", errors.New("upload filename header is invalid")
		}
	}
	decoded, err := url.PathUnescape(encoded)
	if err != nil || !utf8.ValidString(decoded) {
		return "", errors.New("upload filename header is invalid")
	}
	if len(decoded) > maxUploadNameBytes {
		return "", errors.New("upload filename header is too long")
	}
	if strings.TrimSpace(decoded) == "" {
		return "", errors.New("upload filename header is invalid")
	}
	for _, char := range decoded {
		if unicode.IsControl(char) {
			return "", errors.New("upload filename header is invalid")
		}
	}
	return decoded, nil
}

// uploadStatusWriter remembers the status a failed upload was answered with.
type uploadStatusWriter struct {
	http.ResponseWriter
	code int
}

func (w *uploadStatusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// logUploadRejected records one failed upload with the status it was sent.
func logUploadRejected(r *http.Request, name string, status int, err error) {
	diag.Report("server.upload", diag.SeverityWarn, "", fmt.Sprintf("remote=%q name=%q content_length=%d transport=%q status=%d: %v",
		r.RemoteAddr, name, r.ContentLength, r.Header.Get("X-Mewla-Upload-Transport"), status, err))
}

func writeUploadReadError(w http.ResponseWriter, err error, limits uploadLimits) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeUploadTooLarge(w, limits)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func writeUploadTooLarge(w http.ResponseWriter, limits uploadLimits) {
	http.Error(w, fmt.Sprintf("upload exceeds the %s file limit", formatUploadBytes(limits.fileBytes)), http.StatusRequestEntityTooLarge)
}

func formatUploadBytes(size int64) string {
	for _, unit := range []struct {
		bytes int64
		name  string
	}{
		{bytes: 1 << 30, name: "GiB"},
		{bytes: 1 << 20, name: "MiB"},
		{bytes: 1 << 10, name: "KiB"},
	} {
		if size >= unit.bytes && size%unit.bytes == 0 {
			return fmt.Sprintf("%d %s", size/unit.bytes, unit.name)
		}
	}
	return fmt.Sprintf("%d bytes", size)
}

func (s *Server) authenticateRequest(w http.ResponseWriter, r *http.Request, purpose string) (*auth.TrustedDevice, bool) {
	device, err := s.auth.VerifyAuthorization(authorizationFromRequest(r), purpose, 5*time.Minute)
	if err != nil {
		s.logRefusedRequest(r, purpose, err)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	auth.NotifyAuthenticated(r.Context())
	return device, true
}

// authSubprotocolPrefix starts the one WebSocket subprotocol a browser offers
// to carry its device authorization: the browser WebSocket API cannot set an
// Authorization header, and a credential in the URL lands in proxy and tunnel
// logs. The rest of the protocol name is the base64url of the same header
// value, so every signature, purpose and replay check is unchanged.
const authSubprotocolPrefix = "mewla.auth."

func authorizationFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("Authorization")); value != "" {
		return value
	}
	if !websocket.IsWebSocketUpgrade(r) {
		return ""
	}
	for _, protocol := range websocket.Subprotocols(r) {
		encoded, ok := strings.CutPrefix(protocol, authSubprotocolPrefix)
		if !ok || encoded == "" {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			return ""
		}
		return string(decoded)
	}
	return ""
}

// websocketAuthResponseHeader selects the authorization subprotocol the
// browser offered; a browser fails the connection if the server selects none
// of the protocols it asked for. Requests that sent a header get no header.
func websocketAuthResponseHeader(r *http.Request) http.Header {
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" {
		return nil
	}
	for _, protocol := range websocket.Subprotocols(r) {
		if strings.HasPrefix(protocol, authSubprotocolPrefix) {
			return http.Header{"Sec-Websocket-Protocol": {protocol}}
		}
	}
	return nil
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if isAllowedCORSOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isAllowedCORSOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// handleHealth answers a liveness probe. With ?challenge=<16 bytes hex> the
// response is signed over that challenge, which proves the answerer holds the
// daemon key now; the plain response is a recorded-able identity hint only.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":            "ok",
		"daemon_id":         s.auth.DaemonID(),
		"daemon_public_key": s.auth.PublicKeyHex(),
		"version":           s.version,
	}
	challenge, challenged := r.URL.Query()["challenge"]
	if !challenged {
		s.writeJSONWithAssertion(w, http.StatusOK, "mewla-health", payload)
		return
	}
	if len(challenge) != 1 {
		http.Error(w, "challenge must be given once", http.StatusBadRequest)
		return
	}
	assertion, err := s.auth.CreateChallengeAssertion(challenge[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeSignedJSON(w, http.StatusOK, auth.HealthChallengePurpose, assertion, payload)
}

func (s *Server) writeJSONWithAssertion(w http.ResponseWriter, status int, purpose string, payload map[string]any) {
	assertion, err := s.auth.CreateServerAssertion(purpose)
	if err != nil {
		http.Error(w, "failed to sign daemon response", http.StatusInternalServerError)
		return
	}

	writeSignedJSON(w, status, purpose, assertion, payload)
}

func writeSignedJSON(w http.ResponseWriter, status int, purpose string, assertion auth.ServerAssertion, payload map[string]any) {
	payload["assertion_purpose"] = purpose
	payload["assertion_timestamp"] = assertion.Timestamp
	payload["assertion_nonce"] = assertion.NonceHex
	payload["assertion_signature"] = assertion.SignatureHex

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func clientID(conn *websocket.Conn) string {
	return fmt.Sprintf("%p", conn)
}

func applyFrontmatterOverrides(fm *work.Frontmatter, raw map[string]interface{}) {
	if fm == nil || raw == nil {
		return
	}

	extra := make(map[string]interface{}, len(fm.Extra))
	for key, value := range fm.Extra {
		extra[key] = value
	}
	if value, ok := raw["extra"]; ok {
		extra = map[string]interface{}{}
		if nested, ok := value.(map[string]interface{}); ok {
			for nestedKey, nestedValue := range nested {
				extra[nestedKey] = nestedValue
			}
		}
	}
	for key, value := range raw {
		if key == "extra" {
			continue
		}
		switch key {
		case "id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				fm.ID = strings.TrimSpace(s)
			}
		case "kind":
			if s, ok := value.(string); ok {
				fm.Kind = strings.TrimSpace(s)
			}
		case "created":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Created = parsed
			}
		case "done":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Done = &parsed
			} else {
				fm.Done = nil
			}
		case "started":
			if parsed, ok := parseRFC3339Value(value); ok {
				fm.Started = &parsed
			} else {
				fm.Started = nil
			}
		case "status":
			if s, ok := value.(string); ok {
				fm.Status = strings.TrimSpace(s)
			}
		case "title":
			if s, ok := value.(string); ok {
				fm.Title = strings.TrimSpace(s)
			}
		case "worker_session":
			if s, ok := value.(string); ok {
				fm.WorkerSession = s
			}
		default:
			extra[key] = value
		}
	}
	if len(extra) == 0 {
		fm.Extra = nil
		return
	}
	fm.Extra = extra
}

func parseRFC3339Value(value interface{}) (time.Time, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return parsed, true
		}
	case nil:
		return time.Time{}, false
	}
	return time.Time{}, false
}

func buildWorkFilename(now time.Time, body, fallbackID string) string {
	return now.Format("2006-01-02") + "-" + slugifyWorkTitle(firstLine(body), fallbackID) + ".md"
}

func slugifyWorkTitle(line, fallback string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if trimmed == "" {
		return strings.ToLower(fallback)
	}

	out := make([]rune, 0, len(trimmed))
	lastDash := false
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(string(out), "-")
	if slug == "" {
		slug = strings.ToLower(fallback)
	}
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return strings.ToLower(fallback)
	}
	return slug
}

func firstLine(value string) string {
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}

func (s *Server) hasActiveViewer(workerID string) bool {
	target := strings.TrimSpace(workerID)
	if target == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, activeWorkerID := range s.active {
		if strings.TrimSpace(activeWorkerID) == target {
			return true
		}
	}

	return false
}

func (s *Server) outboxStats(conn *websocket.Conn) outboxStats {
	s.mu.Lock()
	outbox := s.outboxes[conn]
	s.mu.Unlock()
	if outbox == nil {
		return outboxStats{}
	}
	return outbox.stats()
}

func (s *Server) outboxFor(conn *websocket.Conn) *clientOutbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outboxes[conn]
}

// queueMessage hands one message to the client's writer. It never writes to
// the socket, so a client that stops reading cannot stall its caller.
func (s *Server) queueMessage(conn *websocket.Conn, messageType int, data []byte, waitForRoom bool) error {
	s.mu.Lock()
	outbox, ok := s.outboxes[conn]
	s.mu.Unlock()
	if !ok {
		return errOutboxClosed
	}
	return outbox.enqueue(messageType, data, waitForRoom)
}
