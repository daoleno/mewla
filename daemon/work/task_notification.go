package work

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

// TaskNotificationConversationSource marks a status event projected from a
// provider background-task notification (a finished subagent, background
// command or monitor event). The provider delivers it as prompt-side input,
// but it is neither user input nor a user message.
const TaskNotificationConversationSource = "task_notification"

const (
	taskNotificationOpen        = "<task-notification>"
	taskNotificationClose       = "</task-notification>"
	taskNotificationReminder    = "<system-reminder>"
	taskNotificationReminderEnd = "</system-reminder>"
	taskNotificationPreamble    = "[SYSTEM NOTIFICATION - NOT USER INPUT]"
)

// TaskNotification is the display-relevant part of one provider task
// notification. Host-local paths and model-directed notes are not retained.
type TaskNotification struct {
	TaskID     string
	ToolUseID  string
	Status     string
	Summary    string
	Body       string
	Tokens     *int
	ToolUses   *int
	DurationMS *int
}

// ParseTaskNotifications decodes text consisting only of one or more
// <task-notification> blocks, optionally inside one <system-reminder> wrapper
// with the provider's system-notification preamble. Any other surrounding
// text fails closed so user-authored messages never become cards.
func ParseTaskNotifications(value string) ([]TaskNotification, bool) {
	rest := unwrapTaskNotificationReminder(value)
	if !strings.HasPrefix(rest, taskNotificationOpen) {
		return nil, false
	}
	var notifications []TaskNotification
	for rest != "" {
		if !strings.HasPrefix(rest, taskNotificationOpen) {
			return nil, false
		}
		end := strings.Index(rest, taskNotificationClose)
		if end < 0 {
			return nil, false
		}
		notification, ok := parseTaskNotificationBlock(rest[len(taskNotificationOpen):end])
		if !ok {
			return nil, false
		}
		notifications = append(notifications, notification)
		rest = strings.TrimSpace(rest[end+len(taskNotificationClose):])
	}
	return notifications, len(notifications) > 0
}

func unwrapTaskNotificationReminder(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, taskNotificationReminder) && strings.HasSuffix(value, taskNotificationReminderEnd) {
		value = strings.TrimSpace(value[len(taskNotificationReminder) : len(value)-len(taskNotificationReminderEnd)])
	}
	if strings.HasPrefix(value, taskNotificationPreamble) {
		// Claude follows the marker with model-directed prose; the block that
		// follows it is the only displayable part.
		if start := strings.Index(value, taskNotificationOpen); start >= 0 {
			return value[start:]
		}
	}
	return value
}

func parseTaskNotificationBlock(block string) (TaskNotification, bool) {
	notification := TaskNotification{
		TaskID:    taskNotificationField(block, "task-id", false),
		ToolUseID: taskNotificationField(block, "tool-use-id", false),
		Status:    normalizeTaskNotificationStatus(taskNotificationField(block, "status", false)),
		// Claude XML-escapes summaries and events (a command's "&&" arrives
		// as "&amp;&amp;"). Results are Markdown, where entities already render.
		Summary: html.UnescapeString(taskNotificationField(block, "summary", false)),
		// Result and event payloads are free-form provider text; their close tag
		// is the last one so nested look-alike tags stay inside the body.
		Body: firstNonEmpty(
			taskNotificationField(block, "result", true),
			html.UnescapeString(taskNotificationField(block, "event", true)),
		),
	}
	if usage := taskNotificationField(block, "usage", false); usage != "" {
		notification.Tokens = taskNotificationInt(usage, "subagent_tokens")
		notification.ToolUses = taskNotificationInt(usage, "tool_uses")
		notification.DurationMS = taskNotificationInt(usage, "duration_ms")
	}
	return notification, notification.Summary != "" || notification.Body != ""
}

func taskNotificationField(block, tag string, lastClose bool) string {
	open, close := "<"+tag+">", "</"+tag+">"
	start := strings.Index(block, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	var end int
	if lastClose {
		end = strings.LastIndex(block, close)
	} else if end = strings.Index(block[start:], close); end >= 0 {
		end += start
	}
	if end < start {
		return ""
	}
	return strings.TrimSpace(block[start:end])
}

func taskNotificationInt(block, tag string) *int {
	value, err := strconv.Atoi(taskNotificationField(block, tag, false))
	if err != nil || value < 0 {
		return nil
	}
	return &value
}

// normalizeTaskNotificationStatus keeps the card vocabulary small. An absent
// status (monitor events) stays empty and renders as a neutral event.
func normalizeTaskNotificationStatus(status string) string {
	switch status = strings.ToLower(strings.TrimSpace(status)); status {
	case "completed", "complete", "success", "succeeded", "done":
		return "done"
	case "failed", "failure", "error":
		return "failed"
	case "killed", "stopped", "cancelled", "canceled", "interrupted":
		return "killed"
	default:
		return status
	}
}

// TaskNotificationEventID keeps the first notification on the ID the record
// previously had as a user message, so earlier durable rows are replaced in
// place. Additional notifications in the same record get stable suffixes.
func TaskNotificationEventID(baseID string, index int) string {
	if index == 0 {
		return baseID
	}
	return fmt.Sprintf("%s:task-notification:%d", baseID, index)
}

// ConversationEvent projects the notification into the shared wire contract.
func (n TaskNotification) ConversationEvent(id string, seq int, timestamp string) CodexConversationEvent {
	title := n.Summary
	if title == "" {
		title = "Background task"
	}
	return CodexConversationEvent{
		ID:             id,
		Seq:            seq,
		Timestamp:      timestamp,
		Kind:           "status",
		Title:          title,
		Body:           n.Body,
		Status:         n.Status,
		CallID:         n.ToolUseID,
		Source:         TaskNotificationConversationSource,
		TaskID:         n.TaskID,
		TaskTokens:     n.Tokens,
		TaskToolUses:   n.ToolUses,
		TaskDurationMS: n.DurationMS,
	}
}
