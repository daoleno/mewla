package brain

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// WorkTranscriptLink says which provider conversation a Worker Session
// attempt of a Work ran in, so a Work can be traced to its transcript after
// the pane, the Attempt and the Work itself are gone. Links are append-only
// facts in state/work_transcripts.jsonl; they are not Work state.
type WorkTranscriptLink struct {
	WorkID            string    `json:"work_id"`
	SessionID         string    `json:"session_id"`
	Executor          string    `json:"executor"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	TranscriptPath    string    `json:"transcript_path,omitempty"`
	ProviderDataRoot  string    `json:"provider_data_root,omitempty"`
	TurnID            string    `json:"turn_id,omitempty"`
	RecordedAt        time.Time `json:"recorded_at"`
}

func (l WorkTranscriptLink) sameConversation(other WorkTranscriptLink) bool {
	return l.WorkID == other.WorkID && l.SessionID == other.SessionID &&
		l.ProviderSessionID == other.ProviderSessionID && l.TranscriptPath == other.TranscriptPath
}

func (s *Store) workTranscriptsPath() string {
	return filepath.Join(s.statePath(), "work_transcripts.jsonl")
}

var workTranscriptsMu sync.Mutex

// RecordWorkTranscript appends link unless the same conversation is already
// recorded for that Work and Session. It reports whether a row was written.
func (s *Store) RecordWorkTranscript(link WorkTranscriptLink) (bool, error) {
	link.WorkID = strings.TrimSpace(link.WorkID)
	link.SessionID = strings.TrimSpace(link.SessionID)
	link.Executor = strings.TrimSpace(link.Executor)
	link.ProviderSessionID = strings.TrimSpace(link.ProviderSessionID)
	link.TranscriptPath = strings.TrimSpace(link.TranscriptPath)
	link.ProviderDataRoot = strings.TrimSpace(link.ProviderDataRoot)
	link.TurnID = strings.TrimSpace(link.TurnID)
	if link.WorkID == "" || link.SessionID == "" || (link.ProviderSessionID == "" && link.TranscriptPath == "") {
		return false, errors.New("work transcript link needs a Work, a Session and a provider conversation")
	}
	if link.RecordedAt.IsZero() {
		link.RecordedAt = s.nowUTC()
	}
	workTranscriptsMu.Lock()
	defer workTranscriptsMu.Unlock()
	existing, err := s.readWorkTranscriptsLocked()
	if err != nil {
		return false, err
	}
	for _, current := range existing {
		if current.sameConversation(link) {
			return false, nil
		}
	}
	return true, appendJSONLine(s.workTranscriptsPath(), link)
}

// WorkTranscripts lists the recorded links of one Work, oldest first.
func (s *Store) WorkTranscripts(workID string) ([]WorkTranscriptLink, error) {
	workID = strings.TrimSpace(workID)
	workTranscriptsMu.Lock()
	defer workTranscriptsMu.Unlock()
	all, err := s.readWorkTranscriptsLocked()
	if err != nil {
		return nil, err
	}
	var out []WorkTranscriptLink
	for _, link := range all {
		if link.WorkID == workID {
			out = append(out, link)
		}
	}
	return out, nil
}

func (s *Store) readWorkTranscriptsLocked() ([]WorkTranscriptLink, error) {
	raw, err := os.ReadFile(s.workTranscriptsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []WorkTranscriptLink
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var link WorkTranscriptLink
		if json.Unmarshal(scanner.Bytes(), &link) != nil {
			continue
		}
		out = append(out, link)
	}
	return out, scanner.Err()
}

func appendJSONLine(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// workTranscriptRecheck bounds how often one live Worker's conversation is
// re-resolved after it was recorded (a /clear or resume starts a new one).
const workTranscriptRecheck = 30 * time.Second

type workTranscriptWatch struct {
	checkedAt time.Time
	recorded  bool
}

// resolveWorkerTranscript names the provider conversation a live Worker
// process is in. Claude and Codex are proven from the live process; Pi from
// the session file Mewla launched it with.
func resolveWorkerTranscript(worker *classifier.Worker) (provider string, identity work.HostTranscriptIdentity, ok bool, err error) {
	provider = work.InferWorkerProvider(worker.Command, worker.Name)
	switch provider {
	case work.WorkerProviderClaude, work.WorkerProviderCodex:
		identity, ok, err = work.ResolveLiveHostTranscriptIdentity(*worker, provider)
		return provider, identity, ok && identity.Bound(), err
	case work.WorkerProviderPi:
		path := work.PiOwnedSessionPath(worker.Command)
		if path == "" {
			return provider, identity, false, nil
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		return provider, work.HostTranscriptIdentity{Provider: provider, SessionID: stem, Path: path}, true, nil
	}
	return provider, identity, false, nil
}

// recordWorkTranscript links an active Work to its Worker's provider
// conversation. Failures are logged and never block reconciliation.
func (s *Service) recordWorkTranscript(item Work, worker *classifier.Worker, turnID string) {
	if s.workTranscriptWatch == nil {
		s.workTranscriptWatch = map[string]workTranscriptWatch{}
	}
	key := fmt.Sprintf("%s|%s|%d|%d", item.ID, worker.ID, worker.ProcessID, worker.StartedAt.Unix())
	now := s.nowUTC()
	if seen, ok := s.workTranscriptWatch[key]; ok && now.Sub(seen.checkedAt) < workTranscriptRecheck && seen.recorded {
		return
	}
	resolve := s.resolveWorkerTranscript
	if resolve == nil {
		resolve = resolveWorkerTranscript
	}
	provider, identity, ok, err := resolve(worker)
	if err != nil {
		log.Printf("brain Work %s transcript link for %s: %v", item.ID, worker.ID, err)
	}
	s.workTranscriptWatch[key] = workTranscriptWatch{checkedAt: now, recorded: ok}
	if !ok {
		return
	}
	if _, err := s.store.RecordWorkTranscript(WorkTranscriptLink{
		WorkID: item.ID, SessionID: worker.ID, Executor: provider,
		ProviderSessionID: identity.SessionID, TranscriptPath: identity.Path,
		ProviderDataRoot: identity.DataRoot, TurnID: turnID, RecordedAt: now,
	}); err != nil {
		log.Printf("brain Work %s transcript link record failed: %v", item.ID, err)
	}
}
