package lifecycle

import (
	"reflect"
	"testing"
	"time"
)

func TestReviewDeliveryFailureRespectsMutationFence(t *testing.T) {
	for _, state := range []string{"unmarked", "transport_started", "ambiguous", "accepted"} {
		t.Run(state, func(t *testing.T) {
			e, _ := newTestEngine(t)
			define(t, e, "work", PolicyBounded)
			if _, err := e.OpenReviewEvent("work", "turn_lost", "worker-turn", "event"); err != nil {
				t.Fatal(err)
			}
			claim, err := e.ClaimReview("work", "host", "handling", "host-turn")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := e.PrepareAdmission("work", PrepareAdmissionInput{SessionID: "host", TurnToken: "host-turn", Receipt: "host-turn", ClaimToken: claim.Review.Handler.HandlerID, PayloadSHA256: "digest", ProcessIdentity: "process", PaneGeneration: "pane", Mode: AdmissionFresh, Purpose: AdmissionPurposeReview, PurposeID: "handling", AttemptedAt: time.Now().UTC()}); err != nil {
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
				if _, err := e.AcceptAdmission("work", "host-turn", AcceptAdmissionInput{SessionID: "host", Receipt: "host-turn", PayloadSHA256: "digest", ActivityID: "activity", AdmissionStream: "provider", AdmissionID: "input", AdmissionSHA256: "digest", AdmissionAt: time.Now().UTC()}); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := e.State("work")
			after, err := e.FailReviewDelivery("work", "host-turn", "not submitted")
			if state == "unmarked" {
				if err != nil || after.ActiveAdmission() != nil || after.Review.Handler != nil || after.Review.DeliveryFailure.Attempts != 1 {
					t.Fatalf("unmarked failure=%+v err=%v", after, err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe release succeeded")
				}
				after, _ = e.State("work")
				if !reflect.DeepEqual(before, after) {
					t.Fatal("rejected failure changed authority")
				}
			}
		})
	}
}
