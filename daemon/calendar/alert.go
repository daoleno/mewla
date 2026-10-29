package calendar

import "time"

// MaxAlertLeadMinutes bounds AlertLeadMinutes: a week.
const MaxAlertLeadMinutes = 7 * 24 * 60

// Default lead times, in minutes, when an item sets none. A reminder is
// already the moment to be told.
const (
	DefaultEventLeadMinutes    = 10
	DefaultDeadlineLeadMinutes = 30
)

// AlertWindow is how late an alert may still be sent. Past it the moment has
// gone (the daemon was off, or the item was created after the fact) and the
// alert is dropped instead of arriving as news from the past.
const AlertWindow = 15 * time.Minute

// AlertLead is how long before the item's time its alert fires.
func (i Item) AlertLead() time.Duration {
	if i.AlertLeadMinutes != nil {
		return time.Duration(*i.AlertLeadMinutes) * time.Minute
	}
	switch i.Kind {
	case KindEvent:
		return DefaultEventLeadMinutes * time.Minute
	case KindDeadline:
		return DefaultDeadlineLeadMinutes * time.Minute
	}
	return 0
}

// AlertAt is the instant the item next alerts the user: its snooze, or its
// time less the lead. Owned items act for Mewla and scheduled actions run;
// neither alerts.
func (i Item) AlertAt() (time.Time, bool) {
	if i.Owner != "" || i.Kind == KindScheduledAction || i.NextAt.IsZero() {
		return time.Time{}, false
	}
	if i.SnoozedUntil != nil {
		return *i.SnoozedUntil, true
	}
	return i.NextAt.Add(-i.AlertLead()), true
}

// AlertPending reports the alert the daemon still owes: AlertAt when it is
// later than the last one fired. A lead lengthened after the fact moves
// AlertAt back past AlertedAt and owes nothing.
func (i Item) AlertPending() (time.Time, bool) {
	at, ok := i.AlertAt()
	if !ok || (i.AlertedAt != nil && !at.After(*i.AlertedAt)) {
		return time.Time{}, false
	}
	return at, true
}

// AlertOpen reports whether the user still owes the item's last alert an
// answer.
func (i Item) AlertOpen() bool {
	if i.AlertedAt == nil || (i.AckedAt != nil && !i.AckedAt.Before(*i.AlertedAt)) {
		return false
	}
	switch i.Status {
	case StatusScheduled, StatusWaiting, StatusRunning:
		return true
	}
	return false
}
