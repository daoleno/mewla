package modelprofiles

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/logonce"
)

// The model list of a managed Codex comes from Codex itself: the installed
// binary's app-server answers `model/list` with the models it offers, their
// display names and the reasoning efforts each accepts, so the picker moves
// with the installed Codex and not with Mewla's releases. When that answer is
// unavailable (Codex is missing, too old for the method, or briefly
// unreachable) the picker falls back to Codex's own models_cache.json
// (ModelSourceCodexCache) and then to the pinned catalog in model_catalog.go
// (ModelSourceBundled); each entry's Source says which one it came from, and
// the failure is logged once.
const (
	// ModelSourceCodexAppServer marks a model Codex's app-server listed.
	ModelSourceCodexAppServer = "codex_app_server"

	codexModelListTTL     = 6 * time.Hour
	codexModelListTimeout = 30 * time.Second
	// codexModelListRetry is how long a failed read is left alone before the
	// next projection tries again.
	codexModelListRetry = 5 * time.Minute
)

// CodexModelLister reads the installed Codex's model list.
type CodexModelLister func(context.Context) ([]codexctl.Model, error)

// InstalledCodexModels asks the codex executable on PATH.
func InstalledCodexModels(ctx context.Context) ([]codexctl.Model, error) {
	binary, err := exec.LookPath("codex")
	if err != nil {
		return nil, err
	}
	return codexctl.ReadModelList(ctx, binary, nil)
}

// codexModelList is the last model list Codex's app-server gave.
type codexModelList struct {
	read  CodexModelLister
	notes logonce.Log

	mu       sync.Mutex
	at       time.Time
	failedAt time.Time
	ids      []string
	metadata map[string]modelPresentationMetadata
	inflight bool
}

func newCodexModelList(read CodexModelLister) *codexModelList {
	return &codexModelList{read: read}
}

// snapshot returns the listed ids and their metadata, and whether Codex has
// answered at all.
func (l *codexModelList) snapshot() ([]string, map[string]modelPresentationMetadata, bool) {
	if l == nil {
		return nil, nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ids...), cloneModelMetadataMap(l.metadata), !l.at.IsZero()
}

// wanted reports a list that should be (re)read now: there is a reader, no
// read is under way or recently failed, and the last answer is missing or
// older than the TTL.
func (l *codexModelList) wanted(now time.Time) bool {
	if l == nil || l.read == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight || (!l.failedAt.IsZero() && now.Sub(l.failedAt) < codexModelListRetry) {
		return false
	}
	return l.at.IsZero() || now.Sub(l.at) >= codexModelListTTL
}

// refresh reads the list now. Callers that find one under way return at once.
func (l *codexModelList) refresh(ctx context.Context, now time.Time) error {
	if l == nil || l.read == nil {
		return errors.New("Codex model list is not configured")
	}
	l.mu.Lock()
	if l.inflight {
		l.mu.Unlock()
		return nil
	}
	l.inflight = true
	l.mu.Unlock()
	readCtx, cancel := context.WithTimeout(ctx, codexModelListTimeout)
	models, err := l.read(readCtx)
	cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inflight = false
	if err != nil {
		l.failedAt = now
		l.notes.Note("modelprofiles.codex_models", fmt.Errorf("Codex's app-server did not list its models (%w); the model picker uses Codex's models_cache.json, then Mewla's pinned list", err))
		return err
	}
	l.notes.Note("modelprofiles.codex_models", nil)
	l.failedAt = time.Time{}
	ids := make([]string, 0, len(models))
	metadata := make(map[string]modelPresentationMetadata, len(models))
	for _, model := range models {
		id := normalizeSpace(model.ID)
		if ValidateModelID(id) != nil {
			continue
		}
		presets := make([]CodexReasoningEffortPreset, 0, len(model.Efforts))
		for _, effort := range model.Efforts {
			presets = append(presets, CodexReasoningEffortPreset{Effort: effort})
		}
		ids = append(ids, id)
		metadata[id] = normalizeModelPresentationMetadata(modelPresentationMetadata{
			DisplayName:              model.DisplayName,
			DefaultReasoningLevel:    model.DefaultEffort,
			SupportedReasoningLevels: presets,
			Modalities:               model.Modalities,
		})
	}
	l.at, l.ids, l.metadata = now, ids, metadata
	return nil
}

// refreshCodexModelsInBackground reads Codex's model list when it is missing
// or stale. It never blocks the caller; Close cancels and joins it.
func (o *Owner) refreshCodexModelsInBackground() {
	if o == nil || o.backgroundCtx == nil || !o.codexModels.wanted(time.Now()) || o.backgroundCtx.Err() != nil {
		return
	}
	o.background.Add(1)
	go func() {
		defer o.background.Done()
		_ = o.codexModels.refresh(o.backgroundCtx, time.Now())
	}()
}
