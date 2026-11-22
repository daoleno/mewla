package work

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// acpClient is one `agy acp` process and the JSON-RPC conversation Mewla holds
// with it over its stdio, as the Agent Client Protocol's client. It never
// advertises file or terminal capabilities: agy runs its own tools.
type acpClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	writeM sync.Mutex
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[string]chan acpReply
	closed  bool
	exitErr error
	done    chan struct{}

	// onNotify receives every notification (session/update and others).
	onNotify func(method string, params json.RawMessage)
	// onRequest receives every request agy makes of the client
	// (session/request_permission) and answers it through respond.
	onRequest func(id json.RawMessage, method string, params json.RawMessage)
}

type acpReply struct {
	Result json.RawMessage
	Err    *acpError
}

type acpError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *acpError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

func (e *acpError) providerDetails() (string, int) {
	if e == nil {
		return "", 0
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(e.Data, &data) != nil {
		return "", 0
	}
	var code string
	_ = json.Unmarshal(data["code"], &code)
	for _, key := range []string{"httpStatus", "http_status", "statusCode"} {
		var status int
		if json.Unmarshal(data[key], &status) == nil && status > 0 {
			return code, status
		}
	}
	return code, 0
}

// errACPClosed is the answer to every call once agy has exited.
var errACPClosed = errors.New("Antigravity stopped")

// startACPClient starts `agy acp` in dir with args and reads its stdout until
// it exits. stderr goes to log (agy writes its own diagnostics there).
func startACPClient(binary, dir string, args []string, env []string, log io.Writer,
	onNotify func(string, json.RawMessage), onRequest func(json.RawMessage, string, json.RawMessage)) (*acpClient, error) {
	cmd := exec.Command(binary, append([]string{"acp"}, args...)...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	if log == nil {
		log = io.Discard
	}
	cmd.Stderr = log
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	bindACPProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &acpClient{cmd: cmd, stdin: stdin, pending: map[string]chan acpReply{}, done: make(chan struct{}), onNotify: onNotify, onRequest: onRequest}
	go c.read(stdout)
	return c, nil
}

func (c *acpClient) read(stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			c.dispatch(trimmed)
		}
		if err != nil {
			break
		}
	}
	waitErr := c.cmd.Wait()
	c.mu.Lock()
	c.closed = true
	c.exitErr = waitErr
	pending := c.pending
	c.pending = map[string]chan acpReply{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- acpReply{Err: &acpError{Code: -32000, Message: errACPClosed.Error()}}
	}
	close(c.done)
}

func (c *acpClient) dispatch(line []byte) {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *acpError       `json:"error"`
	}
	if json.Unmarshal(line, &message) != nil {
		return
	}
	switch {
	case message.Method != "" && len(message.ID) > 0:
		if c.onRequest != nil {
			c.onRequest(message.ID, message.Method, message.Params)
		} else {
			_ = c.respondError(message.ID, -32601, "method not supported")
		}
	case message.Method != "":
		if c.onNotify != nil {
			c.onNotify(message.Method, message.Params)
		}
	case len(message.ID) > 0:
		key := acpIDKey(message.ID)
		c.mu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if ch != nil {
			ch <- acpReply{Result: message.Result, Err: message.Error}
		}
	}
}

func acpIDKey(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return "s:" + text
	}
	return "n:" + strings.TrimSpace(string(raw))
}

func (c *acpClient) write(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeM.Lock()
	defer c.writeM.Unlock()
	_, err = c.stdin.Write(append(payload, '\n'))
	return err
}

// Call sends one request and waits for its answer, ctx or agy's exit.
func (c *acpClient) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	key := "n:" + strconv.FormatInt(id, 10)
	ch := make(chan acpReply, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errACPClosed
	}
	c.pending[key] = ch
	c.mu.Unlock()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case reply := <-ch:
		if reply.Err != nil {
			return reply.Err
		}
		if result != nil && len(reply.Result) > 0 {
			return json.Unmarshal(reply.Result, result)
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Notify sends one notification (session/cancel).
func (c *acpClient) Notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *acpClient) respond(id json.RawMessage, result any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *acpClient) respondError(id json.RawMessage, code int, message string) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

// Done is closed once agy has exited.
func (c *acpClient) Done() <-chan struct{} { return c.done }

// ExitErr is how agy exited, once Done is closed.
func (c *acpClient) ExitErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

// Pid is agy's process id.
func (c *acpClient) Pid() int {
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Close ends agy: stdin closed first (agy exits on EOF), then an interrupt,
// then a kill.
func (c *acpClient) Close(ctx context.Context) {
	_ = c.stdin.Close()
	select {
	case <-c.done:
		return
	case <-ctx.Done():
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
	}
}
