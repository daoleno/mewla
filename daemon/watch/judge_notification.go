package watch

import (
	"fmt"
	"strings"
	"time"
)

// What judgment is shown for a notification: the text as sent, the state of the
// Work it was about, what the person was doing, what they did with it and what
// happened next. Everything is data the daemon recorded; the model decides
// whether the notification was worth interrupting for.

const (
	judgeNotificationExtra   = 900 // characters the context lines add to a batch's budget
	notificationLookBack     = time.Hour
	notificationPersonBefore = 10 * time.Minute
	maxContextLines          = 4
)

var notificationVerdicts = map[string]bool{
	VerdictMeaningful: true, VerdictNoise: true, VerdictPoorlyTimed: true,
	VerdictDuplicate: true, VerdictUnclear: true, VerdictMissed: true, VerdictRightlyQuiet: true,
}

func ago(from, to time.Time) string {
	d := to.Sub(from)
	sign := "+"
	if d < 0 {
		sign, d = "-", -d
	}
	if d < time.Minute {
		return sign + "0m"
	}
	return fmt.Sprintf("%s%dm", sign, int(d.Round(time.Minute)/time.Minute))
}

// renderNotification writes the lines that follow a notification's header.
func (j *judge) renderNotification(b *strings.Builder, signal Signal) {
	refs := signal.Refs
	state := signal.Notification
	fmt.Fprintf(b, "  pushed: %s priority=%s\n", jsonText(signal.Excerpt, judgeTextRunes), firstNonEmpty(refs["priority"], "none"))

	delivery := refs["delivery"]
	switch delivery {
	case deliverySuppressed:
		fmt.Fprintf(b, "  delivery: NOT sent (held back: %s)\n", refs[notificationReason])
	case "not_sent":
		fmt.Fprintf(b, "  delivery: NOT sent (%s)\n", refs[notificationReason])
	case "failed":
		fmt.Fprintf(b, "  delivery: refused at send (%s)\n", firstNonEmpty(refs[notificationReason], "unknown"))
	default:
		receipt := "no receipt"
		if state != nil && state.Receipt == "ok" {
			receipt = "receipt ok"
		} else if state != nil && state.Receipt == "failed" {
			receipt = "receipt failed (" + state.ReceiptReason + ")"
		}
		fmt.Fprintf(b, "  delivery: accepted, %s\n", receipt)
	}
	fmt.Fprintf(b, "  phone at send: app in foreground=%s, app connected=%s, person had this session open=%s\n",
		refs["app_foreground"], refs["app_connected"], refs["viewing_target"])

	if refs["work_title"] != "" {
		line := fmt.Sprintf("  work: %s (status then: %s", jsonText(refs["work_title"], 120), firstNonEmpty(refs["work_status"], "?"))
		if state != nil && state.WorkNow != "" {
			line += ", " + ago(signal.Time, state.SettledAt) + ": " + state.WorkNow
		}
		line += ")"
		if refs["work_question"] != "" {
			line += " asks the person: " + jsonText(refs["work_question"], 160)
		}
		b.WriteString(line + "\n")
	}
	if refs["brain_waiting"] != "" {
		fmt.Fprintf(b, "  Brain was waiting on the person for: %s\n", jsonText(refs["brain_waiting"], 360))
	}

	if state != nil && len(state.Responses) > 0 {
		var parts []string
		for _, response := range state.Responses {
			parts = append(parts, fmt.Sprintf("%s %s after", response.Action, ago(signal.Time, response.At)))
		}
		fmt.Fprintf(b, "  person's response: %s\n", strings.Join(parts, "; "))
	} else if delivery == "accepted" {
		fmt.Fprintf(b, "  person's response: none recorded in %d minutes (no tap, did not open this session, did not bring the app forward)\n", int(notificationWindow/time.Minute))
	}

	for _, line := range j.notificationNeighbours(signal) {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

// notificationNeighbours finds the duplicates, the person's own activity and
// what followed, from signals the store already holds.
func (j *judge) notificationNeighbours(signal Signal) []string {
	var lines []string
	t := signal.Time
	same := func(other Signal) bool {
		if session := signal.Refs["session"]; session != "" && other.Refs["session"] == session {
			return true
		}
		if thread := signal.Refs["thread"]; thread != "" && other.Refs["thread"] == thread {
			return true
		}
		return false
	}

	var earlier []string
	duplicates := 0
	for _, other := range j.store.Signals(Filter{Source: SourceNotification, Since: t.Add(-24 * time.Hour)}) {
		if other.ID == signal.ID || other.Time.After(t) || (other.Refs["delivery"] != "accepted" && other.Refs["delivery"] != "failed") {
			continue
		}
		if other.Excerpt == signal.Excerpt {
			duplicates++
		}
		if t.Sub(other.Time) <= notificationLookBack && same(other) && len(earlier) < maxContextLines {
			earlier = append(earlier, fmt.Sprintf("(%s %s) %s", ago(t, other.Time), other.Kind, jsonText(other.Excerpt, 100)))
		}
	}
	if len(earlier) > 0 {
		lines = append(lines, "earlier pushes about the same session or thread in the last hour: "+strings.Join(earlier, "; "))
	}
	if duplicates > 0 {
		lines = append(lines, fmt.Sprintf("the identical text was pushed %d other time(s) in the previous 24 hours", duplicates))
	}

	var person, followed []string
	for _, other := range j.store.Signals(Filter{Since: t.Add(-notificationPersonBefore)}) {
		if other.Time.After(t.Add(notificationWindow)) || other.ID == signal.ID {
			continue
		}
		switch {
		case other.Source == SourceHuman && other.Kind == kindUserMessage && other.Refs["author"] == "user" && len(person) < maxContextLines:
			person = append(person, fmt.Sprintf("(%s %s) %s", ago(t, other.Time), firstNonEmpty(other.Refs["session_role"], "message"), jsonText(other.Excerpt, 120)))
		case other.Source == SourceProcess && other.Time.After(t) && same(other) && len(followed) < maxContextLines:
			followed = append(followed, fmt.Sprintf("(%s %s) %s", ago(t, other.Time), other.Kind, jsonText(other.Excerpt, 120)))
		}
	}
	if len(person) > 0 {
		lines = append(lines, "what the person wrote around then: "+strings.Join(person, "; "))
	} else {
		lines = append(lines, "the person wrote nothing to Brain or any session from 10 minutes before to 15 minutes after")
	}
	if len(followed) > 0 {
		lines = append(lines, "what followed in the same session: "+strings.Join(followed, "; "))
	}
	return lines
}
