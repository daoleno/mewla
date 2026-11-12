// Package computers lets one Mewla computer hand work to another. The channel
// carries requests, replies and questions between local owners: each computer runs
// its own Brain, Workers, files and logged-in apps, and only the request, its
// questions and its results cross the network.
package computers

import (
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

// ProtocolVersion is exchanged in hello. A peer with a different version is
// told which side to update and receives no messages.
const ProtocolVersion = 1

// Message kinds. The receiving Brain reads the kind; Mewla only routes it.
const (
	KindRequest  = "request"
	KindReply    = "reply"
	KindQuestion = "question"
	KindAnswer   = "answer"
	KindCancel   = "cancel"
)

// Outbox states.
const (
	StateQueued    = "queued"
	StateSent      = "sent"
	StateDelivered = "delivered"
	StateUncertain = "uncertain"
	StateFailed    = "failed"
)

// Delivery dispositions, matching brain.ExternalInputDisposition.
const (
	DispositionAccepted     = "accepted"
	DispositionNotSubmitted = "not_submitted"
	DispositionUncertain    = "uncertain"
	DispositionPending      = "pending"
)

// Peer directions: this computer dials an outbound peer; an inbound peer is a
// computer that paired with this one and dials in.
const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"
)

type Message struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	UserSaid  string    `json:"user_said,omitempty"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
	Attempt   int       `json:"attempt"`
}

// Caps is what a computer reports about itself so the other Brain can decide
// where a task belongs.
type Caps struct {
	OS          string   `json:"os"`
	Arch        string   `json:"arch"`
	Hostname    string   `json:"hostname,omitempty"`
	Agents      []string `json:"agents,omitempty"`
	Apps        []string `json:"apps,omitempty"`
	ComputerUse bool     `json:"computer_use"`
	BrowserUse  bool     `json:"browser_use"`
	// PhoneUse is set only while the person has Phone use on.
	PhoneUse bool `json:"phone_use,omitempty"`
}

type Hello struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	Name     string `json:"name"`
	DaemonID string `json:"daemon_id"`
	Caps     Caps   `json:"caps"`
	// OpenRequests is how many requests on this computer wait for the person
	// (Options.OpenRequests). Absent from a computer that predates it, which
	// is "unknown", never zero.
	OpenRequests *int                  `json:"open_requests,omitempty"`
	Assertion    *auth.ServerAssertion `json:"assertion,omitempty"`
}

type DeliveryResult struct {
	ID          string `json:"id"`
	Attempt     int    `json:"attempt"`
	Disposition string `json:"disposition"`
	Error       string `json:"error,omitempty"`
}

type frame struct {
	Type    string          `json:"type"`
	Hello   *Hello          `json:"hello,omitempty"`
	Message *Message        `json:"message,omitempty"`
	Result  *DeliveryResult `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Origin is local routing authority. It is never sent to another computer.
// PaneGeneration prevents a recycled tmux target from inheriting a reply.
type Origin struct {
	SessionID      string `json:"session_id,omitempty"`
	PaneGeneration string `json:"pane_generation,omitempty"`
	WorkID         string `json:"work_id,omitempty"`
}

// Delivery separates the permanent message identity from an admission attempt.
type Delivery struct {
	Identity string `json:"identity"`
	Receipt  string `json:"receipt"`
	Body     string `json:"body"`
	Origin   Origin `json:"origin"`
}

// Request is the control-socket and admin-route request.
type Request struct {
	SenderSession string   `json:"sender_session,omitempty"`
	Action        string   `json:"action"` // list, send, status, invite, add, remove
	Computer      string   `json:"computer,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	ReplyTo       string   `json:"reply_to,omitempty"`
	UserSaid      string   `json:"user_said,omitempty"`
	Text          string   `json:"text,omitempty"`
	ID            string   `json:"id,omitempty"`
	Invite        string   `json:"invite,omitempty"`
	Name          string   `json:"name,omitempty"`
	URLs          []string `json:"urls,omitempty"`
	// ExpectedDaemonID pins the invite to the computer the caller already
	// trusts (the app passes the key it pinned when it paired).
	ExpectedDaemonID string `json:"expected_daemon_id,omitempty"`
}

type Result struct {
	Computers []PeerView   `json:"computers,omitempty"`
	Computer  *PeerView    `json:"computer,omitempty"`
	Message   *OutboxView  `json:"message,omitempty"`
	Messages  []OutboxView `json:"messages,omitempty"`
	Invite    string       `json:"invite,omitempty"`
	ExpiresAt *time.Time   `json:"expires_at,omitempty"`
}

type PeerView struct {
	Name      string     `json:"name"`
	DaemonID  string     `json:"daemon_id,omitempty"`
	DeviceID  string     `json:"device_id,omitempty"`
	Direction string     `json:"direction"`
	Online    bool       `json:"online"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	Version   string     `json:"version,omitempty"`
	Protocol  int        `json:"protocol,omitempty"`
	Caps      *Caps      `json:"caps,omitempty"`
	URL       string     `json:"url,omitempty"`
	Queued    int        `json:"queued,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	// OpenRequests is the count the computer last reported over its live
	// channel; only while Online. Absent when offline or not reported.
	OpenRequests *int `json:"open_requests,omitempty"`
}

type OutboxView struct {
	ID          string    `json:"id"`
	Computer    string    `json:"computer"`
	Kind        string    `json:"kind"`
	ReplyTo     string    `json:"reply_to,omitempty"`
	State       string    `json:"state"`
	Disposition string    `json:"disposition,omitempty"`
	Error       string    `json:"error,omitempty"`
	Attempt     int       `json:"attempt"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
