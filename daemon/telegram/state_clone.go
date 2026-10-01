package telegram

import (
	"maps"
	"slices"
)

// Clone mutable containers, sharing immutable strings rather than encoding and
// decoding the entire message history on every polling read.
func cloneDurableState(s durableState) durableState {
	c := s
	c.MediaInputs = maps.Clone(s.MediaInputs)
	for k, v := range c.MediaInputs {
		v.DependsOn = slices.Clone(v.DependsOn)
		v.Parts = slices.Clone(v.Parts)
		for i := range v.Parts {
			v.Parts[i].Caption.Entities = cloneEntities(v.Parts[i].Caption.Entities)
		}
		c.MediaInputs[k] = v
	}
	c.Feedback = maps.Clone(s.Feedback)
	for k, v := range c.Feedback {
		v.Reactions = slices.Clone(v.Reactions)
		c.Feedback[k] = v
	}
	c.MessageSources = maps.Clone(s.MessageSources)
	c.Processed = maps.Clone(s.Processed)
	c.Projection = maps.Clone(s.Projection)
	c.WorkMessages = maps.Clone(s.WorkMessages)
	c.TopicProjection = maps.Clone(s.TopicProjection)
	c.TopicMessages = maps.Clone(s.TopicMessages)
	c.CallbackRoutes = maps.Clone(s.CallbackRoutes)
	c.CallbackIDs = maps.Clone(s.CallbackIDs)
	c.ReplySessions = maps.Clone(s.ReplySessions)
	c.BrainTopics = slices.Clone(s.BrainTopics)
	c.SessionChoices = slices.Clone(s.SessionChoices)
	c.Topics = slices.Clone(s.Topics)
	c.TopicOps = slices.Clone(s.TopicOps)
	c.Outbox = slices.Clone(s.Outbox)
	for i := range c.Outbox {
		r := &c.Outbox[i]
		r.Entities = cloneEntities(r.Entities)
		if r.ReplyMarkup != nil {
			m := *r.ReplyMarkup
			m.InlineKeyboard = slices.Clone(m.InlineKeyboard)
			for j := range m.InlineKeyboard {
				m.InlineKeyboard[j] = slices.Clone(m.InlineKeyboard[j])
			}
			r.ReplyMarkup = &m
		}
	}
	if s.LastReceiveAt != nil {
		v := *s.LastReceiveAt
		c.LastReceiveAt = &v
	}
	if s.LastSendAt != nil {
		v := *s.LastSendAt
		c.LastSendAt = &v
	}
	ensureDurableMaps(&c)
	return c
}
func cloneEntities(s []MessageEntity) []MessageEntity {
	c := slices.Clone(s)
	for i := range c {
		if c[i].User != nil {
			v := *c[i].User
			c[i].User = &v
		}
	}
	return c
}
