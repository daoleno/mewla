package lifecycle

import (
	"testing"
	"time"
)

func TestSweepRetainsReviewAdmissionCapability(t *testing.T) {
	for _, state := range []string{"prepared", "transport_started", "ambiguous", "accepted"} {
		t.Run(state, func(t *testing.T) {
			e, _ := newTestEngine(t)
			now := time.Now().UTC()
			e.SetNow(func() time.Time { return now })
			define(t, e, "work", PolicyBounded)
			if _, err := e.OpenReviewEvent("work", "turn_lost", "worker-turn", "event"); err != nil {
				t.Fatal(err)
			}
			if _, err := e.ClaimReview("work", "host", "handling", "host-turn"); err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.PrepareAdmission("work", PrepareAdmissionInput{SessionID: "host", TurnToken: "host-turn", Receipt: "host-turn", ClaimToken: "handling", PayloadSHA256: "digest", ProcessIdentity: "process", PaneGeneration: "pane", Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: "handling", AttemptedAt: now}); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "transport_started":
				if _, err := e.MarkAdmissionTransportStarted("work", "host-turn"); err != nil {
					t.Fatal(err)
				}
			case "ambiguous":
				if _, err := e.MarkAdmissionAmbiguous("work", "host-turn", "unknown"); err != nil {
					t.Fatal(err)
				}
			case "accepted":
				if _, err := e.AcceptAdmission("work", "host-turn", AcceptAdmissionInput{SessionID: "host", Receipt: "host-turn", PayloadSHA256: "digest", ActivityID: "activity", AdmissionStream: "provider", AdmissionID: "input", AdmissionSHA256: "digest", AdmissionAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			now = now.Add(EventClaimTTL + time.Second)
			if err := e.Sweep(); err != nil {
				t.Fatal(err)
			}
			st, _ := e.State("work")
			if st.Review.Handler == nil {
				t.Fatal("sweep erased pending admission's exact recovery capability")
			}
		})
	}
}
