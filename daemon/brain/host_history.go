package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hostSessionHistoryName is the append-only log of every provider
// conversation Brain's Host Session has had. host_session.json holds only the
// current binding, so New chat, an executor switch or a replaced pane would
// otherwise drop the link to the old Brain transcript.
const hostSessionHistoryName = "host_sessions.jsonl"

type hostSessionHistoryEntry struct {
	RecordedAt        time.Time `json:"recorded_at"`
	ID                string    `json:"id,omitempty"`
	ExecutorID        string    `json:"executor_id,omitempty"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	TranscriptPath    string    `json:"transcript_path,omitempty"`
	ProviderDataRoot  string    `json:"provider_data_root,omitempty"`
	BoundAt           time.Time `json:"bound_at,omitempty"`
}

func (e hostSessionHistoryEntry) sameConversation(other hostSessionHistoryEntry) bool {
	return e.ProviderSessionID == other.ProviderSessionID && e.TranscriptPath == other.TranscriptPath
}

// archiveHostSessionBinding records the host binding at path in the history
// log when it names a provider conversation that is not logged yet. It is
// called before host_session.json is overwritten; failure is logged and never
// blocks the write.
func archiveHostSessionBinding(path string, next *hostSessionFile) {
	host, err := readHostSession(path)
	if err != nil {
		return
	}
	if next != nil && strings.TrimSpace(next.ProviderSessionID) == host.ProviderSessionID &&
		strings.TrimSpace(next.TranscriptPath) == host.TranscriptPath {
		return // the same conversation stays bound
	}
	entry := hostSessionHistoryEntry{
		RecordedAt: time.Now().UTC(), ID: host.ID, ExecutorID: host.ExecutorID,
		ProviderSessionID: host.ProviderSessionID, TranscriptPath: host.TranscriptPath,
		ProviderDataRoot: host.ProviderDataRoot, BoundAt: host.UpdatedAt,
	}
	if entry.ProviderSessionID == "" && entry.TranscriptPath == "" {
		return
	}
	logPath := filepath.Join(filepath.Dir(path), hostSessionHistoryName)
	existing, err := readHostSessionHistory(logPath)
	if err != nil {
		log.Printf("brain host session history read failed: %v", err)
		return
	}
	for _, current := range existing {
		if current.sameConversation(entry) {
			return
		}
	}
	if err := appendJSONLine(logPath, entry); err != nil {
		log.Printf("brain host session history append failed: %v", err)
	}
}

// writeHostSessionFile overwrites host_session.json, first keeping the
// binding it replaces in the history log.
func writeHostSessionFile(path string, value any) error {
	next, _ := value.(hostSessionFile)
	archiveHostSessionBinding(path, &next)
	return writeJSONFile(path, value)
}

func readHostSessionHistory(path string) ([]hostSessionHistoryEntry, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []hostSessionHistoryEntry
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry hostSessionHistoryEntry
		if json.Unmarshal(line, &entry) == nil {
			out = append(out, entry)
		}
	}
	return out, nil
}

// HostSessionHistory lists the provider conversations Brain's Host has had,
// oldest first, including the current one once it has been recorded.
func (s *Store) HostSessionHistory() ([]hostSessionHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return readHostSessionHistory(filepath.Join(s.statePath(), hostSessionHistoryName))
}

func (s *Store) archiveCurrentHostSession() {
	archiveHostSessionBinding(s.HostSessionPath(), nil)
}
