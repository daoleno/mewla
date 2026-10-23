package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/procspawn"
)

const pprofAddrEnv = "MEWLA_PPROF_ADDR"

// startPprof serves Go's pprof handlers when MEWLA_PPROF_ADDR is set. It is a
// diagnostics switch for profiling a running daemon without restarting it
// with a different build, so it stays off unless asked for and refuses any
// address that is not loopback: the handlers are unauthenticated.
func startPprof(ctx context.Context) error {
	addr := strings.TrimSpace(os.Getenv(pprofAddrEnv))
	if addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q: %w", pprofAddrEnv, addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("%s %q must be a loopback address", pprofAddrEnv, addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("%s listen: %w", pprofAddrEnv, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/spawns", procspawn.Handler())
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("WARN pprof listener stopped: %v", err)
		}
	}()
	log.Printf("pprof listening on %s", listener.Addr())
	return nil
}
