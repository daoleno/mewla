package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// ErrNotLiveCapable means the registered device never said it can show live
// state, so it gets no data-only pushes (an iOS token cannot use them).
var ErrNotLiveCapable = errors.New("push device did not declare live state support")

// liveCapable remembers, per client, whether the device that registered asked
// for live state. It lives here, not on Client, so push.go stays untouched.
var liveCapable sync.Map // *Client -> bool

// SetLiveCapable records the device's live_state capability from its latest
// registration. Registering again updates it.
func (c *Client) SetLiveCapable(capable bool) { liveCapable.Store(c, capable) }

// LiveCapable reports whether live state may be pushed to the device.
func (c *Client) LiveCapable() bool {
	value, ok := liveCapable.Load(c)
	capable, _ := value.(bool)
	return ok && capable
}

// SendLive pushes a data-only, high-priority message: no title, body or sound,
// so the phone shows nothing by itself and its app code decides what to post.
// No channelId: Expo turns it into an FCM notification block, and a phone whose
// app is not running would then show an empty system notification instead of
// waking the app's service. Values are sent as strings, which is all an FCM data message carries.
// The ticket is handled like any other push (dead tokens dropped, credential
// problems logged once per token); live messages are not receipt-tracked,
// because a newer state always follows and a lost one costs nothing.
func (c *Client) SendLive(data map[string]any) error {
	registration, err := c.registration()
	if err != nil {
		return err
	}
	if !c.LiveCapable() {
		return ErrNotLiveCapable
	}
	token := registration.token
	flat := make(map[string]string, len(data)+2)
	for key, value := range data {
		switch typed := value.(type) {
		case string:
			flat[key] = typed
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				return fmt.Errorf("marshal live push %s: %w", key, err)
			}
			flat[key] = string(encoded)
		}
	}
	flat["mewla_live"] = "1"
	// The phone ignores state from a server that is not its current one.
	flat["server_ref"] = registration.serverRef

	body, err := json.Marshal(map[string]any{
		"to":       token,
		"data":     flat,
		"priority": "high",
		"ttl":      120,
	})
	if err != nil {
		return fmt.Errorf("marshal live push: %w", err)
	}
	respBody, status, err := c.post(expoPushURL, body)
	if err != nil {
		return fmt.Errorf("send live push: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("push API returned %d%s", status, describeAPIErrors(respBody, token))
	}
	var response struct {
		Data struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details struct {
				Error string `json:"error"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil {
		return fmt.Errorf("decode push ticket: %w", err)
	}
	ticket := response.Data
	if ticket.Status == "ok" {
		return nil
	}
	if ticket.Details.Error == errDeviceNotRegistered {
		c.dropToken(token)
	}
	problem := describeProblem(ticket.Details.Error, ticket.Message, token) + receiptHint(ticket.Details.Error)
	if isCredentialError(ticket.Details.Error) {
		c.logCredentialFailureOnce(token, "send"+problem)
		return fmt.Errorf("push API rejected live state%s: %w", problem, ErrCredentials)
	}
	return fmt.Errorf("push API rejected live state%s", problem)
}
