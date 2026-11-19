//go:build unix

package modelprofiles

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/listenhandoff"
)

const restartHelperRootEnv = "MEWLA_TEST_RELAY_RESTART_ROOT"

// A daemon restart must not cut a Worker's model reply, and a request made
// during the restart must be answered by the next daemon rather than refused
// (11-19: every dev reload ended running Workers with "Connection lost
// mid-response" and "ECONNREFUSED").
//
// The reply streams for longer than the old stop allowed (3 s). While it
// streams the Owner drains; a second request is made in the gap; a successor
// process (this test binary, run again) starts an Owner on the same state,
// takes the held socket back, and answers the second request.
func TestRestartDrainsStreamingReplyAndSuccessorAnswersQueuedRequest(t *testing.T) {
	if os.Getenv(restartHelperRootEnv) != "" {
		t.Skip("helper process")
	}
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	const chunks = 20
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		if calls.Add(1) > 1 {
			_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\",\"n\":\"second\"}\n\n")
			return
		}
		for i := 0; i < chunks; i++ {
			if i == 1 {
				started <- struct{}{}
				<-release
			}
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"i\":%d}\n\n", i)
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()
	var releaseOnce sync.Once
	releaseStream := func() { releaseOnce.Do(func() { close(release) }) }
	// A failure must not leave the upstream handler, and so upstream.Close, waiting.
	defer releaseStream()

	root := t.TempDir()
	owner, _ := newDurableOwner(t, root)
	ownerClosed := false
	defer func() {
		if !ownerClosed {
			_ = owner.Close()
		}
	}()
	projection, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		Name: "Claude account", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: upstream.URL, Advanced: true,
	}, "fixture-key", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorClaude, projection.Connections[0].ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "claude-restart"); err != nil {
		t.Fatal(err)
	}
	messages := plan.Env[EnvAnthropicBaseURL] + "/v1/messages"
	post := func() (*http.Response, error) {
		request, err := http.NewRequest(http.MethodPost, messages, bytes.NewBufferString(`{"model":"claude-sonnet-4-6","max_tokens":1,"messages":[]}`))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
		client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}
		return client.Do(request)
	}

	type reply struct {
		status int
		body   string
		err    error
		at     time.Time
	}
	read := func(out chan<- reply) {
		response, err := post()
		if err != nil {
			out <- reply{err: err, at: time.Now()}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		out <- reply{status: response.StatusCode, body: string(body), err: err, at: time.Now()}
	}

	first := make(chan reply, 1)
	go read(first)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream never reached the upstream")
	}

	drained := make(chan DrainResult, 1)
	drainedAt := make(chan time.Time, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result := owner.Drain(ctx)
		drainedAt <- time.Now()
		drained <- result
	}()
	// The listener is closed now; the socket is held. A request in the gap
	// must wait for the successor, not be refused.
	time.Sleep(300 * time.Millisecond)
	second := make(chan reply, 1)
	go read(second)

	// Longer than the old stop's 3 s, while the drain waits.
	time.Sleep(3500 * time.Millisecond)
	select {
	case <-drained:
		t.Fatal("the drain ended while a reply was still streaming")
	default:
	}
	releaseStream()

	got := <-first
	result := <-drained
	endedAt := <-drainedAt
	if got.err != nil || got.status != http.StatusOK {
		t.Fatalf("streaming reply across the restart: status=%d err=%v", got.status, got.err)
	}
	if !strings.Contains(got.body, fmt.Sprintf(`{"i":%d}`, chunks-1)) || !strings.Contains(got.body, "message_stop") {
		t.Fatalf("streaming reply was cut: %q", got.body)
	}
	if endedAt.Before(got.at.Add(-50 * time.Millisecond)) {
		t.Fatalf("drain returned at %v, before the reply ended at %v", endedAt, got.at)
	}
	if result.Waited < 1 || result.Cut != 0 || len(result.Held) == 0 {
		t.Fatalf("drain result %+v", result)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	ownerClosed = true
	select {
	case early := <-second:
		t.Fatalf("the request made during the restart was answered before a successor existed: %+v", early)
	default:
	}

	// The successor: a new process on the same state, given the held socket.
	env, files := listenhandoff.ForChild(os.Environ())
	child := exec.Command(os.Args[0], "-test.run=^TestRestartSuccessorHelper$", "-test.v")
	child.Env = append(env, restartHelperRootEnv+"="+root)
	child.ExtraFiles = files
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
			<-done
		}
	}()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "successor-ready ") {
				ready <- strings.TrimPrefix(line, "successor-ready ")
			}
		}
	}()
	select {
	case addr := <-ready:
		if !strings.Contains(messages, addr) {
			t.Fatalf("successor listens on %s, the route is %s", addr, messages)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("successor did not start")
	}
	// The old process is gone: its copy of the socket goes with it.
	listenhandoff.Drop()

	select {
	case got := <-second:
		if got.err != nil || got.status != http.StatusOK || !strings.Contains(got.body, "second") {
			t.Fatalf("request made during the restart: status=%d err=%v body=%q", got.status, got.err, got.body)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the request made during the restart was never answered")
	}
}

// TestRestartSuccessorHelper is the successor daemon's Owner for the test
// above; it only runs as that test's child.
func TestRestartSuccessorHelper(t *testing.T) {
	root := os.Getenv(restartHelperRootEnv)
	if root == "" {
		t.Skip("runs only as the successor of TestRestartDrainsStreamingReplyAndSuccessorAnswersQueuedRequest")
	}
	owner, _ := newDurableOwner(t, root)
	defer owner.Close()
	listenhandoff.Release()
	fmt.Printf("successor-ready %s\n", owner.ListenAddr())
	_, _ = io.Copy(io.Discard, os.Stdin)
}
