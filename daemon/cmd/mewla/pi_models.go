package main

import (
	"context"
	"errors"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents/pi"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/server"
	"github.com/google/uuid"
)

// Pi's own model menu for a Session, read and changed by Mewla's extension
// inside Pi: the models Pi can use with the person's sign-ins (its scoped
// ones when the person scoped some), each with the thinking levels Pi gives
// it, and pi.setModel / pi.setThinkingLevel, which change the running
// conversation without saving a new default.

var errPiAway = errors.New("this Pi is not connected to Mewla")

// errPiOldExtension: a Pi started before Mewla could ask it for its models
// does not answer; it does after its next start.
var errPiOldExtension = errors.New("this Pi was started before Mewla could ask it for its models; it can after it starts again")

func (c *piConn) answer(msg pi.Message) {
	c.mu.Lock()
	ch := c.answers[msg.ID]
	delete(c.answers, msg.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *piConn) request(ctx context.Context, message map[string]any) (*pi.Models, error) {
	id := uuid.NewString()
	message["id"] = id
	ch := make(chan pi.Message, 1)
	c.mu.Lock()
	if c.answers == nil {
		c.answers = map[string]chan pi.Message{}
	}
	c.answers[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.answers, id)
		c.mu.Unlock()
	}()
	if err := c.write(message); err != nil {
		return nil, err
	}
	select {
	case msg := <-ch:
		if msg.T == "models_failed" || msg.Models == nil {
			if msg.Error == "" {
				msg.Error = "Pi did not answer with its models"
			}
			return nil, errors.New(msg.Error)
		}
		return msg.Models, nil
	case <-ctx.Done():
		return nil, errPiOldExtension
	}
}

// ModelsAvailable: the Session's Pi is linked to Mewla through its extension.
func (p *piEvents) ModelsAvailable(worker *classifier.Worker) bool {
	return p.conn(worker.ID) != nil
}

// Models reads the Session's menu from its Pi.
func (p *piEvents) Models(ctx context.Context, worker *classifier.Worker) (server.AgentModels, error) {
	c := p.conn(worker.ID)
	if c == nil {
		return server.AgentModels{}, errPiAway
	}
	ctx, cancel := context.WithTimeout(ctx, piModelsWait)
	defer cancel()
	models, err := c.request(ctx, map[string]any{"t": "models"})
	if err != nil {
		return server.AgentModels{}, err
	}
	return piAgentModels(*models), nil
}

// SetModel switches the running conversation to the model and thinking level.
func (p *piEvents) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (server.AgentModels, error) {
	c := p.conn(worker.ID)
	if c == nil {
		return server.AgentModels{}, errPiAway
	}
	ctx, cancel := context.WithTimeout(ctx, piModelsWait)
	defer cancel()
	models, err := c.request(ctx, map[string]any{"t": "set_model", "model": model, "thinking": effort})
	if err != nil {
		return server.AgentModels{}, err
	}
	return piAgentModels(*models), nil
}

// piModelsWait bounds an answer from the extension; an extension from before
// the models messages never answers.
const piModelsWait = 8 * time.Second

// piAgentModels names each model "provider/id", the way Pi's --model does.
func piAgentModels(models pi.Models) server.AgentModels {
	out := server.AgentModels{Label: "Pi", Current: models.Current, Effort: models.Thinking}
	for _, model := range models.Available {
		if model.Provider == "" || model.ID == "" {
			continue
		}
		efforts := model.Thinking
		if len(efforts) == 1 && efforts[0] == "off" {
			efforts = nil
		}
		// Pi always thinks at one of its levels: it has no "model's default"
		// to go back to.
		out.Models = append(out.Models, server.AgentModel{ID: model.Provider + "/" + model.ID, Name: model.Name, Efforts: efforts, EffortRequired: len(efforts) > 0})
	}
	return out
}
