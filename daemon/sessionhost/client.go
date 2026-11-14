package sessionhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/ghosttyvt"
)

// ErrClosed is returned by calls on a closed client or one whose host went
// away.
var ErrClosed = errors.New("session host connection closed")

// serverUIDAllowed is the client's half of the socket boundary: a socket put
// there by another user is not the host, whatever it answers.
var serverUIDAllowed = func(uid uint32) bool { return uid == uint32(os.Getuid()) }

// Client is a connection to a host. Calls may run concurrently.
type Client struct {
	conn    *net.UnixConn
	path    string
	version string
	welcome Welcome

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[uint64]chan response
	err     error
	done    chan struct{}
}

// Dial connects to the host on socketPath and handshakes. clientVersion goes
// into the host's log.
func Dial(ctx context.Context, socketPath, clientVersion string) (*Client, error) {
	conn, welcome, err := dialHello(ctx, socketPath, clientVersion)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:    conn,
		path:    socketPath,
		version: clientVersion,
		welcome: welcome,
		pending: make(map[uint64]chan response),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func dialHello(ctx context.Context, socketPath, clientVersion string) (*net.UnixConn, Welcome, error) {
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, Welcome{}, fmt.Errorf("session host %s: %w", socketPath, err)
	}
	conn := raw.(*net.UnixConn)
	uid, err := peerUID(conn)
	if err != nil {
		conn.Close()
		return nil, Welcome{}, fmt.Errorf("session host %s: peer credentials: %w", socketPath, err)
	}
	if !serverUIDAllowed(uid) {
		conn.Close()
		return nil, Welcome{}, fmt.Errorf("session host %s is run by uid %d, not this user; refusing it", socketPath, uid)
	}
	deadline := time.Now().Add(helloTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if err := writeJSON(conn, Hello{Protocol: ProtocolVersion, Client: clientVersion}); err != nil {
		conn.Close()
		return nil, Welcome{}, fmt.Errorf("session host %s: hello: %w", socketPath, err)
	}
	var welcome Welcome
	if err := readJSON(conn, &welcome); err != nil {
		conn.Close()
		return nil, Welcome{}, fmt.Errorf("session host %s refused the connection or did not answer: %w", socketPath, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, welcome, nil
}

// Welcome is the host's handshake answer: its protocol and versions.
func (c *Client) Welcome() Welcome { return c.welcome }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) readLoop() {
	var err error
	for {
		var resp response
		if err = readJSON(c.conn, &resp); err != nil {
			break
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	c.mu.Lock()
	c.err = fmt.Errorf("%w: %v", ErrClosed, err)
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

func (c *Client) call(ctx context.Context, method string, params, result any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err = writeJSON(c.conn, request{ID: id, Method: method, Params: payload})
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%w: %v", ErrClosed, err)
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			return err
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("session host %s result: %w", method, err)
			}
		}
		if resp.Error != nil {
			return resp.Error
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Create starts a Session.
func (c *Client) Create(ctx context.Context, params CreateParams) (Info, error) {
	var info Info
	err := c.call(ctx, MethodCreate, params, &info)
	info.healCwd()
	return info, err
}

// List returns every Session, oldest first.
func (c *Client) List(ctx context.Context) ([]Info, error) {
	var infos []Info
	err := c.call(ctx, MethodList, struct{}{}, &infos)
	for i := range infos {
		infos[i].healCwd()
	}
	return infos, err
}

// Info returns one Session.
func (c *Client) Info(ctx context.Context, id string) (Info, error) {
	var info Info
	err := c.call(ctx, MethodInfo, IDParams{ID: id}, &info)
	info.healCwd()
	return info, err
}

// healCwd turns a cwd a host from before OSC 7 reports were decoded sends
// (file://host/path; a host outlives daemon upgrades) into its path, or the
// start dir when it names no folder here, as a current host does.
func (info *Info) healCwd() {
	if info.Cwd = ghosttyvt.StoredDir(info.Cwd); info.Cwd == "" {
		info.Cwd = info.Dir
	}
}

// Input runs an input script. started reports whether any byte may have
// reached the program, also when the script failed.
func (c *Client) Input(ctx context.Context, id string, steps []InputStep) (started bool, err error) {
	var result InputResult
	err = c.call(ctx, MethodInput, InputParams{ID: id, Steps: steps}, &result)
	if err != nil && !isHostError(err) {
		// The request may have reached the host before the connection
		// failed: nothing proves the script did not start.
		return true, err
	}
	return result.Started, err
}

// Resize resizes a Session's terminal.
func (c *Client) Resize(ctx context.Context, id string, cols, rows int) error {
	return c.call(ctx, MethodResize, ResizeParams{ID: id, Cols: cols, Rows: rows}, nil)
}

// Capture reads a Session's terminal.
func (c *Client) Capture(ctx context.Context, params CaptureParams) (Capture, error) {
	var capture Capture
	err := c.call(ctx, MethodCapture, params, &capture)
	return capture, err
}

// Signal signals a Session's process group.
func (c *Client) Signal(ctx context.Context, id, signal string) error {
	return c.call(ctx, MethodSignal, SignalParams{ID: id, Signal: signal}, nil)
}

// Remove ends a Session's program if it runs and forgets the Session.
func (c *Client) Remove(ctx context.Context, id string) (Info, error) {
	var info Info
	err := c.call(ctx, MethodRemove, IDParams{ID: id}, &info)
	return info, err
}

// GetData reads a value kept on a Session.
func (c *Client) GetData(ctx context.Context, id, key string) (Data, error) {
	var data Data
	err := c.call(ctx, MethodGetData, DataParams{ID: id, Key: key}, &data)
	return data, err
}

// SetData keeps a value on a Session; "" removes it.
func (c *Client) SetData(ctx context.Context, id, key, value string) error {
	return c.call(ctx, MethodSetData, DataParams{ID: id, Key: key, Value: value}, nil)
}

// Shutdown stops the host; without force it refuses while a program runs.
func (c *Client) Shutdown(ctx context.Context, force bool) error {
	return c.call(ctx, MethodShutdown, ShutdownParams{Force: force}, nil)
}

// Subscribe opens a second connection that receives host events until ctx
// ends or the host goes away; the channel is closed then.
func (c *Client) Subscribe(ctx context.Context) (<-chan Event, error) {
	conn, _, err := dialHello(ctx, c.path, c.version)
	if err != nil {
		return nil, err
	}
	if err := writeJSON(conn, request{ID: 1, Method: MethodSubscribe}); err != nil {
		conn.Close()
		return nil, err
	}
	var ack response
	if err := readJSON(conn, &ack); err != nil {
		conn.Close()
		return nil, err
	}
	if ack.Error != nil {
		conn.Close()
		return nil, ack.Error
	}
	events := make(chan Event, subscriberBuffer)
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	go func() {
		defer close(events)
		defer conn.Close()
		for {
			var event Event
			if err := readJSON(conn, &event); err != nil {
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, nil
}

func isHostError(err error) bool {
	var hostErr *Error
	return errors.As(err, &hostErr)
}
