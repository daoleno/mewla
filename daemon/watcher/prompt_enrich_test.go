package watcher

import (
	"testing"

	"github.com/daoleno/mewla/daemon/classifier"
)

func choiceSignal(id string, answered ...bool) classifier.ActivitySignal {
	prompt := classifier.Prompt{ID: id, Kind: classifier.PromptChoice}
	for index, done := range answered {
		prompt.Questions = append(prompt.Questions, classifier.PromptQuestion{Header: string(rune('A' + index)), Answered: done})
	}
	return classifier.ActivitySignal{State: classifier.StateBlocked, Prompt: &prompt}
}

// One transcript read serves every poll of the same prompt, a later prompt
// reads again, and the pane stays the authority on which tabs are answered.
func TestEnrichChoicePromptReadsTheTranscriptOncePerPrompt(t *testing.T) {
	w := New(0)
	reads := 0
	w.SetPromptEnricher(func(sessionID string, prompt classifier.Prompt) classifier.Prompt {
		reads++
		prompt.CallID = "toolu_1"
		prompt.Questions = []classifier.PromptQuestion{
			{Header: "A", Question: "Which fruit?", Options: []classifier.PromptOption{{Number: 1, Label: "Apple", Description: "red"}}},
			{Header: "B", Question: "Which color?", Options: []classifier.PromptOption{{Number: 1, Label: "Red"}}},
		}
		return prompt
	})

	first := w.enrichPrompt("%1", choiceSignal("p1", false, false))
	if first.Prompt.CallID != "toolu_1" || len(first.Prompt.Questions) != 2 || first.Prompt.Questions[0].Options[0].Description != "red" {
		t.Fatalf("not enriched: %+v", first.Prompt)
	}
	second := w.enrichPrompt("%1", choiceSignal("p1", true, false))
	if reads != 1 || !second.Prompt.Questions[0].Answered || second.Prompt.Questions[1].Answered {
		t.Fatalf("reads=%d answered flags not taken from the pane: %+v", reads, second.Prompt.Questions)
	}
	w.enrichPrompt("%1", choiceSignal("p2", false, false))
	if reads != 2 {
		t.Fatalf("a different prompt must read the transcript again, reads=%d", reads)
	}
	w.enrichPrompt("%2", choiceSignal("p2", false, false))
	if reads != 3 {
		t.Fatalf("another Session has its own entry, reads=%d", reads)
	}
}

func TestEnrichChoicePromptLeavesOtherPromptsAndFailuresAlone(t *testing.T) {
	w := New(0)
	reads := 0
	w.SetPromptEnricher(func(sessionID string, prompt classifier.Prompt) classifier.Prompt {
		reads++
		return prompt // the transcript has nothing yet
	})
	menu := classifier.ActivitySignal{State: classifier.StateBlocked, Prompt: &classifier.Prompt{ID: "m", Kind: classifier.PromptStartup}}
	if got := w.enrichPrompt("%1", menu); got.Prompt != menu.Prompt || reads != 0 {
		t.Fatalf("a menu prompt must not read the transcript: reads=%d", reads)
	}
	signal := choiceSignal("p1", false, false)
	for range 3 {
		got := w.enrichPrompt("%1", signal)
		if got.Prompt != signal.Prompt {
			t.Fatalf("an empty transcript must leave the pane prompt as is")
		}
	}
	if reads != 1 {
		t.Fatalf("a failed read must not repeat every poll, reads=%d", reads)
	}
	if got := New(0).enrichPrompt("%1", signal); got.Prompt != signal.Prompt {
		t.Fatalf("without an enricher the prompt is unchanged")
	}
}

// A permission prompt is tied to the tool call it asks about once the
// transcript has the call: the pane's identity stays, the transcript is read
// again until it names the call, and then once per prompt.
func TestEnrichPromptNamesThePermissionsToolCall(t *testing.T) {
	w := New(0)
	reads, ready := 0, false
	w.SetPromptEnricher(func(sessionID string, prompt classifier.Prompt) classifier.Prompt {
		reads++
		if ready {
			prompt.CallID, prompt.Tool, prompt.Summary = "toolu_1", "Bash", "curl -sI https://example.org"
		}
		return prompt
	})
	permission := func() classifier.ActivitySignal {
		return classifier.ActivitySignal{State: classifier.StateBlocked, Prompt: &classifier.Prompt{ID: "perm", Kind: classifier.PromptPermission, Title: "Bash command", Request: "Tip: x\ncurl"}}
	}
	if got := w.enrichPrompt("%1", permission()); got.Prompt.Tool != "" || got.Prompt.ID != "perm" {
		t.Fatalf("the transcript has no call yet: %+v", got.Prompt)
	}
	w.enrichPrompt("%1", permission())
	if reads != 1 {
		t.Fatalf("an empty read must not repeat every poll, reads=%d", reads)
	}
	ready = true
	w.promptEnrich.mu.Lock()
	entry := w.promptEnrich.entries["%1"]
	entry.triedAt = entry.triedAt.Add(-promptEnrichRetry)
	w.promptEnrich.entries["%1"] = entry
	w.promptEnrich.mu.Unlock()
	got := w.enrichPrompt("%1", permission())
	if got.Prompt.ID != "perm" || got.Prompt.CallID != "toolu_1" || got.Prompt.Tool != "Bash" || got.Prompt.Summary != "curl -sI https://example.org" || got.Prompt.Request != "Tip: x\ncurl" {
		t.Fatalf("not enriched: %+v", got.Prompt)
	}
	w.enrichPrompt("%1", permission())
	if reads != 2 {
		t.Fatalf("one read serves every poll of the prompt, reads=%d", reads)
	}
}
