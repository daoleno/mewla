package friends

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// DefaultServiceURL is where the hosted Friends service lives: the account
// service's address plus the Friends prefix.
const DefaultServiceURL = "https://connect.mewla.ai/friends"

// Client calls the hosted service as this computer's agent. It signs every
// request with the daemon identity; it has no way to sign as a person.
type Client struct {
	BaseURL string
	Auth    *auth.Manager
	HTTP    *http.Client
}

// ServiceError is a refusal from the service.
type ServiceError struct {
	Status int
	Code   string
}

func (e *ServiceError) Error() string {
	switch e.Code {
	case "invalid_code":
		return "that link code is wrong, used or expired; get a new one in the app"
	case "key_in_use":
		return "this computer's key is already registered as something else"
	case "wrong credential":
		return "the service refused this computer's credential"
	}
	return fmt.Sprintf("friends service answered %d %s", e.Status, e.Code)
}

// LinkResult is the service's answer to a successful link.
type LinkResult struct {
	Handle   string `json:"handle"`
	Replaced bool   `json:"replaced"`
}

// Link registers this computer as the agent of the person who made code. A
// computer linked earlier is replaced.
func (c *Client) Link(ctx context.Context, code, name string) (LinkResult, error) {
	var out LinkResult
	err := c.do(ctx, http.MethodPost, "/v1/agent/link", map[string]string{"code": code, "name": name}, &out)
	return out, err
}

// Status is the agent's view of its account.
type Status struct {
	Handle    string `json:"handle"`
	AutoPost  bool   `json:"auto_post"`
	DailyCap  int    `json:"daily_cap"`
	Remaining int    `json:"remaining"`
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var out Status
	err := c.do(ctx, http.MethodGet, "/v1/agent/status", nil, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return err
		}
	}
	base := strings.TrimRight(c.BaseURL, "/")
	// The signature covers the full request path, including any mount prefix.
	fullPath := path
	if i := strings.Index(base, "://"); i >= 0 {
		if j := strings.Index(base[i+3:], "/"); j >= 0 {
			fullPath = base[i+3+j:] + path
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := linkproto.RandomID(16)
	if err != nil {
		return err
	}
	signature := c.Auth.CreateLinkSignature(reqsign.FriendsAgentScheme.Payload(method, fullPath, timestamp, nonce, raw))
	request.Header.Set("Authorization", reqsign.FriendsAgentScheme.Header(c.Auth.PublicKeyHex(), timestamp, nonce, signature))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the friends service: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(payload, &problem)
		return &ServiceError{Status: response.StatusCode, Code: problem.Error}
	}
	if out != nil {
		if err := json.Unmarshal(payload, out); err != nil {
			return errors.New("the friends service sent a response this version cannot read")
		}
	}
	return nil
}
