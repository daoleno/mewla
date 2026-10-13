package screen

import (
	"regexp"
	"strconv"
	"strings"
)

var batteryPercent = regexp.MustCompile(`(\d{1,3})%`)

// parsePMSetBattery reads `pmset -g batt`, for example:
//
//	Now drawing from 'AC Power'
//	 -InternalBattery-0 (id=4653155)	87%; charging; 1:02 remaining present: true
func parsePMSetBattery(out string) *Power {
	power := &Power{}
	switch {
	case strings.Contains(out, "'AC Power'"):
		power.Source = "ac"
	case strings.Contains(out, "'Battery Power'"):
		power.Source = "battery"
	}
	if match := batteryPercent.FindStringSubmatch(out); match != nil {
		power.Percent, _ = strconv.Atoi(match[1])
	}
	power.Charging = strings.Contains(out, "; charging;")
	return power
}

// parseClamshell reads `ioreg -r -k AppleClamshellState`; desktops have no lid.
func parseClamshell(out string) *bool {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, `"AppleClamshellState"`) {
			continue
		}
		closed := strings.Contains(line, "= Yes")
		return &closed
	}
	return nil
}
