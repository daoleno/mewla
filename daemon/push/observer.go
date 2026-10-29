package push

import "time"

// Kinds of notification the daemon sends. Watch groups what it learns about
// notifications by kind.
const (
	KindWorkerBlocked   = "worker_blocked"
	KindWorkerFailed    = "worker_failed"
	KindWorkerDone      = "worker_done"
	KindScheduledResult = "scheduled_result"
	KindBrainReply      = "brain_reply"
	KindWatchUrgent     = "watch_urgent"
	KindCalendarAlert   = "calendar_alert"
	KindOther           = "other"
)

// What became of a notification when the daemon tried to send it.
const (
	OutcomeAccepted = "accepted" // Expo took it; a receipt may still say the provider refused it
	OutcomeFailed   = "failed"   // Expo or the network refused it; Reason says how
	OutcomeNotSent  = "not_sent" // it never left the daemon; Reason says why
)

// ReasonNoRegistration is the NotSent reason for a daemon that has no phone.
const ReasonNoRegistration = "no_registration"

// dataNotificationID is the key in the push payload's data that the app sends
// back when the person taps the notification.
const dataNotificationID = "notification_id"

// Sent describes one notification as it was sent. Title and Body are the text
// the phone shows; the push token is never part of it.
type Sent struct {
	ID       string
	Kind     string
	At       time.Time
	Title    string
	Body     string
	Priority string
	// Refs are the routing fields of the payload (worker_id, brain_thread_id,
	// brain_message_id, screen), without the server reference.
	Refs    map[string]string
	Outcome string
	Reason  string
	Detail  string
}

// Receipt is the provider's answer for an accepted notification.
type Receipt struct {
	ID     string
	At     time.Time
	OK     bool
	Reason string
	Detail string
}

// Observer hears of every notification the client sends. Callbacks run on the
// sending or receipt-checking goroutine, never under the client's locks, and
// must return quickly.
type Observer interface {
	NotificationSent(Sent)
	NotificationReceipt(Receipt)
}

// SetObserver registers the observer; nil removes it.
func (c *Client) SetObserver(observer Observer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observer = observer
}

func (c *Client) currentObserver() Observer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.observer
}

func (c *Client) observeSent(event Sent) {
	if observer := c.currentObserver(); observer != nil {
		event.Refs = withoutServerRef(event.Refs)
		observer.NotificationSent(event)
	}
}

func (c *Client) observeReceipt(receipt Receipt) {
	if receipt.ID == "" {
		return
	}
	if observer := c.currentObserver(); observer != nil {
		observer.NotificationReceipt(receipt)
	}
}

func withoutServerRef(refs map[string]string) map[string]string {
	out := make(map[string]string, len(refs))
	for key, value := range refs {
		if key == "server_id" || key == dataNotificationID || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}
