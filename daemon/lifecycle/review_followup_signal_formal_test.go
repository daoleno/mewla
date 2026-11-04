package lifecycle

import (
	"errors"
	"testing"
	"time"
)

// Found by the TLA+ pilot (worklog/pstack-fusion-1104/pilot), seen in the
// 2026-11-04 incident (Work 616e1401: admission.prepared turn:8e2601d7 at 23:19:36Z,
// review.resolved continue at 23:21:54Z, Attempt e55fb4bc still active).
//
// PrepareAdmission admits a review follow-up in fresh mode while the Work's
// Attempt is still active (the boundReview exception). If Brain then resolves
// the review before the Worker's first progress signal arrives, which is what
// `brain work update` does after a `worker send`, AcceptAdmissionBySignal has no
// review for followUpEvents to resolve and no free Attempt slot, and returns
// ErrAttemptActive on every retry. The Worker is acting on the prompt, but its
// progress and final report under the prompt's turn id are all rejected until
// the old Attempt is lost (lease 10 min + lost grace 30 min). Marking the
// admission ambiguous (724b50b3) does not help: ambiguous is retried the same way.
func TestReviewFollowUpSignalIsAcceptedAfterBrainResolvesTheReviewWithAnActiveAttempt(t *testing.T) {
	t.Skip("FORMAL PILOT FINDING, not fixed: AcceptAdmissionBySignal returns ErrAttemptActive for a review follow-up once Brain has resolved the review while the old Attempt is active; remove this Skip when the lifecycle owner decides the intended behaviour")
	for _, name := range []string{"continue", "wait", "continue_then_stale_release"} {
		t.Run(name, func(t *testing.T) {
			e, _ := newTestEngine(t)
			now := time.Now().UTC()
			e.SetNow(func() time.Time { return now })
			define(t, e, "work", PolicyBounded)
			prepareAdmissionFixture(t, e, "work", "worker", "attempt-turn")
			if st, err := e.AcceptAdmissionBySignal("work", "attempt-turn", "worker"); err != nil || st.Attempt == nil {
				t.Fatalf("first Attempt: state=%+v err=%v", st, err)
			}
			if _, err := e.OpenReviewEvent("work", "lease_expired", "attempt-turn", "review-1"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.ClaimReview("work", "host", "handling-1", "host-turn"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.PrepareAdmission("work", PrepareAdmissionInput{
				SessionID: "worker", TurnToken: "follow-up-turn", Receipt: "follow-up-turn",
				PayloadSHA256: "digest-follow-up", ProcessIdentity: "process-1", PaneGeneration: "pane-1",
				Mode: AdmissionFresh, SignalProtocol: true, Purpose: AdmissionPurposeReview, PurposeID: "handling-1",
				AttemptedAt: now,
			}); err != nil {
				t.Fatalf("a review follow-up is allowed while the Attempt is active: %v", err)
			}
			in := ResolveReviewInput{Disposition: DispositionContinue, Actor: "brain"}
			if name == "wait" {
				// docs/work-lifecycle.md: an exact session_terminal wait naming the Attempt.
				in = ResolveReviewInput{Disposition: DispositionWait, Actor: "brain",
					WakeKind: WakeSessionTerminal, WakeRef: "session:worker:turn:attempt-turn"}
			}
			if _, err := e.ResolveReview("work", "review-1", in); err != nil {
				t.Fatal(err)
			}
			if name == "continue_then_stale_release" {
				// 724b50b3: the watcher marks the silent prompt delivery-unknown.
				if _, err := e.MarkAdmissionAmbiguous("work", "follow-up-turn", "provider settled and stayed idle"); err != nil {
					t.Fatal(err)
				}
			}
			st, err := e.AcceptAdmissionBySignal("work", "follow-up-turn", "worker")
			if errors.Is(err, ErrAttemptActive) {
				cur, _ := e.State("work")
				t.Fatalf("the Worker's own signal for the delivered follow-up is rejected: %v (Attempt=%s, follow-up admission=%s)",
					err, cur.Attempt.TurnToken, cur.AdmissionByToken("follow-up-turn").Status)
			}
			if err != nil {
				t.Fatal(err)
			}
			if st.Attempt == nil || st.Attempt.TurnToken != "follow-up-turn" {
				t.Fatalf("follow-up did not become the Attempt: %+v", st.Attempt)
			}
		})
	}
}
