package main

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestStartPprofIsOffUnlessConfigured(t *testing.T) {
	t.Setenv(pprofAddrEnv, "")
	if err := startPprof(context.Background()); err != nil {
		t.Fatalf("unset env must be a no-op: %v", err)
	}
}

func TestStartPprofRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "192.168.1.5:6060", ":6060", "example.com:6060", "nonsense"} {
		t.Setenv(pprofAddrEnv, addr)
		if err := startPprof(context.Background()); err == nil {
			t.Fatalf("%q must be refused", addr)
		}
	}
}

func TestStartPprofServesLoopback(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener")
	}
	addr := probe.Addr().String()
	probe.Close()
	t.Setenv(pprofAddrEnv, addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := startPprof(ctx); err != nil {
		t.Fatalf("startPprof: %v", err)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
	if strings.Contains(addr, "0.0.0.0") {
		t.Fatal("unexpected wildcard")
	}
}
