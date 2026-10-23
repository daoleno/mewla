package push

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	// whose platform has no credential yet does not report on every push.
	ErrCredentials                   = errors.New("push credentials unavailable")
	notificationSessionSuffixPattern = regexp.MustCompile(`\s+\([^)]+\)\s*$`)
	notificationTimestampPrefix      = regexp.MustCompile(`^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}\s*`)
)

// Client sends push notifications via Expo Push API.
type Client struct {
	httpClient *http.Client
	mu         sync.RWMutex
	token      string // Expo push token from mobile app
	serverRef  string // Opaque client-side server identifier for notification routing
	observer   Observer

	receiptDelays    []time.Duration
	credentialLogged map[string]bool
	receiptMu        sync.Mutex
	pending          []pendingReceipt
	receiptTimer     *time.Timer
}

type registration struct {
	token     string
	serverRef string
}

// New creates a push notification client.
func New() *Client {
	return &Client{
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		receiptDelays: defaultReceiptDelays,
	}
}

// SetRegistration sets the Expo push token and client-provided server reference.
func (c *Client) SetRegistration(token, serverRef string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
	c.serverRef = serverRef
}

// HasToken returns true if a push token is registered.
func (c *Client) HasToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token != ""
}

// Message represents a push notification.
type Message struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Priority string            `json:"priority,omitempty"` // "high" or "default"
	Sound    string            `json:"sound,omitempty"`
}

// Send sends a push notification to the registered device.
func (c *Client) Send(msg Message) error {
	return c.dispatch(KindOther, func(registration) Message { return msg })
}

func (c *Client) registration() (registration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.token == "" {
		return registration{}, ErrNoRegistration
	}
	return registration{token: c.token, serverRef: c.serverRef}, nil
}

// dispatch builds one notification for the registered device, sends it and
// tells the observer what became of it. Without a registration nothing is
// sent, but the observer still hears of the notification that would have been.
func (c *Client) dispatch(kind string, build func(registration) Message) error {
	registration, regErr := c.registration()
	msg := build(registration)
	event := Sent{
		ID: newNotificationID(), Kind: kind, At: time.Now().UTC(),
		Title: msg.Title, Body: msg.Body, Priority: msg.Priority,
		Refs: msg.Data,
	}
	if regErr != nil {
		event.Outcome, event.Reason = OutcomeNotSent, ReasonNoRegistration
		c.observeSent(event)
		return regErr
	}
	// The app reports a tap with this id; it changes no visible text.
	data := make(map[string]string, len(msg.Data)+1)
	for key, value := range msg.Data {
		data[key] = value
	}
	data[dataNotificationID] = event.ID
	msg.Data = data
	result, err := c.submit(registration.token, msg, event.ID)
	event.Outcome, event.Reason, event.Detail = result.outcome, result.reason, result.detail
	c.observeSent(event)
	return err
}

func (c *Client) sendTo(token string, msg Message) error {
	_, err := c.submit(token, msg, "")
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

func (c *Client) submit(token string, msg Message, notificationID string) (submitResult, error) {
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
		return result, fmt.Errorf("push API rejected notification%s", problem)
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

func formatNotificationWorkerLabel(workerName, workerID string) string {
	raw := strings.TrimSpace(workerName)
	if raw == "" {
		raw = strings.TrimSpace(workerID)
	}
	if raw == "" {
		return ""
	}

	withoutSessionSuffix := notificationSessionSuffixPattern.ReplaceAllString(raw, "")
	parts := strings.FieldsFunc(withoutSessionSuffix, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return withoutSessionSuffix
	}

	return parts[len(parts)-1]
}

func normalizeNotificationSummary(summary string) string {
	collapsed := notificationTimestampPrefix.ReplaceAllString(strings.Join(strings.Fields(summary), " "), "")
	if collapsed == "" {
		return ""
	}

	runes := []rune(collapsed)
	if len(runes) <= 110 {
		return collapsed
	}

	return string(runes[:107]) + "..."
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
func workerMessage(kind, workerID, workerName, summary, serverRef string) Message {
	label := formatNotificationWorkerLabel(workerName, workerID)
	message := Message{Data: notificationData(workerID, serverRef)}
	switch kind {
	case KindWorkerBlocked:
		message.Title, message.Body, message.Priority = label+" needs input", buildNotificationBody(summary, "Waiting for your response."), "high"
		if label == "" {
			message.Title = "Agent needs input"
		}
	case KindWorkerFailed:
		message.Title, message.Body, message.Priority = label+" failed", buildNotificationBody(summary, "Check the terminal for details."), "high"
		if label == "" {
			message.Title = "Agent failed"
		}
	default:
		message.Title, message.Body, message.Priority = label+" finished", buildNotificationBody(summary, "Session completed."), "default"
		if label == "" {
			message.Title = "Agent finished"
		}
	}
	return message
}

// WorkerMessageText is the title and body a Worker notification of kind would
// carry, for recording a notification that was held back.
func WorkerMessageText(kind, workerID, workerName, summary string) (title, body string) {
	message := workerMessage(kind, workerID, workerName, summary, "")
	return message.Title, message.Body
}

func (c *Client) notifyWorker(kind, workerID, workerName, summary string) error {
	return c.dispatch(kind, func(r registration) Message {
		return workerMessage(kind, workerID, workerName, summary, r.serverRef)
	})
}

// NotifyWorkerBlocked sends a high-priority notification for a blocked agent.
func (c *Client) NotifyWorkerBlocked(workerID, workerName, summary string) error {
	return c.notifyWorker(KindWorkerBlocked, workerID, workerName, summary)
}

// NotifyWorkerFailed sends a high-priority notification for a failed agent.
func (c *Client) NotifyWorkerFailed(workerID, workerName, summary string) error {
	return c.notifyWorker(KindWorkerFailed, workerID, workerName, summary)
}

// NotifyWorkerDone sends a low-priority neutral completion notification.
func (c *Client) NotifyWorkerDone(workerID, workerName, summary string) error {
	return c.notifyWorker(KindWorkerDone, workerID, workerName, summary)
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
