package connections

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Media plugins (fal and Image generation) authenticate with a pasted API key,
// spend money on generation, and return binary output. Output bytes never
// enter a tool result: files are written to disk and the result carries their
// paths, types and sizes.

const (
	mediaMaxFiles     = 20
	mediaMaxFileBytes = 2 << 30
	mediaMaxJSONBytes = 128 << 20
	mediaShortTimeout = 60 * time.Second
	mediaLongTimeout  = 10 * time.Minute
	// Neutral on purpose: an image endpoint is often a third-party gateway.
	mediaUserAgent = "media-client/1"
)

func mediaKind(integration string) bool { return integration == "fal" || integration == "image" }

// MediaCallTimeout bounds the longest media call, so the CLI can wait for it.
const MediaCallTimeout = mediaLongTimeout

func mediaTimeout(tool string) time.Duration {
	switch tool {
	case "run", "result", "upload_file", "generate", "edit":
		return mediaLongTimeout
	}
	return mediaShortTimeout
}

type localCallerKey struct{}

// WithLocalCaller marks a request from the daemon's private control socket,
// which only this computer's user can reach. Only such requests may name local
// files; the paired app can never make the daemon read or write a path.
func WithLocalCaller(ctx context.Context) context.Context {
	return context.WithValue(ctx, localCallerKey{}, true)
}
func isLocalCaller(ctx context.Context) bool {
	local, _ := ctx.Value(localCallerKey{}).(bool)
	return local
}

// keyError is an API-key account whose key is missing or rejected. It counts
// as errAuth, so the account asks for a key, and says where the key goes.
type keyError struct {
	service string
	missing bool
}

func (e keyError) Error() string {
	if e.missing {
		return e.service + " has no API key yet. Add one in Mewla: Extensions → " + e.service + " → Enter key."
	}
	return e.service + " rejected the API key. Enter a new one in Mewla: Extensions → " + e.service + " → Enter key."
}
func (e keyError) Is(target error) bool { return target == errAuth }

// vendorError carries the service's own explanation back to the caller, so an
// agent can fix its input. History keeps only the status line.
type vendorError struct{ short, detail string }

func (e vendorError) Error() string {
	if e.detail == "" {
		return e.short
	}
	return e.short + ": " + e.detail
}

// mediaCall carries one media invocation after the manager lock is released.
// It never touches manager state.
type mediaCall struct {
	service   string
	account   Account
	secret    string
	auth      string          // Authorization header value
	authHosts map[string]bool // the only hosts that ever receive the key
	client    *http.Client
	local     bool
	cwd       string
	root      string // daemon-owned media folder
	dir       string // resolved output folder
	files     []mediaFile
	total     int64
}

type mediaFile struct {
	Path  string `json:"path"`
	Mime  string `json:"mime,omitempty"`
	Bytes int64  `json:"bytes"`
	URL   string `json:"url,omitempty"`
}

func (m *Manager) mediaCall(ctx context.Context, r *record, secret, cwd string, timeout time.Duration) *mediaCall {
	c := &mediaCall{
		service: serviceName(r.Account.Integration),
		account: cloneAccount(r.Account),
		secret:  secret,
		client:  m.mediaClient(r, timeout),
		local:   isLocalCaller(ctx),
		cwd:     cwd,
		root:    filepath.Join(filepath.Dir(m.path), "media", r.Account.Integration),
	}
	if r.Account.Integration == "fal" {
		c.auth = "Key " + secret
		c.authHosts = map[string]bool{"api.fal.ai": true, "fal.run": true, "queue.fal.run": true, "rest.fal.ai": true}
	} else if u, err := url.Parse(r.Account.Endpoint); err == nil {
		c.auth = "Bearer " + secret
		c.authHosts = map[string]bool{u.Host: true}
	}
	return c
}

// mediaClient keeps the checked dialer but allows long generations, and lets
// unauthenticated downloads follow CDN redirects.
func (m *Manager) mediaClient(r *record, timeout time.Duration) *http.Client {
	if !m.networkOwned {
		return m.http
	}
	client := m.clientFor(r)
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.ResponseHeaderTimeout = timeout
	}
	client.Timeout = timeout
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.Method != http.MethodGet || via[0].Header.Get("Authorization") != "" {
			return errors.New("redirects are not allowed")
		}
		return nil
	}
	return client
}

func (c *mediaCall) redact(s string) string {
	if c.secret != "" {
		s = strings.ReplaceAll(s, c.secret, "[REDACTED]")
	}
	return s
}

// do sends one request. The key goes only to the account's own API hosts over
// HTTPS (or its explicitly trusted internal address), never to CDNs or signed
// upload URLs.
func (c *mediaCall) do(ctx context.Context, method, target string, body io.Reader, contentType string, limit int64, capture ...*http.Header) ([]byte, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, errors.New("invalid media URL")
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.New("invalid request")
	}
	if c.authHosts[u.Host] && c.secret != "" {
		if _, err := trustedEndpointURL(u.Scheme+"://"+u.Host, c.account.TrustedNetworks); err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.auth)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", mediaUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, safeError(ctx.Err())
		}
		return nil, errors.New(c.service + " could not be reached")
	}
	defer resp.Body.Close()
	if len(capture) > 0 {
		*capture[0] = resp.Header.Clone()
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errors.New(c.service + " response could not be read")
	}
	if int64(len(raw)) > limit {
		return nil, errors.New(c.service + " response exceeds the size limit")
	}
	if resp.StatusCode == http.StatusUnauthorized && req.Header.Get("Authorization") != "" {
		return nil, keyError{service: c.service}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, vendorError{fmt.Sprintf("%s returned HTTP %d", c.service, resp.StatusCode), c.redact(vendorMessage(raw))}
	}
	return raw, nil
}

// vendorMessage extracts a short, printable explanation from an error body.
func vendorMessage(raw []byte) string {
	var body struct {
		Detail  any    `json:"detail"`
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	msg := ""
	if json.Unmarshal(raw, &body) == nil {
		switch e := body.Error.(type) {
		case string:
			msg = e
		case map[string]any:
			msg, _ = e["message"].(string)
		}
		if msg == "" {
			msg = body.Message
		}
		if msg == "" {
			switch d := body.Detail.(type) {
			case string:
				msg = d
			case []any:
				// Validation detail names fields, never echoes their values.
				parts := []string{}
				for _, item := range d {
					entry, _ := item.(map[string]any)
					loc := []string{}
					if l, ok := entry["loc"].([]any); ok {
						for _, p := range l {
							if s := fmt.Sprint(p); s != "body" {
								loc = append(loc, s)
							}
						}
					}
					text, _ := entry["msg"].(string)
					parts = append(parts, strings.TrimPrefix(strings.Join(loc, ".")+": "+text, ": "))
					if len(parts) == 5 {
						break
					}
				}
				msg = strings.Join(parts, "; ")
			}
		}
	}
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(msg))
	if runes := []rune(msg); len(runes) > 600 {
		msg = string(runes[:600]) + "…"
	}
	return msg
}

// localPath resolves a caller-named file. Relative paths resolve against the
// CLI's working directory.
func (c *mediaCall) localPath(p string) (string, error) {
	if !c.local {
		return "", errors.New("local files can only be named from the mewla CLI on this computer")
	}
	if !filepath.IsAbs(p) {
		if c.cwd == "" || !filepath.IsAbs(c.cwd) {
			return "", errors.New("use an absolute path")
		}
		p = filepath.Join(c.cwd, p)
	}
	return filepath.Clean(p), nil
}

// useOutputDir picks where files go: the caller's folder, or the daemon's
// media folder by date.
func (c *mediaCall) useOutputDir(args map[string]any) error {
	if dir, ok := args["output_dir"].(string); ok {
		resolved, err := c.localPath(dir)
		if err != nil {
			return err
		}
		c.dir = resolved
		return nil
	}
	c.dir = filepath.Join(c.root, time.Now().Format("2006-01-02"))
	return nil
}

var mediaExtensions = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/jpg": ".jpg", "image/webp": ".webp", "image/gif": ".gif",
	"video/mp4": ".mp4", "video/webm": ".webm", "video/quicktime": ".mov",
	"audio/mpeg": ".mp3", "audio/mp3": ".mp3", "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/ogg": ".ogg", "audio/flac": ".flac",
	"model/gltf-binary": ".glb", "application/zip": ".zip", "application/json": ".json", "text/plain": ".txt",
}

func mediaExtension(mimeType, hint string) string {
	if ext, ok := mediaExtensions[mimeType]; ok {
		return ext
	}
	ext := strings.ToLower(path.Ext(hint))
	if len(ext) >= 2 && len(ext) <= 6 && strings.Trim(ext[1:], "abcdefghijklmnopqrstuvwxyz0123456789") == "" {
		return ext
	}
	return ".bin"
}

// save streams one output to a daemon-named file in the output folder.
func (c *mediaCall) save(src io.Reader, mimeType, nameHint string) (mediaFile, error) {
	if len(c.files) >= mediaMaxFiles {
		return mediaFile{}, fmt.Errorf("output has more than %d files", mediaMaxFiles)
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return mediaFile{}, errors.New("output folder could not be created")
	}
	tmp, err := os.CreateTemp(c.dir, ".media-*")
	if err != nil {
		return mediaFile{}, errors.New("output file could not be created")
	}
	defer os.Remove(tmp.Name())
	buffered := bufio.NewReader(src)
	head, _ := buffered.Peek(512)
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if mimeType == "" || mimeType == "application/octet-stream" || mimeType == "binary/octet-stream" {
		mimeType = strings.Split(http.DetectContentType(head), ";")[0]
	}
	n, err := io.Copy(tmp, io.LimitReader(buffered, mediaMaxFileBytes+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return mediaFile{}, errors.New("output file could not be written")
	}
	if n > mediaMaxFileBytes || c.total+n > 2*mediaMaxFileBytes {
		return mediaFile{}, errors.New("output is larger than the media size limit")
	}
	name := fmt.Sprintf("%s-%s-%s%s", c.account.Integration, time.Now().Format("20060102-150405"), uuid.NewString()[:8], mediaExtension(mimeType, nameHint))
	target := filepath.Join(c.dir, name)
	if err := os.Rename(tmp.Name(), target); err != nil {
		return mediaFile{}, errors.New("output file could not be saved")
	}
	_ = os.Chmod(target, 0o644)
	c.total += n
	file := mediaFile{Path: target, Mime: mimeType, Bytes: n}
	c.files = append(c.files, file)
	return file, nil
}

func (c *mediaCall) saveBase64(data, mimeType string) (mediaFile, error) {
	data = strings.TrimSpace(data)
	if strings.HasPrefix(data, "data:") {
		header, payload, ok := strings.Cut(data, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return mediaFile{}, errors.New("output data URI is not base64")
		}
		mimeType = strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		data = payload
	}
	return c.save(base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)), mimeType, "")
}

func (c *mediaCall) download(ctx context.Context, raw, mimeType string) (mediaFile, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return mediaFile{}, errors.New("output URL must be HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return mediaFile{}, errors.New("invalid output URL")
	}
	req.Header.Set("User-Agent", mediaUserAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return mediaFile{}, safeError(ctx.Err())
		}
		return mediaFile{}, errors.New("output file could not be downloaded")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mediaFile{}, fmt.Errorf("output download returned HTTP %d", resp.StatusCode)
	}
	if mimeType == "" {
		mimeType = resp.Header.Get("Content-Type")
	}
	file, err := c.save(resp.Body, mimeType, u.Path)
	if err != nil {
		return mediaFile{}, err
	}
	file.URL = raw
	c.files[len(c.files)-1].URL = raw
	return file, nil
}

// collect walks a service's JSON output. File objects (a url with a content
// type or file name) are downloaded; data URIs and long base64 strings are
// decoded to files. Each becomes a path in place, so the shape stays readable.
func (c *mediaCall) collect(ctx context.Context, value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		if raw, ok := v["url"].(string); ok {
			mimeType, _ := v["content_type"].(string)
			_, named := v["file_name"]
			if strings.HasPrefix(raw, "data:") {
				file, err := c.saveBase64(raw, mimeType)
				if err != nil {
					return nil, err
				}
				delete(v, "url")
				v["path"], v["mime"], v["bytes"] = file.Path, file.Mime, file.Bytes
			} else if mimeType != "" || named || falMediaHost(raw) {
				file, err := c.download(ctx, raw, mimeType)
				if err != nil {
					return nil, err
				}
				v["path"], v["mime"], v["bytes"] = file.Path, file.Mime, file.Bytes
			}
		}
		for key, item := range v {
			if key == "url" || key == "path" {
				continue
			}
			next, err := c.collect(ctx, item)
			if err != nil {
				return nil, err
			}
			v[key] = next
		}
		return v, nil
	case []any:
		for i, item := range v {
			next, err := c.collect(ctx, item)
			if err != nil {
				return nil, err
			}
			v[i] = next
		}
		return v, nil
	case string:
		if strings.HasPrefix(v, "data:") && strings.Contains(v[:min(len(v), 200)], ";base64,") {
			file, err := c.saveBase64(v, "")
			if err != nil {
				return nil, err
			}
			return map[string]any{"path": file.Path, "mime": file.Mime, "bytes": file.Bytes}, nil
		}
		if len(v) > 16<<10 {
			if _, err := base64.StdEncoding.DecodeString(v[:4096]); err == nil {
				file, err := c.saveBase64(v, "")
				if err == nil {
					return map[string]any{"path": file.Path, "mime": file.Mime, "bytes": file.Bytes}, nil
				}
			}
			return fmt.Sprintf("[omitted %d bytes]", len(v)), nil
		}
	}
	return value, nil
}

func falMediaHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "fal.media" || strings.HasSuffix(host, ".fal.media")
}

// decodeJSON keeps large integers such as seeds exact.
func decodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c *mediaCall) result(fields map[string]any) ([]byte, error) {
	if len(c.files) > 0 {
		fields["files"] = c.files
		fields["output_dir"] = c.dir
	}
	return json.Marshal(fields)
}

func mimeForPath(p string) string {
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(p))); t != "" {
		return strings.Split(t, ";")[0]
	}
	return ""
}
