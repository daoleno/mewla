package push

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/diag"
)

// A ticket only says Expo accepted the message. Whether FCM or APNs took it
// (bad credentials, an uninstalled app) shows up in the receipt a little later,
// so each accepted ticket is looked up again on this schedule until it answers.
var defaultReceiptDelays = []time.Duration{20 * time.Second, 2 * time.Minute, 15 * time.Minute}

const maxReceiptBatch = 100

type expoTicket struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type pendingReceipt struct {
	ticketID       string
	notificationID string
	token          string
	attempt        int
	due            time.Time
}

// trackReceipt queues an accepted ticket for a delivery check.
func (c *Client) trackReceipt(ticketID, token, notificationID string) {
	if ticketID == "" {
		return
	}
	c.receiptMu.Lock()
	defer c.receiptMu.Unlock()
	c.pending = append(c.pending, pendingReceipt{
		ticketID:       ticketID,
		notificationID: notificationID,
		token:          token,
		due:            time.Now().Add(c.receiptDelays[0]),
	})
	c.armReceiptTimerLocked()
}

// armReceiptTimerLocked keeps exactly one timer, aimed at the earliest check.
func (c *Client) armReceiptTimerLocked() {
	if len(c.pending) == 0 || c.receiptTimer != nil {
		return
	}
	earliest := c.pending[0].due
	for _, entry := range c.pending[1:] {
		if entry.due.Before(earliest) {
			earliest = entry.due
		}
	}
	c.receiptTimer = time.AfterFunc(max(time.Until(earliest), 0), c.checkReceipts)
}

func (c *Client) checkReceipts() {
	c.receiptMu.Lock()
	c.receiptTimer = nil
	now := time.Now()
	var due, later []pendingReceipt
	for _, entry := range c.pending {
		if entry.due.After(now) {
			later = append(later, entry)
		} else {
			due = append(due, entry)
		}
	}
	c.pending = later
	c.receiptMu.Unlock()

	var retry []pendingReceipt
	for start := 0; start < len(due); start += maxReceiptBatch {
		batch := due[start:min(start+maxReceiptBatch, len(due))]
		retry = append(retry, c.fetchReceipts(batch)...)
	}

	c.receiptMu.Lock()
	c.pending = append(c.pending, retry...)
	c.armReceiptTimerLocked()
	c.receiptMu.Unlock()
}

// fetchReceipts reports each receipt it finds and returns the entries Expo has
// no receipt for yet, rescheduled for their next look.
func (c *Client) fetchReceipts(batch []pendingReceipt) []pendingReceipt {
	ids := make([]string, len(batch))
	for index, entry := range batch {
		ids[index] = entry.ticketID
	}
	payload, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return nil
	}
	body, status, err := c.post(expoReceiptsURL, payload)
	if err != nil || status != 200 {
		// Unknown outcome: look again on the next step rather than guess.
		return c.reschedule(batch)
	}
	var response struct {
		Data map[string]expoTicket `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return c.reschedule(batch)
	}
	var missing []pendingReceipt
	for _, entry := range batch {
		receipt, found := response.Data[entry.ticketID]
		if !found {
			missing = append(missing, entry)
			continue
		}
		if receipt.Status == "ok" {
			c.observeReceipt(Receipt{ID: entry.notificationID, At: time.Now().UTC(), OK: true})
			continue
		}
		if receipt.Details.Error == errDeviceNotRegistered {
			c.dropToken(entry.token)
		}
		problem := describeProblem(receipt.Details.Error, receipt.Message, entry.token) + receiptHint(receipt.Details.Error)
		c.observeReceipt(Receipt{
			ID: entry.notificationID, At: time.Now().UTC(),
			Reason: firstNonEmpty(receipt.Details.Error, "failed"), Detail: strings.TrimPrefix(problem, ": "),
		})
		if isCredentialError(receipt.Details.Error) {
			c.logCredentialFailureOnce(entry.token, "delivery"+problem)
			continue
		}
		diag.Report("push.delivery", diag.SeverityWarn, "", fmt.Sprintf("ticket %s%s", entry.ticketID, problem))
	}
	return c.reschedule(missing)
}

func (c *Client) reschedule(entries []pendingReceipt) []pendingReceipt {
	var next []pendingReceipt
	for _, entry := range entries {
		entry.attempt++
		if entry.attempt >= len(c.receiptDelays) {
			continue
		}
		entry.due = time.Now().Add(c.receiptDelays[entry.attempt])
		next = append(next, entry)
	}
	return next
}

// dropToken forgets a token Expo says is dead, unless the app has already
// registered a newer one. The app registers again on its next connection.
func (c *Client) dropToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != token {
		return
	}
	c.token = ""
	c.serverRef = ""
	log.Printf("push: %s: dropped the registered device; the app registers again when it next connects", errDeviceNotRegistered)
}

func isCredentialError(code string) bool {
	return code == "InvalidCredentials" || code == "MismatchSenderId"
}

// logCredentialFailureOnce logs a credential problem the first time a token
// hits it; every later push to that token fails the same way, silently.
func (c *Client) logCredentialFailureOnce(token, problem string) {
	c.mu.Lock()
	if c.credentialLogged == nil || len(c.credentialLogged) >= 16 {
		c.credentialLogged = map[string]bool{}
	}
	seen := c.credentialLogged[token]
	c.credentialLogged[token] = true
	c.mu.Unlock()
	if !seen {
		diag.Report("push.credentials", diag.SeverityError, "", problem+" (reported once per device)")
	}
}

func receiptHint(code string) string {
	switch code {
	case "InvalidCredentials", "MismatchSenderId":
		return " (Expo has no valid FCM or APNs credential for this app package, or it does not match the build's google-services.json)"
	}
	return ""
}

// describeProblem renders ": code: message" without ever echoing a push token.
func describeProblem(code, message, token string) string {
	parts := make([]string, 0, 2)
	if code != "" {
		parts = append(parts, code)
	}
	if message = strings.TrimSpace(message); message != "" {
		parts = append(parts, redact(message, token))
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ": ")
}

// describeAPIErrors summarizes the {"errors":[{code,message}]} body Expo sends
// with a request-level failure.
func describeAPIErrors(body []byte, token string) string {
	var response struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Errors) == 0 {
		return ""
	}
	return describeProblem(response.Errors[0].Code, response.Errors[0].Message, token)
}

func redact(text, token string) string {
	if token != "" {
		text = strings.ReplaceAll(text, token, "<token>")
	}
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return text
}
