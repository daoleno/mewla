package friendsvc

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

// Identity is who GitHub says signed in.
type Identity struct {
	ID        string
	Login     string
	CreatedAt time.Time
}

// OAuth is a web authorization-code identity provider.
type OAuth interface {
	// AuthorizeURL is where the browser goes first. hints carries the start
	// request's query for providers that need it (only the fake one reads it).
	AuthorizeURL(state, callbackURL string, hints url.Values) string
	// Exchange trades the callback's code for an identity. Implementations
	// keep no token and never log the code.
	Exchange(ctx context.Context, code, callbackURL string) (Identity, error)
}

// GitHubWeb is GitHub's OAuth web flow. It asks for no scope: the public
// profile (id, login, created_at) is all Friends reads, and the token is
// dropped as soon as the profile is read.
type GitHubWeb struct {
	ClientID     string
	ClientSecret string
	// BaseURL defaults to https://github.com; APIURL to https://api.github.com.
	BaseURL string
	APIURL  string
	Client  *http.Client
}

func (g *GitHubWeb) base() string {
	if g.BaseURL != "" {
		return strings.TrimRight(g.BaseURL, "/")
	}
	return "https://github.com"
}

func (g *GitHubWeb) api() string {
	if g.APIURL != "" {
		return strings.TrimRight(g.APIURL, "/")
	}
	return "https://api.github.com"
}

func (g *GitHubWeb) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (g *GitHubWeb) AuthorizeURL(state, callbackURL string, _ url.Values) string {
	query := url.Values{}
	query.Set("client_id", g.ClientID)
	query.Set("redirect_uri", callbackURL)
	query.Set("state", state)
	return g.base() + "/login/oauth/authorize?" + query.Encode()
}

func (g *GitHubWeb) Exchange(ctx context.Context, code, callbackURL string) (Identity, error) {
	form := url.Values{}
	form.Set("client_id", g.ClientID)
	form.Set("client_secret", g.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", callbackURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base()+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := g.doJSON(request, &token); err != nil {
		return Identity{}, fmt.Errorf("github token: %w", err)
	}
	if token.Error != "" || token.AccessToken == "" {
		return Identity{}, errors.New("github token: " + sanitizeCode(token.Error))
	}
	profile, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api()+"/user", nil)
	if err != nil {
		return Identity{}, err
	}
	profile.Header.Set("Authorization", "Bearer "+token.AccessToken)
	profile.Header.Set("Accept", "application/vnd.github+json")
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		CreatedAt string `json:"created_at"`
	}
	if err := g.doJSON(profile, &user); err != nil {
		return Identity{}, fmt.Errorf("github profile: %w", err)
	}
	created, err := time.Parse(time.RFC3339, user.CreatedAt)
	if err != nil || user.ID <= 0 || user.Login == "" {
		return Identity{}, errors.New("github profile: incomplete response")
	}
	return Identity{ID: strconv.FormatInt(user.ID, 10), Login: user.Login, CreatedAt: created}, nil
}

func (g *GitHubWeb) doJSON(request *http.Request, out any) error {
	response, err := g.client().Do(request)
	if err != nil {
		return errors.New("request failed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return errors.New("unreadable response")
	}
	if response.StatusCode >= 500 || (response.StatusCode >= 300 && response.StatusCode != http.StatusUnauthorized) {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("invalid response")
	}
	return nil
}

func sanitizeCode(code string) string {
	if code == "" {
		return "no token"
	}
	for _, c := range code {
		if (c < 'a' || c > 'z') && c != '_' {
			return "error"
		}
	}
	return code
}

// FakeOAuth is DEVELOPMENT ONLY: it signs in whoever the start request names,
// with no GitHub involved, so the flow can be exercised against a local
// service. The start query may carry fake_login and fake_age_days.
type FakeOAuth struct {
	DefaultLogin   string
	DefaultAgeDays int
	Now            func() time.Time
}

func (f *FakeOAuth) AuthorizeURL(state, callbackURL string, hints url.Values) string {
	login := hints.Get("fake_login")
	if login == "" {
		login = f.DefaultLogin
	}
	days := f.DefaultAgeDays
	if n, err := strconv.Atoi(hints.Get("fake_age_days")); err == nil && n >= 0 {
		days = n
	}
	query := url.Values{}
	query.Set("state", state)
	query.Set("code", "fake:"+login+":"+strconv.Itoa(days))
	return callbackURL + "?" + query.Encode()
}

func (f *FakeOAuth) Exchange(_ context.Context, code, _ string) (Identity, error) {
	parts := strings.Split(code, ":")
	if len(parts) != 3 || parts[0] != "fake" || parts[1] == "" {
		return Identity{}, errors.New("fake code")
	}
	days, err := strconv.Atoi(parts[2])
	if err != nil {
		return Identity{}, errors.New("fake code")
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	id := int64(0)
	for _, c := range parts[1] {
		id = id*31 + int64(c)
		id %= 1 << 40
	}
	return Identity{ID: strconv.FormatInt(id+1, 10), Login: parts[1], CreatedAt: now().Add(-time.Duration(days) * 24 * time.Hour)}, nil
}
