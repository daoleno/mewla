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
)

// SessionHostConfig is how the daemon reaches its session host (mewla host),
// which runs Sessions in place of tmux when Settings → Labs → Session host is
// on.
type SessionHostConfig struct {
	SocketPath string
	// Executable is the mewla binary that serves `host`; StateDir goes to it.
	Executable string
	StateDir   string
	LogPath    string
	// Version names this daemon in the host's log.
	Version string
	// Enabled reports the Labs switch; it is read at each create.
	Enabled func() bool
}

// SetSessionHost lets Sessions run in the session host. Sessions the host
// already runs are inventoried whatever the switch says; new ones go there
// only while it is on. Nothing is started, created or dialed while the switch
// is off and no host socket exists.
func (w *Watcher) SetSessionHost(cfg SessionHostConfig) {
	if w == nil || cfg.SocketPath == "" {
		return
	}
	backend := &compositeBackend{w: w, tmux: &tmuxBackend{w: w}, host: &hostBackend{w: w, cfg: cfg}}
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
	hostLabelName          = "name"
	hostLabelOrigin        = "origin"
	hostLabelHidden        = "hidden"
	hostLabelDelegated     = "delegated"
	hostLabelExecutor      = "executor"
	hostLabelAgentControl  = "agent-control"
	hostLabelPiSession     = "pi-session"
	hostLabelResourceUnit  = "resource-unit"
	hostLabelResourceOwner = "resource-owner"
	hostDataInputReceipts  = "input-receipts"
	hostSessionGroup       = "mewla"
	hostDefaultCols        = 200
	hostDefaultRows        = 50
	hostCallTimeout        = 10 * time.Second
)

// errSessionHostGone says the host is not running: no socket, or nothing
// listening on it.
var errSessionHostGone = errors.New("session host is not running")

// hostBackend runs Sessions in the session host. A Session's labels and input
// receipts live in the host with it (labels at create, receipts as data), so
// they last exactly as long as the Session, as tmux pane options do.
type hostBackend struct {
	w   *Watcher
	cfg SessionHostConfig

	mu     sync.Mutex
	client *sessionhost.Client
}

// conn returns a client. With start false it only dials a host that is
// already there, and creates nothing; errSessionHostGone means none is.
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
	ctx, cancel := context.WithTimeout(context.Background(), hostCallTimeout)
	defer cancel()
	if !start {
		if _, err := os.Lstat(b.cfg.SocketPath); errors.Is(err, os.ErrNotExist) {
			return nil, errSessionHostGone
		}
		client, err := sessionhost.Dial(ctx, b.cfg.SocketPath, b.cfg.Version)
		if err != nil {
			if sessionhost.IsAbsent(err) {
				return nil, errSessionHostGone
			}
			return nil, err
		}
		b.client = client
		return client, nil
	}
	client, started, err := sessionhost.Connect(ctx, sessionhost.StartOptions{
		SocketPath:    b.cfg.SocketPath,
		Executable:    b.cfg.Executable,
		Args:          []string{"--state-dir", b.cfg.StateDir},
		Env:           os.Environ(),
		LogPath:       b.cfg.LogPath,
		ClientVersion: b.cfg.Version,
	})
	if err != nil {
		return nil, err
	}
	if started {
		welcome := client.Welcome()
		log.Printf("watcher: started the session host %s (pid %d, protocol %d)", welcome.HostVersion, welcome.PID, welcome.Protocol)
	}
	b.client = client
	return client, nil
}

func hostCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), hostCallTimeout)
}

func (b *hostBackend) validID(id string) bool { return hostSessionIDPattern.MatchString(id) }

func (b *hostBackend) terminal(string) TerminalTarget {
	return TerminalTarget{Backend: "host", Socket: b.cfg.SocketPath}
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
	b.w.screens.inventoryOf(panes, b.validID)
	return panes, nil
}

func hostPane(info sessionhost.Info) sessionPane {
	labels := info.Labels
	pane := sessionPane{
		target:           info.ID,
		sessionName:      hostSessionGroup,
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
	if errors.Is(err, errSessionHostGone) {
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
	if errors.Is(err, errSessionHostGone) {
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

func (b *hostBackend) resolveAbsence(id string, sawInventory bool) (bool, error) {
	client, err := b.conn(false)
	if errors.Is(err, errSessionHostGone) {
		if !sawInventory {
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
	deadStatus := -1
	if !info.Alive && info.ExitCode != nil && info.ExitSignal == "" {
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

// place has nothing to arrange: host Sessions are not grouped. A Session to
// place near must exist.
func (b *hostBackend) place(near string, detached bool, name string) (backendPlacement, error) {
	if near != "" && b.validID(near) {
		if _, present, err := b.info(near); err != nil {
			return backendPlacement{}, err
		} else if !present {
			return backendPlacement{}, fmt.Errorf("preferred Session %q is missing", near)
		}
	}
	return backendPlacement{group: hostSessionGroup, backend: backendHost}, nil
}

func (b *hostBackend) create(at backendPlacement, spec backendCreateSpec) (string, error) {
	client, err := b.conn(true)
	if err != nil {
		return "", fmt.Errorf("session host: %w", err)
	}
	id, err := newHostSessionID()
	if err != nil {
		return "", err
	}
	nonce, err := newOriginNonce()
	if err != nil {
		return "", fmt.Errorf("origin marker: %w", err)
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
	// The host adds MEWLA_SESSION_ID, which the Worker id comes from.
	env := spec.env
	ctx, cancel := hostCtx()
	defer cancel()
	// The Session's first output is its origin marker, as in a tmux pane; the
	// command runs under /bin/sh -c as tmux runs a pane command.
	_, err = client.Create(ctx, sessionhost.CreateParams{
		ID:     id,
		Argv:   []string{"/bin/sh", "-c", originMarkerCommand(nonce) + spec.command},
		Env:    env,
		Dir:    spec.cwd,
		Cols:   hostDefaultCols,
		Rows:   hostDefaultRows,
		Labels: labels,
	})
	if err != nil {
		return "", fmt.Errorf("start Worker Session: %w", err)
	}
	return id, nil
}

// kill removes the Session; one the host no longer has is not an error.
func (b *hostBackend) kill(id string) error {
	client, err := b.conn(false)
	if errors.Is(err, errSessionHostGone) {
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

const hostInputServer = "session-host"

func (io hostInputIO) socket(string) string { return hostInputServer }

func (io hostInputIO) pane(_ string, sessionID string) sessionInputPane {
	if !io.b.validID(sessionID) {
		return sessionInputPane{}
	}
	info, present, err := io.b.info(sessionID)
	if err != nil || !present || !info.Alive {
		return sessionInputPane{}
	}
	return sessionInputPane{alive: true, paneID: sessionID, generation: io.b.observedGeneration(sessionID)}
}

// submitPaste clears the draft, pastes the payload as one paste and submits
// it with the adapter's timing, all in one input script the host runs with
// nothing else in between, as the tmux command queue does.
func (io hostInputIO) submitPaste(_ string, paneID, payload string, adapter sessionInputProvider, beforeStart func() error) (bool, error) {
	steps := []sessionhost.InputStep{{Key: "C-u"}}
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
	steps = append(steps, sessionhost.InputStep{Key: adapter.submitKey})
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

func (io hostInputIO) receiptLedger(_ string, target string) (sessionInputReceiptLedger, error) {
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

func (io hostInputIO) writeReceiptLedger(_ string, target string, ledger sessionInputReceiptLedger) error {
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
