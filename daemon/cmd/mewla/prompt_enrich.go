package main

import (
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

// choicePromptEnricher fills a Claude prompt from its transcript. An
// AskUserQuestion prompt: the pane shows one question at a time; the transcript
// holds all of them, with their option descriptions and the tool call behind
// them. A permission prompt: the tool call it asks about, which names the tool
// and the command or file from the transcript, found by the line the prompt
// shows.
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
		if prompt.Kind == classifier.PromptPermission {
			return promptWithTranscriptTool(prompt, conversation)
		}
		return promptWithTranscriptChoice(prompt, conversation)
	}
}

// promptWithTranscriptTool returns the permission prompt with the tool call it
// asks about: the call of the conversation with no result yet whose line the
// prompt's own text shows (compared without whitespace, so a pane that wraps a
// long command still matches). A call the prompt does not show is not taken,
// whatever is pending: a sub-agent's prompt is for a call in the sub-agent's
// own transcript, and the only call pending in this one is the Task that
// started it. With no match the prompt stays as the pane showed it; missing
// fields are honest, wrong ones are not.
func promptWithTranscriptTool(prompt classifier.Prompt, conversation work.CodexConversation) classifier.Prompt {
	shown := strings.Join(strings.Fields(prompt.Request), "")
	for _, call := range work.PendingConversationToolCalls(conversation) {
		line := strings.Join(strings.Fields(call.Summary), "")
		if line == "" || line == strings.Join(strings.Fields(call.Tool), "") || !strings.Contains(shown, line) {
			continue
		}
		prompt.CallID, prompt.Tool, prompt.Summary = call.CallID, call.Tool, call.Summary
		return prompt
	}
	return prompt
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
