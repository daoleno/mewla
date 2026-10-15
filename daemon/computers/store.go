package computers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

// outboundPeer is a computer this one paired with and dials.
type outboundPeer struct {
	Name            string    `json:"name"`
	DaemonID        string    `json:"daemon_id"`
	DaemonPublicKey string    `json:"daemon_public_key"`
	URLs            []string  `json:"urls"`
	DeviceID        string    `json:"device_id"`
	ClientSeedHex   string    `json:"client_seed_hex"`
	AddedAt         time.Time `json:"added_at"`
}

type outboxItem struct {
	PeerKey       string    `json:"peer_key"`
	Message       Message   `json:"message"`
	State         string    `json:"state"`
	Disposition   string    `json:"disposition,omitempty"`
	Error         string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
}

// handoff remembers a message this computer's Brain sent, for its
// conversation: outbox items go once delivered, the card stays.
type handoff struct {
	ID        string    `json:"id"`
	Thread    string    `json:"thread"`
	PeerKey   string    `json:"peer_key"`
	Computer  string    `json:"computer"`
	Kind      string    `json:"kind"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	UserSaid  string    `json:"user_said,omitempty"`
	Text      string    `json:"text"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type persisted struct {
	Peers    []outboundPeer `json:"peers"`
	Outbox   []outboxItem   `json:"outbox"`
	Handoffs []handoff      `json:"handoffs,omitempty"`
}

const (
	stateFile      = "computers.json"
	keepFinished   = 7 * 24 * time.Hour
	maxOutboxItems = 500
	maxHandoffs    = 300
	handoffText    = 2000
	giveUpAfter    = 24 * time.Hour
)

func loadState(dir string) (persisted, error) {
	raw, err := os.ReadFile(filepath.Join(dir, stateFile))
	if errors.Is(err, os.ErrNotExist) {
		return persisted{}, nil
	}
	if err != nil {
		return persisted{}, err
	}
	var state persisted
	if err := json.Unmarshal(raw, &state); err != nil {
		return persisted{}, fmt.Errorf("decode %s: %w", stateFile, err)
	}
	return state, nil
}

// saveState writes the peers (with their client keys) and the outbox. The
// file holds credentials, so it is private to this user.
func saveState(dir string, state persisted) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	syncHandoffs(&state)
	pruneOutbox(&state, time.Now().UTC())
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, stateFile), payload, 0o600)
}

// syncHandoffs copies each sent message's delivery onto its card and keeps
// the newest cards.
func syncHandoffs(state *persisted) {
	states := make(map[string]string, len(state.Outbox))
	for _, item := range state.Outbox {
		states[item.Message.ID] = item.State
	}
	for i := range state.Handoffs {
		if current, ok := states[state.Handoffs[i].ID]; ok {
			state.Handoffs[i].State = current
		}
	}
	if extra := len(state.Handoffs) - maxHandoffs; extra > 0 {
		state.Handoffs = append([]handoff(nil), state.Handoffs[extra:]...)
	}
}

func pruneOutbox(state *persisted, now time.Time) {
	kept := state.Outbox[:0]
	for _, item := range state.Outbox {
		finished := item.State == StateDelivered || item.State == StateUncertain || item.State == StateFailed
		if finished && now.Sub(item.UpdatedAt) > keepFinished {
			continue
		}
		kept = append(kept, item)
	}
	state.Outbox = kept
	if len(state.Outbox) > maxOutboxItems {
		sort.SliceStable(state.Outbox, func(i, j int) bool {
			return state.Outbox[i].UpdatedAt.Before(state.Outbox[j].UpdatedAt)
		})
		state.Outbox = state.Outbox[len(state.Outbox)-maxOutboxItems:]
	}
}
