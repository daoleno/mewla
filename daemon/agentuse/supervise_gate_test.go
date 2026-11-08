package agentuse

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeGate struct {
	mu      sync.Mutex
	refuse  error
	notes   []string
	begins  int
	ends    int
	pending int
}

func (g *fakeGate) Begin() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refuse != nil {
		return g.refuse
	}
	g.begins++
	g.pending++
	return nil
}
func (g *fakeGate) End() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ends++
	g.pending--
	notes := g.notes
	g.notes = nil
	return notes
}

// The person's take over refuses a call before Playwright sees it, with the
// reason, and the call is not ended since it never began.
func TestAGateRefusalAnswersTheCallWithItsReasonAndPlaywrightNeverSeesIt(t *testing.T) {
	gate := &fakeGate{refuse: errors.New("The person took over this browser")}
	s := superviseFakeGated(t, gate)
	got := s.call(t, 7, "browser_navigate")
	if !strings.Contains(got, `"id":7`) || !strings.Contains(got, `"isError":true`) || !strings.Contains(got, "took over") {
		t.Fatalf("answer %s", got)
	}
	_ = s.in.Close()
	log, _ := s.finish(t)
	if log != "tabs=0 closes=0" || gate.ends != 0 {
		t.Fatalf("Playwright saw %s; ends %d", log, gate.ends)
	}
}

// What the person did is added to the result as one more block of text, once.
func TestAGateTellsTheAgentWhatThePersonDidWithTheResult(t *testing.T) {
	gate := &fakeGate{notes: []string{"The person used the page while you worked in it: 2 clicks."}}
	s := superviseFakeGated(t, gate)
	got := s.call(t, 1, "browser_navigate")
	if !strings.Contains(got, "2 clicks") || !strings.Contains(got, `"content"`) {
		t.Fatalf("answer %s", got)
	}
	if got := s.call(t, 2, "browser_navigate"); strings.Contains(got, "clicks") {
		t.Fatalf("told twice: %s", got)
	}
	if gate.begins != 2 || gate.ends != 2 || gate.pending != 0 {
		t.Fatalf("begins %d ends %d", gate.begins, gate.ends)
	}
	_ = s.in.Close()
	s.finish(t)
}

// browser_read is Mewla's one added tool: it is listed, and answers with the
// page's address and text.
func TestBrowserReadIsListedAndAnswersWithThePageText(t *testing.T) {
	s := superviseFakeGated(t, &fakeGate{})
	list := s.call(t, 1, "browser_snapshot") // answered; the list is asked next
	_ = list
	raw := `{"jsonrpc":"2.0","id":"l","method":"tools/list"}` + "\n"
	if _, err := s.in.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if !s.out.Scan() || !strings.Contains(s.out.Text(), `"browser_read"`) {
		t.Fatalf("browser_read is not listed: %s", s.out.Text())
	}
	got := s.call(t, 2, "browser_read")
	if !strings.Contains(got, "Page: http://example.test/post") || !strings.Contains(got, "first line\\nsecond line") || strings.Contains(got, "### Result") {
		t.Fatalf("answer %s", got)
	}
	_ = s.in.Close()
	s.finish(t)
}
