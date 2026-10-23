package watch

import (
	"reflect"
	"testing"
)

// A project the store hands out is the caller's own: changing it, or what it
// points to, must not change the store's.
func TestCloneProjectSharesNothingWithTheOriginal(t *testing.T) {
	confirmed := &LinearTeam{AccountID: "a", Key: "ENG"}
	original := Project{
		ID:      "p",
		Remotes: []Remote{{Name: "origin", URL: "https://example.com/r.git"}},
		GitHub:  &GitHub{Owner: "o", Repo: "r", Endpoints: map[string]string{"issues": "ok"}},
		Linear:  &Linear{Suggested: []LinearTeam{{AccountID: "a", Key: "ENG"}}, Confirmed: confirmed},
	}
	clone := cloneProject(original)
	if !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone differs: %+v vs %+v", clone, original)
	}
	clone.Remotes[0].URL = "changed"
	clone.GitHub.Endpoints["issues"] = "changed"
	clone.GitHub.Owner = "changed"
	clone.Linear.Suggested[0].Key = "changed"
	clone.Linear.Confirmed.Key = "changed"
	if original.Remotes[0].URL != "https://example.com/r.git" || original.GitHub.Endpoints["issues"] != "ok" || original.GitHub.Owner != "o" ||
		original.Linear.Suggested[0].Key != "ENG" || confirmed.Key != "ENG" {
		t.Fatalf("the clone shares state with the original: %+v", original)
	}
	if empty := cloneProject(Project{ID: "q"}); empty.Remotes != nil || empty.GitHub != nil || empty.Linear != nil {
		t.Fatalf("an empty project grew fields: %+v", empty)
	}
}
