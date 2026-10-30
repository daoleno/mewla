package watch

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/push"
)

// Notification signals are the phone notifications the daemon sends, recorded
// where they are sent rather than collected from a log. One signal holds the
// text as sent and what it was about; what happens afterwards (the provider's
// receipt, a tap, the person opening the session, the verdict) is kept on the
// signal's overlay. Judgment reads a notification only once its response
// window has closed, so the model sees the outcome.

const (
	// notificationWindow is how long the person has to react before judgment
	// reads a notification. A later tap is still recorded.
	notificationWindow = 15 * time.Minute

	deliveryFailureCount = 3
	deliveryRecent       = 7 * 24 * time.Hour

	maxResponses = 8
)

// RuleNotificationDelivery flags notifications the provider keeps refusing.
// Whether a notification was worth sending is judgment's call, not a rule's.
const RuleNotificationDelivery = "notification_delivery"

// RuleNotificationBurst flags the same notification sent over and over: one
// kind, Session and text more than burstCount times within burstWindow. On
// 2026-10-30 one permission prompt sent about 100 identical "needs input"
// pushes in two minutes and nothing noticed. This counts what the daemon
// emitted, delivered or not: the fault is the sending, not the delivery.
const RuleNotificationBurst = "notification_burst"

const (
	burstCount  = 3
	burstWindow = 10 * time.Minute
	burstRecent = 6 * time.Hour
)

// deliverySuppressed is the delivery of a notification the daemon held back.
const deliverySuppressed = "suppressed"

// What the person did with a notification.
const (
	ResponseTapped    = "tapped"
	ResponseViewed    = "viewed"     // opened the Session the notification was about
	ResponseOpenedApp = "opened_app" // brought the app to the foreground
)

const (
	// skippedNotJudged is the annotation Run of a notification that needs no verdict.
	skippedNotJudged = "skipped"
	// Ref keys: why a notification was not sent or failed, and the provider's words.
	notificationReason = "reason"
	notificationDetail = "detail_text"
)

// Verdicts judgment gives a notification.
const (
	VerdictMeaningful   = "meaningful"
	VerdictNoise        = "noise"
	VerdictPoorlyTimed  = "poorly_timed"
	VerdictDuplicate    = "duplicate"
	VerdictUnclear      = "unclear"
	VerdictMissed       = "missed"
	VerdictRightlyQuiet = "rightly_quiet"
)

// NotificationResponse is one thing the person did with a notification.
type NotificationResponse struct {
	Action string    `json:"action"`
	At     time.Time `json:"at"`
}

// NotificationState is what is known about a notification after it was sent.
type NotificationState struct {
	Receipt       string                 `json:"receipt,omitempty"` // ok or failed, once the provider answered
	ReceiptReason string                 `json:"receipt_reason,omitempty"`
	Responses     []NotificationResponse `json:"responses,omitempty"`
	// WorkNow is the status of the related Work when the window closed.
	WorkNow       string    `json:"work_now,omitempty"`
	SettledAt     time.Time `json:"settled_at,omitzero"`
	Verdict       string    `json:"verdict,omitempty"`
	VerdictReason string    `json:"verdict_reason,omitempty"`
}

func mergeNotification(current, change *NotificationState) *NotificationState {
	merged := NotificationState{}
	if current != nil {
		merged = *current
		merged.Responses = append([]NotificationResponse(nil), current.Responses...)
	}
	if change.Receipt != "" {
		merged.Receipt, merged.ReceiptReason = change.Receipt, change.ReceiptReason
	}
	for _, response := range change.Responses {
		if len(merged.Responses) < maxResponses {
			merged.Responses = append(merged.Responses, response)
		}
	}
	sort.SliceStable(merged.Responses, func(i, j int) bool { return merged.Responses[i].At.Before(merged.Responses[j].At) })
	if change.WorkNow != "" {
		merged.WorkNow = change.WorkNow
	}
	if !change.SettledAt.IsZero() {
		merged.SettledAt = change.SettledAt
	}
	if change.Verdict != "" {
		merged.Verdict, merged.VerdictReason = change.Verdict, change.VerdictReason
	}
	return &merged
}

// Presence is what the daemon knows about the phone at the moment of a send.
type Presence struct {
	// Foreground is true when an app connection reported itself in the foreground.
	Foreground bool
	// ForegroundKnown is false when an app is connected that never reports its
	// foreground state (an older version), so "not in the foreground" is unknown.
	ForegroundKnown bool
	// Connected is true when any app connection is open.
	Connected bool
	// Viewing is true when an app connection has the notification's Session open.
	Viewing bool
}

// SetPresence tells Watch how to ask the server about the phone.
func (c *Collector) SetPresence(presence func(workerID string) Presence) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.presence = presence
}

func (c *Collector) presenceFor(workerID string) Presence {
	c.mu.Lock()
	presence := c.presence
	c.mu.Unlock()
	if presence == nil {
		return Presence{}
	}
	return presence(workerID)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// NotificationSent records a notification the push client sent (or could not).
func (c *Collector) NotificationSent(event push.Sent) {
	refs := map[string]string{"notification": event.ID, "delivery": event.Outcome, "priority": event.Priority}
	if event.Reason != "" {
		refs[notificationReason] = event.Reason
	}
	if event.Detail != "" {
		refs[notificationDetail] = excerpt(event.Detail, 300)
	}
	workerID := event.Refs["worker_id"]
	presence := c.presenceFor(workerID)
	c.enqueue(func() {
		c.recordNotification(event.ID, event.Kind, event.At, event.Title, event.Body, workerID, event.Refs, refs, presence)
	})
}

// NotificationSuppressed records a notification the daemon would have sent but
// held back, so a judgment can tell a rightly quiet moment from a missed one.
func (c *Collector) NotificationSuppressed(kind, workerID, title, body, reason string) {
	refs := map[string]string{"delivery": deliverySuppressed, notificationReason: reason}
	id := fmt.Sprintf("s_%s_%d", kind, c.now().UnixNano())
	refs["notification"] = id
	at, presence := c.now(), c.presenceFor(workerID)
	c.enqueue(func() {
		c.recordNotification(id, kind, at, title, body, workerID, map[string]string{"worker_id": workerID}, refs, presence)
	})
}

// enqueue runs notification bookkeeping in order on one goroutine. Callers sit
// in the push client and the server, possibly under their own locks, and must
// never wait on the Work store or the disk.
func (c *Collector) enqueue(work func()) {
	c.queueOnce.Do(func() {
		c.queue = make(chan func(), 1024)
		go func() {
			for work := range c.queue {
				work()
			}
		}()
	})
	select {
	case c.queue <- work:
	default:
		c.noteError("notification", fmt.Errorf("notification queue is full; dropped one record"))
	}
}

func (c *Collector) recordNotification(id, kind string, at time.Time, title, body, workerID string, routing, refs map[string]string, presence Presence) {
	if thread := routing["brain_thread_id"]; thread != "" {
		refs["thread"] = thread
	}
	if message := routing["brain_message_id"]; message != "" {
		refs["message"] = message
	}
	if screen := routing["screen"]; screen != "" {
		refs["screen"] = screen
	}
	refs["app_foreground"], refs["app_connected"], refs["viewing_target"] = yesNo(presence.Foreground), yesNo(presence.Connected), yesNo(presence.Viewing)
	if !presence.ForegroundKnown {
		refs["app_foreground"] = "unknown"
	}
	projectID := ""
	if workerID != "" {
		refs["session"] = workerID
		if c.sessions != nil {
			for _, worker := range c.sessions.Workers() {
				if worker != nil && worker.ID == workerID && worker.Cwd != "" {
					refs["cwd"] = worker.Cwd
					projectID, _ = c.store.ProjectForCWD(worker.Cwd, at)
					break
				}
			}
		}
	}
	c.describeRelatedWork(workerID, refs)
	signal := Signal{
		ID: signalID(SourceNotification, id), Source: SourceNotification, ProjectID: projectID,
		Time: at, Kind: kind, Excerpt: strings.TrimSpace(title + " — " + body), Refs: refs,
	}
	if _, err := c.store.Append([]Signal{signal}); err != nil {
		c.noteError("notification", err)
		return
	}
	if refs["delivery"] == push.OutcomeNotSent && refs[notificationReason] == push.ReasonNoRegistration {
		// A daemon with no phone has nobody to interrupt.
		c.noteError("notification", c.store.Annotate(map[string]Annotation{signal.ID: {Run: skippedNotJudged}}))
	}
}

// describeRelatedWork notes the Work a notification is about, and what Brain is
// waiting to ask the person, so judgment can read the notification in context.
func (c *Collector) describeRelatedWork(workerID string, refs map[string]string) {
	if c.works == nil {
		return
	}
	works, err := c.works.ListWork()
	if err != nil {
		return
	}
	var waiting []string
	for _, item := range works {
		if workerID != "" && item.AttemptSessionID == workerID {
			refs["work"], refs["work_title"], refs["work_status"] = item.ID, excerpt(item.Title, 120), string(item.Status)
			if item.Review != nil {
				refs["work_review"] = "pending"
			}
			if item.Question != "" {
				refs["work_question"] = excerpt(item.Question, 200)
			}
		}
		if item.Question != "" && item.Status != brain.WorkDone && item.Status != brain.WorkCancelled && len(waiting) < 3 {
			waiting = append(waiting, fmt.Sprintf("%s: %s", excerpt(item.Title, 60), excerpt(item.Question, 120)))
		}
	}
	if len(waiting) > 0 {
		refs["brain_waiting"] = strings.Join(waiting, " | ")
	}
}

// NotificationReceipt records the provider's answer for an accepted notification.
func (c *Collector) NotificationReceipt(receipt push.Receipt) {
	state := &NotificationState{Receipt: "ok"}
	if !receipt.OK {
		state = &NotificationState{Receipt: "failed", ReceiptReason: firstNonEmpty(receipt.Reason, "failed")}
	}
	c.enqueue(func() {
		c.noteError("notification", c.store.Annotate(map[string]Annotation{signalID(SourceNotification, receipt.ID): {Notification: state}}))
	})
}

// NotificationResponse records that the person tapped a notification. A tap
// that arrives after the window closed is still recorded.
func (c *Collector) NotificationResponse(id, action string, at time.Time) {
	c.enqueue(func() {
		signal, ok := c.store.Signal(signalID(SourceNotification, id))
		if !ok {
			return
		}
		now := c.now()
		if at.IsZero() || at.After(now) {
			at = now
		}
		if at.Before(signal.Time) {
			at = signal.Time
		}
		if action == "" {
			action = ResponseTapped
		}
		c.noteError("notification", c.store.Annotate(map[string]Annotation{signal.ID: {Notification: &NotificationState{
			Responses: []NotificationResponse{{Action: action, At: at}},
		}}}))
	})
}

// NotificationEngaged records that the person opened the Session a recent
// notification was about (workerID), or brought the app forward (workerID empty).
func (c *Collector) NotificationEngaged(action, workerID string) {
	if action != ResponseOpenedApp || c.engageDelay <= 0 {
		c.enqueue(func() { c.engaged(action, workerID) })
		return
	}
	// Tapping a notification brings the app forward too. Wait for the tap's
	// report so that foreground is not counted as a response to every other push.
	time.AfterFunc(c.engageDelay, func() { c.enqueue(func() { c.engaged(action, workerID) }) })
}

// tapShadow is how close to a tap a foreground event is taken to be its effect.
const tapShadow = 15 * time.Second

func (c *Collector) engaged(action, workerID string) {
	now := c.now()
	changes := map[string]Annotation{}
	recent := c.store.Signals(Filter{Source: SourceNotification, Since: now.Add(-notificationWindow)})
	if action == ResponseOpenedApp {
		for _, signal := range recent {
			if signal.Notification == nil {
				continue
			}
			for _, response := range signal.Notification.Responses {
				if response.Action == ResponseTapped && now.Sub(response.At) <= tapShadow+c.engageDelay {
					return
				}
			}
		}
	}
	for _, signal := range recent {
		if signal.Refs["delivery"] != push.OutcomeAccepted {
			continue
		}
		if workerID != "" && signal.Refs["session"] != workerID {
			continue
		}
		if signal.Notification != nil && hasResponse(signal.Notification, action) {
			continue
		}
		changes[signal.ID] = Annotation{Notification: &NotificationState{Responses: []NotificationResponse{{Action: action, At: now}}}}
	}
	c.noteError("notification", c.store.Annotate(changes))
}

func hasResponse(state *NotificationState, action string) bool {
	for _, response := range state.Responses {
		if response.Action == action {
			return true
		}
	}
	return false
}

// settleNotifications closes the response window of notifications old enough,
// noting where their Work stands, so judgment can read them.
func (c *Collector) settleNotifications() {
	now := c.now()
	var due []Signal
	for _, signal := range c.store.Signals(Filter{Source: SourceNotification}) {
		if signal.Notification != nil && !signal.Notification.SettledAt.IsZero() {
			continue
		}
		if now.Sub(signal.Time) >= notificationWindow {
			due = append(due, signal)
		}
	}
	if len(due) == 0 {
		return
	}
	status := map[string]string{}
	if c.works != nil {
		if works, err := c.works.ListWork(); err == nil {
			for _, item := range works {
				status[item.ID] = string(item.Status)
			}
		}
	}
	changes := map[string]Annotation{}
	for _, signal := range due {
		changes[signal.ID] = Annotation{Notification: &NotificationState{SettledAt: now, WorkNow: status[signal.Refs["work"]]}}
	}
	c.noteError("notification", c.store.Annotate(changes))
}

// ruleNotificationDelivery flags a phone that the push provider keeps
// refusing: the latest notifications that reached the provider all failed,
// whether at the ticket or in the receipt. A daemon with no phone sends nothing
// and is not flagged. This is a mechanical fact; whether notifications were
// worth sending is left to judgment.
func ruleNotificationDelivery(store *Store, now time.Time) []Problem {
	type attempt struct {
		signal Signal
		failed bool
		reason string
	}
	var attempts []attempt
	for _, signal := range store.Signals(Filter{Source: SourceNotification, Since: now.Add(-deliveryRecent)}) {
		switch signal.Refs["delivery"] {
		case push.OutcomeFailed:
			attempts = append(attempts, attempt{signal: signal, failed: true, reason: signal.Refs[notificationReason]})
		case push.OutcomeAccepted:
			switch {
			case signal.Notification != nil && signal.Notification.Receipt == "failed":
				attempts = append(attempts, attempt{signal: signal, failed: true, reason: signal.Notification.ReceiptReason})
			case signal.Notification != nil && signal.Notification.Receipt == "ok":
				attempts = append(attempts, attempt{signal: signal})
			}
		}
	}
	// Signals come newest first.
	var failures []Signal
	reason := ""
	for _, a := range attempts {
		if !a.failed {
			break
		}
		if reason == "" {
			reason = a.reason
		}
		failures = append(failures, a.signal)
	}
	if len(failures) < deliveryFailureCount {
		return nil
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].Time.Before(failures[j].Time) })
	latest := failures[len(failures)-1]
	return []Problem{{
		ID: problemID(RuleNotificationDelivery, reason), ProjectID: "", Rule: RuleNotificationDelivery, Subject: reason,
		Title: "Phone notifications are not being delivered: " + firstNonEmpty(reason, "refused by the push provider"),
		Detail: fmt.Sprintf("The last %d notifications all failed (%s since %s); anything that needs the person is not reaching the phone. Latest: %s",
			len(failures), firstNonEmpty(reason, "failed"), failures[0].Time.Format(time.RFC3339), latest.Excerpt),
		Count: len(failures), Evidence: evidenceIDs(failures), FirstSeen: failures[0].Time, LastSeen: latest.Time,
	}}
}

// ruleNotificationBurst flags each notification (kind, Session, text) that went
// out more than burstCount times inside burstWindow during the last
// burstRecent. Notifications the server held back are not counted: holding
// them back is the fix, not the fault.
func ruleNotificationBurst(store *Store, now time.Time) []Problem {
	groups := map[string][]Signal{}
	for _, signal := range store.Signals(Filter{Source: SourceNotification, Since: now.Add(-burstRecent)}) {
		if signal.Refs["delivery"] == deliverySuppressed {
			continue
		}
		key := signal.Kind + "|" + signal.Refs["session"] + "|" + signal.Excerpt
		groups[key] = append(groups[key], signal)
	}
	var out []Problem
	for key, signals := range groups {
		if len(signals) <= burstCount {
			continue
		}
		sort.Slice(signals, func(i, j int) bool { return signals[i].Time.Before(signals[j].Time) })
		burst, start, burstEnd := 0, 0, 0
		for end := range signals {
			for signals[end].Time.Sub(signals[start].Time) > burstWindow {
				start++
			}
			if end-start+1 > burst {
				burst, burstEnd = end-start+1, end
			}
		}
		if burst <= burstCount {
			continue
		}
		first, latest := signals[burstEnd-burst+1], signals[len(signals)-1]
		out = append(out, Problem{
			ID: problemID(RuleNotificationBurst, key), ProjectID: latest.ProjectID, Rule: RuleNotificationBurst, Subject: key,
			Title: fmt.Sprintf("The same notification was sent %d times: %s", burst, excerpt(latest.Excerpt, 100)),
			Detail: fmt.Sprintf("%d identical %s notifications for %s within %s (from %s); the phone is being flooded and each one asks for the person's attention. Latest: %s",
				burst, latest.Kind, firstNonEmpty(latest.Refs["session"], "no session"), burstWindow, first.Time.Format(time.RFC3339), latest.Excerpt),
			Severe: true, Count: burst,
			Evidence: evidenceIDs(signals[burstEnd-burst+1 : burstEnd+1]), FirstSeen: first.Time, LastSeen: latest.Time,
		})
	}
	return out
}
