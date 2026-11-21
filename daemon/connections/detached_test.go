package connections

import (
	"sync"
	"testing"
)

// A record copied to be used after m.mu is released shares nothing with the
// live one: the manager keeps writing Grants and Updates under its lock while
// the copy is read without it.
func TestDetachedRecordSharesNothingWithTheLiveOne(t *testing.T) {
	live := &record{
		Account: Account{ID: "a", Integration: "notion", Tools: []Tool{{Name: "search"}}},
		Spec:    []byte(`{"a":1}`),
		CLI:     &cloudAccount{Profile: "p", Configurations: []string{"one"}},
		OAuth:   &oauthAccount{ClientID: "c"},
		Grants:  map[string]string{"search": "x"},
		Updates: map[string]string{"search": "y"},
		Aliases: []string{"old"},
	}
	copied := live.detached()

	live.Grants["write"] = "z"
	delete(live.Updates, "search")
	live.Spec[2] = 'b'
	live.CLI.Profile = "other"
	live.CLI.Configurations[0] = "two"
	live.OAuth.ClientID = "d"
	live.Aliases[0] = "new"
	live.Account.Tools[0].Name = "changed"

	if len(copied.Grants) != 1 || copied.Updates["search"] != "y" || string(copied.Spec) != `{"a":1}` ||
		copied.CLI.Profile != "p" || copied.CLI.Configurations[0] != "one" || copied.OAuth.ClientID != "c" ||
		copied.Aliases[0] != "old" || copied.Account.Tools[0].Name != "search" {
		t.Fatalf("the copy followed the live record: %+v", copied)
	}
}

// Run under -race: the manager changes a record's permissions under its lock
// while a call that started from a copy reads the copy.
func TestDetachedRecordIsReadableWhileTheLiveOneChanges(t *testing.T) {
	m := &Manager{records: map[string]*record{}}
	live := &record{Account: Account{ID: "a", Integration: "notion", Tools: []Tool{{Name: "search"}}}, Grants: map[string]string{"search": "x"}, Updates: map[string]string{"search": "y"}}
	m.records["a"] = live
	m.mu.Lock()
	copied := live.detached()
	m.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 2000 {
			_ = len(copied.Grants) + len(copied.Updates)
		}
	}()
	for i := range 2000 {
		m.edit(func() {
			live.Grants["w"] = "z"
			delete(live.Grants, "w")
			live.Updates["w"] = "q"
			delete(live.Updates, "w")
			_ = i
		})
	}
	wg.Wait()
}
