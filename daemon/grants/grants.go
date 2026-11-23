// Package grants runs the permission card: when Brain or a Worker needs a
// permission of this computer the person has not given (Screen recording,
// Accessibility, the Chrome extension), one card in Brain's thread asks for it,
// follows the grant until it counts, and seals. It tells Brain once how the
// card ended, so the Worker is started again with its tool, or not asked for
// again.
package grants

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/work"
)

// The permissions a card can ask for.
const (
	ScreenRecording = "screen_recording"
	Accessibility   = "accessibility"
	Chrome          = "chrome"
)

// Actions on a card (the app's buttons).
const (
	ActionOpenSettings = "open_settings"
	ActionNotNow       = "not_now"
	ActionRestart      = "restart"
)

// State is where a grant stands on this computer.
type State int

const (
	// Missing: not given.
	Missing State = iota
	// Granted and counting in this process.
	Granted
	// Stale: given, but this process does not see it yet (macOS answers an
	// Accessibility check from what the process started with).
	Stale
)

// Outcomes of Need.
const (
	OutcomeAsked          = "asked"
	OutcomeAlreadyAsked   = "already asked"
	OutcomeAlreadyGranted = "already granted"
)

// Errors an action reports, for the endpoint's status codes.
var (
	ErrUnknownPermission = errors.New("the permission is screen_recording, accessibility or chrome")
	ErrUnknownAction     = errors.New("the action is open_settings, not_now or restart")
	// ErrUnsupported: this computer has no such grant to ask for.
	ErrUnsupported = errors.New("this computer has no such permission to give")
	ErrNotFound    = brain.ErrGrantRequestNotFound
)

// ConflictError: the card is not where the action needs it.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

// Backend is this computer's grants.
type Backend interface {
	// Supported: this computer has that grant at all (Screen recording and
	// Accessibility are a Mac's).
	Supported(permission string) bool
	State(ctx context.Context, permission string) State
	// OpenSettings shows the exact pane (Chrome: opens Chrome to add the extension).
	OpenSettings(ctx context.Context, permission string) error
}

// Cards are the timeline cards and the lane to Brain (brain.Store and brain.Service).
type Cards interface {
	GrantRequests() ([]brain.TimelineItem, error)
	UpdateGrantRequest(id string, change func(*brain.TimelineItem) error) (brain.TimelineItem, error)
	AskGrant(permission, reason string, now time.Time) (brain.TimelineItem, error)
	NoteGrantAnswered(card brain.TimelineItem, line string) error
}

// Restarter makes a granted Accessibility count.
type Restarter interface {
	// Quiet: nothing is in flight that a restart would cut (a model reply, a
	// held approval, a message to Brain).
	Quiet() bool
	// InPlace restarts the daemon in this process's place (Sessions and Workers keep running).
	InPlace() error
	// Process stops the daemon so its service manager starts it again, or
	// restarts it in place when nothing supervises it.
	Process() error
}

// Manager owns the cards.
type Manager struct {
	backend   Backend
	cards     Cards
	restarter Restarter
	// Where names this computer in what Brain is told ("this Mac").
	Where string

	now func() time.Time
	// Poll is how often an open card checks its grant.
	Poll time.Duration
	// Suppress is how long a runtime-made card is not asked again after Not now.
	Suppress time.Duration
	// QuietWait is how long a granted Accessibility waits for a quiet moment
	// to restart itself before the card offers the restart button.
	QuietWait time.Duration
	afterFunc func(time.Duration, func())

	mu         sync.Mutex
	wake       chan struct{}
	staleSince map[string]time.Time
	// reexecAt is when this process asked to restart itself in place: until it
	// has gone (or a minute has passed) the card is left as it is.
	reexecAt time.Time
	// following are the grants the person is giving at this computer now,
	// without a card (the Mac app's panel), until when they are watched.
	following map[string]time.Time
	// followTried: this process already restarted (or tried) for a followed
	// grant; a second try is the person's (RestartNow).
	followTried map[string]bool
	// FollowFor is how long a followed grant is watched.
	FollowFor time.Duration
}

// New makes the manager; Run drives it.
func New(backend Backend, cards Cards, restarter Restarter) *Manager {
	return &Manager{
		backend: backend, cards: cards, restarter: restarter, Where: "this computer",
		now: time.Now, Poll: 2 * time.Second, Suppress: 6 * time.Hour, QuietWait: 90 * time.Second,
		afterFunc: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
		wake:      make(chan struct{}, 1), staleSince: map[string]time.Time{},
		following: map[string]time.Time{}, followTried: map[string]bool{}, FollowFor: 15 * time.Minute,
	}
}

// Follow is the person giving a grant at this computer now, from the Mac app's
// panel rather than a card: until it counts (or FollowFor passes) it is
// watched like a card, and once given but not counting in this process, Mewla
// restarts itself in place at a quiet moment to apply it.
func (m *Manager) Follow(permission string) error {
	if permission != ScreenRecording && permission != Accessibility {
		return ErrUnknownPermission
	}
	if !m.backend.Supported(permission) {
		return ErrUnsupported
	}
	m.mu.Lock()
	m.following[permission] = m.now().Add(m.FollowFor)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// RestartNow is the person's Restart Mewla: the process starts again (under
// launchd, a new process), after the answer is out.
func (m *Manager) RestartNow() error {
	m.afterFunc(300*time.Millisecond, func() {
		if err := m.restarter.Process(); err != nil {
			log.Printf("grants: restart: %v", err)
		}
	})
	return nil
}

// followed are the grants still watched, oldest first by name.
func (m *Manager) followed() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for id, until := range m.following {
		if m.now().After(until) {
			delete(m.following, id)
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// checkFollowed applies one followed grant: done once it counts; once given
// but not counting, restarted in place at a quiet moment, once.
func (m *Manager) checkFollowed(ctx context.Context, permission string) {
	switch m.backend.State(ctx, permission) {
	case Granted:
		m.mu.Lock()
		delete(m.following, permission)
		m.mu.Unlock()
	case Stale:
		m.mu.Lock()
		tried := m.followTried[permission]
		restarting := !m.reexecAt.IsZero() && m.now().Sub(m.reexecAt) < time.Minute
		m.mu.Unlock()
		if tried || restarting || !m.restarter.Quiet() {
			return
		}
		m.mu.Lock()
		m.followTried[permission] = true
		m.reexecAt = m.now()
		m.mu.Unlock()
		log.Printf("grants: %s was just allowed but is not counting yet; restarting in place to apply it", permission)
		if err := m.restarter.InPlace(); err != nil {
			log.Printf("grants: restart in place: %v", err)
			m.mu.Lock()
			m.reexecAt = time.Time{}
			m.mu.Unlock()
		}
	}
}

// Valid says whether permission is one a card can ask for.
func Valid(permission string) bool {
	return permission == ScreenRecording || permission == Accessibility || permission == Chrome
}

var wording = map[string]struct{ name, tool string }{
	ScreenRecording: {"Screen recording", "the screen"},
	Accessibility:   {"Accessibility", "the screen"},
	Chrome:          {"Chrome", "the browser"},
}

// Result is what Need did.
type Result struct {
	Outcome string
	Card    *brain.TimelineItem
}

// Need is `mewla agent-use need`: a card for the permission unless it already
// counts or a card for it is already open.
func (m *Manager) Need(ctx context.Context, permission, reason string) (Result, error) {
	if !Valid(permission) {
		return Result{}, ErrUnknownPermission
	}
	return m.ask(ctx, permission, reason, false)
}

// Runtime is a Worker launch that went without a tool only because a grant is
// missing: the same card, at most one per grant, none for a grant the person
// declined lately. It never blocks the launch.
func (m *Manager) Runtime(permission, reason string) {
	if !Valid(permission) {
		return
	}
	go func() {
		if _, err := m.ask(context.Background(), permission, reason, true); err != nil && !errors.Is(err, ErrUnsupported) {
			log.Printf("grants: card for %s: %v", permission, err)
		}
	}()
}

func (m *Manager) ask(ctx context.Context, permission, reason string, runtime bool) (Result, error) {
	if !m.backend.Supported(permission) {
		return Result{}, ErrUnsupported
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.backend.State(ctx, permission) == Granted {
		return Result{Outcome: OutcomeAlreadyGranted}, nil
	}
	existing, err := m.cards.GrantRequests()
	if err != nil {
		return Result{}, err
	}
	for _, card := range existing {
		if card.GrantPermission != permission {
			continue
		}
		if brain.GrantOpen(card.Status) {
			return Result{Outcome: OutcomeAlreadyAsked, Card: &card}, nil
		}
		if runtime && card.Status == brain.GrantDeclined && card.GrantSealedAt != nil && m.now().Sub(*card.GrantSealedAt) < m.Suppress {
			return Result{Outcome: OutcomeAlreadyAsked, Card: &card}, nil
		}
	}
	card, err := m.cards.AskGrant(permission, reason, m.now())
	if err != nil {
		return Result{}, err
	}
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return Result{Outcome: OutcomeAsked, Card: &card}, nil
}

// Act is a button of the card.
func (m *Manager) Act(ctx context.Context, id, action string) (work.CodexConversationEvent, error) {
	card, err := m.find(id)
	if err != nil {
		return work.CodexConversationEvent{}, err
	}
	switch action {
	case ActionOpenSettings:
		if card.Status != brain.GrantAsked && card.Status != brain.GrantOpened {
			return work.CodexConversationEvent{}, &ConflictError{"This request is " + card.Status + "."}
		}
		if err := m.backend.OpenSettings(ctx, card.GrantPermission); err != nil {
			return work.CodexConversationEvent{}, err
		}
		card, err = m.cards.UpdateGrantRequest(id, func(item *brain.TimelineItem) error {
			if item.Status == brain.GrantAsked {
				item.Status = brain.GrantOpened
			}
			return nil
		})
	case ActionNotNow:
		card, err = m.seal(id, brain.GrantDeclined)
	case ActionRestart:
		if card.Status != brain.GrantRestart {
			return work.CodexConversationEvent{}, &ConflictError{"Mewla does not need to restart for this request (" + card.Status + ")."}
		}
		// After the answer is out: the process ends.
		m.afterFunc(300*time.Millisecond, func() {
			if err := m.restarter.Process(); err != nil {
				log.Printf("grants: restart: %v", err)
			}
		})
	default:
		return work.CodexConversationEvent{}, ErrUnknownAction
	}
	if err != nil {
		return work.CodexConversationEvent{}, err
	}
	return brain.GrantRequestEvent(card), nil
}

func (m *Manager) find(id string) (brain.TimelineItem, error) {
	cards, err := m.cards.GrantRequests()
	if err != nil {
		return brain.TimelineItem{}, err
	}
	for _, card := range cards {
		if card.ID == id {
			return card, nil
		}
	}
	return brain.TimelineItem{}, ErrNotFound
}

// seal ends an open card, once: Brain is told the first time only.
func (m *Manager) seal(id, status string) (brain.TimelineItem, error) {
	var sealedNow bool
	card, err := m.cards.UpdateGrantRequest(id, func(item *brain.TimelineItem) error {
		if !brain.GrantOpen(item.Status) {
			return &ConflictError{"This request is " + item.Status + "."}
		}
		at := m.now().UTC()
		item.Status, item.GrantSealedAt = status, &at
		sealedNow = true
		return nil
	})
	if err != nil || !sealedNow {
		return card, err
	}
	m.mu.Lock()
	delete(m.staleSince, id)
	m.mu.Unlock()
	if err := m.cards.NoteGrantAnswered(card, m.line(card)); err != nil {
		log.Printf("grants: tell Brain about %s: %v", card.ID, err)
	}
	return card, nil
}

func (m *Manager) line(card brain.TimelineItem) string {
	words := wording[card.GrantPermission]
	if card.Status == brain.GrantGranted {
		return fmt.Sprintf("%s is now allowed on %s. You can start the Worker again with %s.", words.name, m.Where, words.tool)
	}
	return fmt.Sprintf("The person chose Not now for %s: stop asking for this task.", words.name)
}

// Run follows open cards until ctx ends: every Poll while one is open, not at
// all while none is (a new card wakes it).
func (m *Manager) Run(ctx context.Context) {
	for {
		open := m.open()
		followed := m.followed()
		if len(open) == 0 && len(followed) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-m.wake:
				continue
			}
		}
		for _, card := range open {
			m.check(ctx, card)
		}
		for _, permission := range followed {
			m.checkFollowed(ctx, permission)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.Poll):
		}
	}
}

func (m *Manager) open() []brain.TimelineItem {
	cards, err := m.cards.GrantRequests()
	if err != nil {
		log.Printf("grants: read cards: %v", err)
		return nil
	}
	var open []brain.TimelineItem
	for _, card := range cards {
		if brain.GrantOpen(card.Status) {
			open = append(open, card)
		}
	}
	return open
}

// check follows one open card's grant.
func (m *Manager) check(ctx context.Context, card brain.TimelineItem) {
	switch m.backend.State(ctx, card.GrantPermission) {
	case Granted:
		if _, err := m.seal(card.ID, brain.GrantGranted); err != nil {
			var conflict *ConflictError
			if !errors.As(err, &conflict) {
				log.Printf("grants: seal %s: %v", card.ID, err)
			}
		}
	case Stale:
		m.stale(card)
	}
}

// stale: the person gave it, but this process does not count it yet. Mewla
// first tries to make it count by itself: once, at a quiet moment, in place.
// If it still does not count after that (or no quiet moment comes), the card
// offers the restart.
func (m *Manager) stale(card brain.TimelineItem) {
	m.mu.Lock()
	since, seen := m.staleSince[card.ID]
	if !seen {
		since = m.now()
		m.staleSince[card.ID] = since
	}
	m.mu.Unlock()
	if card.GrantRestartTried {
		m.mu.Lock()
		restarting := !m.reexecAt.IsZero() && m.now().Sub(m.reexecAt) < time.Minute
		m.mu.Unlock()
		if !restarting {
			m.setStatus(card.ID, brain.GrantRestart)
		}
		return
	}
	if !m.restarter.Quiet() {
		if m.now().Sub(since) >= m.QuietWait {
			m.setStatus(card.ID, brain.GrantRestart)
		}
		return
	}
	if _, err := m.cards.UpdateGrantRequest(card.ID, func(item *brain.TimelineItem) error {
		item.GrantRestartTried = true
		return nil
	}); err != nil {
		log.Printf("grants: note the restart of %s: %v", card.ID, err)
		return
	}
	log.Printf("grants: %s is allowed but not counting yet; restarting in place to apply it", card.GrantPermission)
	m.mu.Lock()
	m.reexecAt = m.now()
	m.mu.Unlock()
	if err := m.restarter.InPlace(); err != nil {
		log.Printf("grants: restart in place: %v", err)
		m.mu.Lock()
		m.reexecAt = time.Time{}
		m.mu.Unlock()
		m.setStatus(card.ID, brain.GrantRestart)
	}
}

func (m *Manager) setStatus(id, status string) {
	_, err := m.cards.UpdateGrantRequest(id, func(item *brain.TimelineItem) error {
		if !brain.GrantOpen(item.Status) || item.Status == status {
			return errNoChange
		}
		item.Status = status
		return nil
	})
	if err != nil && !errors.Is(err, errNoChange) {
		log.Printf("grants: set %s %s: %v", id, status, err)
	}
}

var errNoChange = errors.New("no change")
