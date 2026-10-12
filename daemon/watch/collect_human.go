package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// Human signals are the user's turns in Brain and Worker transcripts, each
// with the assistant message it answered. Turns Brain wrote for a Worker are
// kept apart as "delegated_message"; Mewla's own envelopes are skipped
// because the Process source already records them, as are provider-written
// records such as compaction summaries.

const (
	kindUserMessage      = "user_message"
	kindDelegatedMessage = "delegated_message"
	humanExcerptRunes    = 600
	answeredExcerptRunes = 300
)

type humanCursor struct {
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Answered  string `json:"answered,omitempty"`
	// Worker is set once Mewla has delegated a turn into this conversation.
	// Its other user turns may be the user typing or an unmarked Brain
	// follow-up, so they carry session_role=worker for later judgment.
	Worker bool `json:"worker,omitempty"`
}

// transcriptSource is the subset of work's transcript API the collector uses.
type transcriptSource struct {
	discover func(home string, since time.Time) ([]work.TranscriptSource, error)
	scan     func(source work.TranscriptSource, offset int64, visit func(work.TranscriptMessage), checkpoint func(work.TranscriptScan) error) (work.TranscriptScan, error)
}

var defaultTranscripts = transcriptSource{discover: work.DiscoverTranscripts, scan: work.ScanTranscript}

var turnTokenPattern = regexp.MustCompile(`turn:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

var mewlaEnvelopePattern = regexp.MustCompile(`<[a-z]+_(?:work_event|calendar_result)>`)

// Provider-written user-role records, such as Claude's summary that opens a
// compacted conversation.
var providerWrittenPrefixes = []string{
	"This session is being continued from a previous conversation",
}

// classifyUserTurn decides who wrote a user-role message: the user, Brain or
// Mewla delegating to a Worker (kept as delegated), or Mewla and the provider
// talking to themselves (skipped).
func classifyUserTurn(text string, admitted bool) (kind string, keep bool) {
	// Envelopes come first: a Work event quotes its Work's delegation prompt.
	if mewlaEnvelopePattern.MatchString(text) {
		return "", false
	}
	if admitted || delegationText(text) {
		return kindDelegatedMessage, true
	}
	if work.IsPrivateHostPrompt(text) || strings.HasPrefix(text, watcher.TurnContinuationPrompt) {
		return "", false
	}
	for _, prefix := range providerWrittenPrefixes {
		if strings.HasPrefix(text, prefix) {
			return "", false
		}
	}
	return kindUserMessage, true
}

// delegationText reports text Mewla wrote to hand a Worker its turn.
func delegationText(text string) bool {
	return strings.HasPrefix(text, watcher.DelegatedInputPrefix) ||
		strings.Contains(text, "Mewla lifecycle protocol") ||
		strings.Contains(text, "Mewla delegated turn contract") ||
		strings.HasPrefix(text, "Your work item is described in this file:")
}

func (c *Collector) collectHuman(ctx context.Context) (int, error) {
	since := c.horizon()
	sources, err := c.transcripts.discover(c.home, since)
	if err != nil && len(sources) == 0 {
		return 0, err
	}
	admitted := c.admittedDigests()
	total := 0
	failures := map[string]error{}
	for _, source := range sources {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		added, scanErr := c.collectTranscript(source, since, admitted)
		total += added
		if _, seen := failures[source.Provider]; !seen || scanErr != nil {
			failures[source.Provider] = scanErr
		}
		c.pause()
	}
	for provider, scanErr := range failures {
		c.noteError("human:"+provider, scanErr)
	}
	return total, nil
}

func (c *Collector) collectTranscript(source work.TranscriptSource, since time.Time, admitted map[string]bool) (int, error) {
	key := "human:" + source.Path
	var cursor humanCursor
	c.store.Cursor(key, &cursor)
	streamed := work.TranscriptStreamed(source.Provider)
	if streamed && cursor.Offset == source.Size || !streamed && cursor.Size == source.Size && cursor.Offset > 0 {
		return 0, nil
	}
	offset := cursor.Offset
	if !streamed {
		offset = 0
	}
	if streamed && source.Size < cursor.Offset {
		offset, cursor.Answered = 0, ""
	}
	var pending []Signal
	var turns []string
	answered := cursor.Answered
	workerSession := cursor.Worker && offset > 0
	total := 0
	flush := func(scan work.TranscriptScan) error {
		cwd := firstNonEmpty(scan.CWD, source.CWD, cursor.CWD)
		projectID := ""
		if cwd != "" {
			projectID, _ = c.store.ProjectForCWD(cwd, time.Time{})
		}
		for index := range pending {
			pending[index].ProjectID = projectID
			if cwd != "" {
				pending[index].Refs["cwd"] = cwd
			}
		}
		for _, turn := range turns {
			c.store.Place("turn", turn, cwd)
		}
		added, err := c.store.Append(pending)
		if err != nil {
			return err
		}
		total += added
		pending, turns = pending[:0], turns[:0]
		cursor = humanCursor{
			Offset: scan.Offset, Size: source.Size, CWD: cwd,
			SessionID: firstNonEmpty(scan.SessionID, source.SessionID, cursor.SessionID), Answered: answered,
			Worker: workerSession,
		}
		return c.store.SetCursor(key, cursor)
	}
	visit := func(message work.TranscriptMessage) {
		text := strings.TrimSpace(message.Text)
		if message.Role != "user" {
			if text != "" {
				answered = excerpt(text, answeredExcerptRunes)
			}
			return
		}
		if text == "" {
			return
		}
		kind, keep := classifyUserTurn(text, admitted[message.AdmissionSHA256] || admitted[message.AdmissionUnwrappedSHA256])
		if !keep {
			return
		}
		if kind == kindDelegatedMessage {
			workerSession = true
			// Only a turn handed to this Session places that turn here; Brain's
			// own transcript merely mentions turns in Work events.
			for _, match := range turnTokenPattern.FindAllStringSubmatch(text, -1) {
				turns = append(turns, match[1])
			}
		}
		at := message.Timestamp
		if at.IsZero() {
			at = source.ModTime
		}
		if at.Before(since) {
			return
		}
		author, role := "user", "direct"
		if kind == kindDelegatedMessage {
			author = "brain"
		}
		if workerSession {
			role = "worker"
		}
		refs := map[string]string{
			"provider":     source.Provider,
			"author":       author,
			"session":      firstNonEmpty(source.SessionID, cursor.SessionID),
			"session_role": role,
		}
		if answered != "" {
			refs["answered"] = answered
		}
		pending = append(pending, Signal{
			ID:      humanSignalID(source.Provider, message, text, source.Path),
			Source:  SourceHuman,
			Time:    at,
			Kind:    kind,
			Excerpt: excerpt(text, humanExcerptRunes),
			Refs:    refs,
			Pointer: source.Path + "#" + message.ID,
		})
	}
	scan, err := c.transcripts.scan(source, offset, visit, flush)
	if err != nil {
		return total, err
	}
	return total, flush(scan)
}

// humanSignalID keys a message on its time and text, so a resumed or forked
// session that copies earlier history into a new file adds nothing new.
func humanSignalID(provider string, message work.TranscriptMessage, text, path string) string {
	identity := provider + "|" + message.ID + "|" + filepath.Base(path)
	if !message.Timestamp.IsZero() {
		identity = provider + "|" + message.Timestamp.UTC().Format(time.RFC3339Nano) + "|" + text
	}
	return signalID(SourceHuman, identity)
}

func signalID(source, identity string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + identity))
	return source[:1] + "_" + hex.EncodeToString(sum[:])[:20]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
