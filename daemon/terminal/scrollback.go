package terminal

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/sessionhost"
)

// HistoryCursor is where a viewer stands in a Session's scrollback: rows are
// numbered from the first the daemon saw of this epoch, and Next is the
// number of the row after the last one the viewer holds.
type HistoryCursor struct {
	Epoch uint64 `json:"epoch"`
	Next  uint64 `json:"next"`
}

// Row flags in HistoryDelta.Flags.
const (
	RowWrapped      = 1 << iota // soft-wrapped onto the next row
	RowContinuation             // continues the row above
	RowPrompt                   // starts a shell prompt (OSC 133)
	RowPromptMore               // continues a shell prompt
)

// HistoryDelta is the part of a host Session's scrollback a viewer is
// missing, with the modes that decide what a swipe means. It is the answer to
// terminal_history when the request names a cursor.
type HistoryDelta struct {
	PaneID    string `json:"pane_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
	Alternate bool   `json:"alternate"`
	Mouse     bool   `json:"mouse"`
	// AltScroll and AppCursor are left out when the session host predates
	// them: unknown is not off.
	AltScroll *bool `json:"alt_scroll,omitempty"`
	AppCursor *bool `json:"app_cursor,omitempty"`

	Epoch uint64 `json:"epoch"`
	// Reset says the viewer's rows are void: Lines replace them.
	Reset bool `json:"reset"`
	// First is the number of the oldest row the Session still keeps, From the
	// number of Lines[0], and Next the number after the last row.
	First uint64 `json:"first"`
	From  uint64 `json:"from"`
	Next  uint64 `json:"next"`
	// Lines are styled rows, oldest first. Flags, when present, has the Row*
	// flags of each line.
	Lines []string `json:"lines"`
	Flags []int    `json:"flags,omitempty"`
}

type scrollbackProvider interface {
	HistoryFrom(cursor HistoryCursor) (HistoryDelta, error)
}

// HistoryFrom answers terminal_history for a viewer that holds the rows up to
// cursor. Sessions without a numbered scrollback (tmux) do not offer it.
func (m *Manager) HistoryFrom(ownerID, sessionID string, cursor HistoryCursor) (HistoryDelta, bool, error) {
	ms, err := m.withSession(ownerID, sessionID)
	if err != nil {
		return HistoryDelta{}, false, err
	}
	provider, ok := ms.session.(scrollbackProvider)
	if !ok {
		return HistoryDelta{}, false, nil
	}
	delta, err := provider.HistoryFrom(cursor)
	return delta, true, err
}

// scrollbackTracker numbers the rows of a Session's history. The host hands
// out its history whole; a row that scrolls off the screen takes the next
// number, and the rows that stay keep theirs, found by matching what this
// capture shares with the last one. Rows of equal text are told apart by
// their position, so a shift among identical rows changes nothing a viewer
// can see.
type scrollbackTracker struct {
	mu     sync.Mutex
	epoch  uint64
	start  uint64 // number of hashes[0]
	cols   int
	hashes []uint64
}

var trackerEpochs = struct {
	sync.Mutex
	last uint64
}{last: uint64(time.Now().UnixMilli())}

// nextEpoch is unique within the process and differs between daemon runs, so
// a cursor from another run or another view never matches. It stays below
// 2^53, which is what a JSON number in the App can hold.
func nextEpoch() uint64 {
	trackerEpochs.Lock()
	defer trackerEpochs.Unlock()
	trackerEpochs.last++
	return trackerEpochs.last
}

func hashRow(row string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(row))
	return h.Sum64()
}

// overlap is the longest run at the end of known that starts next.
func overlap(known, next []uint64) int {
	if len(known) == 0 || len(next) == 0 {
		return 0
	}
	prefix := make([]int, len(next))
	for i, j := 1, 0; i < len(next); i++ {
		for j > 0 && next[i] != next[j] {
			j = prefix[j-1]
		}
		if next[i] == next[j] {
			j++
		}
		prefix[i] = j
	}
	matched := 0
	for _, h := range known {
		for matched > 0 && (matched == len(next) || h != next[matched]) {
			matched = prefix[matched-1]
		}
		if h == next[matched] {
			matched++
		}
	}
	return matched
}

// advance takes a capture's rows and returns the number of lines[0], and
// whether the numbering started over because nothing of the last capture is
// left in it. Callers hold t.mu.
func (t *scrollbackTracker) advance(cols int, lines []string) (start uint64, reset bool) {
	hashes := make([]uint64, len(lines))
	for i, line := range lines {
		hashes[i] = hashRow(line)
	}
	keep := 0
	if t.epoch != 0 && t.cols == cols {
		keep = overlap(t.hashes, hashes)
	}
	switch {
	case t.epoch == 0 || t.cols != cols || (keep == 0 && len(t.hashes) > 0 && len(hashes) > 0):
		t.epoch, t.start, reset = nextEpoch(), 0, true
	case len(hashes) == 0 && len(t.hashes) > 0:
		t.epoch, t.start, reset = nextEpoch(), 0, true
	default:
		t.start += uint64(len(t.hashes) - keep)
	}
	t.cols, t.hashes = cols, hashes
	return t.start, reset
}

// HistoryFrom serves the viewer the rows after cursor. A viewer whose cursor
// is not this tracker's, or points before the oldest row kept, starts over
// with every row.
func (s *hostSession) HistoryFrom(cursor HistoryCursor) (HistoryDelta, error) {
	client, err := s.started()
	if err != nil {
		return HistoryDelta{}, err
	}
	t := &s.scrollback
	t.mu.Lock()
	defer t.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	capture, err := client.Capture(ctx, sessionhost.CaptureParams{ID: s.targetID, Scope: sessionhost.ScopeHistory,
		History: historyLineLimit, Styled: true, Marks: true})
	if err != nil {
		return HistoryDelta{}, fmt.Errorf("capture Session history: %w", err)
	}
	delta := HistoryDelta{PaneID: s.targetID, Cols: capture.Cols, Rows: capture.Rows,
		Alternate: capture.Alt, Mouse: capture.MouseTracking, Lines: []string{}}
	if capture.Modes {
		altScroll, appCursor := capture.AltScroll, capture.AppCursor
		delta.AltScroll, delta.AppCursor = &altScroll, &appCursor
	}
	if capture.Alt {
		// The alternate screen has no scrollback, and the primary one stays as
		// it was until the program leaves. A viewer in step keeps its rows; one
		// from another epoch starts over with none, and takes what comes next.
		end := t.start + uint64(len(t.hashes))
		delta.Epoch, delta.First = t.epoch, t.start
		if cursor.Epoch == t.epoch && cursor.Next <= end {
			delta.From, delta.Next = cursor.Next, cursor.Next
		} else {
			delta.Reset, delta.First, delta.From, delta.Next = true, end, end, end
		}
		return delta, nil
	}
	var lines []string
	if capture.ScrollbackRows > 0 && capture.Text != "" {
		lines = strings.Split(strings.ReplaceAll(capture.Text, "\r\n", "\n"), "\n")
	}
	if len(lines) > historyLineLimit {
		lines = lines[len(lines)-historyLineLimit:]
	}
	size := 0
	for _, line := range lines {
		size += len(line)
	}
	if size > historyByteLimit {
		return HistoryDelta{}, fmt.Errorf("pane history exceeds 4 MiB")
	}
	var flags []int
	if len(capture.RowFlags) == len(lines) && len(lines) > 0 {
		flags = make([]int, len(lines))
		for i, f := range capture.RowFlags {
			flags[i] = int(f)
		}
	}
	start, reset := t.advance(capture.Cols, lines)
	next := start + uint64(len(lines))
	delta.Epoch, delta.First, delta.Next = t.epoch, start, next
	switch {
	case reset || cursor.Epoch != t.epoch || cursor.Next < start || cursor.Next > next:
		delta.Reset, delta.From = true, start
		delta.Lines, delta.Flags = nonNil(lines), flags
	default:
		skip := int(cursor.Next - start)
		delta.From = cursor.Next
		delta.Lines = nonNil(lines[skip:])
		if flags != nil {
			delta.Flags = flags[skip:]
		}
	}
	return delta, nil
}

func nonNil(lines []string) []string {
	if lines == nil {
		return []string{}
	}
	return lines
}
