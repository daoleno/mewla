package server

import (
	"context"
	"encoding/json"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
)

// AntigravitySessions reaches the Antigravity Sessions Mewla runs
// (cmd/mewla/agy_events.go): the models agy offers a Session and a change of
// them.
type AntigravitySessions interface {
	Models(ctx context.Context, workerID string) (json.RawMessage, error)
	SetModel(ctx context.Context, workerID, model, effort, mode string) (json.RawMessage, error)
}

// SetAntigravitySessions installs the link to the Antigravity Sessions.
func (s *Server) SetAntigravitySessions(sessions AntigravitySessions) {
	if sessions == nil {
		s.SetSessionModels(work.WorkerProviderAntigravity, nil)
		return
	}
	s.SetSessionModels(work.WorkerProviderAntigravity, agySessionModels{sessions})
}

// agyModels is what a Session reports (work.antigravityModels).
type agyModels struct {
	Current   string `json:"current"`
	Effort    string `json:"effort"`
	ModelBase string `json:"model_base"`
	Efforts   []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"efforts"`
	Bases []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"bases"`
	Models []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"models"`
}

// agySessionModels is Antigravity's own model menu: its model families, each
// with agy's thinking levels. A change goes to agy at once and is kept for
// every later start of the Session.
type agySessionModels struct{ sessions AntigravitySessions }

func (a agySessionModels) Models(ctx context.Context, worker *classifier.Worker) (AgentModels, error) {
	raw, err := a.sessions.Models(ctx, worker.ID)
	if err != nil {
		return AgentModels{}, err
	}
	return agyAgentModels(raw), nil
}

func (a agySessionModels) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (AgentModels, error) {
	raw, err := a.sessions.SetModel(ctx, worker.ID, model, effort, "")
	if err != nil {
		return AgentModels{}, err
	}
	return agyAgentModels(raw), nil
}

func agyAgentModels(raw json.RawMessage) AgentModels {
	var models agyModels
	_ = json.Unmarshal(raw, &models)
	efforts := []string{}
	for _, effort := range models.Efforts {
		efforts = append(efforts, effort.ID)
	}
	out := AgentModels{Label: "Antigravity", Current: models.ModelBase, Effort: models.Effort}
	for _, base := range models.Bases {
		out.Models = append(out.Models, AgentModel{ID: base.ID, Name: base.Name, Efforts: efforts})
	}
	if len(out.Models) == 0 {
		// An agy without model families: its full model ids.
		out.Current = models.Current
		for _, model := range models.Models {
			out.Models = append(out.Models, AgentModel{ID: model.ID, Name: model.Name})
		}
	}
	return out
}
