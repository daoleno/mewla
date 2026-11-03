package screen

import (
	"fmt"
	"strings"
)

// An element action says whether it was delivered and whether to try again,
// the way agent-desktop's disposition does, so an agent doesn't press twice
// what already worked, and a toggle doesn't undo itself.

// checkState is a checkbox's or switch's state from its value: 1, true or on
// is checked, 0, false or off is not; known is false for anything else.
func checkState(e Element) (checked, known bool) {
	switch strings.ToLower(strings.TrimSpace(e.Value)) {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	}
	return false, false
}

// elementIntent turns what an agent asked of an element into an accessibility
// action and what it expects afterwards: clear is set_value with nothing;
// check and uncheck press only when the state isn't already the one asked.
type elementIntent struct {
	asked string
	// action overrides what elementAction resolves, with its text.
	action string
	text   string
	// skip is a reason nothing needs doing.
	skip string
	// want is the value set, or the state asked ("checked", "unchecked").
	wantValue string
	wantState string
}

func intentOf(e Element, asked, text string) elementIntent {
	in := elementIntent{asked: strings.ToLower(strings.TrimSpace(asked)), text: text}
	switch in.asked {
	case "clear":
		in.action, in.text, in.wantValue = "set_value", "", ""
		in.wantState = "value"
	case "set_value":
		in.wantState, in.wantValue = "value", text
	case "check", "uncheck":
		in.action = "AXPress"
		in.wantState = map[string]string{"check": "checked", "uncheck": "unchecked"}[in.asked]
		if checked, known := checkState(e); known && checked == (in.asked == "check") {
			in.skip = "already " + in.wantState
		}
	}
	return in
}

// delivery says what the read after the action shows of it.
func (in elementIntent) delivery(after *Element) string {
	switch in.wantState {
	case "value":
		if after == nil || after.secureField() {
			return "Delivery: sent, not verified. Not safe to repeat blindly; read the element to check."
		}
		if strings.TrimSpace(after.Value) == strings.TrimSpace(oneLine(in.wantValue)) || strings.TrimSpace(after.Value) == strings.TrimSpace(in.wantValue) {
			return "Delivery: verified, the element now holds it."
		}
		if strings.HasPrefix(after.Value, strings.TrimSpace(in.wantValue)) || len(in.wantValue) > elementText {
			return "Delivery: sent; the value is longer than a read shows, so use element read to verify."
		}
		return "Delivery: sent, but the element shows something else now; read it before trying again."
	case "checked", "unchecked":
		if after == nil {
			return "Delivery: sent, not verified."
		}
		if checked, known := checkState(*after); known {
			if checked == (in.wantState == "checked") {
				return "Delivery: verified, it is now " + in.wantState + "."
			}
			return fmt.Sprintf("Delivery: sent, but it is still %s; the app may have refused. Read it before trying again.", map[bool]string{true: "checked", false: "unchecked"}[checked])
		}
		return "Delivery: sent, not verified (its state isn't readable)."
	}
	return "Delivery: sent, not verified. Read what changed before repeating it."
}

// notDelivered words a refused action: nothing reached the app, so trying
// again after reading is safe. An action the app didn't answer in time is
// left out: it may have taken effect.
func notDelivered(err error) error {
	if err == nil || strings.Contains(err.Error(), "didn't answer in time") {
		return err
	}
	return fmt.Errorf("Not delivered, safe to retry after reading the app again. %w", err)
}
