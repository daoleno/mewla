package enrollment

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRequestRequiresMatchingSecretAndNumber(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	pub := key.Public().(ed25519.PublicKey)
	req, secret, err := m.Create("browser-1", "Firefox", "web", "https://zen.example", fmtHex(pub))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status(req.ID, "bad"); ok {
		t.Fatal("wrong secret disclosed request")
	}
	if _, err := m.Decide(req.ID, "not-the-number", "phone", true); err == nil {
		t.Fatal("wrong number approved request")
	}
	if _, err := m.Decide(req.ID, req.Number, "phone", true); err != nil {
		t.Fatal(err)
	}
	status, ok := m.Status(req.ID, secret)
	if !ok || status.Status != Approved {
		t.Fatalf("status=%#v ok=%v", status, ok)
	}
}

func fmtHex(value []byte) string {
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2], out[i*2+1] = hexChars[b>>4], hexChars[b&15]
	}
	return string(out)
}

func TestPendingOnlyReturnsUnexpiredPendingRequests(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	requests := []Request{
		{ID: "later", Status: Pending, CreatedAt: now, ExpiresAt: now.Add(time.Minute), SecretHash: "keep-secret"},
		{ID: "earlier", Status: Pending, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)},
		{ID: "approved", Status: Approved, ExpiresAt: now.Add(-time.Minute)},
		{ID: "denied", Status: Denied, ExpiresAt: now.Add(-time.Minute)},
		{ID: "expired", Status: Expired, ExpiresAt: now.Add(-time.Minute)},
		{ID: "overdue", Status: Pending, ExpiresAt: now.Add(-time.Minute)},
		{ID: "at-expiry", Status: Pending, ExpiresAt: now},
	}
	if err := m.saveLocked(requests); err != nil {
		t.Fatal(err)
	}
	got, err := m.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "earlier" || got[1].ID != "later" || got[1].SecretHash != "" {
		t.Fatalf("pending=%+v", got)
	}
	persisted, err := m.loadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if persisted[0].SecretHash != "keep-secret" || persisted[2].Status != Approved || persisted[3].Status != Denied || persisted[5].Status != Expired || persisted[6].Status != Expired {
		t.Fatalf("persisted=%+v", persisted)
	}
	m.now = func() time.Time { return now.Add(2 * time.Minute) }
	got, err = m.Pending()
	if err != nil || len(got) != 0 {
		t.Fatalf("pending=%+v err=%v", got, err)
	}
}

func TestDecidePreservesTerminalAuditRecords(t *testing.T) {
	for _, status := range []Status{Approved, Denied, Expired} {
		for _, afterExpiry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/after-expiry=%t", status, afterExpiry), func(t *testing.T) {
				m, err := New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
				decision := now.Add(-time.Minute)
				expires := now.Add(time.Minute)
				if afterExpiry {
					expires = now.Add(-time.Minute)
				}
				m.now = func() time.Time { return now }
				request := Request{ID: "audit", Status: status, Number: "123", DecisionAt: &decision, ApprovedBy: "original", ExpiresAt: expires}
				if err := m.saveLocked([]Request{request}); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(m.path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := m.Decide("audit", "123", "second", true); err == nil || err.Error() != "enrollment is no longer pending" {
					t.Fatalf("error=%v", err)
				}
				after, err := os.ReadFile(m.path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatalf("terminal audit record mutated: %s", after)
				}
			})
		}
	}
}

func TestDecideOnlyExpiresOverduePendingRequests(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	m.now = func() time.Time { return now }
	if err := m.saveLocked([]Request{{ID: "pending", Status: Pending, Number: "123", ExpiresAt: now}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decide("pending", "123", "phone", true); err == nil {
		t.Fatal("approved at expiry")
	}
	stored, err := m.loadLocked()
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].Status != Expired || stored[0].DecisionAt != nil || stored[0].ApprovedBy != "" {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestTerminalRetentionUsesDecisionOrExpiryAndPersistsCleanup(t *testing.T) {
	for _, status := range []Status{Approved, Denied, Expired} {
		for _, withDecision := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/decision=%t", status, withDecision), func(t *testing.T) {
				m, err := New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
				m.now = func() time.Time { return now }
				var requests []Request
				for i, age := range []time.Duration{time.Hour - time.Second, time.Hour, time.Hour + time.Second} {
					terminalAt := now.Add(-age)
					request := Request{ID: fmt.Sprint(i), Status: status, ExpiresAt: terminalAt}
					if withDecision {
						request.DecisionAt = &terminalAt
						request.ExpiresAt = now.Add(time.Minute) // decision time must take precedence
					}
					requests = append(requests, request)
				}
				requests = append(requests, Request{ID: "live", Status: Pending, ExpiresAt: now.Add(time.Minute)})
				if err := m.saveLocked(requests); err != nil {
					t.Fatal(err)
				}
				if _, err := m.Pending(); err != nil {
					t.Fatal(err)
				}
				stored, err := m.loadLocked()
				if err != nil {
					t.Fatal(err)
				}
				if len(stored) != 2 || stored[0].ID != "0" || stored[0].Status != status || stored[1].ID != "live" {
					t.Fatalf("retained=%+v", stored)
				}
			})
		}
	}
}
