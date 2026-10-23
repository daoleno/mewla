package connections

import "sort"

// The golden dump in golden_test.go reads reviewed data through these few
// functions, so the same dump can be taken from the code that holds the data
// in switch statements and from the code that holds it in the catalog.

type goldenOfficialSet struct {
	Endpoint string   `json:"endpoint"`
	Reads    []string `json:"reads"`
	Writes   []string `json:"writes"`
}

func sortedKeys(set map[string]bool) []string {
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// goldenOfficial lists, per integration, the official MCP endpoint and the
// reviewed read and write tool names.
func goldenOfficial() map[string]goldenOfficialSet {
	out := map[string]goldenOfficialSet{}
	for _, e := range integrations {
		if e.MCP != nil {
			out[e.ID] = goldenOfficialSet{Endpoint: e.MCP.Endpoint, Reads: sortedCopy(e.MCP.Reads), Writes: sortedCopy(e.MCP.Writes)}
		}
	}
	return out
}

// goldenReviewed returns the reviewed built-in tool definitions of an
// integration that has no official MCP list.
func goldenReviewed(id string) []Tool {
	return lookup(id).reviewed()
}

func goldenGoogleScopes(id string, writes bool) []string { return googleScopes(id, writes) }
