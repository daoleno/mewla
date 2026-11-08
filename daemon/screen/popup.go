package screen

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// popupChooser is a backend that can choose an item of a pop-up button in one
// step. An app lists a pop-up's items only while its menu is open, so opening
// it, reading the items and choosing one across several turns of a model keeps
// the menu, and the keyboard it takes from the person's window, open for
// seconds. One step opens it, takes the item and lets it close again, in the
// time the app needs.
type popupChooser interface {
	// ChoosePopupItem checks that e is still the element that was read,
	// opens its menu, presses the item titled title (case and a trailing …
	// do not matter) and makes sure the menu is closed. An unknown title is
	// a *PopupItemError that lists the items; nothing is then chosen.
	ChoosePopupItem(ctx context.Context, e Element, title string) (PopupChoice, error)
}

// PopupChoice is what choosing from a pop-up did.
type PopupChoice struct {
	// Title is the item chosen, as the app titles it.
	Title string
	// Open is how long the menu was open.
	Open time.Duration
}

// PopupItemError says no item of a pop-up has the title asked for.
type PopupItemError struct {
	Want    string
	Options []string
}

func (e *PopupItemError) Error() string {
	if len(e.Options) == 0 {
		return fmt.Sprintf("the pop-up showed no items when its menu was opened, so %q was not chosen; click it and choose with a key or a click on the item", e.Want)
	}
	return fmt.Sprintf("no item %q in this pop-up; it has: %s", e.Want, strings.Join(e.Options, ", "))
}

// matchPopupItem finds the item a title names: exactly, then as a person
// writes it (without case or a trailing …).
func matchPopupItem(options []string, want string) int {
	for i, o := range options {
		if strings.TrimSpace(o) == strings.TrimSpace(want) {
			return i
		}
	}
	for i, o := range options {
		if sameMenuTitle(o, want) {
			return i
		}
	}
	return -1
}

// popupHint ends the row of a pop-up button in an app read.
const popupHint = ` (choose an item with set_value and its title)`
