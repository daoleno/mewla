package push

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	expoPushURL     = "https://exp.host/--/api/v2/push/send"
	expoReceiptsURL = "https://exp.host/--/api/v2/push/getReceipts"

	// errDeviceNotRegistered is Expo's word for a token its provider (FCM or
	// APNs) no longer accepts: the app was uninstalled or lost its permission.
	errDeviceNotRegistered = "DeviceNotRegistered"
)

var (
	ErrNoRegistration = errors.New("no push token registered")
	// ErrCredentials means Expo has no usable FCM or APNs credential for the
	// registered app. The client reports it once per token itself, so a phone
	// whose platform has no credential yet does not report (and raise a Watch
	// problem) on every push.
	ErrCredentials = errors.New("push credentials unavailable")
	// ErrDeviceGone means Expo says the device's token is no longer accepted;
	// the client has dropped it and reported that once.
	ErrDeviceGone = errors.New("push device no longer registered")
	// A summary read off a terminal can start with the log line's timestamp.
	notificationTimestampPrefix = regexp.MustCompile(`^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}\s*`)
)

// maxNotificationBodyRunes is how much of a summary a lock screen shows
// without cutting a word the person needs.
const maxNotificationBodyRunes = 110

// Client sends push notifications via Expo Push API.
type Client struct {
	httpClient *http.Client
	mu         sync.RWMutex
	// registrations are the paired devices' Expo push tokens, by device ID: a
	// notification goes to every phone that asked for them.
	registrations map[string]registration
	// liveDevices are the devices that asked for live state; a re-registration
	// does not change it, and ForgetDevice drops it.
	liveDevices map[string]bool
	observer    Observer

	receiptDelays    []time.Duration
	credentialLogged map[string]bool
	receiptMu        sync.Mutex
	pending          []pendingReceipt
	receiptTimer     *time.Timer
}

type registration struct {
	deviceID  string
	token     string
	serverRef string // Opaque client-side server identifier for notification routing
}

// New creates a push notification client.
func New() *Client {
	return &Client{
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		receiptDelays: defaultReceiptDelays,
	}
}

// SetRegistration records a device's Expo push token and client-provided
// server reference. A device registers again on every connection, replacing
// its earlier entry; a token a different device held before is that install's
// and moves with it.
func (c *Client) SetRegistration(deviceID, token, serverRef string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token == "" {
		return
	}
	for id, existing := range c.registrations {
		if existing.token == token {
			delete(c.registrations, id)
		}
	}
	if c.registrations == nil {
		c.registrations = map[string]registration{}
	}
	c.registrations[deviceID] = registration{deviceID: deviceID, token: token, serverRef: serverRef}
}

// ForgetDevice drops a device's registration, such as a revoked one's: it must
// not keep hearing what its agents say.
func (c *Client) ForgetDevice(deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.registrations, deviceID)
	delete(c.liveDevices, deviceID)
}

// HasToken returns true if any device has a push token registered.
func (c *Client) HasToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.registrations) > 0
}

// Message represents a push notification.
type Message struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Priority string            `json:"priority,omitempty"` // "high" or "default"
	Sound    string            `json:"sound,omitempty"`
}

// Send sends a push notification to every registered device.
func (c *Client) Send(msg Message) error {
	return c.dispatch(KindOther, func(registration) Message { return msg })
}

// snapshot returns the registrations in a stable order.
func (c *Client) snapshot() []registration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]registration, 0, len(c.registrations))
	for _, r := range c.registrations {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b registration) int { return strings.Compare(a.deviceID, b.deviceID) })
	return out
}

// dispatch builds one notification for every registered device, sends each and
// tells the observer what became of it: one event for the notification, with
// one id every device's copy carries, so a tap on any phone answers it. Without
// a registration nothing is sent, but the observer still hears of the
// notification that would have been. It succeeds when any device took the
// notification; when none did, the returned error carries every device's
// failure.
func (c *Client) dispatch(kind string, build func(registration) Message) error {
	registrations := c.snapshot()
	event := Sent{ID: newNotificationID(), Kind: kind, At: time.Now().UTC()}
	if len(registrations) == 0 {
		msg := build(registration{})
		event.Title, event.Body, event.Priority, event.Refs = msg.Title, msg.Body, msg.Priority, msg.Data
		event.Outcome, event.Reason = OutcomeNotSent, ReasonNoRegistration
		c.observeSent(event)
		return ErrNoRegistration
	}
	// Watch judges and learns from one record per notification, however many
	// phones got it; the delivery receipt it hears of is the first device's
	// that Expo accepted.
	receipt := &receiptClaim{id: event.ID}
	var failures []error
	var first submitResult
	accepted := false
	for index, registration := range registrations {
		msg := build(registration)
		if index == 0 {
			event.Title, event.Body, event.Priority, event.Refs = msg.Title, msg.Body, msg.Priority, msg.Data
		}
		result, err := c.deliver(event.ID, receipt, registration, msg)
		if err == nil {
			accepted = true
			continue
		}
		if len(failures) == 0 {
			first = result
		}
		failures = append(failures, err)
	}
	if accepted {
		event.Outcome = OutcomeAccepted
	} else {
		event.Outcome, event.Reason, event.Detail = first.outcome, first.reason, first.detail
	}
	c.observeSent(event)
	if len(failures) == len(registrations) {
		if len(failures) == 1 {
			return failures[0]
		}
		return errors.Join(failures...)
	}
	for _, err := range failures {
		// A device whose credentials or registration is gone was logged when
		// it was found out; saying it on every push would only repeat it.
		if !errors.Is(err, ErrCredentials) && !errors.Is(err, ErrDeviceGone) {
			log.Printf("push: one of %d devices did not get the notification: %v", len(registrations), err)
		}
	}
	return nil
}

// receiptClaim hands a notification's id to the first delivery receipt that
// asks for it.
type receiptClaim struct {
	mu    sync.Mutex
	id    string
	taken bool
}

func (r *receiptClaim) take() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.taken {
		return ""
	}
	r.taken = true
	return r.id
}

// deliver sends one device's copy. The id in its payload is the
// notification's, which the app reports back on a tap.
func (c *Client) deliver(id string, receipt *receiptClaim, registration registration, msg Message) (submitResult, error) {
	data := make(map[string]string, len(msg.Data)+1)
	for key, value := range msg.Data {
		data[key] = value
	}
	data[dataNotificationID] = id
	msg.Data = data
	return c.submit(registration.token, msg, receipt)
}

func (c *Client) sendTo(token string, msg Message) error {
	_, err := c.submit(token, msg, nil)
	return err
}

// submitResult is what Expo's ticket said, in words the observer can keep.
type submitResult struct {
	outcome string
	reason  string
	detail  string
}

func failedResult(reason, detail string) submitResult {
	return submitResult{outcome: OutcomeFailed, reason: reason, detail: detail}
}

// receipt, when not nil, lends the notification id to the delivery receipt of
// the first copy Expo accepts.
func (c *Client) submit(token string, msg Message, receipt *receiptClaim) (submitResult, error) {
	payload := map[string]any{
		"to":    token,
		"title": msg.Title,
		"body":  msg.Body,
		"sound": "default",
		// The channel the app creates for alerts (importance max).
		"channelId": "mewla-agents",
	}
	if msg.Priority != "" {
		payload["priority"] = msg.Priority
	}
	if msg.Data != nil {
		payload["data"] = msg.Data
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return failedResult("marshal", ""), fmt.Errorf("marshal push: %w", err)
	}

	respBody, status, err := c.post(expoPushURL, body)
	if err != nil {
		return failedResult("network", ""), fmt.Errorf("send push: %w", err)
	}
	if status != http.StatusOK {
		problem := describeAPIErrors(respBody, token)
		return failedResult(fmt.Sprintf("http_%d", status), strings.TrimPrefix(problem, ": ")), fmt.Errorf("push API returned %d%s", status, problem)
	}
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return failedResult("bad_ticket", ""), fmt.Errorf("decode push ticket: %w", err)
	}
	// One message answers with one ticket object; a batch would answer with a list.
	var ticket expoTicket
	if err := json.Unmarshal(response.Data, &ticket); err != nil {
		return failedResult("bad_ticket", ""), fmt.Errorf("decode push ticket: %w", err)
	}
	if ticket.Status != "ok" {
		if ticket.Details.Error == errDeviceNotRegistered {
			c.dropToken(token)
		}
		problem := describeProblem(ticket.Details.Error, ticket.Message, token) + receiptHint(ticket.Details.Error)
		result := failedResult(firstNonEmpty(ticket.Details.Error, "rejected"), strings.TrimPrefix(problem, ": "))
		if isCredentialError(ticket.Details.Error) {
			c.logCredentialFailureOnce(token, "send"+problem)
			return result, fmt.Errorf("push API rejected notification%s: %w", problem, ErrCredentials)
		}
		if ticket.Details.Error == errDeviceNotRegistered {
			return result, fmt.Errorf("push API rejected notification%s: %w", problem, ErrDeviceGone)
		}
		return result, fmt.Errorf("push API rejected notification%s", problem)
	}
	notificationID := ""
	if receipt != nil {
		notificationID = receipt.take()
	}
	c.trackReceipt(ticket.ID, token, notificationID)
	return submitResult{outcome: OutcomeAccepted}, nil
}

func newNotificationID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("n_%x", time.Now().UnixNano())
	}
	return "n_" + hex.EncodeToString(raw[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *Client) post(url string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, 0, err
	}
	return respBody, resp.StatusCode, nil
}

// WorkerNotice is what a Worker notification says, as the sender knows it.
type WorkerNotice struct {
	WorkerID string
	// Window is the Worker's tmux window name, which names it in the title. It
	// can be a command path; only its last element is shown.
	Window string
	// Summary is the Worker's one-line state.
	Summary string
}

// label is the short name a title carries: the window name's last path
// element, or the Worker's ID when it has none.
func (n WorkerNotice) label() string {
	window := strings.TrimSpace(n.Window)
	if parts := strings.FieldsFunc(window, func(r rune) bool { return r == '/' || r == '\\' }); len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return strings.TrimSpace(n.WorkerID)
}

func normalizeNotificationSummary(summary string) string {
	collapsed := notificationTimestampPrefix.ReplaceAllString(strings.Join(strings.Fields(summary), " "), "")
	if collapsed == "" {
		return ""
	}

	runes := []rune(collapsed)
	if len(runes) <= maxNotificationBodyRunes {
		return collapsed
	}

	return string(runes[:maxNotificationBodyRunes-3]) + "..."
}

func buildNotificationBody(summary, fallback string) string {
	normalized := normalizeNotificationSummary(summary)
	if normalized != "" {
		return normalized
	}
	return fallback
}

func notificationData(workerID, serverRef string) map[string]string {
	return map[string]string{
		"worker_id": workerID,
		"screen":    "terminal",
		"server_id": serverRef,
	}
}

// workerMessage is the notification for a Worker that became blocked, failed or done.
func workerMessage(kind string, notice WorkerNotice, serverRef string) Message {
	label := notice.label()
	message := Message{Data: notificationData(notice.WorkerID, serverRef)}
	switch kind {
	case KindWorkerBlocked:
		message.Title, message.Body, message.Priority = label+" needs input", buildNotificationBody(notice.Summary, "Waiting for your response."), "high"
		if label == "" {
			message.Title = "Agent needs input"
		}
	case KindWorkerFailed:
		message.Title, message.Body, message.Priority = label+" failed", buildNotificationBody(notice.Summary, "Check the terminal for details."), "high"
		if label == "" {
			message.Title = "Agent failed"
		}
	default:
		message.Title, message.Body, message.Priority = label+" finished", buildNotificationBody(notice.Summary, "Session completed."), "default"
		if label == "" {
			message.Title = "Agent finished"
		}
	}
	return message
}

// WorkerMessageText is the title and body a Worker notification of kind would
// carry, for recording a notification that was held back.
func WorkerMessageText(kind string, notice WorkerNotice) (title, body string) {
	message := workerMessage(kind, notice, "")
	return message.Title, message.Body
}

// NotifyWorker sends the notification for a Worker that became blocked or
// failed (high priority) or done (neutral); kind is KindWorkerBlocked,
// KindWorkerFailed or KindWorkerDone.
func (c *Client) NotifyWorker(kind string, notice WorkerNotice) error {
	return c.dispatch(kind, func(r registration) Message {
		return workerMessage(kind, notice, r.serverRef)
	})
}

// BrainNotifier returns a sender for notifications that open the Brain
// thread, recorded under kind (KindBrainReply, KindWatchUrgent). The body is
// one line Brain chose to show; the message itself stays in Brain.
func (c *Client) BrainNotifier(kind string) func(threadID, title, body string) error {
	return func(threadID, title, body string) error {
		return c.dispatch(kind, func(r registration) Message {
			return Message{
				Title:    title,
				Body:     buildNotificationBody(body, "Brain has a question."),
				Priority: "high",
				Data: map[string]string{
					"brain_thread_id": threadID,
					"screen":          "brain",
					"server_id":       r.serverRef,
				},
			}
		})
	}
}

// NotifyScheduledResult sends a notification that deep-links to one canonical
// Calendar result projected in Brain. Result and failure contents deliberately
// never cross this boundary.
func (c *Client) NotifyScheduledResult(title, status, threadID, resultID string) error {
	label := strings.TrimSpace(title)
	if label == "" {
		label = "Scheduled Work"
	}
	body := "Open Brain for the scheduled result."
	priority := "default"
	switch status {
	case "completed":
		title = label + " is ready"
	case "failed":
		title = label + " failed"
		body = "Open Brain for the failure outcome."
		priority = "high"
	default:
		return fmt.Errorf("invalid scheduled result status %q", status)
	}
	return c.dispatch(KindScheduledResult, func(r registration) Message {
		return Message{
			Title:    title,
			Body:     body,
			Priority: priority,
			Data: map[string]string{
				"brain_message_id": resultID,
				"brain_thread_id":  threadID,
				"screen":           "brain",
				"server_id":        r.serverRef,
			},
		}
	})
}
