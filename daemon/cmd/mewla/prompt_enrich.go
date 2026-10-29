package main

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// choicePromptEnricher fills an AskUserQuestion prompt from the Claude
// transcript. The pane shows one question at a time; the transcript holds all
// of them, with their option descriptions and the tool call behind them.
func choicePromptEnricher(w *watcher.Watcher, execs *work.ExecutorConfig) watcher.PromptEnricher {
	var mu sync.Mutex
	reader := work.NewProviderConversationReader()
	return func(sessionID string, prompt classifier.Prompt) classifier.Prompt {
		worker := w.GetWorker(sessionID)
		if worker == nil || providerOf(execs, worker.ExecutorID, worker.Command) != work.WorkerProviderClaude {
			return prompt
		}
		mu.Lock()
		conversation, err := reader.Load(*worker, work.WorkerProviderClaude, time.Now())
		mu.Unlock()
		if err != nil {
			return prompt
		}
		return promptWithTranscriptChoice(prompt, conversation)
	}
}

// promptWithTranscriptChoice returns the prompt with every question of the
// conversation's pending choice. A conversation with no pending choice leaves
// the prompt as the pane showed it.
func promptWithTranscriptChoice(prompt classifier.Prompt, conversation work.CodexConversation) classifier.Prompt {
	event, ok := work.LatestPendingConversationChoice(conversation)
	if !ok || event.Choice == nil || len(event.Choice.Questions) == 0 {
		return prompt
	}
	questions := make([]classifier.PromptQuestion, len(event.Choice.Questions))
	for index, question := range event.Choice.Questions {
		item := classifier.PromptQuestion{
			Header:      question.Header,
			Question:    question.Question,
			MultiSelect: question.MultiSelect,
			AllowsOther: question.AllowsOther(),
			Options:     make([]classifier.PromptOption, len(question.Options)),
		}
		for number, option := range question.Options {
			item.Options[number] = classifier.PromptOption{Number: number + 1, Label: option.Label, Description: option.Description}
		}
		questions[index] = item
	}
	prompt.Questions = questions
	prompt.CallID = event.CallID
	return prompt
}
