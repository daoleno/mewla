package ghosttyvt

import "testing"

func TestParseKey(t *testing.T) {
	for spec, want := range map[string]keyPress{
		"Enter":   {name: keyEnter},
		"BTab":    {name: keyTab, shift: true},
		"BSpace":  {name: keyBackspace},
		"PPage":   {name: keyPageUp},
		"NPage":   {name: keyPageDown},
		"F12":     {name: keyF1 + 11},
		"C-c":     {name: keyText, text: 'c', ctrl: true},
		"C-C":     {name: keyText, text: 'c', ctrl: true},
		"C-M-x":   {name: keyText, text: 'x', ctrl: true, alt: true},
		"S-Up":    {name: keyUp, shift: true},
		"q":       {name: keyText, text: 'q'},
		"Q":       {name: keyText, text: 'Q', shift: true},
		"-":       {name: keyText, text: '-'},
		"日":       {name: keyText, text: '日'},
		"M-Enter": {name: keyEnter, alt: true},
	} {
		got, err := parseKey(spec)
		if err != nil || got != want {
			t.Errorf("parseKey(%q) = %+v, %v; want %+v", spec, got, err, want)
		}
	}
	for _, bad := range []string{"", "Enterr", "F13", "F0", "X-a", "ab", "\x03", "\x7f", "C-"} {
		if err := ValidKey(bad); err == nil {
			t.Errorf("ValidKey(%q) accepted", bad)
		}
	}
}
