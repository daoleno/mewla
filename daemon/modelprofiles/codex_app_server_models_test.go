package modelprofiles

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/codexctl"
)

func listedCodex(models ...codexctl.Model) CodexModelLister {
	return func(context.Context) ([]codexctl.Model, error) { return models, nil }
}

func projectCodexModels(t *testing.T, owner *Owner) map[string]ProviderModelEntry {
	t.Helper()
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	byID := map[string]ProviderModelEntry{}
	for _, entry := range owner.projectConnectionModels(Profile{ID: "official", ExecutorID: ExecutorCodex, Model: "gpt-5.5"}, discoveryEntry{}) {
		byID[entry.ID] = entry
	}
	return byID
}

func TestCodexPickerListsWhatTheInstalledCodexOffers(t *testing.T) {
	owner := &Owner{codexModels: newCodexModelList(listedCodex(
		codexctl.Model{ID: "gpt-6.1-sol", DisplayName: "GPT-6.1-Sol", DefaultEffort: "low", Efforts: []string{"low", "medium", "high", "ultra"}, Modalities: []string{"text", "image"}},
		// A model Mewla pins too: Codex's own answer wins.
		codexctl.Model{ID: "gpt-5.5", DisplayName: "GPT-5.5 (from Codex)", DefaultEffort: "high", Efforts: []string{"medium", "high"}},
	))}
	if err := owner.codexModels.refresh(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	byID := projectCodexModels(t, owner)

	fresh, ok := byID["gpt-6.1-sol"]
	if !ok || fresh.Source != ModelSourceCodexAppServer || !fresh.Available || fresh.DisplayName != "GPT-6.1-Sol" || fresh.ReasoningEffortDefault != "low" {
		t.Fatalf("a model only Codex knows: %#v", fresh)
	}
	for _, effort := range fresh.ReasoningEfforts {
		if effort == "ultra" {
			t.Fatalf("an effort outside Mewla's vocabulary was offered: %v", fresh.ReasoningEfforts)
		}
	}
	if len(fresh.ReasoningEfforts) != 3 {
		t.Fatalf("efforts = %v", fresh.ReasoningEfforts)
	}
	pinned := byID["gpt-5.5"]
	if pinned.DisplayName != "GPT-5.5 (from Codex)" || pinned.ReasoningEffortDefault != "high" || len(pinned.ReasoningEfforts) != 2 {
		t.Fatalf("the pinned entry overrode Codex's own: %#v", pinned)
	}
	if pinned.Source != ModelSourceCodexAppServer {
		t.Fatalf("source = %q", pinned.Source)
	}
}

func TestCodexPickerFallsBackToThePinnedCatalogAndSaysSo(t *testing.T) {
	owner := &Owner{codexModels: newCodexModelList(func(context.Context) ([]codexctl.Model, error) {
		return nil, errors.New("codex: command not found")
	})}
	if err := owner.codexModels.refresh(t.Context(), time.Now()); err == nil {
		t.Fatal("a failed read reported success")
	}
	byID := projectCodexModels(t, owner)
	entry, ok := byID["gpt-5.6-sol"]
	if !ok || entry.Source != ModelSourceBundled || entry.DisplayName == "" {
		t.Fatalf("pinned fallback: %#v", entry)
	}
	for _, e := range byID {
		if e.Source == ModelSourceCodexAppServer {
			t.Fatalf("an entry claims Codex listed it: %#v", e)
		}
	}
}

func TestCodexModelListIsReadOnceAndRetriedAfterAFailure(t *testing.T) {
	var reads atomic.Int64
	fail := atomic.Bool{}
	fail.Store(true)
	list := newCodexModelList(func(context.Context) ([]codexctl.Model, error) {
		reads.Add(1)
		if fail.Load() {
			return nil, errors.New("unreachable")
		}
		return []codexctl.Model{{ID: "gpt-a"}}, nil
	})
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if !list.wanted(now) {
		t.Fatal("an unread list is not wanted")
	}
	_ = list.refresh(t.Context(), now)
	if list.wanted(now.Add(time.Minute)) {
		t.Fatal("a failed read was retried at once")
	}
	if !list.wanted(now.Add(codexModelListRetry)) {
		t.Fatal("a failed read was never retried")
	}
	fail.Store(false)
	if err := list.refresh(t.Context(), now.Add(codexModelListRetry)); err != nil {
		t.Fatal(err)
	}
	if ids, _, ok := list.snapshot(); !ok || len(ids) != 1 {
		t.Fatalf("snapshot = %v %v", ids, ok)
	}
	if list.wanted(now.Add(codexModelListRetry + time.Hour)) {
		t.Fatal("a fresh list is wanted again")
	}
	if !list.wanted(now.Add(codexModelListRetry + codexModelListTTL)) {
		t.Fatal("a stale list is not wanted")
	}
	if reads.Load() != 2 {
		t.Fatalf("reads = %d", reads.Load())
	}
	var none *codexModelList
	if none.wanted(now) {
		t.Fatal("a missing list is wanted")
	}
	if _, _, ok := none.snapshot(); ok {
		t.Fatal("a missing list has an answer")
	}
	if newCodexModelList(nil).wanted(now) {
		t.Fatal("a list without a reader is wanted")
	}
}
