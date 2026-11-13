package answered

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func record(id, session string, at time.Time, by string) Request {
	return Request{ID: id, Kind: KindPermission, SessionID: session, Asked: "git push origin main", Answer: "Yes",
		Outcome: OutcomeApproved, By: Answerer{Kind: by, DeviceID: "phone-1"}, AnsweredAt: at}
}

// A record survives a restart; the first explicit answer stands, and one
// inferred from the terminal gives way to the explicit one written after it.
func TestLedgerKeepsTheFirstExplicitAnswerAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	ledger, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var heard []Request
	ledger.OnRecord(func(r Request) { heard = append(heard, r) })
	at := time.Date(2026, 11, 12, 13, 40, 0, 0, time.UTC)
	asked := at.Add(-time.Minute)

	terminal := record("prompt:%1:p1", "%1", at, ByTerminal)
	terminal.Answer, terminal.AskedAt = "", &asked
	if _, wrote, err := ledger.Record(terminal); err != nil || !wrote {
		t.Fatalf("terminal: %v %v", wrote, err)
	}
	device := record("prompt:%1:p1", "%1", at.Add(time.Second), ByDevice)
	if got, wrote, err := ledger.Record(device); err != nil || !wrote || got.By.Kind != ByDevice || got.AskedAt == nil || !got.AskedAt.Equal(asked) {
		t.Fatalf("device after terminal: %+v %v %v", got, wrote, err)
	}
	later := record("prompt:%1:p1", "%1", at.Add(2*time.Second), ByBrain)
	if got, wrote, _ := ledger.Record(later); wrote || got.By.Kind != ByDevice {
		t.Fatalf("a second explicit answer replaced the first: %+v", got)
	}
	if len(heard) != 2 || ledger.Version() != 2 {
		t.Fatalf("heard %d records, version %d", len(heard), ledger.Version())
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get("prompt:%1:p1")
	if !ok || got.By.Kind != ByDevice || got.By.DeviceID != "phone-1" || got.Answer != "Yes" || got.At() != asked {
		t.Fatalf("after restart: %+v %v", got, ok)
	}
}

// A Session keeps its newest SessionLimit records (its thread's window);
// Work questions are all kept; the file is compacted as it grows.
func TestLedgerRetentionAndHistoryOrder(t *testing.T) {
	dir := t.TempDir()
	ledger, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	for i := 0; i < SessionLimit+10; i++ {
		if _, _, err := ledger.Record(record(fmt.Sprintf("prompt:%%1:%d", i), "%1", start.Add(time.Duration(i)*time.Second), ByDevice)); err != nil {
			t.Fatal(err)
		}
	}
	work := Request{ID: "work:w1:q", Kind: KindWorkQuestion, WorkID: "w1", Asked: "Keep both?", Answer: "Keep both", Outcome: OutcomeAnswered, By: Answerer{Kind: ByDevice}, AnsweredAt: start}
	if _, _, err := ledger.Record(work); err != nil {
		t.Fatal(err)
	}
	session := ledger.List(Filter{SessionID: "%1"})
	if len(session) != SessionLimit || session[0].ID != fmt.Sprintf("prompt:%%1:%d", SessionLimit+9) {
		t.Fatalf("session keeps %d, newest %s", len(session), session[0].ID)
	}
	if _, ok := ledger.Get("prompt:%1:0"); ok {
		t.Fatal("the oldest record past the window was kept")
	}
	if got := ledger.List(Filter{Kind: KindWorkQuestion}); len(got) != 1 {
		t.Fatalf("work questions = %+v", got)
	}
	if got := ledger.List(Filter{Limit: 3}); len(got) != 3 || got[0].AnsweredAt.Before(got[1].AnsweredAt) {
		t.Fatalf("history order = %+v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "answered_requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "\n"); lines > 2*(SessionLimit+1)+64 {
		t.Fatalf("file not compacted: %d lines", lines)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.List(Filter{})) != SessionLimit+1 {
		t.Fatalf("reopened keeps %d", len(reopened.List(Filter{})))
	}
}

func TestLedgerRefusesIncompleteRecordsAndFoldsText(t *testing.T) {
	ledger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.Record(Request{ID: "x"}); err == nil {
		t.Fatal("a record with no kind or outcome was kept")
	}
	long := strings.Repeat("word ", 200)
	got, _, err := ledger.Record(Request{ID: "x", Kind: KindApproval, Asked: "  rm -rf\n  build  ", Answer: long, Outcome: OutcomeDenied, By: Answerer{Kind: ByDevice}})
	if err != nil || got.Asked != "rm -rf build" || len([]rune(got.Answer)) != fieldLimit || !strings.HasSuffix(got.Answer, "…") {
		t.Fatalf("folded = %q / %d runes, %v", got.Asked, len([]rune(got.Answer)), err)
	}
}

// A terminal answer whose outcome was not known is told better once the
// provider's record of the call says how it ended: the same answer, not a
// second one. Nothing else replaces a terminal answer but an explicit one.
func TestTerminalAnswerIsSettledOnceItsOutcomeIsKnown(t *testing.T) {
	ledger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 13, 4, 0, 0, 0, time.UTC)
	asked := at.Add(-time.Minute)
	unknown := record("approval:%1:a", "%1", at, ByTerminal)
	unknown.Answer, unknown.Outcome, unknown.AskedAt = "", OutcomeAnswered, &asked
	if _, wrote, _ := ledger.Record(unknown); !wrote {
		t.Fatal("not written")
	}
	again := unknown
	if _, wrote, _ := ledger.Record(again); wrote {
		t.Fatal("the same unknown outcome was written twice")
	}
	settled := unknown
	settled.Outcome, settled.AskedAt = OutcomeApproved, nil
	if got, wrote, _ := ledger.Record(settled); !wrote || got.Outcome != OutcomeApproved || got.AskedAt == nil || !got.AskedAt.Equal(asked) {
		t.Fatalf("settled = %+v %v", got, wrote)
	}
	changed := unknown
	changed.Outcome = OutcomeDenied
	if got, wrote, _ := ledger.Record(changed); wrote || got.Outcome != OutcomeApproved {
		t.Fatalf("a known outcome was rewritten: %+v %v", got, wrote)
	}
	if rows := ledger.List(Filter{SessionID: "%1"}); len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}
