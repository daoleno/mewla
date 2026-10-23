package main

import (
	"encoding/json"
	"net/http"
	"sync"
)

// debugHandlers are extra read-only pages served on the opt-in loopback
// diagnostics listener (MEWLA_PPROF_ADDR) next to pprof.
var debugHandlers sync.Map // path -> http.Handler

func registerDebugHandler(path string, handler http.Handler) { debugHandlers.Store(path, handler) }

func mountDebugHandlers(mux *http.ServeMux) {
	debugHandlers.Range(func(path, handler any) bool {
		mux.Handle(path.(string), handler.(http.Handler))
		return true
	})
}

func jsonDebugHandler(snapshot func() any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot())
	})
}
