package account

import (
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
)

const (
	gitHubDeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	gitHubDefaultBaseURL  = "https://github.com"
	gitHubDefaultAPIURL   = "https://api.github.com"
	// GitHubDeviceURL is where people enter a device code on github.com.
	GitHubDeviceURL      = gitHubDefaultBaseURL + "/login/device"
	maxIdentityBodyBytes = 1 << 20
)

// GitHubProvider implements the GitHub OAuth device flow.
type GitHubProvider struct {
	ClientID string
	// VerificationURI, when set, replaces GitHub's in what users are shown,
	// so they see the service's own address; it should redirect to
	// GitHubDeviceURL.
	VerificationURI string
	// BaseURL defaults to https://github.com; APIURL to https://api.github.com.
	BaseURL string
	APIURL  string
	Client  *http.Client
	Now     func() time.Time
}

func (p *GitHubProvider) Name() string { return "github" }

func (p *GitHubProvider) StartDevice(ctx context.Context) (DeviceStart, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("scope", "read:user")
	var response struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
		Error                   string `json:"error"`
	}
	if err := p.postForm(ctx, "/login/device/code", form, &response); err != nil {
		return DeviceStart{}, err
	}
	if response.Error != "" {
		return DeviceStart{}, fmt.Errorf("github device code: %s", sanitizeErrorCode(response.Error))
	}
	if response.DeviceCode == "" || response.UserCode == "" || response.VerificationURI == "" {
		return DeviceStart{}, errors.New("github device code: incomplete response")
	}
	interval := time.Duration(response.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if p.VerificationURI != "" {
		response.VerificationURI, response.VerificationURIComplete = p.VerificationURI, ""
	}
	expiresIn := time.Duration(response.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 15 * time.Minute
	}
	return DeviceStart{
		Handle:                  response.DeviceCode,
		UserCode:                response.UserCode,
		VerificationURI:         response.VerificationURI,
		VerificationURIComplete: response.VerificationURIComplete,
		Interval:                interval,
		ExpiresAt:               p.now().Add(expiresIn),
	}, nil
}

func (p *GitHubProvider) Poll(ctx context.Context, handle string) (PollResult, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("device_code", handle)
	form.Set("grant_type", gitHubDeviceGrantType)
	var response struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Interval    int64  `json:"interval"`
	}
	if err := p.postForm(ctx, "/login/oauth/access_token", form, &response); err != nil {
		return PollResult{}, err
	}
	switch response.Error {
	case "":
	case "authorization_pending":
		return PollResult{Status: PollPending}, nil
	case "slow_down":
		interval := time.Duration(response.Interval) * time.Second
		return PollResult{Status: PollPending, SlowDown: true, Interval: interval}, nil
	case "expired_token":
		return PollResult{Status: PollExpired}, nil
	case "access_denied":
		return PollResult{Status: PollDenied}, nil
	default:
		return PollResult{}, fmt.Errorf("github access token: %s", sanitizeErrorCode(response.Error))
	}
	if response.AccessToken == "" {
		return PollResult{}, errors.New("github access token: empty token")
	}
	subject, err := p.user(ctx, response.AccessToken)
	if err != nil {
		return PollResult{}, err
	}
	return PollResult{Status: PollComplete, Subject: subject}, nil
}

func (p *GitHubProvider) user(ctx context.Context, token string) (Subject, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiURL()+"/user", nil)
	if err != nil {
		return Subject{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := p.client().Do(request)
	if err != nil {
		return Subject{}, fmt.Errorf("github user: %w", redactURLError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Subject{}, fmt.Errorf("github user returned %s", response.Status)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxIdentityBodyBytes)).Decode(&user); err != nil {
		return Subject{}, errors.New("github user: malformed response")
	}
	if user.ID <= 0 {
		return Subject{}, errors.New("github user: missing id")
	}
	return Subject{ID: strconv.FormatInt(user.ID, 10), Login: user.Login}, nil
}

func (p *GitHubProvider) postForm(ctx context.Context, path string, form url.Values, out any) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL()+path,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := p.client().Do(request)
	if err != nil {
		return fmt.Errorf("github %s: %w", path, redactURLError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("github %s returned %s", path, response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxIdentityBodyBytes)).Decode(out); err != nil {
		return fmt.Errorf("github %s: malformed response", path)
	}
	return nil
}

func (p *GitHubProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return gitHubDefaultBaseURL
}

func (p *GitHubProvider) apiURL() string {
	if p.APIURL != "" {
		return strings.TrimRight(p.APIURL, "/")
	}
	return gitHubDefaultAPIURL
}

func (p *GitHubProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *GitHubProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// sanitizeErrorCode keeps provider error codes printable and short.
func sanitizeErrorCode(code string) string {
	var builder strings.Builder
	for _, character := range code {
		if builder.Len() >= 64 {
			break
		}
		if character == '_' || character == '-' ||
			(character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return "unknown_error"
	}
	return builder.String()
}

// redactURLError drops the request URL from transport errors.
func redactURLError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Err
	}
	return err
}
