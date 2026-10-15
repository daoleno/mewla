package computers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/gorilla/websocket"
)

// Timings, shortened by tests.
var (
	// sendsPerHour caps messages to one computer: a loop between two Brains
	// must fail loudly instead of running up both sides' bills.
	sendsPerHour = 30

	retryPending      = 10 * time.Second
	retryNotSubmitted = 30 * time.Second
	sendTick          = 5 * time.Second
	redialMin         = 2 * time.Second
)

const maxFrameBytes = 1 << 20

// DeliverFunc hands one envelope to this computer's Brain with an idempotent
// receipt and reports a brain.ExternalInputDisposition value.
type DeliverFunc func(receipt, body string) (string, error)

type Options struct {
	StateDir string
	Auth     *auth.Manager
	Deliver  DeliverFunc
	Version  string
	// Name is how this computer introduces itself; the hostname by default.
	Name string
	// Addresses lists the URLs other computers may use to reach this one.
	Addresses func() []string
	Caps      func() Caps
	// Thread names the Brain conversation a message is sent from, so its
	// card shows there.
	Thread func() string
}

type Manager struct {
	opts Options
	dir  string

	mu       sync.Mutex
	state    persisted
	conns    map[string]*peerConn
	hellos   map[string]Hello
	lastSeen map[string]time.Time
	lastErr  map[string]string
	loops    map[string]context.CancelFunc
	runCtx   context.Context
	inflight map[string]bool
	// running tracks every live channel and outbound loop so Run returns
	// only after they stop touching the state file.
	running sync.WaitGroup

	unsubscribe func()
}

type peerConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	url     string
	wake    chan struct{}
}

func (c *peerConn) write(f frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return c.ws.WriteJSON(f)
}

func (c *peerConn) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func NewManager(opts Options) (*Manager, error) {
	if opts.Auth == nil {
		return nil, errors.New("computers need the daemon identity")
	}
	dir := opts.StateDir
	if strings.TrimSpace(dir) == "" {
		dir = opts.Auth.StorageDir()
	}
	dir = dir + "/computers"
	state, err := loadState(dir)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		opts: opts, dir: dir, state: state,
		conns: map[string]*peerConn{}, hellos: map[string]Hello{},
		lastSeen: map[string]time.Time{}, lastErr: map[string]string{},
		loops: map[string]context.CancelFunc{}, inflight: map[string]bool{},
	}
	// A message cut off by a restart is sent again; the receiving Brain's
	// receipt makes the resend harmless.
	for index := range m.state.Outbox {
		if m.state.Outbox[index].State == StateSent {
			m.state.Outbox[index].State = StateQueued
		}
	}
	m.unsubscribe = opts.Auth.SubscribeRevocations(m.forgetInbound)
	return m, nil
}

// Run keeps a channel open to every outbound peer until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.runCtx = ctx
	for _, peer := range m.state.Peers {
		m.startLoopLocked(peer)
	}
	m.mu.Unlock()
	<-ctx.Done()
	if m.unsubscribe != nil {
		m.unsubscribe()
	}
	// Channels are added under mu only while ctx is live, so none can start
	// after this point.
	m.mu.Lock()
	m.mu.Unlock()
	m.running.Wait()
}

func outboundKey(daemonID string) string { return "out:" + daemonID }
func inboundKey(deviceID string) string  { return "in:" + deviceID }

func (m *Manager) startLoopLocked(peer outboundPeer) {
	if m.runCtx == nil {
		return
	}
	key := outboundKey(peer.DaemonID)
	if _, running := m.loops[key]; running {
		return
	}
	ctx, cancel := context.WithCancel(m.runCtx)
	m.loops[key] = cancel
	m.running.Add(1)
	go func() {
		defer m.running.Done()
		m.outboundLoop(ctx, peer.DaemonID)
	}()
}

func (m *Manager) outboundLoop(ctx context.Context, daemonID string) {
	key := outboundKey(daemonID)
	backoff := redialMin
	for ctx.Err() == nil {
		m.mu.Lock()
		peer, ok := m.outboundPeerLocked(daemonID)
		m.mu.Unlock()
		if !ok {
			return
		}
		ws, remote, url, err := dial(ctx, peer, m.hello())
		if err != nil {
			m.setError(key, err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = redialMin
		m.serve(ctx, key, &peerConn{ws: ws, url: url, wake: make(chan struct{}, 1)}, remote)
		if !sleepCtx(ctx, redialMin) {
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	// Another daemon is not a browser; a browser page cannot sign as a
	// computer-kind device anyway.
	CheckOrigin: func(*http.Request) bool { return true },
}

// ServeInbound runs the channel for a computer-kind device that dialed in.
// The caller has already verified the device's signature for /computer.
func (m *Manager) ServeInbound(w http.ResponseWriter, r *http.Request, device *auth.TrustedDevice) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	var first frame
	if err := ws.ReadJSON(&first); err != nil || first.Type != "hello" || first.Hello == nil {
		ws.Close()
		return
	}
	_ = ws.SetReadDeadline(time.Time{})
	conn := &peerConn{ws: ws, wake: make(chan struct{}, 1)}
	if first.Hello.Protocol != ProtocolVersion {
		_ = conn.write(frame{Type: "error", Error: versionMismatch(ProtocolVersion, m.opts.Version).Error()})
		ws.Close()
		return
	}
	hello := m.hello()
	assertion, err := m.opts.Auth.CreateServerAssertion(helloPurpose)
	if err != nil {
		ws.Close()
		return
	}
	hello.Assertion = &assertion
	if err := conn.write(frame{Type: "hello", Hello: &hello}); err != nil {
		ws.Close()
		return
	}
	// A hijacked connection outlives the HTTP server, so the channel also
	// ends with Run.
	m.mu.Lock()
	runCtx := m.runCtx
	if runCtx == nil || runCtx.Err() != nil {
		m.mu.Unlock()
		ws.Close()
		return
	}
	m.running.Add(1)
	m.mu.Unlock()
	defer m.running.Done()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(runCtx, cancel)
	defer stop()
	m.serve(ctx, inboundKey(device.ID), conn, *first.Hello)
}

func (m *Manager) hello() Hello {
	caps := Caps{}
	if m.opts.Caps != nil {
		caps = m.opts.Caps()
	}
	return Hello{
		Protocol: ProtocolVersion,
		Version:  m.opts.Version,
		Name:     m.name(),
		DaemonID: m.opts.Auth.DaemonID(),
		Caps:     caps,
	}
}

func (m *Manager) name() string {
	if name := strings.TrimSpace(m.opts.Name); name != "" {
		return name
	}
	return "computer"
}

// serve owns one live channel: it sends due outbox items for this peer and
// delivers what the peer sends to this computer's Brain.
func (m *Manager) serve(ctx context.Context, key string, conn *peerConn, remote Hello) {
	m.mu.Lock()
	if old := m.conns[key]; old != nil {
		old.ws.Close()
	}
	m.conns[key] = conn
	m.hellos[key] = remote
	m.lastSeen[key] = time.Now().UTC()
	delete(m.lastErr, key)
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	// Closing the socket is what unblocks the read below.
	stopClose := context.AfterFunc(ctx, func() { conn.ws.Close() })
	defer stopClose()
	var workers sync.WaitGroup
	defer func() {
		cancel()
		conn.ws.Close()
		workers.Wait()
		m.mu.Lock()
		if m.conns[key] == conn {
			delete(m.conns, key)
		}
		m.lastSeen[key] = time.Now().UTC()
		m.requeueInflightLocked(key)
		m.mu.Unlock()
	}()

	workers.Add(2)
	go func() {
		defer workers.Done()
		m.sendLoop(ctx, key, conn)
	}()
	conn.ws.SetPongHandler(func(string) error {
		_ = conn.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	inbox := make(chan Message, 32)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-inbox:
				result := m.deliver(key, message)
				if err := conn.write(frame{Type: "result", Result: &result}); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	for {
		_ = conn.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		var incoming frame
		if err := conn.ws.ReadJSON(&incoming); err != nil {
			return
		}
		m.mu.Lock()
		m.lastSeen[key] = time.Now().UTC()
		m.mu.Unlock()
		switch incoming.Type {
		case "message":
			if incoming.Message == nil {
				continue
			}
			select {
			case inbox <- *incoming.Message:
			case <-ctx.Done():
				return
			}
		case "result":
			if incoming.Result != nil {
				m.applyResult(key, *incoming.Result)
				conn.poke()
			}
		}
	}
}

func (m *Manager) sendLoop(ctx context.Context, key string, conn *peerConn) {
	ticker := time.NewTicker(sendTick)
	defer ticker.Stop()
	pings := 0
	for {
		for _, message := range m.claimDue(key) {
			if err := conn.write(frame{Type: "message", Message: &message}); err != nil {
				conn.ws.Close()
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-conn.wake:
		case <-ticker.C:
			pings++
			if pings%6 == 0 {
				conn.writeMu.Lock()
				err := conn.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
				conn.writeMu.Unlock()
				if err != nil {
					conn.ws.Close()
					return
				}
			}
		}
	}
}

// claimDue marks every due item for key as sent and returns its messages.
func (m *Manager) claimDue(key string) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var out []Message
	changed := false
	for index := range m.state.Outbox {
		item := &m.state.Outbox[index]
		if item.PeerKey != key || item.State != StateQueued || now.Before(item.NextAttemptAt) {
			continue
		}
		if now.Sub(item.Message.CreatedAt) > giveUpAfter {
			item.State, item.Error, item.UpdatedAt = StateFailed, "not delivered within a day", now
			changed = true
			continue
		}
		item.State, item.UpdatedAt = StateSent, now
		m.inflight[item.Message.ID] = true
		out = append(out, item.Message)
		changed = true
	}
	if changed {
		m.saveLocked()
	}
	return out
}

func (m *Manager) requeueInflightLocked(key string) {
	changed := false
	for index := range m.state.Outbox {
		item := &m.state.Outbox[index]
		if item.PeerKey == key && item.State == StateSent {
			item.State = StateQueued
			delete(m.inflight, item.Message.ID)
			changed = true
		}
	}
	if changed {
		m.saveLocked()
	}
}

// applyResult follows the Brain's delivery answer. The same receipt always
// returns its first answer, so resending with it never delivers twice; an
// uncertain delivery is never resent, like a Telegram message.
func (m *Manager) applyResult(key string, result DeliveryResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for index := range m.state.Outbox {
		item := &m.state.Outbox[index]
		if item.PeerKey != key || item.Message.ID != result.ID || item.Message.Attempt != result.Attempt {
			continue
		}
		delete(m.inflight, item.Message.ID)
		item.Disposition, item.Error, item.UpdatedAt = result.Disposition, result.Error, now
		switch result.Disposition {
		case DispositionAccepted:
			item.State = StateDelivered
		case DispositionUncertain:
			item.State = StateUncertain
		case DispositionPending:
			item.State, item.NextAttemptAt = StateQueued, now.Add(retryPending)
		default:
			// Not submitted: the other Brain was unavailable. A fresh receipt
			// lets the next attempt be a new submission.
			item.State, item.NextAttemptAt = StateQueued, now.Add(retryNotSubmitted)
			item.Message.Attempt++
		}
		m.saveLocked()
		return
	}
}

// deliver hands a peer's message to this computer's Brain.
func (m *Manager) deliver(key string, message Message) DeliveryResult {
	result := DeliveryResult{ID: message.ID, Attempt: message.Attempt}
	if m.opts.Deliver == nil || strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.Text) == "" {
		result.Disposition = DispositionNotSubmitted
		result.Error = "Brain is not available on this computer"
		return result
	}
	m.mu.Lock()
	from := m.peerNameLocked(key)
	m.mu.Unlock()
	receipt := fmt.Sprintf("computer:%s:%s:%d", key, message.ID, message.Attempt)
	disposition, err := m.opts.Deliver(receipt, Envelope(from, message))
	result.Disposition = disposition
	if err != nil {
		result.Error = err.Error()
	}
	if result.Disposition == "" {
		result.Disposition = DispositionNotSubmitted
	}
	return result
}

// Envelope is the text the receiving Brain reads. The sender is a relay of
// your other Brain, not you; your own words travel separately in user_said.
func Envelope(from string, message Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<mewla_computer_message from=%q id=%q kind=%q", cleanAttr(from), cleanAttr(message.ID), cleanAttr(message.Kind))
	if message.ReplyTo != "" {
		fmt.Fprintf(&b, " reply_to=%q", cleanAttr(message.ReplyTo))
	}
	b.WriteString(">\n")
	if message.UserSaid != "" {
		b.WriteString("<user_said>" + cleanBody(message.UserSaid) + "</user_said>\n")
	}
	b.WriteString(cleanBody(message.Text))
	b.WriteString("\n</mewla_computer_message>\n")
	switch message.Kind {
	case KindRequest, KindQuestion, KindCancel:
		fmt.Fprintf(&b, "Answer once with: mewla computers send %q --reply-to %s --kind reply|question \"...\"", cleanAttr(from), cleanAttr(message.ID))
	default:
		// Acknowledging a reply would start a loop between two Brains.
		b.WriteString("This needs no reply unless it asks you something.")
	}
	return b.String()
}

func cleanAttr(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '<', '>', '\n', '\r':
			return -1
		}
		return r
	}, strings.TrimSpace(value))
}

func cleanBody(value string) string {
	value = strings.ReplaceAll(value, "</mewla_computer_message", "&lt;/mewla_computer_message")
	value = strings.ReplaceAll(value, "<mewla_computer_message", "&lt;mewla_computer_message")
	value = strings.ReplaceAll(value, "</user_said", "&lt;/user_said")
	return strings.TrimSpace(value)
}

func (m *Manager) setError(key string, err error) {
	m.mu.Lock()
	m.lastErr[key] = err.Error()
	m.mu.Unlock()
}

func (m *Manager) saveLocked() {
	if err := saveState(m.dir, m.state); err != nil {
		log.Printf("computers: save state: %v", err)
	}
}

func (m *Manager) outboundPeerLocked(daemonID string) (outboundPeer, bool) {
	for _, peer := range m.state.Peers {
		if peer.DaemonID == daemonID {
			return peer, true
		}
	}
	return outboundPeer{}, false
}

func (m *Manager) peerNameLocked(key string) string {
	if strings.HasPrefix(key, "out:") {
		if peer, ok := m.outboundPeerLocked(strings.TrimPrefix(key, "out:")); ok {
			return peer.Name
		}
	}
	if strings.HasPrefix(key, "in:") {
		id := strings.TrimPrefix(key, "in:")
		for _, device := range m.opts.Auth.ListDevices() {
			if device.ID == id {
				return device.Name
			}
		}
	}
	return key
}

// forgetInbound drops a revoked computer's channel and undelivered messages.
func (m *Manager) forgetInbound(deviceID string) {
	key := inboundKey(deviceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn := m.conns[key]; conn != nil {
		conn.ws.Close()
	}
	m.dropOutboxLocked(key)
}

func (m *Manager) dropOutboxLocked(key string) {
	kept := m.state.Outbox[:0]
	for _, item := range m.state.Outbox {
		if item.PeerKey == key && (item.State == StateQueued || item.State == StateSent) {
			continue
		}
		kept = append(kept, item)
	}
	m.state.Outbox = kept
	m.saveLocked()
}

// peers lists every computer this one can message.
func (m *Manager) peersLocked() []PeerView {
	var views []PeerView
	for _, peer := range m.state.Peers {
		key := outboundKey(peer.DaemonID)
		view := PeerView{Name: peer.Name, DaemonID: peer.DaemonID, Direction: DirectionOutbound}
		m.fillLiveLocked(key, &view)
		views = append(views, view)
	}
	for _, device := range m.opts.Auth.ListDevices() {
		if device.Kind != auth.DeviceKindComputer {
			continue
		}
		key := inboundKey(device.ID)
		view := PeerView{Name: device.Name, DeviceID: device.ID, Direction: DirectionInbound}
		if !device.LastSeenAt.IsZero() {
			seen := device.LastSeenAt
			view.LastSeen = &seen
		}
		m.fillLiveLocked(key, &view)
		views = append(views, view)
	}
	sort.SliceStable(views, func(i, j int) bool { return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name) })
	return views
}

func (m *Manager) fillLiveLocked(key string, view *PeerView) {
	if conn := m.conns[key]; conn != nil {
		view.Online = true
		view.URL = conn.url
	}
	if hello, ok := m.hellos[key]; ok {
		caps := hello.Caps
		view.Caps = &caps
		view.Version = hello.Version
		view.Protocol = hello.Protocol
		if view.DaemonID == "" {
			view.DaemonID = hello.DaemonID
		}
	}
	if seen, ok := m.lastSeen[key]; ok {
		seenCopy := seen
		view.LastSeen = &seenCopy
	}
	view.LastError = m.lastErr[key]
	for _, item := range m.state.Outbox {
		if item.PeerKey == key && (item.State == StateQueued || item.State == StateSent) {
			view.Queued++
		}
	}
}

// resolveLocked finds a computer by name, then by a unique name prefix, then
// by daemon or device id prefix.
func (m *Manager) resolveLocked(name string) (string, PeerView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", PeerView{}, errors.New("name the computer; see mewla computers list")
	}
	views := m.peersLocked()
	keyOf := func(view PeerView) string {
		if view.Direction == DirectionOutbound {
			return outboundKey(view.DaemonID)
		}
		return inboundKey(view.DeviceID)
	}
	lower := strings.ToLower(name)
	var prefix []PeerView
	for _, view := range views {
		if strings.ToLower(view.Name) == lower {
			return keyOf(view), view, nil
		}
		if strings.HasPrefix(strings.ToLower(view.Name), lower) ||
			(len(lower) >= 6 && (strings.HasPrefix(view.DaemonID, lower) || strings.HasPrefix(view.DeviceID, lower))) {
			prefix = append(prefix, view)
		}
	}
	if len(prefix) == 1 {
		return keyOf(prefix[0]), prefix[0], nil
	}
	if len(prefix) > 1 {
		return "", PeerView{}, fmt.Errorf("%q matches more than one computer", name)
	}
	return "", PeerView{}, fmt.Errorf("no computer named %q; see mewla computers list", name)
}

func newMessageID() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	return "m_" + hex.EncodeToString(raw)
}

// Handle serves the control socket and the app's admin routes.
func (m *Manager) Handle(ctx context.Context, req Request) (Result, error) {
	switch strings.TrimSpace(req.Action) {
	case "list", "":
		m.mu.Lock()
		defer m.mu.Unlock()
		return Result{Computers: m.peersLocked()}, nil
	case "send":
		return m.send(req)
	case "status":
		return m.status(req)
	case "invite":
		return m.invite()
	case "add":
		return m.add(ctx, req)
	case "remove":
		return m.remove(req)
	}
	return Result{}, fmt.Errorf("unknown computers action %q", req.Action)
}

func (m *Manager) send(req Request) (Result, error) {
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = KindRequest
		if strings.TrimSpace(req.ReplyTo) != "" {
			kind = KindReply
		}
	}
	switch kind {
	case KindRequest, KindReply, KindQuestion, KindAnswer, KindCancel:
	default:
		return Result{}, fmt.Errorf("kind must be request, reply, question, answer or cancel")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Result{}, errors.New("message text is empty")
	}
	if len(text) > 64<<10 || len(req.UserSaid) > 16<<10 {
		return Result{}, errors.New("message is too long; send a short brief and let the other Brain ask")
	}
	thread := ""
	if m.opts.Thread != nil {
		thread = m.opts.Thread()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key, view, err := m.resolveLocked(req.Computer)
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	recent := 0
	for _, existing := range m.state.Outbox {
		if existing.PeerKey == key && now.Sub(existing.Message.CreatedAt) < time.Hour {
			recent++
		}
	}
	if recent >= sendsPerHour {
		return Result{}, fmt.Errorf("already %d messages to %s in the last hour; this looks like a loop between the two Brains, so stop and tell the user", recent, view.Name)
	}
	item := outboxItem{
		PeerKey: key,
		Message: Message{
			ID: newMessageID(), Kind: kind, ReplyTo: strings.TrimSpace(req.ReplyTo),
			UserSaid: strings.TrimSpace(req.UserSaid), Text: text, CreatedAt: now, Attempt: 1,
		},
		State: StateQueued, UpdatedAt: now, NextAttemptAt: now,
	}
	m.state.Outbox = append(m.state.Outbox, item)
	if thread != "" {
		m.state.Handoffs = append(m.state.Handoffs, handoff{
			ID: item.Message.ID, Thread: thread, PeerKey: key, Computer: view.Name, Kind: kind,
			ReplyTo: item.Message.ReplyTo, UserSaid: clip(item.Message.UserSaid), Text: clip(text),
			State: StateQueued, CreatedAt: now,
		})
	}
	if err := saveState(m.dir, m.state); err != nil {
		m.state.Outbox = m.state.Outbox[:len(m.state.Outbox)-1]
		if thread != "" {
			m.state.Handoffs = m.state.Handoffs[:len(m.state.Handoffs)-1]
		}
		return Result{}, err
	}
	if conn := m.conns[key]; conn != nil {
		conn.poke()
	}
	out := outboxView(item, view.Name)
	return Result{Message: &out, Computer: &view}, nil
}

func outboxView(item outboxItem, name string) OutboxView {
	return OutboxView{
		ID: item.Message.ID, Computer: name, Kind: item.Message.Kind, ReplyTo: item.Message.ReplyTo,
		State: item.State, Disposition: item.Disposition, Error: item.Error, Attempt: item.Message.Attempt,
		CreatedAt: item.Message.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func (m *Manager) status(req Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(req.ID)
	var views []OutboxView
	for _, item := range m.state.Outbox {
		if id != "" && item.Message.ID != id {
			continue
		}
		views = append(views, outboxView(item, m.peerNameLocked(item.PeerKey)))
	}
	if id != "" {
		if len(views) == 0 {
			return Result{}, fmt.Errorf("no message %s", id)
		}
		return Result{Message: &views[0]}, nil
	}
	return Result{Messages: views}, nil
}

// invite issues a one-time invite another computer uses to pair with this
// one as a computer-kind device.
func (m *Manager) invite() (Result, error) {
	token, err := m.opts.Auth.IssuePairingTokenForKind(auth.DefaultPairingTTL, auth.DeviceKindComputer)
	if err != nil {
		return Result{}, err
	}
	var urls []string
	if m.opts.Addresses != nil {
		urls = usableURLs(m.opts.Addresses())
	}
	invite := Invite{
		V: 1, DaemonID: m.opts.Auth.DaemonID(), DaemonPublicKey: m.opts.Auth.PublicKeyHex(),
		Name: m.name(), URLs: urls, Token: token.Value, ExpiresAt: token.ExpiresAt,
	}
	invite.Signature = m.opts.Auth.CreateComputerInviteSignature(invite.signedPayload())
	expires := token.ExpiresAt
	return Result{Invite: EncodeInvite(invite), ExpiresAt: &expires}, nil
}

// add pairs with the computer an invite names and keeps a channel open to it.
func (m *Manager) add(ctx context.Context, req Request) (Result, error) {
	invite, err := DecodeInvite(req.Invite, time.Now())
	if err != nil {
		return Result{}, err
	}
	if expected := strings.ToLower(strings.TrimSpace(req.ExpectedDaemonID)); expected != "" && expected != invite.DaemonID {
		return Result{}, errors.New("the invite is from a different computer than expected")
	}
	if invite.DaemonID == m.opts.Auth.DaemonID() {
		return Result{}, errors.New("this invite is from this computer itself")
	}
	var candidates []string
	for _, raw := range req.URLs {
		if parsed, err := checkTransport(raw); err == nil {
			candidates = append(candidates, parsed.Scheme+"://"+parsed.Host)
		}
	}
	candidates = append(candidates, invite.URLs...)
	if len(candidates) == 0 {
		return Result{}, errors.New("the invite has no address this computer may use; add a Tailscale or HTTPS address on the other computer (mewla address add)")
	}
	seed, err := newClientSeed()
	if err != nil {
		return Result{}, err
	}
	key, _ := clientKey(seed)
	ourID := m.opts.Auth.DaemonID()
	deviceID := "computer-" + ourID[:16]
	var reached string
	var lastErr error
	for _, base := range candidates {
		if _, err := checkTransport(base); err != nil {
			lastErr = err
			continue
		}
		pairCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		lastErr = pair(pairCtx, base, invite, deviceID, m.name(), key)
		cancel()
		if lastErr == nil {
			reached = base
			break
		}
	}
	if reached == "" {
		return Result{}, fmt.Errorf("could not reach %s: %v", invite.Name, lastErr)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = invite.Name
	}
	urls := []string{reached}
	for _, candidate := range candidates {
		if candidate != reached {
			urls = append(urls, candidate)
		}
	}
	peer := outboundPeer{
		Name: name, DaemonID: invite.DaemonID, DaemonPublicKey: invite.DaemonPublicKey, URLs: urls,
		DeviceID: deviceID, ClientSeedHex: seed, AddedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	replaced := false
	for index := range m.state.Peers {
		if m.state.Peers[index].DaemonID == peer.DaemonID {
			if cancel := m.loops[outboundKey(peer.DaemonID)]; cancel != nil {
				cancel()
				delete(m.loops, outboundKey(peer.DaemonID))
			}
			m.state.Peers[index] = peer
			replaced = true
		}
	}
	if !replaced {
		m.state.Peers = append(m.state.Peers, peer)
	}
	if err := saveState(m.dir, m.state); err != nil {
		return Result{}, err
	}
	m.startLoopLocked(peer)
	view := PeerView{Name: peer.Name, DaemonID: peer.DaemonID, Direction: DirectionOutbound}
	m.fillLiveLocked(outboundKey(peer.DaemonID), &view)
	return Result{Computer: &view}, nil
}

// remove forgets a computer. An inbound computer's key is revoked here; an
// outbound one stops being dialed (the other computer still lists this one
// until it is revoked there). With ExpectedDaemonID it forgets that computer
// in both directions, and finding nothing is not an error: the app removes a
// pair from each side, and either side may already be done.
func (m *Manager) remove(req Request) (Result, error) {
	if daemonID := strings.ToLower(strings.TrimSpace(req.ExpectedDaemonID)); daemonID != "" {
		return m.forget(daemonID)
	}
	m.mu.Lock()
	key, view, err := m.resolveLocked(req.Computer)
	if err != nil {
		m.mu.Unlock()
		return Result{}, err
	}
	if view.Direction == DirectionInbound {
		m.mu.Unlock()
		if _, err := m.opts.Auth.RevokeDevice(view.DeviceID); err != nil {
			return Result{}, err
		}
		return Result{Computer: &view}, nil
	}
	defer m.mu.Unlock()
	m.removeOutboundLocked(key)
	return Result{Computer: &view}, nil
}

// forget removes every entry for one computer: the one this computer dials
// and the key that computer uses to reach this one.
func (m *Manager) forget(daemonID string) (Result, error) {
	device := ""
	if len(daemonID) >= 16 {
		device = "computer-" + daemonID[:16]
	}
	m.mu.Lock()
	var removed, inbound []PeerView
	for _, view := range m.peersLocked() {
		switch {
		case view.Direction == DirectionOutbound && view.DaemonID == daemonID:
			m.removeOutboundLocked(outboundKey(view.DaemonID))
			removed = append(removed, view)
		case view.Direction == DirectionInbound && (view.DaemonID == daemonID || (device != "" && view.DeviceID == device)):
			inbound = append(inbound, view)
		}
	}
	m.mu.Unlock()
	for _, view := range inbound {
		if _, err := m.opts.Auth.RevokeDevice(view.DeviceID); err != nil {
			return Result{}, err
		}
		removed = append(removed, view)
	}
	return Result{Computers: removed}, nil
}

func (m *Manager) removeOutboundLocked(key string) {
	if cancel := m.loops[key]; cancel != nil {
		cancel()
		delete(m.loops, key)
	}
	if conn := m.conns[key]; conn != nil {
		conn.ws.Close()
	}
	kept := m.state.Peers[:0]
	for _, peer := range m.state.Peers {
		if outboundKey(peer.DaemonID) != key {
			kept = append(kept, peer)
		}
	}
	m.state.Peers = kept
	// Its cards say what never arrived.
	for i, h := range m.state.Handoffs {
		if h.PeerKey == key && (h.State == StateQueued || h.State == StateSent) {
			m.state.Handoffs[i].State = StateFailed
		}
	}
	m.dropOutboxLocked(key)
}
