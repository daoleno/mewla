package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// slackUploadFiles is how many files one send_files message carries.
const slackUploadFiles = 10

// slackUploadLimit bounds one uploaded file: Slack's own limit is 1 GB.
var slackUploadLimit int64 = 1 << 30

// slackUploadTimeout bounds one send_files call: a base for the handshakes,
// and time for the bytes at a slow uplink's pace.
func slackUploadTimeout(total int64) time.Duration {
	return slackFileTimeout + time.Duration(total>>19)*time.Second
}

var slackMimePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*$`)

// slackUpload is one file ready to send: opened, measured and named.
type slackUpload struct {
	file     *os.File
	size     int64
	name     string
	mimetype string
	title    string
	alt      string
}

// slackUploadSource opens the file an agent named. The path must be absolute
// (the daemon's working folder is not the agent's), a regular file, and not
// inside the daemon's own state folder, which holds the account tokens; files
// Mewla downloaded from Slack there may be sent on.
func slackUploadSource(path, state, downloads string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, callRefused("Pass each file's absolute path on this computer: " + cutRunes(path, 120) + " is not absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return nil, nil, callRefused("No file at " + cutRunes(path, 120) + " on this computer")
	}
	if inside(resolved, resolveOr(state)) && !inside(resolved, resolveOr(filepath.Dir(downloads))) {
		return nil, nil, callRefused("Mewla's own state folder is not sent to Slack; copy the file elsewhere first if you mean it")
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, nil, callRefused("The file at " + cutRunes(path, 120) + " could not be opened")
	}
	info, err := f.Stat()
	switch {
	case err != nil:
		f.Close()
		return nil, nil, callRefused("The file at " + cutRunes(path, 120) + " could not be read")
	case !info.Mode().IsRegular():
		f.Close()
		return nil, nil, callRefused(cutRunes(path, 120) + " is not a regular file; zip a folder first")
	case info.Size() == 0:
		f.Close()
		return nil, nil, callRefused(cutRunes(path, 120) + " is empty; Slack takes no empty files")
	case info.Size() > slackUploadLimit:
		f.Close()
		return nil, nil, callRefused(fmt.Sprintf("%s is %d MB; Slack takes files up to %d MB", cutRunes(path, 120), info.Size()>>20, slackUploadLimit>>20))
	}
	return f, info, nil
}

func resolveOr(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// slackMime is the file's type: the one the agent gave, else the one its
// extension stands for, else what its first bytes look like.
func slackMime(given, name string, f *os.File) string {
	if given != "" {
		return given
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); t != "" {
		if media, _, err := mime.ParseMediaType(t); err == nil {
			return media
		}
	}
	head := make([]byte, 512)
	n, _ := f.ReadAt(head, 0)
	if media, _, err := mime.ParseMediaType(http.DetectContentType(head[:n])); err == nil {
		return media
	}
	return "application/octet-stream"
}

// slackUploadName is the file's name as Slack shows it: the agent's, else the
// file's own, without any folder.
func slackUploadName(given, path string) string {
	name := given
	if name == "" {
		name = filepath.Base(path)
	}
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "file"
	}
	return name
}

// slackUploadAddress accepts only what Slack's upload step should hand out:
// an https address on files.slack.com. The token never goes anywhere else.
func slackUploadAddress(raw string) (*url.URL, error) {
	address, err := url.Parse(raw)
	if err != nil || address.Scheme != "https" || address.Hostname() != "files.slack.com" || address.User != nil || address.Port() != "" {
		return nil, errors.New("Slack gave no valid upload address")
	}
	return address, nil
}

// uploadClient sends to files.slack.com: the checked public-address dialing of
// every service request and no redirects. The call's own deadline bounds it.
func (m *Manager) uploadClient() *http.Client {
	if !m.networkOwned {
		return m.http
	}
	return &http.Client{Transport: checkedTransport("", nil), CheckRedirect: noRedirects}
}

// slackSendFiles posts local files with Slack's upload flow: for each file
// files.getUploadURLExternal, then its bytes streamed to the address Slack
// gave, then one files.completeUploadExternal for all of them, which shares
// them in the conversation as one message.
func (m *Manager) slackSendFiles(parent context.Context, r *record, secret string, args map[string]any, state, downloads string) ([]byte, error) {
	body, _ := args["body"].(map[string]any)
	channel, _ := body["channel"].(string)
	text, _ := body["text"].(string)
	thread, _ := body["thread_ts"].(string)
	entries, _ := body["files"].([]any)

	var uploads []*slackUpload
	defer func() {
		for _, u := range uploads {
			u.file.Close()
		}
	}()
	var total int64
	for _, entry := range entries {
		e, _ := entry.(map[string]any)
		path, _ := e["path"].(string)
		given, _ := e["filename"].(string)
		kind, _ := e["mimetype"].(string)
		title, _ := e["title"].(string)
		alt, _ := e["alt_text"].(string)
		if kind != "" && !slackMimePattern.MatchString(kind) {
			return nil, callRefused("mimetype must look like image/png or application/pdf")
		}
		f, info, err := slackUploadSource(path, state, downloads)
		if err != nil {
			return nil, err
		}
		name := slackUploadName(given, path)
		uploads = append(uploads, &slackUpload{file: f, size: info.Size(), name: name, mimetype: slackMime(strings.ToLower(kind), name, f), title: title, alt: alt})
		total += info.Size()
	}

	ctx, cancel := context.WithTimeout(parent, slackUploadTimeout(total))
	defer cancel()
	timedOut := func(err error) error {
		if ctx.Err() != nil {
			return errors.New("Slack file upload timed out; try again")
		}
		return err
	}

	type shared struct {
		ID    string `json:"id"`
		Title string `json:"title,omitempty"`
	}
	var complete []shared
	for _, u := range uploads {
		query := url.Values{"filename": {u.name}, "length": {fmt.Sprint(u.size)}}
		if u.alt != "" {
			query.Set("alt_txt", u.alt)
		}
		answer, err := m.request(ctx, r, r.Account.Endpoint, secret, "POST", "/files.getUploadURLExternal?"+query.Encode(), nil)
		if err != nil {
			return nil, timedOut(err)
		}
		var slot struct {
			UploadURL string `json:"upload_url"`
			FileID    string `json:"file_id"`
		}
		if json.Unmarshal(answer, &slot) != nil || slot.FileID == "" || !slackFileIDPattern.MatchString(slot.FileID) {
			return nil, errors.New("Slack did not open an upload for this file")
		}
		address, err := slackUploadAddress(slot.UploadURL)
		if err != nil {
			return nil, err
		}
		if err := m.slackUploadBytes(ctx, address, u); err != nil {
			return nil, timedOut(err)
		}
		title := u.title
		if title == "" {
			title = u.name
		}
		complete = append(complete, shared{ID: slot.FileID, Title: title})
	}

	request := map[string]any{"files": complete, "channel_id": channel}
	if text != "" {
		request["initial_comment"] = text
	}
	if thread != "" {
		request["thread_ts"] = thread
	}
	raw, _ := json.Marshal(request)
	answer, err := m.request(ctx, r, r.Account.Endpoint, secret, "POST", "/files.completeUploadExternal", bytes.NewReader(raw))
	if err != nil {
		return nil, timedOut(err)
	}
	var done struct {
		Files []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Title     string `json:"title"`
			Mimetype  string `json:"mimetype"`
			Permalink string `json:"permalink"`
		} `json:"files"`
	}
	_ = json.Unmarshal(answer, &done)
	sent := make([]map[string]any, 0, len(uploads))
	for i, u := range uploads {
		entry := map[string]any{"id": complete[i].ID, "name": u.name, "mimetype": u.mimetype, "size": u.size}
		for _, f := range done.Files {
			if f.ID == complete[i].ID && f.Permalink != "" {
				entry["permalink"] = f.Permalink
			}
		}
		sent = append(sent, entry)
	}
	out, _ := json.Marshal(map[string]any{"channel": channel, "thread_ts": thread, "files": sent})
	return out, nil
}

// slackUploadBytes streams one file to the address Slack's upload step gave.
// The file is never read into memory, and the token is not sent: the address
// itself is the credential.
func (m *Manager) slackUploadBytes(ctx context.Context, address *url.URL, u *slackUpload) error {
	req, err := http.NewRequestWithContext(ctx, "POST", address.String(), io.NewSectionReader(u.file, 0, u.size))
	if err != nil {
		return errors.New("invalid upload address")
	}
	req.ContentLength = u.size
	req.Header.Set("Content-Type", u.mimetype)
	req.Header.Set("User-Agent", "Mewla-Integrations")
	resp, err := m.uploadClient().Do(req)
	if err != nil {
		// The transport error names the address; only a fixed cause travels on.
		if ctx.Err() != nil {
			return errors.New("Slack file upload timed out; try again")
		}
		return errors.New("Slack file upload failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return fmt.Errorf("Slack file upload returned HTTP %d", resp.StatusCode)
	}
	return nil
}
