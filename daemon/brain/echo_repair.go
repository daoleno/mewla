package brain

import (
	"fmt"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// StoredEchoRepair is the outcome of RepairStoredEcho: which stored provider
// echo row an admission claims, and the known transform under which the echo
// is its bytes.
type StoredEchoRepair struct {
	ThreadID    string `json:"thread_id"`
	AdmissionID string `json:"admission_id"`
	EchoID      string `json:"echo_id"`
	Transform   string `json:"transform"`
	Applied     bool   `json:"applied"`
}

// storedEchoTransforms are the known ways a provider's stored echo row differs
// from the admitted bytes: Claude's prompt editor (tabs as four spaces) and the
// display cleaning every echo row's body went through.
var storedEchoTransforms = []struct {
	name  string
	apply func(string) string
}{
	{"exact", func(body string) string { return body }},
	{"display", work.CleanCodexDisplayText},
	{"claude", classifier.ClaudeSubmittedInput},
	{"claude+display", func(body string) string { return work.CleanCodexDisplayText(classifier.ClaudeSubmittedInput(body)) }},
}

// RepairStoredEcho has an accepted Brain admission claim one named stored
// echo row that the automatic claim missed (rows stored before a provider's
// echo digest was right), exactly as an automatic claim does. It is an
// operator repair for named pairs, never a rule: it refuses unless the echo is
// a transcript row of the admission's thread, recorded no earlier than the
// admission was prepared, with no other user message between the two rows, and
// its body is the admitted bytes under one known transform. Without apply it
// only reports what it would do.
func (s *Store) RepairStoredEcho(admissionID, echoID string, apply bool) (StoredEchoRepair, error) {
	admissionID, echoID = strings.TrimSpace(admissionID), strings.TrimSpace(echoID)
	repair := StoredEchoRepair{AdmissionID: admissionID, EchoID: echoID}
	if admissionID == "" || echoID == "" {
		return repair, fmt.Errorf("an admission id and an echo id are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readAllTimelineItemsLocked()
	if err != nil {
		return repair, err
	}
	admissionIndex, echoIndex := -1, -1
	for index, item := range items {
		switch strings.TrimSpace(item.ID) {
		case admissionID:
			admissionIndex = index
		case echoID:
			echoIndex = index
		}
	}
	if admissionIndex < 0 || !IsBrainInputAdmission(items[admissionIndex]) {
		return repair, fmt.Errorf("%s is not a Brain admission row", admissionID)
	}
	row := items[admissionIndex]
	repair.ThreadID = strings.TrimSpace(row.ThreadID)
	if strings.TrimSpace(row.AdmissionEchoEventID) != "" {
		return repair, fmt.Errorf("admission %s already claimed %s", admissionID, row.AdmissionEchoEventID)
	}
	var admission *BrainInputAdmission
	for _, candidate := range s.readPresentationLocked().BrainInputAdmissions {
		if AdmissionTimelineItemID(candidate.RequestID) == admissionID && candidate.ThreadID == repair.ThreadID {
			admission = &candidate
			break
		}
	}
	if admission == nil || admission.State != BrainInputAdmissionAccepted || admission.AcceptedAt == nil ||
		!timelineItemMatchesBrainInputAdmission(row, *admission) {
		return repair, fmt.Errorf("admission %s has no accepted admission record matching its row", admissionID)
	}
	if echoIndex < 0 {
		return repair, fmt.Errorf("no stored row %s", echoID)
	}
	echo := items[echoIndex]
	if strings.TrimSpace(echo.ThreadID) != repair.ThreadID || strings.TrimSpace(echo.Kind) != timelineKindUserMessage ||
		!strings.EqualFold(strings.TrimSpace(echo.Role), "user") || IsBrainInputAdmission(echo) ||
		strings.TrimSpace(echo.Origin) != TimelineOriginTranscript {
		return repair, fmt.Errorf("%s is not a transcript user row of thread %s", echoID, repair.ThreadID)
	}
	for _, item := range items {
		if IsBrainInputAdmission(item) && strings.TrimSpace(item.AdmissionEchoEventID) == echoID {
			return repair, fmt.Errorf("%s is already the echo of %s", echoID, item.ID)
		}
	}
	if echo.CreatedAt.Before(admission.CreatedAt.UTC().Truncate(time.Millisecond)) {
		return repair, fmt.Errorf("%s was recorded before admission %s was prepared", echoID, admissionID)
	}
	low, high := min(admissionIndex, echoIndex), max(admissionIndex, echoIndex)
	for _, item := range items[low+1 : high] {
		if strings.TrimSpace(item.ThreadID) == repair.ThreadID && strings.TrimSpace(item.Kind) == timelineKindUserMessage {
			return repair, fmt.Errorf("%s and %s are not adjacent: user row %s lies between them", admissionID, echoID, item.ID)
		}
	}
	echoBody := strings.TrimSpace(echo.Body)
	for _, transform := range storedEchoTransforms {
		if strings.TrimSpace(transform.apply(admission.DisplayBody)) == echoBody {
			repair.Transform = transform.name
			break
		}
	}
	if repair.Transform == "" {
		return repair, fmt.Errorf("%s is not admission %s's bytes under any known transform", echoID, admissionID)
	}
	if !apply {
		return repair, nil
	}
	next := make([]TimelineItem, 0, len(items)-1)
	for index, item := range items {
		switch index {
		case echoIndex:
			continue
		case admissionIndex:
			claimEcho(&item, echoID, strings.TrimSpace(echo.SessionID))
		}
		next = append(next, item)
	}
	if err := s.rewriteTimelineLocked(next); err != nil {
		return repair, err
	}
	repair.Applied = true
	return repair, nil
}
