package push

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// calendarChannel is the Android channel calendar alerts use: heads-up with a
// sound, and the one the phone's own alarm posts to.
const calendarChannel = "mewla-calendar"

// calendarAlertTTL is how long a provider keeps an alert it could not deliver
// yet. Past the daemon's alert window the moment has gone.
const calendarAlertTTL = 15 * 60

// CalendarAlert is one occurrence to tell the user about.
type CalendarAlert struct {
	ID    string
	Kind  string // event, reminder or deadline
	Title string
	Notes string
	// At is when the item starts or falls due; AlertAt when this alert fires,
	// which is also its identity for the phone's dedup.
	At      time.Time
	AlertAt time.Time
	Now     time.Time
}

// CalendarAlertText is the title and body the phone shows.
func CalendarAlertText(alert CalendarAlert) (title, body string) {
	title = strings.TrimSpace(alert.Title)
	if title == "" {
		title = "Calendar"
	}
	var when string
	switch until := alert.At.Sub(alert.Now); {
	case alert.Kind == "reminder":
		when = "Reminder"
	case until <= 30*time.Second && alert.Kind == "deadline":
		when = "Due now"
	case until <= 30*time.Second:
		when = "Starting now"
	case alert.Kind == "deadline":
		when = "Due in " + duration(until)
	default:
		when = "Starts in " + duration(until)
	}
	body = when
	if notes := strings.TrimSpace(alert.Notes); notes != "" {
		body = when + " · " + notes
	}
	return title, buildNotificationBody(body, "Calendar")
}

func duration(d time.Duration) string {
	minutes := int((d + 30*time.Second) / time.Minute)
	switch {
	case minutes < 1:
		return "1 min"
	case minutes < 60:
		return fmt.Sprintf("%d min", minutes)
	case minutes%60 == 0:
		return fmt.Sprintf("%d h", minutes/60)
	case minutes < 24*60:
		return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
	}
	return fmt.Sprintf("%d d", minutes/(24*60))
}

// calendarAlertKey is what the phone dedups its own alarm and this push by.
func calendarAlertKey(id string, alertAt time.Time) string {
	return id + "@" + strconv.FormatInt(alertAt.UnixMilli(), 10)
}

func calendarMessage(alert CalendarAlert, r registration) Message {
	title, body := CalendarAlertText(alert)
	msg := Message{
		Title:    title,
		Body:     body,
		Priority: "high",
		Data: map[string]string{
			"screen":        "calendar",
			"calendar_id":   alert.ID,
			"alert_key":     calendarAlertKey(alert.ID, alert.AlertAt),
			"alert_at":      strconv.FormatInt(alert.AlertAt.UnixMilli(), 10),
			"at":            strconv.FormatInt(alert.At.UnixMilli(), 10),
			"calendar_kind": alert.Kind,
			"strong":        strconv.FormatBool(!r.quiet),
			"server_id":     r.serverRef,
		},
		ChannelID: calendarChannel,
		TTL:       calendarAlertTTL,
	}
	switch r.platform {
	case "android":
		// The phone's service posts it, so a copy its own alarm already showed
		// is dropped instead of shown twice.
		msg.DataOnly = true
		msg.Data["mewla_calendar"] = "1"
	case "ios":
		if !r.quiet {
			msg.InterruptionLevel = "time-sensitive"
		}
	}
	return msg
}

// NotifyCalendarAlert tells every phone an event, reminder or deadline is
// near, recorded under KindCalendarAlert. Android phones get a data-only
// message their app posts (and dedups against its own alarm); iOS phones get
// a visible one, time-sensitive unless the person turned strong alerts off.
func (c *Client) NotifyCalendarAlert(alert CalendarAlert) error {
	return c.dispatch(KindCalendarAlert, func(r registration) Message {
		return calendarMessage(alert, r)
	})
}
