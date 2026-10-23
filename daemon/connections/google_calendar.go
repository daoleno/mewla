package connections

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	calendarPageDefault = 25
	calendarPageMax     = 100
)

var calendarReadEvents = []string{"calendar.events.readonly", "calendar.events"}
var calendarWriteEvents = []string{"calendar.events"}

func calendarTools() []googleTool {
	calendarID := map[string]any{"type": "string", "minLength": 1, "maxLength": 1024, "description": `Calendar id or "primary" (the default)`}
	moment := text(64)
	notify := choice("none", "all", "externalOnly")
	eventFields := func() map[string]any {
		return map[string]any{
			"calendar_id": calendarID, "summary": text(1024), "description": text(8192), "location": text(1024),
			"start": moment, "end": moment, "time_zone": text(64), "send_updates": notify,
		}
	}
	create := eventFields()
	create["attendees"] = list(text(320), 100)
	update := eventFields()
	update["event_id"] = text(1024)
	update["add_attendees"] = list(text(320), 100)
	update["remove_attendees"] = list(text(320), 100)
	return []googleTool{
		{googleToolDef("list_calendars", "List the calendars this account can see, with id, name, time zone and access role", true, object(map[string]any{"max_results": intRange(1, 250), "page_token": text(512)})), []string{"calendar.calendarlist.readonly"}, calendarList},
		{googleToolDef("list_events", fmt.Sprintf(`List events between time_min and time_max (RFC 3339 with an offset such as 2026-10-23T00:00:00+08:00, or a date YYYY-MM-DD). Recurring events come as single occurrences in start order. Up to %d per page; times are in the calendar's time zone unless time_zone is given`, calendarPageMax), true, object(map[string]any{
			"calendar_id": calendarID, "time_min": moment, "time_max": moment, "query": text(1024), "time_zone": text(64),
			"max_results": intRange(1, calendarPageMax), "page_token": text(512),
		}, "time_min", "time_max")), calendarReadEvents, calendarListEvents},
		{googleToolDef("get_event", "Read one event in full: guests and their replies, location, description and meeting link", true, object(map[string]any{"calendar_id": calendarID, "event_id": text(1024)}, "event_id")), calendarReadEvents, calendarGetEvent},
		{googleToolDef("check_availability", "Find when calendars are busy between time_min and time_max (RFC 3339 or a date). Defaults to the primary calendar; up to 20 calendar ids or people's email addresses", true, object(map[string]any{
			"time_min": moment, "time_max": moment, "calendar_ids": list(text(1024), 20), "time_zone": text(64),
		}, "time_min", "time_max")), []string{"calendar.freebusy"}, calendarFreeBusy},
		{googleToolDef("create_event", `Create an event. start and end are RFC 3339 with an offset (or with time_zone, an IANA name such as Asia/Shanghai), or dates for an all-day event, where end is the day after the last day. Guests are not emailed unless send_updates is "all"`, false, object(create, "summary", "start", "end")), calendarWriteEvents, calendarCreateEvent},
		{googleToolDef("update_event", "Change an event. Only the fields given change. add_attendees and remove_attendees edit the guest list without touching other guests", false, object(update, "event_id")), calendarWriteEvents, calendarUpdateEvent},
		{googleToolDef("delete_event", "Delete an event from a calendar. Guests are not emailed unless send_updates is \"all\"", false, object(map[string]any{"calendar_id": calendarID, "event_id": text(1024), "send_updates": notify}, "event_id")), calendarWriteEvents, calendarDeleteEvent},
		{googleToolDef("respond_to_event", "Reply to an invitation for this account: accepted, declined or tentative. The organizer is not emailed unless send_updates is \"all\"", false, object(map[string]any{
			"calendar_id": calendarID, "event_id": text(1024), "response": choice("accepted", "declined", "tentative"), "send_updates": notify,
		}, "event_id", "response")), calendarWriteEvents, calendarRespond},
	}
}

// Times.

var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// calendarMoment turns an RFC 3339 time or a date into an RFC 3339 time. A
// date starts at midnight in zone (the account's primary calendar zone when
// none is named).
func calendarMoment(c *googleCall, value, zone string) (string, error) {
	if !dateOnly.MatchString(value) {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return "", fmt.Errorf("%q is not a time: use RFC 3339 such as 2026-10-23T09:00:00+08:00, or a date YYYY-MM-DD", value)
		}
		return value, nil
	}
	location, err := calendarZone(c, zone)
	if err != nil {
		return "", err
	}
	day, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return "", fmt.Errorf("%q is not a date", value)
	}
	return day.Format(time.RFC3339), nil
}

func calendarZone(c *googleCall, name string) (*time.Location, error) {
	if name == "" {
		var primary struct {
			TimeZone string `json:"timeZone"`
		}
		if err := c.do("", "GET", "/calendar/v3/users/me/calendarList/primary", nil, nil, &primary); err != nil {
			return nil, err
		}
		name = primary.TimeZone
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%q is not a time zone name such as Asia/Shanghai", name)
	}
	return location, nil
}

// eventTime is an event's start or end as Google takes it: a date for an
// all-day event, otherwise a date-time with its zone.
func eventTime(value, zone string) (map[string]any, error) {
	if dateOnly.MatchString(value) {
		return map[string]any{"date": value}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if local, localErr := time.Parse("2006-01-02T15:04:05", value); localErr == nil && zone != "" {
			return map[string]any{"dateTime": local.Format("2006-01-02T15:04:05"), "timeZone": zone}, nil
		}
		return nil, fmt.Errorf("%q is not a time: use RFC 3339 such as 2026-10-23T09:00:00+08:00, or a date YYYY-MM-DD", value)
	}
	out := map[string]any{"dateTime": parsed.Format(time.RFC3339)}
	if zone != "" {
		out["timeZone"] = zone
	}
	return out, nil
}

// Events.

type calendarEvent struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	Description      string `json:"description"`
	Location         string `json:"location"`
	HTMLLink         string `json:"htmlLink"`
	HangoutLink      string `json:"hangoutLink"`
	RecurringEventID string `json:"recurringEventId"`
	Start            struct {
		Date, DateTime, TimeZone string
	} `json:"start"`
	End struct {
		Date, DateTime, TimeZone string
	} `json:"end"`
	Organizer struct {
		Email, DisplayName string
		Self               bool
	} `json:"organizer"`
	Attendees []calendarGuest `json:"attendees"`
}

type calendarGuest struct {
	Email            string `json:"email"`
	DisplayName      string `json:"displayName,omitempty"`
	ResponseStatus   string `json:"responseStatus,omitempty"`
	Optional         bool   `json:"optional,omitempty"`
	Self             bool   `json:"self,omitempty"`
	Organizer        bool   `json:"organizer,omitempty"`
	Resource         bool   `json:"resource,omitempty"`
	Comment          string `json:"comment,omitempty"`
	AdditionalGuests int    `json:"additionalGuests,omitempty"`
}

func (e *calendarEvent) shaped(detail bool) map[string]any {
	start, end := e.Start.DateTime, e.End.DateTime
	allDay := start == ""
	if allDay {
		start, end = e.Start.Date, e.End.Date
	}
	out := map[string]any{"id": e.ID, "summary": e.Summary, "start": start, "end": end, "all_day": allDay, "status": e.Status}
	if e.Location != "" {
		out["location"] = e.Location
	}
	if link := e.HangoutLink; link != "" {
		out["meeting_link"] = link
	}
	if e.RecurringEventID != "" {
		out["recurring_event_id"] = e.RecurringEventID
	}
	if e.Organizer.Email != "" {
		out["organizer"] = e.Organizer.Email
	}
	if len(e.Attendees) > 0 {
		guests := make([]map[string]any, 0, len(e.Attendees))
		for _, a := range e.Attendees {
			guest := map[string]any{"email": a.Email, "response": a.ResponseStatus}
			if a.DisplayName != "" {
				guest["name"] = a.DisplayName
			}
			if a.Self {
				guest["you"] = true
			}
			guests = append(guests, guest)
		}
		out["attendees"] = guests
	}
	limit := 500
	if detail {
		limit = 8000
		out["html_link"] = e.HTMLLink
	}
	if e.Description != "" {
		out["description"], _ = truncate(e.Description, limit)
	}
	return out
}

func calendarPath(args map[string]any) string {
	id := argString(args, "calendar_id")
	if id == "" {
		id = "primary"
	}
	return "/calendar/v3/calendars/" + pathID(id)
}

func calendarList(c *googleCall, args map[string]any) (any, error) {
	query := url.Values{"maxResults": {fmt.Sprint(argInt(args, "max_results", 100))}}
	if t := argString(args, "page_token"); t != "" {
		query.Set("pageToken", t)
	}
	var page struct {
		Items []struct {
			ID, Summary, Description, TimeZone, AccessRole string
			Primary, Selected                              bool
		} `json:"items"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.do("", "GET", "/calendar/v3/users/me/calendarList", query, nil, &page); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, item := range page.Items {
		rows = append(rows, map[string]any{"id": item.ID, "name": item.Summary, "primary": item.Primary, "time_zone": item.TimeZone, "access_role": item.AccessRole, "description": item.Description})
	}
	out := map[string]any{"calendars": rows}
	if page.NextPageToken != "" {
		out["next_page_token"] = page.NextPageToken
	}
	return out, nil
}

func calendarListEvents(c *googleCall, args map[string]any) (any, error) {
	zone := argString(args, "time_zone")
	min, err := calendarMoment(c, argString(args, "time_min"), zone)
	if err != nil {
		return nil, err
	}
	max, err := calendarMoment(c, argString(args, "time_max"), zone)
	if err != nil {
		return nil, err
	}
	// Single occurrences in start order: without these Google returns each
	// recurring series once, with no way to tell what falls today.
	query := url.Values{"timeMin": {min}, "timeMax": {max}, "singleEvents": {"true"}, "orderBy": {"startTime"}, "maxResults": {fmt.Sprint(argInt(args, "max_results", calendarPageDefault))}}
	if q := argString(args, "query"); q != "" {
		query.Set("q", q)
	}
	if zone != "" {
		query.Set("timeZone", zone)
	}
	if t := argString(args, "page_token"); t != "" {
		query.Set("pageToken", t)
	}
	var page struct {
		TimeZone      string          `json:"timeZone"`
		Items         []calendarEvent `json:"items"`
		NextPageToken string          `json:"nextPageToken"`
	}
	if err = c.do("", "GET", calendarPath(args)+"/events", query, nil, &page); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for i := range page.Items {
		if page.Items[i].Status != "cancelled" {
			rows = append(rows, page.Items[i].shaped(false))
		}
	}
	out := map[string]any{"time_zone": page.TimeZone, "events": rows}
	if page.NextPageToken != "" {
		out["next_page_token"] = page.NextPageToken
	}
	return out, nil
}

func calendarGetEvent(c *googleCall, args map[string]any) (any, error) {
	var event calendarEvent
	if err := c.do("", "GET", calendarPath(args)+"/events/"+pathID(argString(args, "event_id")), nil, nil, &event); err != nil {
		return nil, err
	}
	return event.shaped(true), nil
}

func calendarFreeBusy(c *googleCall, args map[string]any) (any, error) {
	zone := argString(args, "time_zone")
	min, err := calendarMoment(c, argString(args, "time_min"), zone)
	if err != nil {
		return nil, err
	}
	max, err := calendarMoment(c, argString(args, "time_max"), zone)
	if err != nil {
		return nil, err
	}
	ids := argStrings(args, "calendar_ids")
	if len(ids) == 0 {
		ids = []string{"primary"}
	}
	items := []map[string]string{}
	for _, id := range ids {
		items = append(items, map[string]string{"id": id})
	}
	body := map[string]any{"timeMin": min, "timeMax": max, "items": items}
	if zone != "" {
		body["timeZone"] = zone
	}
	var answer struct {
		Calendars map[string]struct {
			Busy   []struct{ Start, End string } `json:"busy"`
			Errors []struct{ Reason string }     `json:"errors"`
		} `json:"calendars"`
	}
	if err = c.do("", "POST", "/calendar/v3/freeBusy", nil, body, &answer); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for id, calendar := range answer.Calendars {
		busy := []map[string]string{}
		for _, b := range calendar.Busy {
			busy = append(busy, map[string]string{"start": b.Start, "end": b.End})
		}
		row := map[string]any{"busy": busy}
		if len(calendar.Errors) > 0 {
			row["error"] = calendar.Errors[0].Reason
		}
		out[id] = row
	}
	return map[string]any{"calendars": out}, nil
}

func sendUpdates(args map[string]any) url.Values {
	mode := argString(args, "send_updates")
	if mode == "" {
		mode = "none"
	}
	return url.Values{"sendUpdates": {mode}}
}

func guestList(emails []string) ([]map[string]string, error) {
	out := []map[string]string{}
	for _, value := range emails {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("attendees: an address cannot contain a line break")
		}
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			return nil, fmt.Errorf("attendees: %q is not an email address", value)
		}
		guest := map[string]string{"email": parsed.Address}
		if parsed.Name != "" {
			guest["displayName"] = parsed.Name
		}
		out = append(out, guest)
	}
	return out, nil
}

// bareEmail reduces "Name <a@b.c>" to a@b.c for comparing guests.
func bareEmail(value string) string {
	if i := strings.LastIndex(value, "<"); i >= 0 {
		value = strings.TrimSuffix(value[i+1:], ">")
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func calendarCreateEvent(c *googleCall, args map[string]any) (any, error) {
	zone := argString(args, "time_zone")
	start, err := eventTime(argString(args, "start"), zone)
	if err != nil {
		return nil, err
	}
	end, err := eventTime(argString(args, "end"), zone)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"summary": argString(args, "summary"), "start": start, "end": end}
	for key, field := range map[string]string{"description": "description", "location": "location"} {
		if v := argString(args, key); v != "" {
			body[field] = v
		}
	}
	if emails := argStrings(args, "attendees"); len(emails) > 0 {
		if body["attendees"], err = guestList(emails); err != nil {
			return nil, err
		}
	}
	var event calendarEvent
	if err = c.do("", "POST", calendarPath(args)+"/events", sendUpdates(args), body, &event); err != nil {
		return nil, err
	}
	return event.shaped(true), nil
}

func calendarUpdateEvent(c *googleCall, args map[string]any) (any, error) {
	path := calendarPath(args) + "/events/" + pathID(argString(args, "event_id"))
	zone := argString(args, "time_zone")
	body := map[string]any{}
	for _, key := range []string{"summary", "description", "location"} {
		if v := argString(args, key); v != "" {
			body[key] = v
		}
	}
	for _, key := range []string{"start", "end"} {
		if v := argString(args, key); v != "" {
			moment, err := eventTime(v, zone)
			if err != nil {
				return nil, err
			}
			body[key] = moment
		}
	}
	add, remove := argStrings(args, "add_attendees"), argStrings(args, "remove_attendees")
	if len(add)+len(remove) > 0 {
		// Google replaces the whole guest list on a change, so start from it.
		var current calendarEvent
		if err := c.do("", "GET", path, nil, nil, &current); err != nil {
			return nil, err
		}
		added, err := mailAddresses("add_attendees", add)
		if err != nil {
			return nil, err
		}
		dropped := map[string]bool{}
		for _, email := range remove {
			dropped[bareEmail(email)] = true
		}
		guests := []calendarGuest{}
		have := map[string]bool{}
		for _, g := range current.Attendees {
			if !dropped[strings.ToLower(g.Email)] {
				guests = append(guests, g)
				have[strings.ToLower(g.Email)] = true
			}
		}
		for _, a := range added {
			if email := bareEmail(a); !have[email] {
				guests = append(guests, calendarGuest{Email: email})
				have[email] = true
			}
		}
		body["attendees"] = guests
	}
	if len(body) == 0 {
		return nil, errNoChange
	}
	var event calendarEvent
	if err := c.do("", "PATCH", path, sendUpdates(args), body, &event); err != nil {
		return nil, err
	}
	return event.shaped(true), nil
}

func calendarDeleteEvent(c *googleCall, args map[string]any) (any, error) {
	if err := c.do("", "DELETE", calendarPath(args)+"/events/"+pathID(argString(args, "event_id")), sendUpdates(args), nil, nil); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true}, nil
}

func calendarRespond(c *googleCall, args map[string]any) (any, error) {
	path := calendarPath(args) + "/events/" + pathID(argString(args, "event_id"))
	var current calendarEvent
	if err := c.do("", "GET", path, nil, nil, &current); err != nil {
		return nil, err
	}
	found := false
	for i := range current.Attendees {
		if current.Attendees[i].Self {
			current.Attendees[i].ResponseStatus = argString(args, "response")
			found = true
		}
	}
	if !found {
		return nil, errors.New("this account is not on the event's guest list, so it has no reply to give")
	}
	var event calendarEvent
	if err := c.do("", "PATCH", path, sendUpdates(args), map[string]any{"attendees": current.Attendees}, &event); err != nil {
		return nil, err
	}
	return event.shaped(true), nil
}
