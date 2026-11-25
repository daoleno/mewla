package main

import (
	"context"
	"errors"
	"strings"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/server"
)

// OpenCode's own model menu for a Session: the models its service can use
// with the person's providers (`model.list`), each with OpenCode's variants
// as reasoning levels, the session's model, and `session.switchModel`, which
// changes the model of the session's next turns.

type opencodeModelService interface {
	Models(ctx context.Context) ([]opencodeapi.ModelInfo, error)
	DefaultModel(ctx context.Context) (opencodeapi.ModelRef, error)
	SessionModel(ctx context.Context, sessionID string) (opencodeapi.ModelRef, bool, error)
	SwitchModel(ctx context.Context, sessionID string, ref opencodeapi.ModelRef) error
}

var errOpencodeAway = errors.New("OpenCode's service is not connected to Mewla")

// errOpencodeNoSession: OpenCode's service creates the session with its first
// prompt; until then there is no session to switch.
var errOpencodeNoSession = errors.New("OpenCode starts this session with its first message; switch the model after it")

func (o *opencodeEvents) modelService(workerID string) (opencodeModelService, string, error) {
	w := o.worker(workerID)
	service, _ := o.current().(opencodeModelService)
	if w == nil || w.session == "" || service == nil {
		return nil, "", errOpencodeAway
	}
	return service, w.session, nil
}

// ModelsAvailable: the service has the Session's session. OpenCode creates it
// with the first message typed in its terminal UI, which until then holds the
// model itself; a session created any other way would leave that UI behind.
func (o *opencodeEvents) ModelsAvailable(worker *classifier.Worker) bool {
	w := o.worker(worker.ID)
	if w == nil || o.current() == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}

// Models reads the Session's menu from the service.
func (o *opencodeEvents) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	service, session, err := o.modelService(worker.ID)
	if err != nil {
		return server.AgentModels{}, err
	}
	models, err := service.Models(ctx)
	if err != nil {
		return server.AgentModels{}, err
	}
	current, found, err := service.SessionModel(ctx, session)
	if err != nil {
		return server.AgentModels{}, err
	}
	if !found {
		// Before its first prompt the session does not exist yet; it will
		// start on the service's default.
		if current, err = service.DefaultModel(ctx); err != nil {
			return server.AgentModels{}, err
		}
	}
	out := opencodeAgentModels(models, current)
	out.Pending, out.Failed = o.later.of(worker.ID), o.later.failure(worker.ID)
	return out, nil
}

// SetModel switches the session's next turns to the model and variant.
func (o *opencodeEvents) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (server.AgentModels, error) {
	service, session, err := o.modelService(worker.ID)
	if err != nil {
		return server.AgentModels{}, err
	}
	provider, id, ok := strings.Cut(model, "/")
	if !ok || provider == "" || id == "" {
		return server.AgentModels{}, errors.New("OpenCode names a model as provider/model")
	}
	if _, found, err := service.SessionModel(ctx, session); err != nil {
		return server.AgentModels{}, err
	} else if !found {
		return server.AgentModels{}, errOpencodeNoSession
	}
	ref := opencodeapi.ModelRef{ID: id, ProviderID: provider, Variant: effort}
	// A turn under way keeps the model it started with and writes it back to
	// the session as it ends, over a switch made meanwhile: the choice is
	// held and made when the turn ends.
	if o.later.busy(worker.ID) {
		o.later.hold(worker.ID, server.AgentChoice{Model: model, Effort: effort}, func(ctx context.Context) error {
			service, session, err := o.modelService(worker.ID)
			if err != nil {
				return err
			}
			return service.SwitchModel(ctx, session, ref)
		})
		return o.Models(ctx, worker)
	}
	o.later.drop(worker.ID)
	if err := service.SwitchModel(ctx, session, ref); err != nil {
		return server.AgentModels{}, err
	}
	return o.Models(ctx, worker)
}

// opencodeDefaultVariant is how a session reports no variant: the model's own default.
const opencodeDefaultVariant = "default"

// opencodeAgentModels lists the enabled, not deprecated models as
// "provider/model", the way OpenCode names them, with their variants.
func opencodeAgentModels(models []opencodeapi.ModelInfo, current opencodeapi.ModelRef) server.AgentModels {
	out := server.AgentModels{Label: "OpenCode", Effort: current.Variant}
	if out.Effort == opencodeDefaultVariant {
		out.Effort = ""
	}
	if current.ProviderID != "" && current.ID != "" {
		out.Current = current.ProviderID + "/" + current.ID
	}
	seen := map[string]bool{}
	for _, model := range models {
		id := strings.TrimSpace(model.ModelID)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if id == "" || model.ProviderID == "" || (model.Enabled != nil && !*model.Enabled) || model.Status == "deprecated" {
			continue
		}
		// OpenCode lists a model once per way it can reach it; the menu
		// names it once, as OpenCode's own picker does.
		if seen[model.ProviderID+"/"+id] {
			continue
		}
		seen[model.ProviderID+"/"+id] = true
		entry := server.AgentModel{ID: model.ProviderID + "/" + id, Name: model.Name}
		for _, variant := range model.Variants {
			if variant.ID != "" && variant.ID != opencodeDefaultVariant {
				entry.Efforts = append(entry.Efforts, variant.ID)
			}
		}
		out.Models = append(out.Models, entry)
	}
	return out
}

// Settle makes a pick held during the turn that just ended before the next
// message is sent, so that message runs on it.
func (o *opencodeEvents) Settle(ctx context.Context, worker *classifier.Worker) error {
	if worker == nil {
		return nil
	}
	return o.later.settle(ctx, worker.ID)
}
