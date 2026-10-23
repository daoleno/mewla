package main

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/memorysearch"
)

// memoryMu serializes index refreshes: one writer per index file.
var memoryMu sync.Mutex

const memoryIndexName = "memory.sqlite"

// handleMemory answers memory_search and memory_show from the Brain chat log.
// The index opens, catches up with the log and closes per call: a warm refresh
// reads only the rows appended since the last one.
func (a *controlApp) handleMemory(req control.Request) control.Response {
	if a == nil || a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain chat is not configured.")
	}
	if req.Memory == nil {
		return control.ErrorResponse("invalid_memory_request", "Memory request is required.")
	}
	stateDir := a.brainStore.StateDir()

	memoryMu.Lock()
	defer memoryMu.Unlock()
	ctx := context.Background()
	index, err := memorysearch.Open(filepath.Join(stateDir, memoryIndexName))
	if err != nil {
		return control.ErrorResponse("memory_unavailable", err.Error())
	}
	defer index.Close()
	if _, err := index.Refresh(ctx, a.brainStore); err != nil {
		return control.ErrorResponse("memory_unavailable", err.Error())
	}

	switch req.Type {
	case "memory_search":
		hits, err := index.Search(ctx, memorysearch.SearchOptions{
			Query: req.Memory.Query, From: req.Memory.From,
			Since: req.Memory.Since, Until: req.Memory.Until, Limit: req.Memory.Limit,
		})
		if err != nil {
			return control.ErrorResponse("memory_search_failed", err.Error())
		}
		return control.Response{OK: true, MemoryHits: hits}
	default:
		messages, err := index.Show(ctx, req.Memory.ID, req.Memory.Around)
		if errors.Is(err, memorysearch.ErrNotFound) {
			return control.ErrorResponse("memory_not_found", err.Error())
		}
		if err != nil {
			return control.ErrorResponse("memory_show_failed", err.Error())
		}
		return control.Response{OK: true, MemoryMessages: messages}
	}
}
