package screen

import (
	"fmt"
	"strings"
)

// appScope narrows a read of an app to part of it, the way agent-desktop's
// --root, --max-depth and find do: the inside of one element (Under, an id
// from an earlier read), only so many levels down it (Depth, 0 for all), and
// only the elements a Match names. The read is still of the whole app, so ids
// and the latest read are the same as an unscoped one; only the answer is
// smaller.
type appScope struct {
	Under int
	Depth int
	Match *ElementMatch
	// Interactive keeps only what can be acted on: buttons, fields and the
	// like, or an element with an action of its own (agent-desktop's -i).
	Interactive bool
}

func (s appScope) active() bool {
	return s.Under != 0 || s.Depth != 0 || s.Match != nil || s.Interactive
}

// matchesShown is how many matches an answer lists.
const matchesShown = 10

// pick keeps the rows in scope, and says what it kept.
func (s appScope) pick(tree ElementTree, rows []appRow) ([]appRow, string, error) {
	if !s.active() {
		return rows, "", nil
	}
	var note []string
	keep := map[int]bool{}
	for _, r := range rows {
		keep[r.id] = true
	}
	if s.Under != 0 || s.Depth != 0 {
		start, end, base := 0, len(tree.Elements), 0
		if s.Under != 0 {
			i := tree.index(s.Under)
			if i < 0 {
				return nil, "", fmt.Errorf("no element [%d] in the latest read; read the app with app_state first", s.Under)
			}
			start, base = i, tree.Elements[i].Depth
			end = start + 1
			for end < len(tree.Elements) && tree.Elements[end].Depth > base {
				end++
			}
			note = append(note, fmt.Sprintf("inside [%d]", s.Under))
		}
		inside := map[int]bool{}
		for i := start; i < end; i++ {
			if s.Depth == 0 || tree.Elements[i].Depth-base <= s.Depth {
				inside[tree.id(i)] = true
			}
		}
		if s.Depth != 0 {
			note = append(note, fmt.Sprintf("%d levels down", s.Depth))
		}
		for id := range keep {
			keep[id] = keep[id] && inside[id]
		}
	}
	if s.Match != nil {
		ids, err := tree.matchIDs(*s.Match)
		if err != nil {
			return nil, "", err
		}
		matched := map[int]bool{}
		for _, id := range ids {
			matched[id] = true
		}
		for id := range keep {
			keep[id] = keep[id] && matched[id]
		}
		note = append(note, "matching "+s.Match.String())
	}
	if s.Interactive {
		note = append(note, "elements you can act on")
		for i, e := range tree.Elements {
			special := false
			for _, action := range e.Actions {
				special = special || !commonActions[action]
			}
			if !actionableRoles[e.Role] && !special {
				keep[tree.id(i)] = false
			}
		}
	}
	var out []appRow
	for _, r := range rows {
		if keep[r.id] {
			out = append(out, r)
		}
	}
	text := "Showing only " + strings.Join(note, ", ")
	if s.Match != nil {
		text += fmt.Sprintf(": %d found", len(out))
		if len(out) > matchesShown {
			out = out[:matchesShown]
			text += fmt.Sprintf(", the first %d listed", matchesShown)
		}
	}
	if s.Match != nil {
		return out, text + ".\n", nil
	}
	return out, text + ". Ids are as in the whole app.\n", nil
}
