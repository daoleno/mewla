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

	first := w.enrichChoicePrompt("%1", choiceSignal("p1", false, false))
	if first.Prompt.CallID != "toolu_1" || len(first.Prompt.Questions) != 2 || first.Prompt.Questions[0].Options[0].Description != "red" {
		t.Fatalf("not enriched: %+v", first.Prompt)
	}
	second := w.enrichChoicePrompt("%1", choiceSignal("p1", true, false))
	if reads != 1 || !second.Prompt.Questions[0].Answered || second.Prompt.Questions[1].Answered {
		t.Fatalf("reads=%d answered flags not taken from the pane: %+v", reads, second.Prompt.Questions)
	}
	w.enrichChoicePrompt("%1", choiceSignal("p2", false, false))
	if reads != 2 {
		t.Fatalf("a different prompt must read the transcript again, reads=%d", reads)
	}
	w.enrichChoicePrompt("%2", choiceSignal("p2", false, false))
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
	menu := classifier.ActivitySignal{State: classifier.StateBlocked, Prompt: &classifier.Prompt{ID: "m", Kind: classifier.PromptPermission}}
	if got := w.enrichChoicePrompt("%1", menu); got.Prompt != menu.Prompt || reads != 0 {
		t.Fatalf("a menu prompt must not read the transcript: reads=%d", reads)
	}
	signal := choiceSignal("p1", false, false)
	for range 3 {
		got := w.enrichChoicePrompt("%1", signal)
		if got.Prompt != signal.Prompt {
			t.Fatalf("an empty transcript must leave the pane prompt as is")
		}
	}
	if reads != 1 {
		t.Fatalf("a failed read must not repeat every poll, reads=%d", reads)
	}
	if got := New(0).enrichChoicePrompt("%1", signal); got.Prompt != signal.Prompt {
		t.Fatalf("without an enricher the prompt is unchanged")
	}
}
