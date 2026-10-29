package watcher

import "github.com/daoleno/mewla/daemon/classifier"

// AnswerCommand is the command line that answers a prompt open in a Session.
func AnswerCommand(sessionID string, prompt *classifier.Prompt) string {
	return answerWith(sessionID, prompt)
}
