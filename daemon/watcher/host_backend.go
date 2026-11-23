package watcher

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/sessionhost"
	"github.com/daoleno/mewla/daemon/userunit"
)

// SessionHostConfig is how the daemon reaches its session host (mewla host),
// which runs every new Session.
type SessionHostConfig struct {
	SocketPath string
	// Executable is the mewla binary that serves `host`; StateDir goes to it.
	Executable string
	StateDir   string
	LogPath    string
	// Version names this daemon in the host's log.
	Version string
}

// SetSessionHost runs Sessions in the session host. Call it once before Run
// or CreateSession.
func (w *Watcher) SetSessionHost(cfg SessionHostConfig) {
	if w == nil || cfg.SocketPath == "" {
		return
	}
	backend := &hostBackend{w: w, cfg: cfg}
	w.mu.Lock()
	w.backend = backend
	w.listPanes = backend.listOwned
	w.mu.Unlock()
	w.sessionInput.setIO(backend.inputIO())
}

// Host Session ids are random, 96 bits, and never reused: a replaced Session
// can never carry the id, so never the generation, of the one it replaced.
var hostSessionIDPattern = regexp.MustCompile(`^mh-[0-9a-f]{24}$`)

func newHostSessionID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "mh-" + hex.EncodeToString(b[:]), nil
}

// Label keys the host backend records on its Sessions.
const (
	hostLabelName           = "name"
	hostLabelOrigin         = "origin"
	hostLabelHidden         = "hidden"
	hostLabelDelegated      = "delegated"
	hostLabelExecutor       = "executor"
	hostLabelAgentControl   = "agent-control"
	hostLabelPiSession      = "pi-session"
	hostLabelResourceUnit   = "resource-unit"
	hostLabelResourceOwner  = "resource-owner"
	hostDataInputReceipts   = "input-receipts"
	hostDataAgentEnd        = "agent-end"
	hostDataDelegatedPrompt = "delegated-prompt"
	hostDefaultCols         = 200
	hostDefaultRows         = 50
	hostCallTimeout         = 10 * time.Second
)

// ErrSessionHostGone says the host is not running: no socket, or nothing
// listening on it. Nothing is running at all.
var ErrSessionHostGone = errors.New("session host is not running")

// errNoSessionHost: this watcher was never given a session host.
var errNoSessionHost = errors.New("Mewla has no session host")

// hostBackend runs Sessions in the session host. A Session's labels and input
// receipts live in the host with it (labels at create, receipts as data), so
// they last exactly as long as the Session.
type hostBackend struct {
	w   *Watcher
	cfg SessionHostConfig

	mu     sync.Mutex
	client *sessionhost.Client
	// reached says this process has had a connection to a host. Until it has, a
	// host that is not running holds nothing: a Session an earlier run recorded
	// (Brain's, before a reboot or a restart) went with it. Once it has, a vanished
	// host is unknown (its socket can be gone for a moment while it lives, as when
	// logind empties XDG_RUNTIME_DIR, see sessionhost keepSocket).
	reached bool
}

// conn returns a client. With start false it only dials a host that is
// already there, and creates nothing; ErrSessionHostGone means none is.
func (b *hostBackend) conn(start bool) (*sessionhost.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		select {
		case <-b.client.Done():
			b.client.Close()
			b.client = nil
		default:
			return b.client, nil
		}
	}
	if b.cfg.SocketPath == "" {
		if !start {
			return nil, ErrSessionHostGone
		}
		return nil, errNoSessionHost
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	if !start {
		if _, err := os.Lstat(b.cfg.SocketPath); errors.Is(err, os.ErrNotExist) {
			return nil, ErrSessionHostGone
		}
		client, err := sessionhost.Dial(ctx, b.cfg.SocketPath, b.cfg.Version)
		if err != nil {
			if sessionhost.IsAbsent(err) {
				return nil, ErrSessionHostGone
			}
			return nil, err
		}
		b.client = client
		b.reached = true
		return client, nil
	}
	client, started, err := sessionhost.Connect(ctx, sessionhost.StartOptions{
		SocketPath:     b.cfg.SocketPath,
		Executable:     b.cfg.Executable,
		Args:           []string{"--state-dir", b.cfg.StateDir},
		Env:            os.Environ(),
		LogPath:        b.cfg.LogPath,
		ClientVersion:  b.cfg.Version,
		EnclosingSlice: b.enclosingSlice(),
	})
	if err != nil {
		return nil, err
	}
	if started {
		welcome := client.Welcome()
		log.Printf("watcher: started the session host %s (pid %d, protocol %d)", welcome.HostVersion, welcome.PID, welcome.Protocol)
	}
	b.client = client
	b.reached = true
	return client, nil
}

// enclosingSlice is the Session slice of another Mewla this daemon runs in,
// "" when none or when it is one of this daemon's own Sessions (a daemon
// restarted from a shell in one of its Workers): see userunit.NestingSlice.
func (b *hostBackend) enclosingSlice() string {
	b.w.mu.Lock()
	resources := b.w.resources
	b.w.mu.Unlock()
	owned, _ := resources.(sessionTokenOwner)
	return userunit.NestingSliceOf(func(token string) bool {
		return owned != nil && owned.ownsSessionToken(token)
	})
}

// hostIdentity names the host of the current connection; zero without one.
func (b *hostBackend) hostIdentity() sessionHostIdentity {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client == nil {
		return sessionHostIdentity{}
	}
	welcome := b.client.Welcome()
	return sessionHostIdentity{PID: welcome.PID, StartedAt: welcome.StartedAt}
}

// hasReached reports whether this process has had a connection to a host.
func (b *hostBackend) hasReached() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reached
}

func hostCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), hostCallTimeout)
}

func (b *hostBackend) validID(id string) bool {
	return hostSessionIDPattern.MatchString(id)
}

// ready starts the host if needed and checks it can start Sessions by
// running a probe Session.
func (b *hostBackend) ready() error {
	client, err := b.conn(true)
	if err != nil {
		return fmt.Errorf("session host: %w", err)
	}
	if !client.Welcome().Engine {
		return errors.New("the session host has no terminal engine")
	}
	id, err := newHostSessionID()
	if err != nil {
		return err
	}
	probe := "probe-" + strings.TrimPrefix(id, "mh-")
	ctx, cancel := hostCtx()
	defer cancel()
	if _, err := client.Create(ctx, sessionhost.CreateParams{
		ID: probe, Argv: []string{"/bin/sh", "-c", "sleep 5"}, Env: []string{"PATH=/usr/bin:/bin"}, Dir: "/", Cols: 80, Rows: 24,
	}); err != nil {
		return fmt.Errorf("session host probe: %w", err)
	}
	if _, err := client.Remove(ctx, probe); err != nil {
		return fmt.Errorf("session host probe: %w", err)
	}
	return nil
}

func (b *hostBackend) terminal(string) TerminalTarget {
	return TerminalTarget{Socket: b.cfg.SocketPath}
}

func (b *hostBackend) observedGeneration(id string) string {
	if !b.validID(id) {
		return ""
	}
	digest := sha256.Sum256([]byte(id))
	return hex.EncodeToString(digest[:])
}

func (b *hostBackend) list() ([]sessionhost.Info, error) {
	client, err := b.conn(false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	return client.List(ctx)
}

func (b *hostBackend) listOwned() ([]sessionPane, error) {
	infos, err := b.list()
	if err != nil {
		return nil, err
	}
	panes := make([]sessionPane, 0, len(infos))
	for _, info := range infos {
		if !b.validID(info.ID) {
			continue
		}
		panes = append(panes, hostPane(info))
	}
	b.w.screens.inventory(panes)
	return panes, nil
}

func hostPane(info sessionhost.Info) sessionPane {
	labels := info.Labels
	pane := sessionPane{
		target:           info.ID,
		name:             labels[hostLabelName],
		cwd:              info.Cwd,
		command:          info.Command,
		piSessionBinding: labels[hostLabelPiSession],
		panePID:          info.PID,
		hidden:           labels[hostLabelHidden] == "1",
		delegated:        labels[hostLabelDelegated] == "1",
		resourceUnit:     labels[hostLabelResourceUnit],
		executorID:       labels[hostLabelExecutor],
		agentControl:     labels[hostLabelAgentControl],
		dead:             !info.Alive,
	}
	if pane.name == "" {
		pane.name = info.ID
	}
	if !info.LastOutputAt.IsZero() {
		pane.activity = info.LastOutputAt.Unix()
	} else {
		pane.activity = info.CreatedAt.Unix()
	}
	return pane
}

func (b *hostBackend) listServices() ([]servicePane, error) {
	infos, err := b.list()
	if errors.Is(err, ErrSessionHostGone) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	panes := make([]servicePane, 0, len(infos))
	for _, info := range infos {
		if !b.validID(info.ID) || !info.Alive {
			continue
		}
		panes = append(panes, servicePane{
			target:  info.ID,
			paneID:  info.ID,
			name:    info.Labels[hostLabelName],
			cwd:     info.Cwd,
			command: info.Command,
			panePID: info.PID,
			active:  true,
		})
	}
	return panes, nil
}

// info reads one Session; present is false when the host does not have it or
// is not running.
func (b *hostBackend) info(id string) (info sessionhost.Info, present bool, err error) {
	client, err := b.conn(false)
	if errors.Is(err, ErrSessionHostGone) {
		return sessionhost.Info{}, false, nil
	}
	if err != nil {
		return sessionhost.Info{}, false, err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	info, err = client.Info(ctx, id)
	if sessionhost.IsCode(err, sessionhost.ErrCodeNotFound) {
		return sessionhost.Info{}, false, nil
	}
	if err != nil {
		return sessionhost.Info{}, false, err
	}
	return info, true, nil
}

// Every Session in this daemon's host is Mewla's: the host is private to the
// state directory.
func (b *hostBackend) probeOwnership(id string) (bool, bool, error) {
	_, present, err := b.info(id)
	if err != nil {
		return false, false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, err)
	}
	return present, present, nil
}

// resolveAbsence asks the host. What counts is whether this process ever
// reached a host: until it has, a host that is not running proves a Session an
// earlier run recorded is gone.
func (b *hostBackend) resolveAbsence(id string) (bool, error) {
	client, err := b.conn(false)
	if errors.Is(err, ErrSessionHostGone) {
		if !b.hasReached() {
			// No host has run since this process started: a Session an earlier
			// run recorded is gone with the host that held it.
			return true, nil
		}
		return false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, err)
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, err)
	}
	ctx, cancel := hostCtx()
	defer cancel()
	if _, err := client.Info(ctx, id); sessionhost.IsCode(err, sessionhost.ErrCodeNotFound) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("%w: %v", ErrOwnershipProbeUnavailable, err)
	}
	return false, nil
}

func (b *hostBackend) present(id string) (sessionhost.Info, error) {
	info, present, err := b.info(id)
	if err != nil {
		return sessionhost.Info{}, err
	}
	if !present {
		return sessionhost.Info{}, fmt.Errorf("session host has no Session %s", id)
	}
	return info, nil
}

func (b *hostBackend) processRoot(id string) (int, error) {
	info, err := b.present(id)
	if err != nil {
		return 0, err
	}
	if !info.Alive {
		return 0, fmt.Errorf("session host Session %s has exited", id)
	}
	return info.PID, nil
}

func (b *hostBackend) currentPath(id string) (string, error) {
	info, err := b.present(id)
	if err != nil {
		return "", err
	}
	if info.Cwd == "" {
		return info.Dir, nil
	}
	return info.Cwd, nil
}

func (b *hostBackend) delegatedResource(id string) (bool, string) {
	info, present, err := b.info(id)
	if err != nil || !present {
		return false, ""
	}
	return info.Labels[hostLabelDelegated] == "1", info.Labels[hostLabelResourceUnit]
}

func (b *hostBackend) capture(params sessionhost.CaptureParams) (sessionhost.Capture, error) {
	client, err := b.conn(false)
	if err != nil {
		return sessionhost.Capture{}, err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	return client.Capture(ctx, params)
}

func (b *hostBackend) captureState(id string) (string, bool, int) {
	if !b.validID(id) {
		return "", false, -1
	}
	capture, err := b.capture(sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeRecent, History: 200, Trim: true})
	if err != nil {
		return "", false, -1
	}
	info, present, err := b.info(id)
	if err != nil || !present {
		return "", false, -1
	}
	// A program ended by a signal (kill -9, the OOM killer) has ended as
	// surely as one that exited: the host reports 128+signal as its code.
	deadStatus := -1
	if !info.Alive && info.ExitCode != nil {
		deadStatus = *info.ExitCode
	}
	return capture.Text, info.Alive, deadStatus
}

func (b *hostBackend) captureRecent(id string) (string, error) {
	capture, err := b.capture(sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeRecent, History: 200, Trim: true})
	return capture.Text, err
}

func (b *hostBackend) captureVisible(id string, styled bool) (string, error) {
	capture, err := b.capture(sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeScreen, Trim: true, Styled: styled})
	return capture.Text, err
}

// captureDialog is the whole history with wrapped lines joined and trailing
// blanks kept. The origin marker the Session printed first (its nonce is a
// label) proves no line after it was evicted.
func (b *hostBackend) captureDialog(id string) (string, classifier.CaptureInfo, error) {
	capture, err := b.capture(sessionhost.CaptureParams{ID: id, Scope: sessionhost.ScopeAll, Unwrap: true})
	if err != nil {
		return "", classifier.CaptureInfo{}, fmt.Errorf("capture Session: %w", err)
	}
	info := classifier.CaptureInfo{Exact: true}
	if labels, present, err := b.info(id); err == nil && present {
		if nonce := labels.Labels[hostLabelOrigin]; nonce != "" {
			info.Origin = classifier.OriginMarkerLine(nonce)
		}
	}
	return capture.Text, info, nil
}

func (b *hostBackend) input(id string, steps []sessionhost.InputStep) (bool, error) {
	client, err := b.conn(false)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout+inputScriptDuration(steps))
	defer cancel()
	return client.Input(ctx, id, steps)
}

func inputScriptDuration(steps []sessionhost.InputStep) time.Duration {
	var total time.Duration
	for _, step := range steps {
		total += time.Duration(step.SleepMS) * time.Millisecond
	}
	return total
}

func (b *hostBackend) sendKeys(id string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	steps := make([]sessionhost.InputStep, 0, len(keys))
	for _, key := range keys {
		steps = append(steps, sessionhost.InputStep{Key: key})
	}
	_, err := b.input(id, steps)
	return err
}

func (b *hostBackend) repeatKey(id, key string, count int) error {
	if count <= 0 {
		return nil
	}
	steps := make([]sessionhost.InputStep, count)
	for i := range steps {
		steps[i] = sessionhost.InputStep{Key: key}
	}
	_, err := b.input(id, steps)
	return err
}

func (b *hostBackend) sendLiteral(id, text string) error {
	if text == "" {
		return nil
	}
	_, err := b.input(id, []sessionhost.InputStep{{Bytes: []byte(text)}})
	return err
}

func (b *hostBackend) sendBytes(id string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	_, err := b.input(id, []sessionhost.InputStep{{Bytes: data}})
	return err
}

// checkNear: a Session to start near must exist.
func (b *hostBackend) checkNear(near string) error {
	if near != "" && b.validID(near) {
		if _, present, err := b.info(near); err != nil {
			return err
		} else if !present {
			return fmt.Errorf("preferred Session %q is missing", near)
		}
	}
	return nil
}

func (b *hostBackend) create(spec backendCreateSpec) (string, error) {
	id, err := newHostSessionID()
	if err != nil {
		return "", err
	}
	return id, b.createAs(id, spec, nil)
}

// createAs starts a Session with the given id; extra labels are added to the
// ones spec names.
// withEnv sets key in an environment list, in place of any value it had.
func withEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if name, _, _ := strings.Cut(entry, "="); name != key {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+value)
}

func (b *hostBackend) createAs(id string, spec backendCreateSpec, extra map[string]string) error {
	client, err := b.conn(true)
	if err != nil {
		return fmt.Errorf("session host: %w", err)
	}
	params, err := b.createParams(id, spec, extra)
	if err != nil {
		return err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	if _, err := client.Create(ctx, params); err != nil {
		return fmt.Errorf("start Worker Session: %w", err)
	}
	return nil
}

// createParams is what the host is asked to start for spec under id.
func (b *hostBackend) createParams(id string, spec backendCreateSpec, extra map[string]string) (sessionhost.CreateParams, error) {
	nonce, err := newOriginNonce()
	if err != nil {
		return sessionhost.CreateParams{}, fmt.Errorf("origin marker: %w", err)
	}
	labels := map[string]string{
		hostLabelName:   spec.name,
		hostLabelOrigin: nonce,
	}
	setLabel := func(key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			labels[key] = value
		}
	}
	if spec.labels.hidden {
		labels[hostLabelHidden] = "1"
	}
	if spec.labels.delegated {
		labels[hostLabelDelegated] = "1"
		setLabel(hostLabelResourceUnit, spec.labels.resourceUnit)
		setLabel(hostLabelResourceOwner, spec.labels.resourceOwner)
	}
	setLabel(hostLabelExecutor, spec.labels.executorID)
	setLabel(hostLabelAgentControl, spec.labels.agentControl)
	setLabel(hostLabelPiSession, spec.labels.piSession)
	for key, value := range extra {
		labels[key] = value
	}
	// The host adds MEWLA_SESSION_ID, which the Worker id comes from. An
	// agent never runs without the daemon's private TMUX_TMPDIR: a plain
	// `tmux` in it (`tmux kill-server` too) must never reach the person's own
	// server. A person's own shell keeps their environment and their tmux.
	env := spec.env
	if spec.agent {
		tmuxDir, err := b.w.privateTmuxDir()
		if err != nil {
			return sessionhost.CreateParams{}, fmt.Errorf("start Worker Session: %w", err)
		}
		if tmuxDir != "" {
			env = withEnv(env, "TMUX_TMPDIR", tmuxDir)
		}
	}
	// The Session's first output is its origin marker; the command runs
	// under /bin/sh -c (`mewla sh -c` on Windows, see hostLaunchArgv).
	return sessionhost.CreateParams{
		ID:     id,
		Argv:   hostLaunchArgv(originMarkerCommand(nonce) + spec.command),
		Env:    env,
		Dir:    spec.cwd,
		Cols:   hostDefaultCols,
		Rows:   hostDefaultRows,
		Labels: labels,
	}, nil
}

// replace starts spec in place of a Session whose program has ended, under
// the same id. The host starts the new program first and only then lets it
// take the Session's place, with the data recorded on it (the input receipts,
// so an input that already landed is never sent again): the ended Session is
// never removed before its replacement runs. A Session that is gone, or whose
// program still runs, is not replaced. A host from before respawn cannot
// replace in place: errHostCannotRespawn, and nothing changes.
func (b *hostBackend) replace(id string, spec backendCreateSpec) error {
	if !b.validID(id) {
		return fmt.Errorf("replace Session %q: not a session host id", id)
	}
	info, present, err := b.info(id)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("replace Session %s: %w", id, errSessionGone)
	}
	if info.Alive {
		return fmt.Errorf("replace Session %s: its program still runs", id)
	}
	client, err := b.conn(false)
	if err != nil {
		return err
	}
	params, err := b.createParams(id, spec, nil)
	if err != nil {
		return err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	if _, err := client.Respawn(ctx, params); err != nil {
		if sessionhost.IsCode(err, sessionhost.ErrCodeUnknownMethod) {
			return errHostCannotRespawn
		}
		return fmt.Errorf("start the agent again: %w", err)
	}
	return nil
}

// kill removes the Session; one the host no longer has is not an error.
func (b *hostBackend) kill(id string) error {
	client, err := b.conn(false)
	if errors.Is(err, ErrSessionHostGone) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	if _, err := client.Remove(ctx, id); err != nil && !sessionhost.IsCode(err, sessionhost.ErrCodeNotFound) {
		return fmt.Errorf("remove host Session: %w", err)
	}
	return nil
}

func (b *hostBackend) inputIO() sessionInputIO { return hostInputIO{b: b} }

// hostInputIO is the Session input surface on the host: one input script per
// submission, and the receipt ledger kept as the Session's data.
type hostInputIO struct{ b *hostBackend }

func (io hostInputIO) pane(sessionID string) sessionInputPane {
	if !io.b.validID(sessionID) {
		return sessionInputPane{}
	}
	info, present, err := io.b.info(sessionID)
	if err != nil || !present || !info.Alive {
		return sessionInputPane{}
	}
	return sessionInputPane{alive: true, paneID: sessionID, generation: io.b.observedGeneration(sessionID)}
}

// submitSteps is the input script of one submission: clear the draft, wait
// what the provider needs, paste the payload and submit it.
func submitSteps(payload string, adapter sessionInputProvider) []sessionhost.InputStep {
	var steps []sessionhost.InputStep
	for _, key := range draftClearKeys() {
		steps = append(steps, sessionhost.InputStep{Key: key})
	}
	if adapter.prepare > 0 {
		steps = append(steps, sessionhost.InputStep{SleepMS: int(adapter.prepare / time.Millisecond)})
	}
	if adapter.delegatedIntent {
		steps = append(steps, sessionhost.InputStep{Bytes: []byte(DelegatedInputPrefix)}, sessionhost.InputStep{SleepMS: 50})
	}
	steps = append(steps, sessionhost.InputStep{Paste: payload})
	if adapter.settle > 0 {
		steps = append(steps, sessionhost.InputStep{SleepMS: int(adapter.settle / time.Millisecond)})
	}
	return append(steps, sessionhost.InputStep{Key: adapter.submitKey})
}

// submitPaste clears the draft (every line of it), pastes the payload as one paste and submits
// it with the adapter's timing, all in one input script the host runs with
// nothing else in between.
func (io hostInputIO) submitPaste(paneID, payload string, adapter sessionInputProvider, beforeStart func() error) (bool, error) {
	steps := submitSteps(payload, adapter)
	if _, err := io.b.conn(false); err != nil {
		return false, fmt.Errorf("start session host input: %w", err)
	}
	if beforeStart != nil {
		if err := beforeStart(); err != nil {
			return false, err
		}
	}
	started, err := io.b.input(paneID, steps)
	if err != nil {
		if started {
			return true, fmt.Errorf("run session host input: %w", err)
		}
		return false, fmt.Errorf("start session host input: %w", err)
	}
	return true, nil
}

func (io hostInputIO) receiptLedger(target string) (sessionInputReceiptLedger, error) {
	client, err := io.b.conn(false)
	if err != nil {
		return sessionInputReceiptLedger{}, err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	data, err := client.GetData(ctx, target, hostDataInputReceipts)
	if err != nil {
		return sessionInputReceiptLedger{}, err
	}
	return decodeSessionInputReceiptLedger(data.Value)
}

func (io hostInputIO) writeReceiptLedger(target string, ledger sessionInputReceiptLedger) error {
	if err := validateSessionInputReceiptLedger(ledger); err != nil {
		return err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("encode Session input receipt ledger: %w", err)
	}
	client, err := io.b.conn(false)
	if err != nil {
		return err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	return client.SetData(ctx, target, hostDataInputReceipts, string(raw))
}

func (b *hostBackend) relaunchData(id, key string) (string, error) {
	client, err := b.conn(false)
	if err != nil {
		return "", err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	data, err := client.GetData(ctx, id, key)
	if err != nil {
		return "", err
	}
	return data.Value, nil
}

func (b *hostBackend) setRelaunchData(id, key, value string) error {
	client, err := b.conn(false)
	if err != nil {
		return err
	}
	ctx, cancel := hostCtx()
	defer cancel()
	return client.SetData(ctx, id, key, value)
}
