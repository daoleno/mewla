package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/statedir"
)

// An HTML file a Session wrote, shown as a page: served by URL under a signed grant for
// its folder, never pasted into the app. Threat model: the page is untrusted (an agent can
// be talked into writing anything). It runs its scripts and loads public https resources,
// and reads the files of its own folder downward while the grant lasts; it never gets the
// daemon's or the app's origin (every response is a CSP sandbox without allow-same-origin),
// its requests are confined to the grant's path and https, and it cannot frame or navigate
// the app.
const (
	sessionPagePrefix = "/session-page/"
	// A page's later loads (a chart's data, a tab's script) come while the person reads it.
	sessionPageGrantTTL = 30 * time.Minute
	// Pages inline their libraries and data (a Plotly report is 3 to 5 MB); this bounds one file.
	maxSessionPageFileBytes = 64 << 20
	sessionPageGrantPurpose = "mewla-session-page-grant"
)

var (
	errSessionPageNotHTML   = errors.New("Session file is not an HTML page")
	errSessionPageForbidden = errors.New("not reachable from this page")
)

// sessionPageGrant is what a page's URL carries, signed by the daemon: the folder it may
// read (Siblings) or only its own file, for one paired device, until it expires.
type sessionPageGrant struct {
	Version     int    `json:"v"`
	DaemonID    string `json:"daemon"`
	DeviceID    string `json:"device"`
	Root        string `json:"root"`
	Entry       string `json:"entry"`
	Siblings    bool   `json:"siblings"`
	ExpiresAtMS int64  `json:"exp"`
}

// handleSessionPageGrant signs a grant for an HTML file of a live Session (device-signed,
// as the file capability): its URL path, relative to the daemon, and when it lapses.
func (s *Server) handleSessionPageGrant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	device, ok := s.authenticateRequest(w, r, sessionFileAuthPurpose)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSessionFileCapabilityBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request sessionFileCapabilityRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONBodyEnded(decoder) != nil {
		http.Error(w, "invalid Session page request", http.StatusBadRequest)
		return
	}
	raw := clientMessage{
		WorkerID:       request.WorkerID,
		ProcessID:      request.ProcessID,
		StartedAt:      json.RawMessage(strconv.FormatInt(request.StartedAt, 10)),
		Path:           request.Path,
		FileGeneration: request.Generation,
	}
	resolved, err := s.resolveCurrentSessionFile(raw)
	if err != nil {
		writeSessionFileHTTPError(w, err)
		return
	}
	defer resolved.file.Close()
	if strings.TrimSpace(raw.FileGeneration) == "" || raw.FileGeneration != resolved.generation {
		writeSessionFileHTTPError(w, fmt.Errorf("%w; refresh the preview", errSessionFileChanged))
		return
	}
	if !isSessionPageFile(resolved) {
		http.Error(w, errSessionPageNotHTML.Error(), http.StatusUnsupportedMediaType)
		return
	}
	if resolved.info.Size() > maxSessionPageFileBytes {
		writeSessionFileHTTPError(w, fmt.Errorf("%w: %d bytes; limit is %d bytes", errSessionFileTooLarge, resolved.info.Size(), maxSessionPageFileBytes))
		return
	}
	root := filepath.Dir(resolved.canonicalPath)
	grant := sessionPageGrant{
		Version:     1,
		DaemonID:    s.auth.DaemonID(),
		DeviceID:    device.ID,
		Root:        root,
		Entry:       filepath.Base(resolved.canonicalPath),
		Siblings:    sessionPageServesFolder(root),
		ExpiresAtMS: s.sessionFileCapabilityNow().Add(sessionPageGrantTTL).UnixMilli(),
	}
	token, err := s.signSessionPageGrant(grant)
	if err != nil {
		http.Error(w, "failed to create Session page grant", http.StatusInternalServerError)
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, sessionPageGrantPurpose, map[string]any{
		"version":       1,
		"path":          sessionPagePrefix + token + "/" + url.PathEscape(grant.Entry),
		"expires_at_ms": grant.ExpiresAtMS,
	})
}

// isSessionPageFile: an HTML file the preview reads as text (UTF-8, no binary marker).
func isSessionPageFile(resolved *resolvedSessionFile) bool {
	if resolved == nil || resolved.kind != "text" {
		return false
	}
	switch strings.ToLower(filepath.Ext(resolved.canonicalPath)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// sessionPageServesFolder says whether a page in root may read the files beside it. Not where
// the folder downward is the account's or the daemon's private ground: the filesystem root, the
// home folder or a folder holding it, the state root or a folder holding it, and the state root's
// own files (identity, paired devices, credentials) but for the folders agents write pages in.
func sessionPageServesFolder(root string) bool {
	root = filepath.Clean(root)
	if filepath.Dir(root) == root {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	home = statedir.Canonical(home)
	state := statedir.Canonical(statedir.Default(home))
	if pathWithin(home, root) || pathWithin(state, root) {
		return false
	}
	if !pathWithin(root, state) {
		return true
	}
	for _, open := range []string{filepath.Join("brain", "workspace"), "worktrees", "t"} {
		if pathWithin(root, filepath.Join(state, open)) {
			return true
		}
	}
	return false
}

// pathWithin reports whether path is ancestor itself or below it.
func pathWithin(path, ancestor string) bool {
	path, ancestor = filepath.Clean(path), filepath.Clean(ancestor)
	if path == ancestor {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(ancestor, string(filepath.Separator))+string(filepath.Separator))
}

func (s *Server) signSessionPageGrant(grant sessionPageGrant) (string, error) {
	payload, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + s.auth.CreateSessionPageSignature(payload), nil
}

// verifySessionPageGrant: the token's grant, signed by this daemon for a device still paired, not expired.
func (s *Server) verifySessionPageGrant(token string) (sessionPageGrant, error) {
	var grant sessionPageGrant
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok || encoded == "" || signature == "" {
		return grant, errors.New("invalid Session page grant")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return grant, errors.New("invalid Session page grant")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&grant); err != nil || grant.Version != 1 {
		return grant, errors.New("invalid Session page grant")
	}
	if err := s.auth.VerifySessionPageSignature(grant.DeviceID, payload, signature); err != nil {
		return grant, err
	}
	if grant.DaemonID != s.auth.DaemonID() || grant.Root == "" || grant.Entry == "" {
		return grant, errors.New("invalid Session page grant")
	}
	now := s.sessionFileCapabilityNow()
	expires := time.UnixMilli(grant.ExpiresAtMS)
	if !now.Before(expires) || expires.After(now.Add(sessionPageGrantTTL+time.Second)) {
		return grant, errors.New("expired Session page grant")
	}
	return grant, nil
}

// handleSessionPage serves /session-page/<grant>/<path below the page's folder>.
func (s *Server) handleSessionPage(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), sessionPagePrefix)
	token, escaped, _ := strings.Cut(rest, "/")
	// Every answer, a refusal too, is sandboxed: nothing served here runs with the daemon's origin.
	setSessionPageHeaders(w, r.Host, sessionPagePrefix+token+"/")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	grant, err := s.verifySessionPageGrant(token)
	if err != nil {
		http.Error(w, "This page link expired. Open the file again in Mewla.", http.StatusUnauthorized)
		return
	}
	relative, err := sessionPageRelativePath(escaped)
	if err != nil || (!grant.Siblings && relative != grant.Entry) {
		http.Error(w, errSessionPageForbidden.Error(), http.StatusForbidden)
		return
	}
	file, info, err := openSessionPageFile(grant.Root, relative)
	if err != nil {
		writeSessionPageError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, no-cache")
	if contentType := sessionPageContentType(file, relative); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), io.NewSectionReader(file, 0, info.Size()))
}

// setSessionPageHeaders: the page's policy. A sandbox without allow-same-origin gives it an opaque
// origin (no storage, cookies or service workers of the daemon's, no reach into a parent frame), and
// its own loads go only below its grant, to https and to data:/blob:.
func setSessionPageHeaders(w http.ResponseWriter, host, grantPath string) {
	header := w.Header()
	scope := "http://" + host + grantPath + " https://" + host + grantPath
	header.Set("Content-Security-Policy", strings.Join([]string{
		"sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-forms allow-modals allow-downloads",
		"default-src " + scope + " https: data: blob: 'unsafe-inline' 'unsafe-eval'",
		"connect-src " + scope + " https: wss: data: blob:",
		"base-uri " + scope,
		"form-action 'none'",
		"frame-ancestors http://" + host + " https://" + host,
	}, "; "))
	// The page's opaque origin reads its own files (fetch, modules) cross-origin; the grant in the URL is the key.
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
}

// sessionPageRelativePath: the path below the page's folder, refused when any part is empty,
// "." or "..", hidden (a dotfile or dot folder) or holds a separator of another system.
func sessionPageRelativePath(escaped string) (string, error) {
	if escaped == "" {
		return "", errSessionPageForbidden
	}
	parts := strings.Split(escaped, "/")
	for i, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil || decoded == "" || strings.HasPrefix(decoded, ".") || strings.ContainsAny(decoded, "/\\:\x00") {
			return "", errSessionPageForbidden
		}
		parts[i] = decoded
	}
	return strings.Join(parts, "/"), nil
}

// openSessionPageFile opens root/relative when it resolves, through any symlink, to a regular file
// still inside root, within the page size bound.
func openSessionPageFile(root, relative string) (*os.File, os.FileInfo, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || canonicalRoot != root {
		return nil, nil, errSessionFileMissing
	}
	canonical, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return nil, nil, errSessionFileMissing
	}
	if canonical == root || !pathWithin(canonical, root) {
		return nil, nil, errSessionPageForbidden
	}
	file, err := os.Open(canonical)
	if err != nil {
		return nil, nil, errSessionFileMissing
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errSessionPageForbidden
	}
	if info.Size() > maxSessionPageFileBytes {
		file.Close()
		return nil, nil, errSessionFileTooLarge
	}
	return file, info, nil
}

// sessionPageContentType names a file by its extension; an HTML page without a charset of its own is
// UTF-8 (what agents write). Empty: the server sniffs it.
func sessionPageContentType(file *os.File, relative string) string {
	extension := strings.ToLower(filepath.Ext(relative))
	if extension == ".html" || extension == ".htm" {
		head := make([]byte, 1024)
		n, _ := file.ReadAt(head, 0)
		if bytes.Contains(bytes.ToLower(head[:n]), []byte("charset")) {
			return "text/html"
		}
		return "text/html; charset=utf-8"
	}
	return mime.TypeByExtension(extension)
}

func writeSessionPageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSessionPageForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, errSessionFileTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}
