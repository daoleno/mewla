package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/agentevents/opencode"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/google/uuid"
)

const (
	opencodeRetryMin = time.Second
	opencodeRetryMax = 30 * time.Second
)

// opencodeService is the part of the OpenCode background service the follower
// uses. *opencodeapi.Live is the real one.
type opencodeService interface {
	Stream(ctx context.Context, handle func(raw []byte)) error
	Prompt(ctx context.Context, sessionID, id, text string) (string, error)
	Interrupt(ctx context.Context, sessionID string) error
	Reply(ctx context.Context, sessionID, requestID, decision string) error
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	Active(ctx context.Context) (map[string]bool, error)
	Permissions(ctx context.Context, sessionID string) ([]opencodeapi.Permission, error)
	LastOutcome(ctx context.Context, sessionID string) (opencodeapi.Outcome, error)
}

// opencodeEvents follows every OpenCode Worker through the background service
// they share: one event stream for all of them, and ordinary API calls to send
// a message, answer a permission or stop a turn. It feeds the Hub.
type opencodeEvents struct {
	ctx      context.Context
	hub      *agentevents.Hub
	now      func() time.Time
	provider func(*classifier.Worker) string
	open     func(context.Context) (opencodeService, error)

	mu      sync.Mutex
	workers map[string]*opencodeWorker
	service opencodeService
	stop    context.CancelFunc
}

type opencodeWorker struct {
	id      string
	session string
	cancel  context.CancelFunc

	mu        sync.Mutex
	mapper    *opencode.Mapper
	connected bool
}

func newOpencodeEvents(ctx context.Context, hub *agentevents.Hub, provider func(*classifier.Worker) string) *opencodeEvents {
	return &opencodeEvents{
		ctx: ctx, hub: hub, now: time.Now, provider: provider,
		open:    func(ctx context.Context) (opencodeService, error) { return opencodeapi.OpenLive(ctx) },
		workers: map[string]*opencodeWorker{},
	}
}

// Observe follows the Watcher: an OpenCode Worker launched with a session id
// is followed from the moment it appears, and one that is gone is let go.
func (o *opencodeEvents) Observe(event watcher.SessionEvent) {
	switch event.Type {
	case "worker_discovered", "worker_state_change", "worker_metadata_change":
		w := event.Worker
		if w == nil || o.provider(w) != work.WorkerProviderOpenCode {
			return
		}
		if session, present, err := work.ProviderResumeToken(work.WorkerProviderOpenCode, w.Command); err == nil && present {
			o.follow(w.ID, session)
		}
	case "worker_removed":
		o.unfollow(event.WorkerID)
	}
}

func (o *opencodeEvents) follow(workerID, session string) {
	o.mu.Lock()
	if existing := o.workers[workerID]; existing != nil && existing.session == session {
		o.mu.Unlock()
		return
	} else if existing != nil {
		existing.cancel()
	}
	ctx, cancel := context.WithCancel(o.ctx)
	w := &opencodeWorker{id: workerID, session: session, cancel: cancel, mapper: opencode.NewMapper(workerID, session)}
	o.workers[workerID] = w
	service := o.service
	if o.stop == nil {
		var runCtx context.Context
		runCtx, o.stop = context.WithCancel(o.ctx)
		go o.run(runCtx)
	}
	o.mu.Unlock()

	go o.answers(ctx, w)
	if service != nil {
		go o.connectWorker(o.ctx, service, w)
	}
}

func (o *opencodeEvents) unfollow(workerID string) {
	o.mu.Lock()
	w := o.workers[workerID]
	delete(o.workers, workerID)
	var stop context.CancelFunc
	if len(o.workers) == 0 {
		stop, o.stop = o.stop, nil
	}
	o.mu.Unlock()
	if w != nil {
		w.cancel()
		o.hub.SetConnected(workerID, false)
	}
	if stop != nil {
		stop()
	}
}

func (o *opencodeEvents) worker(workerID string) *opencodeWorker {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.workers[workerID]
}

func (o *opencodeEvents) current() opencodeService {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.service
}

func (o *opencodeEvents) setService(s opencodeService) {
	o.mu.Lock()
	o.service = s
	o.mu.Unlock()
}

func (o *opencodeEvents) snapshot() []*opencodeWorker {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]*opencodeWorker, 0, len(o.workers))
	for _, w := range o.workers {
		out = append(out, w)
	}
	return out
}

// run keeps one connection to the service while any Worker is followed,
// reconnecting with a growing pause when it is down or drops.
func (o *opencodeEvents) run(ctx context.Context) {
	delay := opencodeRetryMin
	var lastLogged string
	for ctx.Err() == nil {
		service, err := o.open(ctx)
		if err == nil {
			started := o.now()
			o.setService(service)
			err = o.follow1(ctx, service)
			o.setService(nil)
			for _, w := range o.snapshot() {
				o.disconnect(w)
			}
			if o.now().Sub(started) > 10*time.Second {
				delay = opencodeRetryMin
			}
		}
		if err != nil && ctx.Err() == nil {
			// An absent OpenCode is the normal state of a machine without one:
			// say it once, not on every retry.
			if msg := err.Error(); msg != lastLogged {
				log.Printf("opencode events: %v (retrying)", err)
				lastLogged = msg
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay *= 2; delay > opencodeRetryMax {
			delay = opencodeRetryMax
		}
	}
}

// follow1 settles what happened while no stream was open, then streams.
func (o *opencodeEvents) follow1(ctx context.Context, service opencodeService) error {
	for _, w := range o.snapshot() {
		o.connectWorker(ctx, service, w)
	}
	return service.Stream(ctx, func(raw []byte) {
		frame, err := opencode.ParseFrame(raw)
		if err != nil {
			return
		}
		session := opencode.SessionOf(frame)
		if session == "" {
			return
		}
		for _, w := range o.snapshot() {
			if w.session != session {
				continue
			}
			w.mu.Lock()
			// The first word of a session launched but not yet prompted is
			// also how it is learned to exist.
			first := !w.connected
			var events []agentevents.Event
			if first {
				w.connected = true
				events = append(events, w.mapper.Bound(o.now().UTC()))
			}
			events = append(events, w.mapper.Map(frame, o.now().UTC())...)
			w.mu.Unlock()
			if first {
				o.hub.SetConnected(w.id, true)
			}
			for _, e := range events {
				o.hub.Publish(e)
			}
		}
	})
}

// connectWorker brings one Worker up to date with its session: bound if the
// session exists, a turn the Hub still holds as running settled if it ended
// while no stream was open, and permission requests made meanwhile listed.
func (o *opencodeEvents) connectWorker(ctx context.Context, service opencodeService, w *opencodeWorker) {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	exists, err := service.SessionExists(callCtx, w.session)
	if err != nil || !exists {
		return
	}
	at := o.now().UTC()
	var events []agentevents.Event
	w.mu.Lock()
	if !w.connected {
		w.connected = true
		events = append(events, w.mapper.Bound(at))
	}
	w.mu.Unlock()
	o.hub.SetConnected(w.id, true)

	if state, ok := o.hub.State(w.id); ok && state.Current != nil && state.Current.Status == agentevents.TurnRunning {
		if active, err := service.Active(callCtx); err == nil && !active[w.session] {
			if outcome, err := service.LastOutcome(callCtx, w.session); err == nil {
				events = append(events, settledTurn(w.id, state.Current.ID, outcome, at)...)
			}
		}
	}
	if pending, err := service.Permissions(callCtx, w.session); err == nil {
		w.mu.Lock()
		for _, p := range pending {
			events = append(events, w.mapper.Asked(p.ID, p.Action, p.Resources, at)...)
		}
		w.mu.Unlock()
	}
	for _, e := range events {
		o.hub.Publish(e)
	}
}

func (o *opencodeEvents) disconnect(w *opencodeWorker) {
	w.mu.Lock()
	w.connected = false
	w.mu.Unlock()
	o.hub.SetConnected(w.id, false)
}

// settledTurn ends a turn whose end the follower missed, from how the session
// says its latest execution ended.
func settledTurn(workerID, turnID string, outcome opencodeapi.Outcome, at time.Time) []agentevents.Event {
	done := agentevents.Event{WorkerID: workerID, Kind: agentevents.KindTurnFinished, At: at, TurnID: turnID}
	switch outcome.Status {
	case "succeeded":
		done.Status = agentevents.TurnCompleted
	case "interrupted":
		done.Status = agentevents.TurnInterrupted
	case "failed":
		done.Status = agentevents.TurnFailed
		done.Error = opencode.ErrorFrom(outcome.ErrorType, outcome.ErrorText)
	default:
		return nil
	}
	return []agentevents.Event{done}
}

// answers carries a decision made through Mewla to the service.
func (o *opencodeEvents) answers(ctx context.Context, w *opencodeWorker) {
	decided, cancel := o.hub.Subscribe(w.id)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-decided:
			if e.Kind != agentevents.KindApprovalResolved || e.By != "mewla" {
				continue
			}
			w.mu.Lock()
			known := w.mapper.Pending(e.ApprovalID)
			w.mapper.Settled(e.ApprovalID)
			w.mu.Unlock()
			reply, ok := opencode.Decision(e.OptionID)
			service := o.current()
			if !known || !ok || service == nil {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := service.Reply(callCtx, w.session, e.ApprovalID, reply); err != nil {
				log.Printf("opencode events: %s could not answer permission %s: %v", w.id, e.ApprovalID, err)
			}
			cancel()
		}
	}
}

// send submits a message to the Worker's session through the service. handled
// is false for a Worker whose session the service does not have yet (OpenCode
// creates it with the first prompt), which is typed into its pane instead.
// The service's own message id is derived from the receipt, so a retry of the
// same receipt is the same message to the service.
func (o *opencodeEvents) send(sessionID, payload, receipt string, begin func() error) (handled bool, err error) {
	w, service := o.worker(sessionID), o.current()
	if w == nil || service == nil {
		return false, nil
	}
	w.mu.Lock()
	connected := w.connected
	w.mu.Unlock()
	if !connected {
		return false, nil
	}
	if receipt == "" {
		receipt = uuid.NewString()
	}
	if err := begin(); err != nil {
		return true, err
	}
	ctx, cancel := context.WithTimeout(o.ctx, 15*time.Second)
	defer cancel()
	if _, err := service.Prompt(ctx, w.session, opencodeMessageID(receipt), payload); err != nil {
		return true, &watcher.StructuredInputError{Err: err, NotSent: opencodeapi.Rejected(err)}
	}
	return true, nil
}

// interrupt stops the Worker's running execution. A Worker with no running
// turn has nothing to stop.
func (o *opencodeEvents) interrupt(sessionID string) (handled bool, err error) {
	w, service := o.worker(sessionID), o.current()
	if w == nil || service == nil {
		return false, nil
	}
	w.mu.Lock()
	connected := w.connected
	w.mu.Unlock()
	if !connected {
		return false, nil
	}
	state, ok := o.hub.State(sessionID)
	if !ok || state.Current == nil || state.Current.Status != agentevents.TurnRunning {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(o.ctx, 10*time.Second)
	defer cancel()
	return true, service.Interrupt(ctx, w.session)
}

// opencodeMessageID is the service's message id for a receipt: OpenCode
// requires the "msg_" prefix and nothing else of a client's id.
func opencodeMessageID(receipt string) string {
	var b strings.Builder
	b.WriteString("msg_")
	for _, r := range receipt {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// firstHandled offers a send to each agent's follower in turn; the first to
// take the Worker handles it, and a Worker none takes is typed into its pane.
func firstHandled(senders ...watcher.StructuredSender) watcher.StructuredSender {
	return func(sessionID, payload, receipt string, begin func() error) (bool, error) {
		for _, send := range senders {
			if handled, err := send(sessionID, payload, receipt, begin); handled || err != nil {
				return handled, err
			}
		}
		return false, nil
	}
}

// firstHandledStop is firstHandled for a stop.
func firstHandledStop(stops ...func(string) (bool, error)) func(string) (bool, error) {
	return func(sessionID string) (bool, error) {
		for _, stop := range stops {
			if handled, err := stop(sessionID); handled || err != nil {
				return handled, err
			}
		}
		return false, nil
	}
}
