package brain

import "github.com/daoleno/mewla/daemon/gc"

// SessionWork is the Work a Session works on, for mewla gc why.
func (s *Service) SessionWork(sessionID string) (gc.WhyLink, bool) {
	if s == nil || s.store == nil {
		return gc.WhyLink{}, false
	}
	id, ok := s.store.WorkOfSession(sessionID)
	if !ok {
		return gc.WhyLink{}, false
	}
	item, err := s.store.Work(id)
	if err != nil {
		return gc.WhyLink{ID: id}, true
	}
	return gc.WhyLink{ID: item.ID, Name: item.Title, State: string(item.Status)}, true
}
