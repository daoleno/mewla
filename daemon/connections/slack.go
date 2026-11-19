package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// slackReadScopes are the user scopes a Slack sign-in asks for to read: every
// kind of conversation (public and private channels, DMs and group DMs), its
// threads, search, and the files people share in them.
var slackReadScopes = []string{
	"channels:read", "channels:history",
	"groups:read", "groups:history",
	"im:read", "im:history",
	"mpim:read", "mpim:history",
	"search:read", "files:read",
}

// These are reviewed Slack Web API capabilities. Available tools are filtered
// against auth.test's X-OAuth-Scopes response; bot tokens never advertise the
// user-only search.messages operation, even with misleading token annotations.
var slackToolScopes = map[string][]string{
	"list_channels":   {"channels:read", "groups:read", "im:read", "mpim:read"},
	"channel_history": {"channels:history", "groups:history", "im:history", "mpim:history"},
	"thread_replies":  {"channels:history", "groups:history", "im:history", "mpim:history"},
	"search_messages": {"search:read"},
	"get_file":        {"files:read"},
	"send_message":    {"chat:write", "chat:write:bot", "chat:write:user"},
	"update_message":  {"chat:write", "chat:write:bot", "chat:write:user"},
}

func slackTools() []Tool {
	// The definitions of tools that existing accounts hold stay byte for byte:
	// a grant is bound to its definition, so a changed one would wait for the
	// person's confirmation. New reach goes into new tools.
	history := map[string]any{"type": "integer", "minimum": 1, "maximum": 15}
	return []Tool{
		builtin("get_me", "Read the authenticated Slack account and workspace", "GET", "/auth.test", true, nil, nil, nil),
		builtin("list_channels", "List channels visible to this account (one page)", "GET", "/conversations.list", true, nil, object(map[string]any{"limit": pageSize(), "cursor": str(), "types": str()}), nil),
		builtin("channel_history", "Read messages from a channel (one page)", "GET", "/conversations.history", true, nil, object(map[string]any{"channel": str(), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 15}, "cursor": str()}, "channel"), nil),
		builtin("thread_replies", "Read a thread: its parent message and replies, with their files (one page). ts is the parent message's ts. Works in channels, private channels and DMs; read a file with get_file", "GET", "/conversations.replies", true, nil, object(map[string]any{"channel": str(), "ts": str(), "limit": history, "cursor": str()}, "channel", "ts"), nil),
		builtin("search_messages", "Search messages using this user's search:read grant (one page)", "GET", "/search.messages", true, nil, object(map[string]any{"query": str(), "count": pageSize(), "page": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}}, "query"), nil),
		builtin("get_file", "Download a file shared in Slack (an image, PDF, document) to this computer. Pass its id (F…) from a message's files, or its files.slack.com address. Returns the local path, name, type and size; open the path to read it, an image with your image reader", "GET", "/files.info", true, nil, object(map[string]any{"file": str()}, "file"), nil),
		builtin("send_message", "Send a message to a Slack conversation", "POST", "/chat.postMessage", false, nil, nil, object(map[string]any{"channel": str(), "text": str(), "thread_ts": str()}, "channel", "text")),
		builtin("update_message", "Update a message posted by this account", "POST", "/chat.update", false, nil, nil, object(map[string]any{"channel": str(), "ts": str(), "text": str()}, "channel", "ts", "text")),
	}
}

func discoverSlack(m *Manager, ctx context.Context, r *record, secret string) ([]Tool, error) {
	var headers http.Header
	raw, err := m.request(ctx, r, r.Account.Endpoint, secret, "GET", "/auth.test", nil, &headers)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Team   string `json:"team"`
		User   string `json:"user"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.UserID == "" {
		return nil, errors.New("Slack did not return an account identity")
	}
	r.Account.Identity = identity.Team + " · " + identity.User
	granted := map[string]bool{}
	for _, scope := range strings.Split(headers.Get("X-OAuth-Scopes"), ",") {
		granted[strings.TrimSpace(scope)] = true
	}
	var tools []Tool
	for _, tool := range slackTools() {
		allowed := tool.Name == "get_me"
		for _, scope := range slackToolScopes[tool.Name] {
			if granted[scope] {
				allowed = true
			}
		}
		if tool.Name == "search_messages" && identity.BotID != "" {
			allowed = false
		}
		if allowed {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

// slackReconnect is what an old Slack sign-in says when it cannot do what a
// newer Mewla offers: the scope was never granted, and only signing in again
// asks Slack for it.
const slackReconnect = "This Slack sign-in was made before Mewla could read files and private conversations. Ask the person to Reconnect Slack in Extensions, then call again"

// slackRejected is a Slack Web API answer of {"ok": false}. Slack's error
// code is a short fixed word (missing_scope, channel_not_found), never the
// request's content, so it travels on; anything else is dropped.
func slackRejected(code string) error {
	switch {
	case code == "missing_scope":
		return errors.New("Slack refused the call: " + slackReconnect)
	case code != "" && oauthCode(code) == code:
		return fmt.Errorf("Slack rejected the call (%s); check account authorization and required scopes", code)
	}
	return errors.New("Slack rejected the call; check account authorization and required scopes")
}

// slackFileLimit bounds one downloaded file. Screenshots and documents are far
// smaller; a video export that is not gets a clear refusal instead. A
// variable only so a test can lower it.
var slackFileLimit int64 = 50 << 20

// slackFileTimeout bounds files.info and the download together.
const slackFileTimeout = 3 * time.Minute

// slackFileKeep is how long a downloaded file stays in the account's folder.
const slackFileKeep = 7 * 24 * time.Hour

var slackFileIDPattern = regexp.MustCompile(`^F[A-Z0-9]{6,}$`)

// slackFileID reads a file id from what an agent has at hand: the id itself,
// or one of Slack's addresses for the file (files.slack.com/files-pri/T…-F…/…,
// a workspace's /files/U…/F…/… permalink).
func slackFileID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if slackFileIDPattern.MatchString(value) {
		return value, nil
	}
	u, err := url.Parse(value)
	if err == nil && u.Scheme == "https" && (u.Hostname() == "slack.com" || strings.HasSuffix(u.Hostname(), ".slack.com")) {
		for _, segment := range strings.Split(u.Path, "/") {
			for _, part := range strings.Split(segment, "-") {
				if slackFileIDPattern.MatchString(part) {
					return part, nil
				}
			}
		}
	}
	return "", callRefused("Pass a Slack file id (F…) from a message's files, or the file's files.slack.com address")
}

// slackFileName keeps a downloaded file's own name readable and safe: letters
// and digits of any script, dot, dash and underscore, nothing that leaves the
// folder.
func slackFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := []rune(strings.Trim(b.String(), "."))
	if len(out) > 80 {
		out = append(out[:40], out[len(out)-40:]...)
	}
	if len(out) == 0 {
		return "file"
	}
	return string(out)
}

// slackFile is the part of files.info's answer get_file uses.
type slackFile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Mimetype   string `json:"mimetype"`
	Filetype   string `json:"filetype"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	IsExternal bool   `json:"is_external"`
	Download   string `json:"url_private_download"`
	Private    string `json:"url_private"`
	Permalink  string `json:"permalink"`
}

// invokeSlackFile runs get_file outside the manager lock: a download takes
// longer than an account change should wait. It is handled only for a Slack
// account's get_file.
func (m *Manager) invokeSlackFile(parent context.Context, q Request) (Response, bool, error) {
	m.mu.Lock()
	r, ok := m.records[q.ID]
	if !ok || m.isPending(q.ID) || r.Account.Integration != "slack" || q.Tool != "get_file" || usesMCP(r) {
		m.mu.Unlock()
		return Response{}, false, nil
	}
	t, args, err := checkInvoke(r, q)
	if err == nil && !slackReviewed(t) {
		err = errors.New("tool definition changed; refresh this account, then confirm the update in Extensions")
	}
	if err == nil && r.OAuth != nil && !contains(r.Account.Scopes, "files:read") {
		err = callRefused(slackReconnect)
	}
	secret := ""
	if err == nil {
		prep, cancel := context.WithTimeout(parent, 20*time.Second)
		secret, err = m.accountToken(prep, r)
		cancel()
		if err == nil && secret == "" {
			err = errAuth
		}
	}
	if err != nil {
		if errors.Is(err, errAuth) {
			r.Account.Status = "authorization_required"
			m.event(r, q.Tool, err)
			_ = m.save()
		}
		m.mu.Unlock()
		return Response{}, true, err
	}
	snapshot := *r
	snapshot.Account = cloneAccount(r.Account)
	sensitive := m.sensitiveValues(r, secret)
	dir := filepath.Join(filepath.Dir(m.path), "slack-files", r.Account.ID)
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(foreground(parent), slackFileTimeout)
	defer cancel()
	result, err := m.slackGetFile(ctx, &snapshot, secret, args, dir)
	if ctx.Err() != nil && err != nil {
		err = errors.New("Slack file download timed out; try again")
	}
	if err == nil {
		result = redactValues(result, sensitive)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok = m.records[q.ID]; ok {
		switch {
		case isTransportError(err, errAuth):
			r.Account.Status = "authorization_required"
		case err == nil:
			r.Account.Status = "connected"
			now := time.Now().UTC()
			r.Account.VerifiedAt = &now
		}
		m.event(r, q.Tool, err)
		_ = m.save()
	}
	if err != nil {
		return Response{}, true, err
	}
	return Response{Result: result}, true, nil
}

// slackReviewed: the tool is Mewla's reviewed get_file definition.
func slackReviewed(t Tool) bool {
	for _, candidate := range slackTools() {
		if candidate.Name == t.Name {
			return fingerprint(candidate) == fingerprint(t)
		}
	}
	return false
}

// slackGetFile reads a file's details with files.info, then streams the file
// from files.slack.com into dir with the account's token in the
// Authorization header, never in an address.
func (m *Manager) slackGetFile(ctx context.Context, r *record, secret string, args map[string]any, dir string) ([]byte, error) {
	query, _ := args["query"].(map[string]any)
	raw, _ := query["file"].(string)
	id, err := slackFileID(raw)
	if err != nil {
		return nil, err
	}
	answer, err := m.request(ctx, r, r.Account.Endpoint, secret, "GET", "/files.info?"+url.Values{"file": {id}, "count": {"1"}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var info struct {
		File slackFile `json:"file"`
	}
	if json.Unmarshal(answer, &info) != nil || info.File.ID == "" {
		return nil, errors.New("Slack did not describe this file")
	}
	f := info.File
	switch {
	case f.Mode == "tombstone":
		return nil, callRefused("This file was deleted from Slack")
	case f.Mode == "hidden_by_limit":
		return nil, callRefused("Slack hides this file: it is older than the workspace's plan shows")
	case f.IsExternal || f.Mode == "external":
		return nil, callRefused("This file lives outside Slack (" + f.Filetype + "); Slack has no copy to download")
	case f.Size > slackFileLimit:
		return nil, callRefused(fmt.Sprintf("This file is %d MB; Mewla downloads Slack files up to %d MB", f.Size>>20, slackFileLimit>>20))
	}
	source := f.Download
	if source == "" {
		source = f.Private
	}
	address, err := url.Parse(source)
	if err != nil || address.Scheme != "https" || address.Hostname() != "files.slack.com" || address.User != nil || address.Port() != "" {
		return nil, errors.New("Slack gave no downloadable address for this file")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.New("the download folder could not be created")
	}
	pruneSlackFiles(dir, time.Now())
	req, err := http.NewRequestWithContext(ctx, "GET", address.String(), nil)
	if err != nil {
		return nil, errors.New("invalid file address")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("User-Agent", "Mewla-Integrations")
	resp, err := m.fileClient().Do(req)
	if err != nil {
		// The transport error names the address; only a fixed cause travels on.
		if ctx.Err() != nil {
			return nil, errors.New("Slack file download timed out; try again")
		}
		return nil, errors.New("Slack file download failed")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, callRefused(slackReconnect)
	case resp.StatusCode == 404:
		return nil, callRefused("Slack has no file at this address any more")
	case resp.StatusCode != 200:
		return nil, fmt.Errorf("Slack file download returned HTTP %d", resp.StatusCode)
	case strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") && !strings.HasPrefix(strings.ToLower(f.Mimetype), "text/html"):
		// Without files:read Slack answers with its sign-in page, not the file.
		return nil, callRefused(slackReconnect)
	}
	part, err := os.CreateTemp(dir, ".part-*")
	if err != nil {
		return nil, errors.New("the download could not be saved")
	}
	defer os.Remove(part.Name())
	n, copyErr := io.Copy(part, io.LimitReader(resp.Body, slackFileLimit+1))
	closeErr := part.Close()
	switch {
	case n > slackFileLimit:
		return nil, callRefused(fmt.Sprintf("This file is larger than %d MB; Mewla downloads Slack files up to that size", slackFileLimit>>20))
	case copyErr != nil:
		if ctx.Err() != nil {
			return nil, errors.New("Slack file download timed out; try again")
		}
		return nil, errors.New("Slack file download was cut off; try again")
	case closeErr != nil:
		return nil, errors.New("the download could not be saved")
	}
	name := f.Name
	if name == "" {
		name = f.Title
	}
	path := filepath.Join(dir, f.ID+"-"+slackFileName(name))
	if err := os.Chmod(part.Name(), 0o600); err != nil || os.Rename(part.Name(), path) != nil {
		return nil, errors.New("the download could not be saved")
	}
	out, _ := json.Marshal(map[string]any{
		"path": path, "id": f.ID, "name": f.Name, "title": f.Title,
		"mimetype": f.Mimetype, "filetype": f.Filetype, "size": n, "permalink": f.Permalink,
	})
	return out, nil
}

// fileClient downloads from files.slack.com: the checked public-address
// dialing of every service request, no redirects (a redirect is Slack's
// sign-in page), and time for a large file.
func (m *Manager) fileClient() *http.Client {
	if !m.networkOwned {
		return m.http
	}
	return &http.Client{Transport: checkedTransport("", nil), Timeout: slackFileTimeout, CheckRedirect: noRedirects}
}

// pruneSlackFiles removes downloads older than slackFileKeep, and parts a
// stopped download left behind.
func pruneSlackFiles(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		age := now.Sub(info.ModTime())
		if age > slackFileKeep || strings.HasPrefix(entry.Name(), ".part-") && age > slackFileTimeout {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
