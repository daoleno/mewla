package phone

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AuditFile is the name of the audit log in the state directory.
const AuditFile = "phone-agent-audit.jsonl"

// AuditMaxAge is how long audit lines are kept.
const AuditMaxAge = 30 * 24 * time.Hour

// Audit is the log of what agents did on the phone: which action, on what, where. It never holds
// a screenshot, what an agent read, or text an agent typed beyond its length and a fingerprint.
type Audit struct {
	dir    string
	now    func() time.Time
	mu     sync.Mutex
	pruned bool
}

func NewAudit(stateDir string) *Audit { return &Audit{dir: stateDir, now: time.Now} }

type auditLine struct {
	Time       time.Time `json:"time"`
	Worker     string    `json:"worker"`
	Action     string    `json:"action"`
	App        string    `json:"app,omitempty"`
	Element    int       `json:"element,omitempty"`
	Label      string    `json:"label,omitempty"`
	X          *int      `json:"x,omitempty"`
	Y          *int      `json:"y,omitempty"`
	X2         *int      `json:"x2,omitempty"`
	Y2         *int      `json:"y2,omitempty"`
	Direction  string    `json:"direction,omitempty"`
	Key        string    `json:"key,omitempty"`
	TextLength int       `json:"text_length,omitempty"`
	TextHash   string    `json:"text_sha256,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// trace is what an action turned out to touch, beyond what the agent asked.
type trace struct {
	app     string
	element *Element
	x, y    *int
	x2, y2  *int
	key     string
}

func intp(v int) *int { return &v }

func (t *trace) point(x, y int) { t.x, t.y = intp(x), intp(y) }

const (
	auditLabelRunes = 40
	auditErrorRunes = 200
)

// Record appends one line. A failure to write is not the agent's problem.
func (a *Audit) Record(worker string, act AgentAction, t trace, actErr error) {
	if a == nil {
		return
	}
	line := auditLine{Time: a.now().UTC(), Worker: worker, Action: act.Action, App: t.app, Direction: act.Direction, Key: t.key, X: t.x, Y: t.y, X2: t.x2, Y2: t.y2}
	if line.App == "" {
		line.App = act.App
	}
	if e := t.element; e != nil {
		line.Element = e.ID
		line.Label = auditLabel(*e)
	}
	if act.Action == "type" {
		sum := sha256.Sum256([]byte(act.Text))
		line.TextLength, line.TextHash = len([]rune(act.Text)), hex.EncodeToString(sum[:8])
	}
	if actErr != nil {
		// The phone's own words, so kept short.
		line.Error = actErr.Error()
		if r := []rune(line.Error); len(r) > auditErrorRunes {
			line.Error = string(r[:auditErrorRunes]) + "…"
		}
	}
	raw, err := json.Marshal(line)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path := filepath.Join(a.dir, AuditFile)
	if !a.pruned {
		a.pruned = true
		pruneAudit(path, a.now().Add(-AuditMaxAge))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(raw, '\n'))
}

// auditLabel names an element the way a person would point at it. A field's text is the
// person's, so a field is named by its description or hint only.
func auditLabel(e Element) string {
	label := e.Desc
	if e.Flags&FlagEditable != 0 {
		if label == "" {
			label = e.Hint
		}
	} else if e.Text != "" {
		label = e.Text
	}
	if r := []rune(label); len(r) > auditLabelRunes {
		label = strings.TrimSpace(string(r[:auditLabelRunes])) + "…"
	}
	return label
}

func pruneAudit(path string, before time.Time) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var entry auditLine
		if len(line) == 0 || json.Unmarshal(line, &entry) != nil || entry.Time.Before(before) {
			continue
		}
		kept = append(append(kept, line...), '\n')
	}
	if os.WriteFile(path, kept, 0o600) == nil {
		_ = os.Chmod(path, 0o600)
	}
}
