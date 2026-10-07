package brain

import (
	"encoding/json"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudePasteWrappedPendingAdmission(t *testing.T) {
	// Exact outer bytes from host transcript line 5020, Claude Code 2.1.285.
	// Synthetic inner event avoids committing private daemon state.
	payload := "<zen_work_event>\n{\"summary\":\"" + strings.Repeat("large event 你好 ", 150) + "\"}\n</zen_work_event>"
	wrapped := "\n\n<pasted_content id=\"4f28\">\n" + payload + "\n</pasted_content id=\"4f28\">\n"
	at := time.Date(2026, 10, 6, 8, 56, 53, 983000000, time.UTC)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	row, _ := json.Marshal(map[string]any{"type": "user", "sessionId": "session", "uuid": "native-input", "timestamp": at.Format(time.RFC3339Nano), "message": map[string]any{"role": "user", "content": wrapped}})
	if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	conversation, err := work.LoadHostConversationByIdentity(work.HostTranscriptIdentity{Provider: work.WorkerProviderClaude, SessionID: "session", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	conversation.Activity = &work.ProviderActivity{ID: "activity", Status: work.ProviderActivityRunning, StartedAt: at.Format(time.RFC3339Nano)}
	submission := watcher.InputAdmission{SessionID: "host", ProposedTurnID: "turn", Receipt: "turn", PayloadSHA256: AdmissionDigest(payload), AcceptedAt: at.Add(-time.Second)}
	resolution, _, matched := boundHostConversationSubmissionResolution(conversation, submission, at.Add(time.Second))
	if !matched {
		t.Fatal("exact wrapped native user input must reconcile pending admission")
	}
	if resolution.Admission.SHA256 != submission.PayloadSHA256 {
		t.Fatal("canonical admission must use submitted inner digest")
	}
	submission.PayloadSHA256 = AdmissionDigest(payload + "\n")
	if _, _, matched := boundHostConversationSubmissionResolution(conversation, submission, at.Add(time.Second)); matched {
		t.Fatal("byte-different inner payload matched")
	}
	submission.PayloadSHA256 = AdmissionDigest(wrapped)
	if _, _, matched := boundHostConversationSubmissionResolution(conversation, submission, at.Add(time.Second)); !matched {
		t.Fatal("literal raw wrapper must remain matchable")
	}
}
