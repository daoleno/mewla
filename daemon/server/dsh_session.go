package server

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/gorilla/websocket"
)

type dshModels struct {
	Current struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Effort   string `json:"reasoningEffort"`
	} `json:"current"`
	Routable bool `json:"routable"`
	Groups   []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Models []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Reasoning struct {
				Default string `json:"defaultEffort"`
				Efforts []struct {
					ID string `json:"id"`
				} `json:"efforts"`
			} `json:"reasoning"`
		} `json:"models"`
	} `json:"groups"`
}

// dshSessionModels is DSH's own model menu: the models of the provider the
// Session runs on, switched by DSH itself.
type dshSessionModels struct{}

func (dshSessionModels) Models(ctx context.Context, worker *classifier.Worker) (AgentModels, error) {
	var models dshModels
	if err := work.CallDSH(ctx, work.DSHSessionID(worker.Command), "session.models", map[string]any{}, &models); err != nil {
		return AgentModels{}, err
	}
	return dshAgentModels(models), nil
}

func (d dshSessionModels) SetModel(ctx context.Context, worker *classifier.Worker, model, effort string) (AgentModels, error) {
	id := work.DSHSessionID(worker.Command)
	var models dshModels
	if err := work.CallDSH(ctx, id, "session.models", map[string]any{}, &models); err != nil {
		return AgentModels{}, err
	}
	choice := map[string]any{"provider": models.Current.Provider, "model": model}
	if effort != "" {
		choice["reasoningEffort"] = effort
	}
	if err := work.CallDSH(ctx, id, "session.selectModel", choice, nil); err != nil {
		return AgentModels{}, err
	}
	after, err := d.Models(ctx, worker)
	// Native acknowledgement applies the selection; DSH saves its session log
	// asynchronously, so no daemon fsync is claimed.
	return after, err
}

func dshAgentModels(models dshModels) AgentModels {
	out := AgentModels{Label: models.Current.Provider, Current: models.Current.Model, Effort: models.Current.Effort}
	for _, group := range models.Groups {
		if group.ID != models.Current.Provider {
			continue
		}
		out.Label = group.Name
		for _, model := range group.Models {
			efforts := []string{}
			for _, effort := range model.Reasoning.Efforts {
				efforts = append(efforts, effort.ID)
			}
			out.Models = append(out.Models, AgentModel{ID: model.ID, Name: model.Name, Efforts: efforts, DefaultEffort: model.Reasoning.Default})
		}
	}
	return out
}

func (s *Server) handleDSHImage(conn *websocket.Conn, raw clientMessage) {
	worker := s.currentSessionFileWorker(raw.WorkerID)
	if err := validateSessionFileIdentity(worker, raw); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_file_changed", err.Error())
		return
	}
	id := work.DSHSessionID(worker.Command)
	attachmentID := strings.TrimPrefix(raw.Path, "dsh-attachment:")
	if id == "" || attachmentID == raw.Path || attachmentID == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", "Unsupported image owner")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var image struct {
		Data       string `json:"data"`
		Attachment struct {
			MediaType string `json:"mediaType"`
			Bytes     int64  `json:"bytes"`
			Width     int64  `json:"width"`
			Height    int64  `json:"height"`
		} `json:"attachment"`
	}
	if err := work.CallDSH(ctx, id, "session.attachment", map[string]any{"attachmentId": attachmentID}, &image); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", err.Error())
		return
	}
	if image.Attachment.Bytes > 5<<20 || len(image.Data) > 8<<20 || image.Attachment.Width*image.Attachment.Height > 40_000_000 || !strings.HasPrefix(image.Attachment.MediaType, "image/") {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_image_failed", "Image exceeds preview limits")
		return
	}
	s.sendJSON(conn, map[string]any{"type": "session_image", "request_id": raw.RequestID, "data_url": "data:" + image.Attachment.MediaType + ";base64," + image.Data})
}

func (s *Server) handleDSHInteraction(conn *websocket.Conn, raw clientMessage) {
	worker := s.currentSessionFileWorker(raw.WorkerID)
	if err := validateSessionFileIdentity(worker, raw); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "session_changed", err.Error())
		return
	}
	id := work.DSHSessionID(worker.Command)
	if id == "" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "unsupported_session", "DSH Session required")
		return
	}
	method := "session.interactions"
	payload := map[string]any{}
	if raw.DSHAfterRev != nil {
		payload["afterRev"] = *raw.DSHAfterRev
	}
	if len(raw.DSHAnswer) > 0 {
		method = "session.respond"
		if json.Unmarshal(raw.DSHAnswer, &payload) != nil || payload == nil {
			s.sendErrorWithRequestID(conn, raw.RequestID, "invalid_answer", "Invalid answer")
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// What was asked, read before the answer settles it.
	var asked *work.DSHInteraction
	if method == "session.respond" && s.answeredLedger() != nil {
		var pending struct {
			Items []work.DSHInteraction `json:"items"`
		}
		if work.CallDSH(ctx, id, "session.interactions", map[string]any{}, &pending) == nil {
			for _, item := range pending.Items {
				if rpcID, _ := payload["rpcId"].(string); item.RPCID == rpcID {
					copied := item
					asked = &copied
				}
			}
		}
	}
	var result json.RawMessage
	if err := work.CallDSH(ctx, id, method, payload, &result); err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "dsh_interaction_failed", err.Error())
		return
	}
	if asked != nil {
		var outcome struct {
			Accepted bool `json:"accepted"`
		}
		if json.Unmarshal(result, &outcome) == nil && outcome.Accepted {
			if record, ok := DSHAnswerRecord(worker.ID, *asked, payload, s.answeredByConn(conn), time.Now()); ok {
				s.recordAnswered(record)
			}
		}
	}
	s.sendJSON(conn, map[string]any{"type": "dsh_interaction", "request_id": raw.RequestID, "result": result})
}
