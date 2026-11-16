package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
)

func TestRunWithReadyCallsBackAfterListenAndShutsDownCompatibly(t *testing.T) {
	addr := availableAddress(t)
	srv := newEventSubscriptionServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := false
	err := srv.RunWithReady(ctx, addr, func() {
		ready = true
		second, listenErr := net.Listen("tcp", addr)
		if listenErr == nil {
			_ = second.Close()
			t.Errorf("ready callback ran before address was acquired")
		}
		cancel()
	})
	if !ready {
		t.Fatal("ready callback was not called")
	}
	if !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("RunWithReady error = %v, want http.ErrServerClosed", err)
	}
	assertServerEventSubscriptionsClosed(t, srv)
}

func TestRunWithReadyDoesNotCallBackWhenListenFails(t *testing.T) {
	occupied := testListener(t)
	srv := newEventSubscriptionServer(t)
	called := false
	err := srv.RunWithReady(context.Background(), occupied.Addr().String(), func() {
		called = true
	})
	if err == nil {
		t.Fatal("RunWithReady unexpectedly succeeded")
	}
	if called {
		t.Fatal("ready callback ran after listen failure")
	}
	assertServerEventSubscriptionsClosed(t, srv)
}

func testListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func availableAddress(t *testing.T) string {
	t.Helper()
	listener := testListener(t)
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func newEventSubscriptionServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	w := watcher.New(time.Second)
	workStore, err := work.NewStore(filepath.Join(root, "work"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workStore.Close() })
	brainStore, err := brain.NewStore(filepath.Join(root, "brain"))
	if err != nil {
		t.Fatal(err)
	}
	brainService := brain.NewService(brainStore, w, nil)
	calendarStore, err := calendar.NewStore(filepath.Join(root, "calendar"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(nil, w, nil, nil, workStore, nil, brainService)
	srv.SetCalendar(calendarStore, nil)
	return srv
}

func assertServerEventSubscriptionsClosed(t *testing.T, srv *Server) {
	t.Helper()
	assertSubscriptionClosed(t, "Work", srv.workSub)
	assertSubscriptionClosed(t, "Calendar", srv.calendarSub)
	assertSubscriptionClosed(t, "Brain Work", srv.brainWorkSub)
}

func assertSubscriptionClosed[T any](t *testing.T, name string, subscription <-chan T) {
	t.Helper()
	select {
	case _, ok := <-subscription:
		if ok {
			t.Fatalf("%s subscription remained open", name)
		}
	default:
		t.Fatalf("%s subscription remained open", name)
	}
}
