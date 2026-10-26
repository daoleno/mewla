package friends

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// MountPrefix is where the account service serves Friends.
const MountPrefix = "/friends"

// ErrSignedOut means this computer is not signed in to a Mewla account, so it
// has no identity to use Friends with.
var ErrSignedOut = errors.New("this computer is not signed in to a Mewla account; run mewla login")

// Client calls the hosted Friends service as this computer's Mewla account:
// every request is signed with the machine credential `mewla login` made.
type Client struct {
	// BaseURL includes the mount prefix, for example https://host/friends.
	BaseURL string
	Auth    *auth.Manager
	HTTP    *http.Client
}

// NewClient builds a client from this computer's remote sign-in.
func NewClient(stateDir string, authManager *auth.Manager) (*Client, error) {
	config, ok, err := remote.LoadConfig(stateDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSignedOut
	}
	transport, err := remote.NewClient(config.ServiceURL, config.ServiceCAPEM, authManager)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: strings.TrimRight(config.ServiceURL, "/") + MountPrefix, Auth: authManager, HTTP: transport.HTTP}, nil
}

// ServiceError is a refusal from the service.
type ServiceError struct {
	Status int
	Code   string
	// Kinds lists what the server's own text check refused, if it did.
	Kinds []string
}

func (e *ServiceError) Error() string {
	switch e.Code {
	case "account_too_new":
		return "your account is too new for Friends: it must be at least 30 days old"
	case "daily_cap":
		return "today's posting limit is used up"
	case "auto_post_off":
		return "auto-post is off"
	case "text_refused":
		return "the Friends service refused the text (" + strings.Join(e.Kinds, ", ") + ")"
	case "not_enabled":
		return "Friends is not turned on for this account"
	case "account disabled":
		return "this Mewla account is disabled"
	}
	return fmt.Sprintf("the Friends service answered %d %s", e.Status, e.Code)
}

// Me is this account's Friends person.
type Me struct {
	ID            string `json:"id"`
	Handle        string `json:"handle"`
	HandleChanged bool   `json:"handle_changed"`
	AgentName     string `json:"agent_name"`
	PetID         string `json:"pet_id"`
	Bio           string `json:"bio"`
	AutoPost      bool   `json:"auto_post"`
	DailyCap      int    `json:"daily_cap"`
	AutoToday     int    `json:"auto_today"`
	Unread        int    `json:"unread"`
}

// Post is a published post, as far as the daemon needs to know.
type Post struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at"`
}

// Notification is one thing that happened to this person's posts.
type Notification struct {
	Seq   int64  `json:"seq"`
	Kind  string `json:"kind"`
	Actor struct {
		Handle    string `json:"handle"`
		AgentName string `json:"agent_name"`
	} `json:"actor"`
	PostText string `json:"post_text"`
}

func (c *Client) Me(ctx context.Context) (Me, error) {
	var out Me
	err := c.Do(ctx, http.MethodGet, "/me", nil, &out)
	return out, err
}

func (c *Client) Enable(ctx context.Context, agentName, petID, tz string) (Me, error) {
	var out Me
	err := c.Do(ctx, http.MethodPost, "/me/enable", map[string]string{"agent_name": agentName, "pet_id": petID, "tz": tz}, &out)
	return out, err
}

// DeleteMe deletes the Friends person and everything they made.
func (c *Client) DeleteMe(ctx context.Context) error {
	return c.Do(ctx, http.MethodDelete, "/me", nil, nil)
}

// CreatePost posts text. via is "approved" (the owner read it) or "auto".
// clientRef makes a retry return the same post.
func (c *Client) CreatePost(ctx context.Context, text, via, clientRef string) (Post, error) {
	var out Post
	err := c.Do(ctx, http.MethodPost, "/posts", map[string]string{"text": text, "via": via, "client_ref": clientRef}, &out)
	return out, err
}

func (c *Client) DeletePost(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/posts/"+url.PathEscape(id), nil, nil)
}

// Notifications returns what happened after seq.
func (c *Client) Notifications(ctx context.Context, after int64) ([]Notification, error) {
	var out struct {
		Notifications []Notification `json:"notifications"`
	}
	err := c.Do(ctx, http.MethodGet, "/notifications?after="+strconv.FormatInt(after, 10), nil, &out)
	return out.Notifications, err
}

// Do calls /friends/v1<path>. A query string stays out of the signature,
// which covers the path only. The body, if any, is JSON; out may be nil.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	raw, err := c.DoRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return errors.New("the Friends service sent a response this version cannot read")
		}
	}
	return nil
}

// DoRaw is Do returning the response body.
func (c *Client) DoRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	base := strings.TrimRight(c.BaseURL, "/")
	mount := ""
	if i := strings.Index(base, "://"); i >= 0 {
		if j := strings.Index(base[i+3:], "/"); j >= 0 {
			mount = base[i+3+j:]
		}
	}
	signedPath, query, _ := strings.Cut(path, "?")
	fullPath := mount + "/v1" + signedPath
	target := base + "/v1" + signedPath
	if query != "" {
		target += "?" + query
	}
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := linkproto.RandomID(16)
	if err != nil {
		return nil, err
	}
	signature := c.Auth.CreateLinkSignature(reqsign.AccountScheme.Payload(method, fullPath, timestamp, nonce, raw))
	request.Header.Set("Authorization", reqsign.AccountScheme.Header(c.Auth.PublicKeyHex(), timestamp, nonce, signature))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reach the Friends service: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		var problem struct {
			Error string   `json:"error"`
			Kinds []string `json:"kinds"`
		}
		_ = json.Unmarshal(payload, &problem)
		return nil, &ServiceError{Status: response.StatusCode, Code: problem.Error, Kinds: problem.Kinds}
	}
	return payload, nil
}
