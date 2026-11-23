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
	return opencodeAgentModels(models, current), nil
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
	if err := service.SwitchModel(ctx, session, opencodeapi.ModelRef{ID: id, ProviderID: provider, Variant: effort}); err != nil {
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
	for _, model := range models {
		id := strings.TrimSpace(model.ModelID)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if id == "" || model.ProviderID == "" || (model.Enabled != nil && !*model.Enabled) || model.Status == "deprecated" {
			continue
		}
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
