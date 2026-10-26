package remote

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// ErrSignedOut means the service no longer knows this computer: it was
// signed out elsewhere or its account was disabled.
var ErrSignedOut = errors.New("this computer is no longer signed in to remote access")

// ErrLoginPending is returned by PollLogin until the person finishes in the browser.
var ErrLoginPending = errors.New("sign-in is not finished yet")

// Client calls the hosted service, signing every request with the daemon key.
type Client struct {
	BaseURL string
	Auth    *auth.Manager
	HTTP    *http.Client
}

func NewClient(baseURL, caPEM string, authManager *auth.Manager) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(caPEM) != "" {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, errors.New("remote access service CA contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &Client{
		BaseURL: strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
		Auth:    authManager,
		HTTP:    &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}, nil
}

type LoginStart struct {
	LoginID                 string `json:"login_id"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	IntervalSeconds         int    `json:"interval_seconds"`
	ExpiresAtMS             int64  `json:"expires_at_ms"`
}

func (client *Client) StartLogin(ctx context.Context, machineName, routeID string) (LoginStart, error) {
	var start LoginStart
	_, err := client.do(ctx, http.MethodPost, "/v1/login/start", map[string]string{
		"machine_name": machineName,
		"route_id":     routeID,
	}, &start)
	return start, err
}

// PollLogin returns ErrLoginPending until the login completes.
func (client *Client) PollLogin(ctx context.Context, loginID string) (Machine, error) {
	var response struct {
		Status  string  `json:"status"`
		Machine Machine `json:"machine"`
	}
	status, err := client.do(ctx, http.MethodPost, "/v1/login/poll", map[string]string{"login_id": loginID}, &response)
	if err != nil {
		return Machine{}, err
	}
	if status == http.StatusAccepted || response.Status == "pending" {
		return Machine{}, ErrLoginPending
	}
	return response.Machine, nil
}

func (client *Client) Machine(ctx context.Context) (Machine, error) {
	var machine Machine
	_, err := client.do(ctx, http.MethodGet, "/v1/machine", nil, &machine)
	return machine, err
}

// Rename gives this computer a new address label. The route and pairings
// stay; only the browser address changes.
func (client *Client) Rename(ctx context.Context, name string) (Machine, error) {
	var machine Machine
	_, err := client.do(ctx, http.MethodPut, "/v1/machine/name", map[string]string{"name": name}, &machine)
	return machine, err
}

func (client *Client) SetACMEChallenge(ctx context.Context, fqdn, value string) error {
	_, err := client.do(ctx, http.MethodPut, "/v1/machine/acme-challenge", map[string]string{"fqdn": fqdn, "value": value}, nil)
	return err
}

func (client *Client) ClearACMEChallenge(ctx context.Context, fqdn, value string) error {
	_, err := client.do(ctx, http.MethodDelete, "/v1/machine/acme-challenge", map[string]string{"fqdn": fqdn, "value": value}, nil)
	return err
}

func (client *Client) Logout(ctx context.Context) error {
	_, err := client.do(ctx, http.MethodDelete, "/v1/machine", nil, nil)
	return err
}

// DeleteAccount deletes this computer's account at the service and signs
// out every computer signed in with it.
func (client *Client) DeleteAccount(ctx context.Context) error {
	_, err := client.do(ctx, http.MethodDelete, "/v1/account", nil, nil)
	return err
}

func (client *Client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var raw []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		raw = encoded
	}
	request, err := http.NewRequestWithContext(ctx, method, client.BaseURL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", client.authorization(method, path, raw))
	response, err := client.HTTP.Do(request)
	if err != nil {
		return 0, fmt.Errorf("reach remote access service: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return response.StatusCode, err
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden && path == "/v1/machine" ||
		response.StatusCode == http.StatusNotFound && (strings.HasPrefix(path, "/v1/machine") || path == "/v1/account"):
		return response.StatusCode, ErrSignedOut
	case response.StatusCode >= 300:
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(payload, &problem)
		if problem.Error == "" {
			problem.Error = strings.TrimSpace(string(payload))
		}
		return response.StatusCode, &ServiceError{Status: response.StatusCode, Code: problem.Error}
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return response.StatusCode, fmt.Errorf("decode remote access response: %w", err)
		}
	}
	return response.StatusCode, nil
}

func (client *Client) authorization(method, path string, body []byte) string {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonce, _ := linkproto.RandomID(16)
	signature := client.Auth.CreateLinkSignature(reqsign.AccountScheme.Payload(method, path, timestamp, nonce, body))
	return reqsign.AccountScheme.Header(client.Auth.PublicKeyHex(), timestamp, nonce, signature)
}

// ServiceError is a non-success answer from the service.
type ServiceError struct {
	Status int
	Code   string
}

func (err *ServiceError) Error() string {
	switch err.Code {
	case "expired":
		return "the sign-in code expired; run mewla login again"
	case "denied":
		return "sign-in was declined in the browser"
	case "quota":
		return "this account already has the most computers it can sign in; sign one out first"
	case "account disabled":
		return "this account is disabled; to delete it, write to the address at https://mewla.ai/privacy"
	}
	if err.Code == "" {
		return fmt.Sprintf("remote access service answered %d", err.Status)
	}
	return fmt.Sprintf("remote access service: %s", err.Code)
}
