package connections

import (
	"context"
	"encoding/json"
	"errors"
)

const googleScopeBase = "https://www.googleapis.com/auth/"

func googleScopes(writes bool) []string {
	scopes := []string{"openid", "email", googleScopeBase + "drive.readonly", googleScopeBase + "gmail.readonly", googleScopeBase + "calendar.readonly"}
	if writes {
		scopes = append(scopes, googleScopeBase+"drive.file", googleScopeBase+"gmail.send", googleScopeBase+"calendar.events")
	}
	return scopes
}

type googleOperation struct {
	tool     Tool
	endpoint string
	scopes   []string
}

func googleOperations() []googleOperation {
	id := object(map[string]any{"id": str()}, "id")
	calendar := object(map[string]any{"calendarId": str()}, "calendarId")
	event := object(map[string]any{"calendarId": str(), "eventId": str()}, "calendarId", "eventId")
	eventBody := object(map[string]any{"summary": str(), "description": str(), "start": map[string]any{"type": "object"}, "end": map[string]any{"type": "object"}, "attendees": map[string]any{"type": "array", "maxItems": 100, "items": map[string]any{"type": "object"}}}, "summary", "start", "end")
	ops := []googleOperation{
		{builtin("get_me", "Read the authorized Google account", "GET", "/v1/userinfo", true, nil, nil, nil), "https://openidconnect.googleapis.com", []string{"openid"}},
		{builtin("drive_list_files", "List or search Drive files (one page)", "GET", "/drive/v3/files", true, nil, object(map[string]any{"q": str(), "pageSize": pageSize(), "pageToken": str()}), nil), "https://www.googleapis.com", []string{"drive.readonly", "drive.file", "drive"}},
		{builtin("drive_get_file", "Read Drive file metadata", "GET", "/drive/v3/files/{id}", true, id, nil, nil), "https://www.googleapis.com", []string{"drive.readonly", "drive.file", "drive"}},
		{builtin("drive_update_file", "Update metadata of a file authorized for this app", "PATCH", "/drive/v3/files/{id}", false, id, nil, object(map[string]any{"name": str(), "description": str(), "starred": map[string]any{"type": "boolean"}})), "https://www.googleapis.com", []string{"drive.file", "drive"}},
		{builtin("gmail_list_messages", "Search Gmail messages (one page)", "GET", "/gmail/v1/users/me/messages", true, nil, object(map[string]any{"q": str(), "maxResults": pageSize(), "pageToken": str()}), nil), "https://gmail.googleapis.com", []string{"gmail.readonly", "gmail.modify"}},
		{builtin("gmail_get_message", "Read a Gmail message", "GET", "/gmail/v1/users/me/messages/{id}", true, id, object(map[string]any{"format": map[string]any{"type": "string", "enum": []string{"metadata", "minimal", "full"}}}), nil), "https://gmail.googleapis.com", []string{"gmail.readonly", "gmail.modify"}},
		{builtin("gmail_send_message", "Send an RFC 2822 message encoded as base64url", "POST", "/gmail/v1/users/me/messages/send", false, nil, nil, object(map[string]any{"raw": map[string]any{"type": "string", "minLength": 1, "maxLength": 60000, "pattern": "^[A-Za-z0-9_-]+=*$"}}, "raw")), "https://gmail.googleapis.com", []string{"gmail.send", "gmail.modify"}},
		{builtin("calendar_list_calendars", "List Google calendars (one page)", "GET", "/calendar/v3/users/me/calendarList", true, nil, object(map[string]any{"maxResults": pageSize(), "pageToken": str()}), nil), "https://www.googleapis.com", []string{"calendar.readonly", "calendar"}},
		{builtin("calendar_list_events", "List calendar events (one page)", "GET", "/calendar/v3/calendars/{calendarId}/events", true, calendar, object(map[string]any{"maxResults": pageSize(), "pageToken": str(), "timeMin": str(), "timeMax": str(), "q": str()}), nil), "https://www.googleapis.com", []string{"calendar.readonly", "calendar.events", "calendar"}},
		{builtin("calendar_create_event", "Create a calendar event", "POST", "/calendar/v3/calendars/{calendarId}/events", false, calendar, nil, eventBody), "https://www.googleapis.com", []string{"calendar.events", "calendar"}},
		{builtin("calendar_update_event", "Update a calendar event", "PATCH", "/calendar/v3/calendars/{calendarId}/events/{eventId}", false, event, nil, eventBody), "https://www.googleapis.com", []string{"calendar.events", "calendar"}},
	}
	return ops
}
func googleAllowed(op googleOperation, scopes []string) bool {
	for _, scope := range op.scopes {
		if scope != "openid" {
			scope = googleScopeBase + scope
		}
		if contains(scopes, scope) {
			return true
		}
	}
	return false
}
func (m *Manager) discoverGoogle(ctx context.Context, r *record, secret string) error {
	raw, err := m.request(ctx, r, "https://openidconnect.googleapis.com", secret, "GET", "/v1/userinfo", nil)
	if err != nil {
		return err
	}
	var identity struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.Sub == "" {
		return errors.New("Google did not return account identity")
	}
	r.Account.Identity = identity.Email
	if r.Account.Identity == "" {
		r.Account.Identity = identity.Sub
	}
	r.Account.Tools = nil
	for _, op := range googleOperations() {
		if googleAllowed(op, r.Account.Scopes) {
			r.Account.Tools = append(r.Account.Tools, op.tool)
		}
	}
	return nil
}
func (m *Manager) invokeGoogle(ctx context.Context, r *record, secret string, t Tool, args map[string]any) ([]byte, error) {
	for _, op := range googleOperations() {
		if op.tool.Name == t.Name && fingerprint(op.tool) == fingerprint(t) && googleAllowed(op, r.Account.Scopes) {
			return m.dispatchHTTP(ctx, r, op.endpoint, secret, op.tool, args)
		}
	}
	return nil, errors.New("Google tool unavailable for the authorized scopes; reconnect with the needed access")
}
