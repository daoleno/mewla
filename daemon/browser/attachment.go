package browser

import (
	"context"
	"errors"
)

// ValidateAttachment checks the user-selected resource before creating a task.
// It never starts a browser or grants access as a side effect of task creation.
func (m *Manager) ValidateAttachment(ctx context.Context, id string) error {
	if m == nil {
		return ErrUnavailable
	}
	r, err := m.Handle(ctx, Owner{Kind: "human", ID: "server-owner"}, Request{Action: "status", ID: id})
	if err != nil {
		return err
	}
	if r.Resource == nil || !r.Resource.AllowAgents {
		return errors.New("Enable managed Agent access for this Browser first")
	}
	if r.Resource.State != "running" {
		return errors.New("Open this Browser before attaching a task")
	}
	return nil
}
