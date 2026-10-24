package brain

import "fmt"

// WorkByID reads one Work from the durable record, current or terminal. The
// cat uses it to learn how a Work it sent away ended; a Work absent from the
// snapshot's current list is not thereby done.
func (s *Service) WorkByID(id string) (Work, error) {
	if s == nil || s.store == nil {
		return Work{}, fmt.Errorf("brain service is not configured")
	}
	return s.store.Work(id)
}
