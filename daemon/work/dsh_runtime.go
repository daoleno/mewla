package work

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/statedir"
)

var dshSessionIDPattern = regexp.MustCompile(`^session-[a-zA-Z0-9-]{1,100}$`)

func DSHRoot() string {
	home, _ := os.UserHomeDir()
	root := os.Getenv("MEWLA_STATE_DIR")
	if root == "" {
		root = statedir.Default(home)
	}
	return filepath.Join(root, "provider-sessions", "dsh")
}

func DSHSessionID(command string) string {
	options, ok := inspectLaunchCommandOptions(command)
	if !ok || len(options.argv) == 0 || options.argv[0] != "dsh-session" || !isProductExecutable(options.executable) && !strings.HasSuffix(options.executable, ".test") {
		return ""
	}
	_, id := options.optionValue("--dsh-session")
	if !dshSessionIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func EnsureDSHSessionLaunchCommand(command string) (string, error) {
	if InferWorkerProvider(command) != WorkerProviderDSH {
		return command, nil
	}
	if DSHSessionID(command) != "" {
		return command, nil
	}
	options, ok := inspectLaunchCommandOptions(command)
	if !ok || len(options.argv) != 0 {
		return "", fmt.Errorf("DSH launch uses native saved settings; use the Session model picker after launch")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return shellquote.Word(executable) + " dsh-session --dsh-session " + "session-" + uuid.NewString(), nil
}

func dshSocket(id string) (string, error) {
	if !dshSessionIDPattern.MatchString(id) {
		return "", fmt.Errorf("invalid DSH Session identity")
	}
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(DSHRoot(), fmt.Sprintf("%x.sock", sum[:8])), nil
}

// CallDSH only reaches the private socket of the explicitly owned Session.
// dshInteractionWait bounds how long one snapshot request is held open.
const dshInteractionWait = 10 * time.Second

func CallDSH(ctx context.Context, id, method string, payload any, result any) error {
	socket, err := dshSocket(id)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://dsh/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	response, err := (&http.Client{Transport: transport, Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("DSH Session is not ready: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("DSH: %s", strings.TrimSpace(string(message)))
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(result)
}

// dshNative is one authenticated client of the private `dsh web` process.
// The browser-session cookie minted from the process launch token admits the
// owner Peer, which serves unary RPCs and answers forwarded events alike.
type dshNative struct {
	address string
	cookie  string
}

var dshLaunchURLPattern = regexp.MustCompile(`dsh web: (http://\S+)`)

// dshLaunchToken reads the launch token `dsh web` prints with its URL.
func dshLaunchToken(output []byte) string {
	match := dshLaunchURLPattern.FindSubmatch(output)
	if match == nil {
		return ""
	}
	parsed, err := url.Parse(string(match[1]))
	if err != nil {
		return ""
	}
	return parsed.Query().Get("token")
}

// authenticateDSHNative exchanges the launch token for the signed session
// cookie the native `/api` fence requires.
func authenticateDSHNative(ctx context.Context, address, token string) (*dshNative, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/?token="+url.QueryEscape(token), nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	for _, cookie := range response.Cookies() {
		if strings.HasPrefix(cookie.Name, "dsh-auth-") && cookie.Value != "" {
			return &dshNative{address: address, cookie: cookie.Name + "=" + cookie.Value}, nil
		}
	}
	return nil, fmt.Errorf("DSH did not accept its launch token (HTTP %d)", response.StatusCode)
}

// openDSHSession reopens the stored Session named id, or creates it.
// session/create reuses a named Session only while it is blank, so a resumed
// conversation is found through session/list first.
func openDSHSession(ctx context.Context, client *dshNative, id, cwd string) error {
	var listed struct {
		Items []struct {
			SessionID string `json:"sessionId"`
		} `json:"items"`
	}
	// session/list names its reserved empty parameter `_request`.
	if err := client.rpc(ctx, "session/list", map[string]any{"_request": map[string]any{}}, &listed); err != nil {
		return err
	}
	for _, item := range listed.Items {
		if item.SessionID == id {
			return nil
		}
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.call(ctx, "session/create", map[string]any{"sessionId": id, "cwd": cwd}, &created); err != nil {
		return err
	}
	if created.SessionID != id {
		return fmt.Errorf("DSH created Session %q instead of %q", created.SessionID, id)
	}
	return nil
}

// call invokes one native session endpoint such as `session/prompt`; its one
// parameter is named `request`.
func (n *dshNative) call(ctx context.Context, endpoint string, request any, result any) error {
	args := map[string]any{}
	if request != nil {
		args["request"] = request
	}
	return n.rpc(ctx, endpoint, args, result)
}

// rpc posts one Typert client-request envelope carrying `payload.args`.
func (n *dshNative) rpc(ctx context.Context, endpoint string, args any, result any) error {
	id := uuid.NewString()
	body, err := json.Marshal(map[string]any{"type": "client-request", "rpcId": id, "method": endpoint, "payload": map[string]any{"args": args}})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.address+"/api/"+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", n.cookie)
	response, err := (&http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s: HTTP %d %s", endpoint, response.StatusCode, strings.TrimSpace(string(message)))
	}
	var envelope struct {
		Type   string `json:"type"`
		RPCID  string `json:"rpcId"`
		Result struct {
			OK    bool            `json:"ok"`
			Value json.RawMessage `json:"value"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Type != "server-response" || envelope.RPCID != id {
		return fmt.Errorf("%s: unexpected native response", endpoint)
	}
	if !envelope.Result.OK {
		return fmt.Errorf("%s: %s", envelope.Result.Error.Code, envelope.Result.Error.Message)
	}
	if result == nil || len(envelope.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(envelope.Result.Value, result)
}

type dshModelChoice struct {
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// models composes the deployment catalog with the Session's next selection
// into the `current/routable/groups` view the daemon server renders.
func (n *dshNative) models(ctx context.Context, id string) (any, error) {
	var catalog struct {
		Default           dshModelChoice    `json:"default"`
		RoutableProviders []string          `json:"routableProviders"`
		Groups            []json.RawMessage `json:"groups"`
	}
	if err := n.call(ctx, "session/modelCatalog", nil, &catalog); err != nil {
		return nil, err
	}
	var projections *struct {
		Values struct {
			ModelSelection struct {
				Next *dshModelChoice `json:"next"`
			} `json:"modelSelection"`
		} `json:"values"`
	}
	if err := n.call(ctx, "session/projections", map[string]any{"sessionId": id}, &projections); err != nil {
		return nil, err
	}
	current := catalog.Default
	if projections != nil && projections.Values.ModelSelection.Next != nil {
		current = *projections.Values.ModelSelection.Next
	}
	groups := catalog.Groups
	if groups == nil {
		groups = []json.RawMessage{}
	}
	return map[string]any{"current": current, "routable": slices.Contains(catalog.RoutableProviders, current.Provider), "groups": groups}, nil
}

// RunDSHSession owns one native web-profile subprocess and one session socket.
// The mobile app never imports the harness runtime; the native API owns its logs/settings.
func RunDSHSession(ctx context.Context, id, cwd string) error {
	socket, err := dshSocket(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(DSHRoot(), 0700); err != nil {
		return err
	}
	unlock, err := lockDSHSession(filepath.Join(DSHRoot(), id+".lock"))
	if err != nil {
		return fmt.Errorf("DSH Session already has an owner: %w", err)
	}
	defer unlock()
	// Listen fails closed if a previous owner is still present. Stale sockets are
	// removed only after a failed connect; two concurrent starts still race on Listen.
	if connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
		connection.Close()
		return fmt.Errorf("DSH Session already has a live owner")
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		return err
	}
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := reserve.Addr().(*net.TCPAddr).Port
	reserve.Close()
	// Keep native persistence under Mewla's provider-session owner; do not edit DSH
	// user profiles, credentials, defaults, or existing Sessions. The webserver row
	// is restated so a user profile layer cannot widen this private listener.
	patchPath := filepath.Join(DSHRoot(), id+".patch.yml")
	patch := fmt.Sprintf("- id: session-persistence-jsonl\n  config:\n    root: %q\n- id: webserver\n  config:\n    host: 127.0.0.1\n    port: %d\n", filepath.Join(DSHRoot(), "logs"), port)
	if err := os.WriteFile(patchPath, []byte(patch), 0600); err != nil {
		return err
	}
	defer os.Remove(patchPath)
	command := exec.Command("dsh", "--profile", "web", "--patch", patchPath, "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--no-open")
	command.Dir = cwd
	bindDSHProcess(command)
	logPath := filepath.Join(DSHRoot(), id+".log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		_ = command.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-exited
		}
	}()
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	var native *dshNative
	var startErr error
	for deadline := time.Now().Add(30 * time.Second); native == nil && time.Now().Before(deadline); {
		if output, err := os.ReadFile(logPath); err == nil {
			if token := dshLaunchToken(output); token != "" {
				client, err := authenticateDSHNative(ctx, address, token)
				if err == nil {
					err = openDSHSession(ctx, client, id, cwd)
				}
				if err == nil {
					native = client
					break
				}
				startErr = err
			}
		}
		select {
		case err := <-exited:
			exited <- err
			return fmt.Errorf("DSH exited during startup; inspect its private Session log: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if native == nil {
		return fmt.Errorf("DSH startup timed out (%v); inspect its private Session log", startErr)
	}
	interactions := newDSHInteractionOwner(id)
	interactionContext, stopInteractions := context.WithCancel(ctx)
	defer stopInteractions()
	go interactions.run(interactionContext, native)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/")
		switch method {
		case "session.interactions", "session.respond", "session.prompt", "session.cancel", "session.models", "session.selectModel", "session.attachment":
		default:
			http.Error(w, "unsupported DSH operation", 400)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&payload); err != nil {
			http.Error(w, "invalid DSH request", 400)
			return
		}
		if value, ok := payload["sessionId"]; ok && value != id {
			http.Error(w, "wrong Session owner", 403)
			return
		}
		payload["sessionId"] = id
		var result any
		switch method {
		case "session.interactions":
			// A caller that names the revision it last saw is held until the
			// snapshot changes, so it learns of a request without polling.
			if after, ok := payload["afterRev"].(float64); ok {
				interactions.wait(r.Context(), int64(after), dshInteractionWait)
			}
			result = interactions.snapshot()
		case "session.respond":
			if err := interactions.answer(r.Context(), native, payload); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			result = map[string]bool{"accepted": true}
		case "session.models":
			models, err := native.models(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			result = models
		default:
			if method == "session.prompt" {
				if requestID, _ := payload["requestId"].(string); requestID == "" {
					payload["requestId"] = uuid.NewString()
				}
			}
			var value json.RawMessage
			if err := native.call(r.Context(), "session/"+strings.TrimPrefix(method, "session."), payload, &value); err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			result = value
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	fmt.Fprintln(os.Stdout, "DSH ready. Native saved model/settings apply.")
	return runDSHTerminal(ctx, id, exited)
}

// isProductExecutable accepts the mewla CLI, including dev builds such as
// mewla-dev.
func isProductExecutable(executable string) bool {
	return strings.HasPrefix(filepath.Base(executable), "mewla")
}
