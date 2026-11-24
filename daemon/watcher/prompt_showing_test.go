package watcher

import (
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
)

// Two questions in one AskUserQuestion: the pane shows one tab at a time, so
// only the transcript holds the options of the other.
const twoTabChoicePane = "Claude Code\n←  ☐ City  ☐ Season  ✔ Submit  →\nWhich city?\n❯ 1. Beijing\n  2. Shanghai\n  3. Type something.\n────\n  4. Chat about this\n\nEnter to select · Tab/Arrow keys to navigate · Esc to cancel\n"

func choiceWindows() []sessionPane {
	return []sessionPane{{target: "sess-a:@1", name: "alpha", cwd: "/repo/a", command: "claude", panePID: 111}}
}

// Windows, after the folder-trust prompt (0.4.28 gate, f-w01): the question
// was on screen before Claude's transcript had its tool call, so the first
// poll read only the tab headers. The transcript completed it a moment later
// under the same prompt ID, and that never reached the phones: their card
// stayed "Waiting for your choice · Open terminal" until a reload.
func TestPollPublishesAChoiceTheTranscriptCompletesLater(t *testing.T) {
	w := New(time.Second)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	now := time.Date(2026, 11, 24, 17, 49, 28, 0, time.UTC)
	w.pollNow = func() time.Time { return now }
	ready := false
	w.SetPromptEnricher(func(_ string, prompt classifier.Prompt) classifier.Prompt {
		if !ready {
			return prompt // the tool call is not in the transcript yet
		}
		prompt.CallID = "toolu_city"
		prompt.Questions = []classifier.PromptQuestion{
			{Header: "City", Question: "Which city?", Options: []classifier.PromptOption{{Number: 1, Label: "Beijing"}, {Number: 2, Label: "Shanghai"}}},
			{Header: "Season", Question: "Which season?", Options: []classifier.PromptOption{{Number: 1, Label: "Spring"}, {Number: 2, Label: "Autumn"}}},
		}
		return prompt
	})
	restore := installFakePollSeams(w, choiceWindows(), map[string]string{"sess-a:@1": twoTabChoicePane}, liveSessionProcesses())
	defer restore()

	w.poll()
	first := workerByID(w.Workers(), "sess-a:@1")
	if first.Prompt == nil || first.Prompt.CallID != "" || len(first.Prompt.Questions) != 2 || len(first.Prompt.Questions[1].Options) != 0 {
		t.Fatalf("fixture: the first poll should read the pane alone: %+v", first.Prompt)
	}
	drainWatcherEvents(w)

	ready = true
	now = now.Add(2 * time.Second)
	w.promptEnrich.mu.Lock()
	entry := w.promptEnrich.entries["sess-a:@1"]
	entry.triedAt = time.Now().Add(-time.Minute)
	w.promptEnrich.entries["sess-a:@1"] = entry
	w.promptEnrich.mu.Unlock()
	w.poll()

	var published *classifier.Prompt
	for _, event := range collectWatcherEvents(w) {
		if event.WorkerID == "sess-a:@1" && event.Worker != nil && (event.Type == "worker_metadata_change" || event.Type == "worker_state_change") {
			published = event.Worker.Prompt
		}
	}
	if published == nil || published.CallID != "toolu_city" || len(published.Questions[1].Options) != 2 {
		t.Fatalf("the completed question never reached the phones: %+v", published)
	}
	if published.ID != first.Prompt.ID || published.AskedAt == nil || !published.AskedAt.Equal(*first.Prompt.AskedAt) {
		t.Fatalf("completing a question must keep its identity and showing: first %+v, then %+v", first.Prompt, published)
	}
}

// 0.4.28 gate (lane A f25): Claude asked again, word for word, a question
// answered a moment earlier. Same screen, same ID: each showing has its own
// time, so a phone tells the new one from the answered one.
func TestPollStampsEachShowingOfARepeatedQuestion(t *testing.T) {
	w := New(time.Second)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	now := time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC)
	w.pollNow = func() time.Time { return now }
	restore := installFakePollSeams(w, choiceWindows(), map[string]string{"sess-a:@1": twoTabChoicePane}, liveSessionProcesses())
	w.poll()
	restore()
	first := workerByID(w.Workers(), "sess-a:@1").Prompt
	if first == nil || first.AskedAt == nil {
		t.Fatalf("a prompt carries when it was shown: %+v", first)
	}
	now = now.Add(time.Second)
	restore = installFakePollSeams(w, choiceWindows(), map[string]string{"sess-a:@1": contentA}, liveSessionProcesses())
	w.poll()
	restore()
	if got := workerByID(w.Workers(), "sess-a:@1").Prompt; got != nil {
		t.Fatalf("answered: no prompt, got %+v", got)
	}
	now = now.Add(10 * time.Second)
	restore = installFakePollSeams(w, choiceWindows(), map[string]string{"sess-a:@1": twoTabChoicePane}, liveSessionProcesses())
	defer restore()
	w.poll()
	again := workerByID(w.Workers(), "sess-a:@1").Prompt
	if again == nil || again.ID != first.ID || again.AskedAt == nil || !again.AskedAt.After(*first.AskedAt) {
		t.Fatalf("the question asked again is a new showing: first %+v, again %+v", first, again)
	}
	now = now.Add(time.Second)
	w.poll()
	if still := workerByID(w.Workers(), "sess-a:@1").Prompt; still == nil || !still.AskedAt.Equal(*again.AskedAt) {
		t.Fatalf("the same showing read again keeps its time: %+v then %+v", again, still)
	}
}

func TestPromptShowingRules(t *testing.T) {
	at := time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC)
	prompt := func(id, call string) *classifier.Prompt { return &classifier.Prompt{ID: id, CallID: call} }
	stampOf := func(p *classifier.Prompt) time.Time { return *p.AskedAt }
	var shows promptShowings
	first := stampOf(shows.stamp("s", prompt("p", ""), at))
	if got := stampOf(shows.stamp("s", prompt("p", "toolu_1"), at.Add(time.Second))); !got.Equal(first) {
		t.Fatalf("the transcript naming the call later is the same showing")
	}
	shows.stamp("s", nil, at.Add(2*time.Second))
	if got := stampOf(shows.stamp("s", prompt("p", "toolu_1"), at.Add(4*time.Second))); !got.Equal(first) {
		t.Fatalf("a blink is the same showing")
	}
	shows.answered("s")
	if got := stampOf(shows.stamp("s", prompt("p", "toolu_1"), at.Add(5*time.Second))); !got.Equal(first) {
		t.Fatalf("still on screen while its answer goes in: the same showing")
	}
	shows.stamp("s", nil, at.Add(6*time.Second))
	answeredAgain := stampOf(shows.stamp("s", prompt("p", ""), at.Add(7*time.Second)))
	if answeredAgain.Equal(first) {
		t.Fatalf("back after Mewla answered it: a new showing, however soon")
	}
	if got := stampOf(shows.stamp("s", prompt("p", "toolu_2"), at.Add(8*time.Second))); !got.Equal(answeredAgain) {
		t.Fatalf("a call named for the first time keeps the showing")
	}
	if got := stampOf(shows.stamp("s", prompt("p", "toolu_3"), at.Add(9*time.Second))); got.Equal(answeredAgain) {
		t.Fatalf("another tool call is another showing")
	}
	shows.stamp("s", nil, at.Add(10*time.Second))
	if got := stampOf(shows.stamp("s", prompt("p", ""), at.Add(16*time.Second))); !got.After(at.Add(15 * time.Second)) {
		t.Fatalf("back after longer than a blink: a new showing")
	}
	if got := stampOf(shows.stamp("t", prompt("p", ""), at)); !got.Equal(at) {
		t.Fatalf("each Session has its own showing")
	}
}
