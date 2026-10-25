package catpark

// Style is the art the person chose for the house: one whole set of pictures.
// The choice lives on the daemon, per user, so every phone and the web UI show
// the same room. An absent choice reads as the first style.
type Style string

const DefaultStyle Style = "crayon"

// Styles lists every style the page has art for, in the order the picker shows.
var Styles = []Style{"crayon", "watercolor", "gouache", "papercut", "pixel", "clay"}

// ValidStyle reports whether the page has art for the style.
func ValidStyle(style string) bool {
	for _, known := range Styles {
		if string(known) == style {
			return true
		}
	}
	return false
}

// SetStyle records the person's choice. Anything the page has no art for is
// ignored.
func (s *State) SetStyle(style string) bool {
	if !ValidStyle(style) || s.Style == style {
		return false
	}
	s.Style = style
	return true
}

// StyleOrDefault is the style the page draws.
func (s *State) StyleOrDefault() Style {
	if ValidStyle(s.Style) {
		return Style(s.Style)
	}
	return DefaultStyle
}
