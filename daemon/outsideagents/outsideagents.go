// Package outsideagents lets an agent that Mewla did not launch, such as
// Claude Code or Codex in a terminal, hand work to Brain through an MCP door.
//
// An outside agent never reaches a Worker. Its request becomes a Brain turn,
// worded as relayed (not the user's own words), and Brain decides what to do.
// Each agent gets in once: the person approves it by name in the app or at
// this computer's terminal, and can revoke it at any time.
package outsideagents

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	// ApprovalTTL is how long a connection request waits for the person.
	ApprovalTTL = 5 * time.Minute
	// MaxPending bounds unanswered connection requests.
	MaxPending = 8
	// MaxAgents bounds approved agents.
	MaxAgents = 32
	// MaxRequests bounds the remembered hand-offs.
	MaxRequests = 200
	// MaxTextBytes bounds one request or follow-up.
	MaxTextBytes = 8 << 10
	// requestsPerHour caps one agent's hand-offs: a loop between an agent and
	// Brain must run out, not run forever.
	requestsPerHour = 30
	// TokenPrefix marks Mewla agent tokens, so a leaked one is recognizable.
	TokenPrefix = "mwa_"

	stateFile = "outside-agents.json"
)

// Pending statuses.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
	StatusExpired  = "expired"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrTooMany     = errors.New("too many pending agent requests; answer or wait for them to expire")
	ErrRateLimited = errors.New("this agent has sent too many requests in the last hour; wait before sending more")
	ErrTooLong     = errors.New("message is too long; send a short brief and let Brain ask")
)

// Pending is one agent asking to connect. The agent keeps the secret token and
// sends only its hash, so approval never moves a secret through the daemon.
type Pending struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Number    string    `json:"number"`
	TokenHash string    `json:"token_hash"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Agent is an approved outside agent.
type Agent struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"token_hash"`
	ApprovedAt time.Time  `json:"approved_at"`
	ApprovedBy string     `json:"approved_by,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Reply is something Brain told an outside agent about a request.
type Reply struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Request is one hand-off from an agent to Brain.
type Request struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	AgentName   string    `json:"agent_name"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	Disposition string    `json:"disposition"`
	Error       string    `json:"error,omitempty"`
	WorkID      string    `json:"work_id,omitempty"`
	Followups   int       `json:"followups,omitempty"`
	Replies     []Reply   `json:"replies,omitempty"`
}

type stored struct {
	Pending  []Pending `json:"pending"`
	Agents   []Agent   `json:"agents"`
	Requests []Request `json:"requests"`
}

// Manager owns the pending requests, the approved agents and the hand-offs.
type Manager struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	state stored
	// hits are one agent's recent calls that count toward its hourly cap.
	hits map[string][]time.Time
}

// New opens the state in stateDir. A missing file is an empty state.
func New(stateDir string) (*Manager, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, errors.New("state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{path: filepath.Join(stateDir, stateFile), now: time.Now, hits: map[string][]time.Time{}}
	raw, err := os.ReadFile(m.path)
	switch {
	case err == nil:
		if err := unmarshalState(raw, &m.state); err != nil {
			return nil, fmt.Errorf("read %s: %w", stateFile, err)
		}
	case !os.IsNotExist(err):
		return nil, err
	}
	return m, nil
}

// NewToken makes a secret an agent keeps and presents as a bearer token.
func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return TokenPrefix + hex.EncodeToString(raw), nil
}

// HashToken is what the daemon stores in place of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// CleanName makes a name safe to show and to put in a quoted attribute. It
// arrives from a local process, but the person approves it by this text.
func CleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '"' || r == '<' || r == '>':
			return -1
		case unicode.IsPrint(r):
			return r
		default:
			return ' '
		}
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	runes := []rune(name)
	if len(runes) > 48 {
		name = string(runes[:48])
	}
	return name
}

// Connect records an agent's request to connect and returns it. The number is
// what the person matches between this computer's terminal and the app.
func (m *Manager) Connect(name, tokenHash string) (Pending, error) {
	name = CleanName(name)
	if name == "" {
		return Pending{}, errors.New("the agent needs a name")
	}
	if len(tokenHash) != sha256.Size*2 {
		return Pending{}, errors.New("token hash is invalid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	open := 0
	for _, p := range m.state.Pending {
		if p.Status == StatusPending {
			open++
		}
		if p.TokenHash == tokenHash {
			return Pending{}, errors.New("this token was already used")
		}
	}
	if open >= MaxPending {
		return Pending{}, ErrTooMany
	}
	if len(m.state.Agents) >= MaxAgents {
		return Pending{}, fmt.Errorf("already %d agents; revoke one first", MaxAgents)
	}
	id, err := randomID("ag_")
	if err != nil {
		return Pending{}, err
	}
	number, err := m.freeNumberLocked()
	if err != nil {
		return Pending{}, err
	}
	now := m.now().UTC()
	pending := Pending{ID: id, Name: name, Number: number, TokenHash: tokenHash, Status: StatusPending, CreatedAt: now, ExpiresAt: now.Add(ApprovalTTL)}
	m.state.Pending = append(m.state.Pending, pending)
	if err := m.saveLocked(); err != nil {
		m.state.Pending = m.state.Pending[:len(m.state.Pending)-1]
		return Pending{}, err
	}
	return pending, nil
}

func (m *Manager) freeNumberLocked() (string, error) {
	used := map[string]bool{}
	for _, p := range m.state.Pending {
		if p.Status == StatusPending {
			used[p.Number] = true
		}
	}
	for attempt := 0; attempt < 50; attempt++ {
		raw := make([]byte, 2)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		number := fmt.Sprintf("%03d", (int(raw[0])<<8|int(raw[1]))%1000)
		if !used[number] {
			return number, nil
		}
	}
	return "", ErrTooMany
}

// Status reports a connection request, for the agent's side to poll.
func (m *Manager) Status(id string) (Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	for _, p := range m.state.Pending {
		if p.ID == id {
			return p, nil
		}
	}
	return Pending{}, ErrNotFound
}

// PendingList lists requests still waiting for the person.
func (m *Manager) PendingList() []Pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	list := []Pending{}
	for _, p := range m.state.Pending {
		if p.Status == StatusPending {
			list = append(list, p)
		}
	}
	return list
}

// Decide approves or denies a waiting request by id, or by the number it
// shows when id is empty. approvedBy names who decided (a device or the
// terminal).
func (m *Manager) Decide(id, number string, approve bool, approvedBy string) (Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	index := -1
	for i, p := range m.state.Pending {
		if p.Status != StatusPending {
			continue
		}
		if (id != "" && p.ID == id) || (id == "" && number != "" && p.Number == number) {
			if index >= 0 {
				return Pending{}, errors.New("two requests show that number; use the request id")
			}
			index = i
		}
	}
	if index < 0 {
		return Pending{}, ErrNotFound
	}
	pending := m.state.Pending[index]
	previousAgents := len(m.state.Agents)
	if approve {
		pending.Status = StatusApproved
		now := m.now().UTC()
		m.state.Agents = append(m.state.Agents, Agent{ID: pending.ID, Name: pending.Name, TokenHash: pending.TokenHash, ApprovedAt: now, ApprovedBy: CleanName(approvedBy)})
	} else {
		pending.Status = StatusDenied
	}
	previous := m.state.Pending[index]
	m.state.Pending[index] = pending
	if err := m.saveLocked(); err != nil {
		m.state.Pending[index] = previous
		m.state.Agents = m.state.Agents[:previousAgents]
		return Pending{}, err
	}
	return pending, nil
}

// Agents lists approved agents.
func (m *Manager) Agents() []Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Agent(nil), m.state.Agents...)
}

// Revoke removes an approved agent. Its token stops working at once.
func (m *Manager) Revoke(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.state.Agents {
		if a.ID == id {
			previous := append([]Agent(nil), m.state.Agents...)
			m.state.Agents = append(m.state.Agents[:i], m.state.Agents[i+1:]...)
			if err := m.saveLocked(); err != nil {
				m.state.Agents = previous
				return err
			}
			return nil
		}
	}
	return ErrNotFound
}

// Authenticate finds the approved agent a bearer token belongs to.
func (m *Manager) Authenticate(token string) (Agent, bool) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, TokenPrefix) {
		return Agent{}, false
	}
	hash := HashToken(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	found := -1
	for i, a := range m.state.Agents {
		// Compare every entry, so timing does not say which one matched.
		if subtle.ConstantTimeCompare([]byte(a.TokenHash), []byte(hash)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return Agent{}, false
	}
	now := m.now().UTC()
	last := m.state.Agents[found].LastUsedAt
	// Recording every call would rewrite the file for each tool call.
	if last == nil || now.Sub(*last) > time.Minute {
		m.state.Agents[found].LastUsedAt = &now
		_ = m.saveLocked()
	}
	return m.state.Agents[found], true
}

// Record stores a new hand-off from an agent after checking its limits. The
// caller submits it to Brain, then calls Finish.
func (m *Manager) Record(agent Agent, text string) (Request, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Request{}, errors.New("the request is empty")
	}
	if len(text) > MaxTextBytes {
		return Request{}, ErrTooLong
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.allowLocked(agent.ID) {
		return Request{}, ErrRateLimited
	}
	id, err := randomID("req_")
	if err != nil {
		return Request{}, err
	}
	request := Request{ID: id, AgentID: agent.ID, AgentName: agent.Name, Text: text, CreatedAt: m.now().UTC(), Disposition: "pending"}
	m.state.Requests = append(m.state.Requests, request)
	if len(m.state.Requests) > MaxRequests {
		m.state.Requests = m.state.Requests[len(m.state.Requests)-MaxRequests:]
	}
	if err := m.saveLocked(); err != nil {
		return Request{}, err
	}
	return request, nil
}

// allowLocked counts one call against the agent's hourly cap.
func (m *Manager) allowLocked(agentID string) bool {
	now := m.now()
	cutoff := now.Add(-time.Hour)
	recent := m.hits[agentID][:0]
	for _, at := range m.hits[agentID] {
		if at.After(cutoff) {
			recent = append(recent, at)
		}
	}
	if len(recent) >= requestsPerHour {
		m.hits[agentID] = recent
		return false
	}
	m.hits[agentID] = append(recent, now)
	return true
}

// Finish records how Brain's admission of a request ended.
func (m *Manager) Finish(id, disposition, failure string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.state.Requests {
		if m.state.Requests[i].ID == id {
			m.state.Requests[i].Disposition = disposition
			m.state.Requests[i].Error = failure
			return m.saveLocked()
		}
	}
	return ErrNotFound
}

// Followup counts a follow-up on one of the agent's own requests and returns
// the request and the receipt number for the Brain admission.
func (m *Manager) Followup(agent Agent, requestID, text string) (Request, int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Request{}, 0, errors.New("the message is empty")
	}
	if len(text) > MaxTextBytes {
		return Request{}, 0, ErrTooLong
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.allowLocked(agent.ID) {
		return Request{}, 0, ErrRateLimited
	}
	for i := range m.state.Requests {
		r := &m.state.Requests[i]
		if r.ID == requestID && r.AgentID == agent.ID {
			r.Followups++
			if err := m.saveLocked(); err != nil {
				r.Followups--
				return Request{}, 0, err
			}
			return *r, r.Followups, nil
		}
	}
	return Request{}, 0, ErrNotFound
}

// Request returns one of an agent's own hand-offs.
func (m *Manager) Request(agentID, id string) (Request, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.state.Requests {
		if r.ID == id && (agentID == "" || r.AgentID == agentID) {
			return r, nil
		}
	}
	return Request{}, ErrNotFound
}

// Requests lists an agent's hand-offs, newest first.
func (m *Manager) Requests(agentID string, limit int) []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := []Request{}
	for i := len(m.state.Requests) - 1; i >= 0; i-- {
		r := m.state.Requests[i]
		if agentID == "" || r.AgentID == agentID {
			list = append(list, r)
			if limit > 0 && len(list) >= limit {
				break
			}
		}
	}
	return list
}

// Reply is what Brain calls to answer an outside agent. A request can also be
// tied to the Work that carries it out.
func (m *Manager) Reply(id, text, workID string) (Request, error) {
	text = strings.TrimSpace(text)
	if text == "" && strings.TrimSpace(workID) == "" {
		return Request{}, errors.New("give a reply, a Work id, or both")
	}
	if len(text) > MaxTextBytes {
		return Request{}, ErrTooLong
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.state.Requests {
		r := &m.state.Requests[i]
		if r.ID != id {
			continue
		}
		previous := *r
		previous.Replies = append([]Reply(nil), r.Replies...)
		if text != "" {
			r.Replies = append(r.Replies, Reply{At: m.now().UTC(), Text: text})
			if len(r.Replies) > 20 {
				r.Replies = r.Replies[len(r.Replies)-20:]
			}
		}
		if w := strings.TrimSpace(workID); w != "" {
			r.WorkID = w
		}
		if err := m.saveLocked(); err != nil {
			*r = previous
			return Request{}, err
		}
		return *r, nil
	}
	return Request{}, ErrNotFound
}

func (m *Manager) expireLocked() {
	now := m.now()
	changed := false
	kept := m.state.Pending[:0]
	for _, p := range m.state.Pending {
		if p.Status == StatusPending && !now.Before(p.ExpiresAt) {
			p.Status = StatusExpired
			changed = true
		}
		// Decided and expired entries stay briefly so the agent can read how
		// its request ended.
		if p.Status != StatusPending && now.After(p.ExpiresAt.Add(10*time.Minute)) {
			changed = true
			continue
		}
		kept = append(kept, p)
	}
	m.state.Pending = kept
	if changed {
		_ = m.saveLocked()
	}
}

func (m *Manager) saveLocked() error {
	sort.SliceStable(m.state.Agents, func(i, j int) bool { return m.state.Agents[i].ApprovedAt.Before(m.state.Agents[j].ApprovedAt) })
	raw, err := marshalState(m.state)
	if err != nil {
		return err
	}
	return atomicfile.Write(m.path, raw, 0o600)
}

func randomID(prefix string) (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}
