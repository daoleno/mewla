package account

import (
	"context"
	"crypto/rand"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	fakeCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
	fakeLoginTTL     = 15 * time.Minute
	maxFakeLogins    = 1024
)

// FakeProvider is a development identity provider: the person types the user
// code and any username into GET /device and becomes "dev:<username>". It must
// only be enabled by an explicit operator flag.
type FakeProvider struct {
	// VerificationURI is the absolute URL of the /device page.
	VerificationURI string
	Interval        time.Duration
	Now             func() time.Time

	mu      sync.Mutex
	pending map[string]*fakeLogin
}

type fakeLogin struct {
	userCode  string
	expiresAt time.Time
	subject   Subject
	done      bool
}

func (p *FakeProvider) Name() string { return "dev" }

func (p *FakeProvider) StartDevice(context.Context) (DeviceStart, error) {
	handle, err := randomHex(16)
	if err != nil {
		return DeviceStart{}, err
	}
	userCode, err := fakeUserCode()
	if err != nil {
		return DeviceStart{}, err
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == nil {
		p.pending = make(map[string]*fakeLogin)
	}
	for key, login := range p.pending {
		if !now.Before(login.expiresAt) {
			delete(p.pending, key)
		}
	}
	if len(p.pending) >= maxFakeLogins {
		return DeviceStart{}, errors.New("too many pending development logins")
	}
	expiresAt := now.Add(fakeLoginTTL)
	p.pending[handle] = &fakeLogin{userCode: userCode, expiresAt: expiresAt}
	interval := p.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return DeviceStart{
		Handle:                  handle,
		UserCode:                userCode,
		VerificationURI:         p.VerificationURI,
		VerificationURIComplete: p.VerificationURI + "?user_code=" + url.QueryEscape(userCode),
		Interval:                interval,
		ExpiresAt:               expiresAt,
	}, nil
}

func (p *FakeProvider) Poll(_ context.Context, handle string) (PollResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	login, ok := p.pending[handle]
	if !ok || !p.now().Before(login.expiresAt) {
		delete(p.pending, handle)
		return PollResult{Status: PollExpired}, nil
	}
	if !login.done {
		return PollResult{Status: PollPending}, nil
	}
	delete(p.pending, handle)
	return PollResult{Status: PollComplete, Subject: login.subject}, nil
}

// complete marks the pending login with userCode as signed in by username.
func (p *FakeProvider) complete(userCode, username string) bool {
	userCode = normalizeUserCode(userCode)
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, login := range p.pending {
		if login.userCode == userCode && !login.done && now.Before(login.expiresAt) {
			login.done = true
			login.subject = Subject{ID: username, Login: username}
			return true
		}
	}
	return false
}

var fakeDevicePage = template.Must(template.New("device").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Mewla sign in (development)</title>
<style>
body{margin:0;font:16px/1.5 system-ui,-apple-system,sans-serif;background:#f6f6f4;color:#1d1d1b}
main{max-width:380px;margin:12vh auto;padding:0 16px}
h1{font-size:20px;font-weight:600;margin:0 0 4px}
p{color:#5c5c58;margin:0 0 24px}
label{display:block;font-size:14px;margin:16px 0 6px}
input{box-sizing:border-box;width:100%;padding:10px 12px;font:inherit;border:1px solid #cfcfca;border-radius:8px;background:#fff}
button{margin-top:24px;width:100%;padding:11px;font:inherit;font-weight:600;border:0;border-radius:8px;background:#1d1d1b;color:#fff;cursor:pointer}
.note{margin-top:24px;font-size:13px;color:#8a8a84}
.error{color:#b42318}
</style>
</head>
<body>
<main>
{{if .Done}}
<h1>You're signed in</h1>
<p>Return to Mewla. Your machine will connect in a few seconds.</p>
{{else}}
<h1>Sign in to Mewla remote access (development)</h1>
<p>Enter the code shown by Mewla and any username.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="">
<label for="user_code">Code</label>
<input id="user_code" name="user_code" value="{{.UserCode}}" autocomplete="off" autocapitalize="characters" required>
<label for="username">Username</label>
<input id="username" name="username" value="{{.Username}}" autocomplete="username" required>
<button type="submit">Sign in</button>
</form>
<p class="note">Development sign-in. Anyone who can reach this page can sign in as any username.</p>
{{end}}
</main>
</body>
</html>
`))

type fakeDevicePageData struct {
	UserCode string
	Username string
	Error    string
	Done     bool
}

// ServeHTTP serves GET and POST /device.
func (p *FakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	switch r.Method {
	case http.MethodGet:
		_ = fakeDevicePage.Execute(w, fakeDevicePageData{
			UserCode: normalizeUserCode(r.URL.Query().Get("user_code")),
		})
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = fakeDevicePage.Execute(w, fakeDevicePageData{Error: "The form could not be read."})
			return
		}
		userCode := normalizeUserCode(r.PostForm.Get("user_code"))
		username := sanitizeUsername(r.PostForm.Get("username"))
		data := fakeDevicePageData{UserCode: userCode, Username: username}
		if username == "" {
			w.WriteHeader(http.StatusBadRequest)
			data.Error = "Use letters, numbers and hyphens for the username."
			_ = fakeDevicePage.Execute(w, data)
			return
		}
		if !p.complete(userCode, username) {
			w.WriteHeader(http.StatusBadRequest)
			data.Error = "That code is not valid or has expired."
			_ = fakeDevicePage.Execute(w, data)
			return
		}
		_ = fakeDevicePage.Execute(w, fakeDevicePageData{Done: true})
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *FakeProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func fakeUserCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := make([]byte, 0, 9)
	for index, value := range raw {
		if index == 4 {
			code = append(code, '-')
		}
		code = append(code, fakeCodeAlphabet[int(value)%len(fakeCodeAlphabet)])
	}
	return string(code), nil
}

func normalizeUserCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) > 16 {
		value = value[:16]
	}
	return value
}

func sanitizeUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, character := range value {
		if builder.Len() >= 39 {
			break
		}
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '-' {
			builder.WriteRune(character)
		}
	}
	return strings.Trim(builder.String(), "-")
}
