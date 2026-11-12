package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/connections"
)

// repoAccess is what searching for exactly one repository answers when the
// full answer is asked for: the account's permissions there.
func repoAccess(slug string, push bool) []byte {
	raw, _ := json.Marshal(map[string]any{"total_count": 1, "items": []any{map[string]any{
		"full_name": slug, "default_branch": "main",
		"permissions": map[string]any{"admin": false, "maintain": false, "push": push, "triage": push, "pull": true},
	}}})
	return raw
}

// accessFixture is a clone of acme/upstream whose main has two failed runs.
type accessFixture struct {
	collector *Collector
	store     *Store
	external  *fakeExternal
}

// accessAccount is a connected account and what searching the repository
// through it answers.
type accessAccount struct {
	id     string
	search []byte
}

func newAccessFixture(t *testing.T, connected ...accessAccount) *accessFixture {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "work", "upstream")
	gitRepo(t, repo, "https://github.com/acme/upstream.git")
	collector, store := newTestCollector(t, home)
	if _, ok := store.ProjectForCWD(repo, time.Now()); !ok {
		t.Fatal("no project")
	}
	now := time.Now().UTC()
	run := func(id int, conclusion string, ago time.Duration) map[string]any {
		return map[string]any{"id": id, "name": "Website", "display_title": "commit " + fmt.Sprint(id), "head_branch": "main",
			"status": "completed", "conclusion": conclusion, "run_attempt": 1, "updated_at": now.Add(-ago).Format(time.RFC3339)}
	}
	body := func(value any) []byte { raw, _ := json.Marshal(value); return raw }
	external := &fakeExternal{
		responses: map[string]fakeResponse{
			"actions_list acme/upstream":           {Status: 200, Body: body(map[string]any{"workflow_runs": []any{run(1, "failure", 2*time.Hour), run(2, "failure", time.Hour)}})},
			"list_issues acme/upstream":            {Status: 200, Body: []byte(`{"issues":[]}`)},
			"list_pull_requests acme/upstream":     {Status: 200, Body: []byte("[]")},
			"list_dependabot_alerts acme/upstream": {Status: 200, Body: []byte(`[]`)},
		},
		linear:    map[string]string{},
		accounts:  map[string][]connections.ReadAccount{"linear": nil},
		byAccount: map[string]fakeResponse{},
	}
	var accounts []connections.ReadAccount
	for _, account := range connected {
		external.byAccount[account.id+" search_repositories acme/upstream"] = fakeResponse{Status: 200, Body: account.search}
		accounts = append(accounts, connections.ReadAccount{ID: account.id, Name: account.id, Integration: "github", Enabled: true, Reads: allReads(githubReads...)})
	}
	external.accounts["github"] = accounts
	collector.external = external
	return &accessFixture{collector: collector, store: store, external: external}
}

func (f *accessFixture) collect() {
	f.collector.Collect(context.Background(), []string{SourceExternal}, true)
}

func (f *accessFixture) github(t *testing.T) *GitHub {
	t.Helper()
	project := f.store.Projects()[0]
	if project.GitHub == nil {
		t.Fatal("no GitHub link")
	}
	return project.GitHub
}

// recheck makes the next pass resolve the account and the access again, as
// the daily recheck does.
func (f *accessFixture) recheck() {
	_, _ = f.store.UpdateProject(f.store.Projects()[0].ID, func(p *Project) error { p.GitHub.CheckedAt = time.Time{}; return nil })
}

func (f *accessFixture) called(tool string) bool {
	for _, call := range f.external.calls {
		if call == tool+" acme/upstream" {
			return true
		}
	}
	return false
}

func TestReadOnlyRepositoryRaisesNoCIProblem(t *testing.T) {
	f := newAccessFixture(t, accessAccount{"first", repoAccess("acme/upstream", false)})
	f.collect()

	if problems := f.store.Problems(Filter{Kind: RuleCIRed}); len(problems) != 0 {
		t.Fatalf("somebody else's red main raised %+v", problems)
	}
	if failed := f.store.Signals(Filter{Kind: kindCIFailed}); len(failed) != 0 {
		t.Fatalf("somebody else's runs were stored: %+v", failed)
	}
	if f.called("actions_list") || f.called("list_dependabot_alerts") || f.called("list_code_scanning_alerts") {
		t.Fatalf("repository health was read: %v", f.external.calls)
	}
	link := f.github(t)
	if link.Access != AccessRead || link.Coverage != CoverageCollecting || link.Endpoints["actions"] != notResponsible {
		t.Fatalf("link = %+v", link)
	}
	if got := f.external.args["search_repositories"]["minimal_output"]; got != false {
		t.Fatalf("the repository search did not ask for the full answer: %v", f.external.args["search_repositories"])
	}
	// What the person's own pull requests and issues say is another signal and is still read.
	if !f.called("list_pull_requests") || !f.called("list_issues") {
		t.Fatalf("issues and pull requests were not read: %v", f.external.calls)
	}
}

func TestPushAccessRaisesCIProblem(t *testing.T) {
	f := newAccessFixture(t, accessAccount{"first", repoAccess("acme/upstream", true)})
	f.collect()

	problems := f.store.Problems(Filter{Kind: RuleCIRed})
	if len(problems) != 1 || !problems[0].Active || !problems[0].Severe || problems[0].Count != 2 {
		t.Fatalf("a red main the person can push to raised %+v", problems)
	}
	if link := f.github(t); link.Access != AccessWrite || link.Endpoints["actions"] != "ok" {
		t.Fatalf("link = %+v", link)
	}
}

func TestUnreadablePermissionStaysAsBefore(t *testing.T) {
	// An answer without permissions (the field is gone, or GitHub trimmed it) is
	// not evidence of somebody else's project: CI is read and raised as ever.
	f := newAccessFixture(t, accessAccount{"first", repoMeta("acme/upstream")})
	f.collect()
	if problems := f.store.Problems(Filter{Kind: RuleCIRed}); len(problems) != 1 || !problems[0].Active {
		t.Fatalf("problems = %+v", problems)
	}
	if link := f.github(t); link.Access != AccessUnknown {
		t.Fatalf("access = %q", link.Access)
	}
}

func TestAnyConnectedAccountThatPushesMakesTheRepositoryTheirs(t *testing.T) {
	// The person's own repository, first reached through an account that only
	// pulls from it, is still theirs and is read through the one that pushes.
	f := newAccessFixture(t,
		accessAccount{"first", repoAccess("acme/upstream", false)},
		accessAccount{"second", repoAccess("acme/upstream", true)})
	f.collect()
	link := f.github(t)
	if link.Access != AccessWrite || link.AccountID != "second" {
		t.Fatalf("link = %+v", link)
	}
	if problems := f.store.Problems(Filter{Kind: RuleCIRed}); len(problems) != 1 || !problems[0].Active {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestAccessLostClosesTheProblemOnTheNextPass(t *testing.T) {
	f := newAccessFixture(t, accessAccount{"first", repoAccess("acme/upstream", true)})
	f.collect()
	problems := f.store.Problems(Filter{Kind: RuleCIRed})
	if len(problems) != 1 || !problems[0].Active {
		t.Fatalf("problems = %+v", problems)
	}
	id := problems[0].ID

	// The same runs are stored; only the person's access to the repository
	// changes (or becomes known).
	f.external.byAccount["first search_repositories acme/upstream"] = fakeResponse{Status: 200, Body: repoAccess("acme/upstream", false)}
	f.recheck()
	f.collect()

	if problem := f.store.Problem(id); problem.ID != id || problem.Active {
		t.Fatalf("the problem stayed open: %+v", f.store.Problem(id))
	}
	if failed := f.store.Signals(Filter{Kind: kindCIFailed}); len(failed) != 2 {
		t.Fatalf("stored runs = %d", len(failed))
	}
}

func TestAnAccountThatCannotBeReachedKeepsTheEarlierAccess(t *testing.T) {
	f := newAccessFixture(t,
		accessAccount{"first", repoAccess("acme/upstream", false)},
		accessAccount{"second", repoAccess("acme/upstream", true)})
	f.collect()
	if f.github(t).Access != AccessWrite {
		t.Fatalf("link = %+v", f.github(t))
	}
	// The account that pushes is out of reach for one pass: the other only
	// reads, which is not an answer about the person.
	f.external.down = map[string]bool{"second": true}
	f.recheck()
	f.collect()
	if link := f.github(t); link.Access != AccessWrite {
		t.Fatalf("access flipped to %q", link.Access)
	}
	if problems := f.store.Problems(Filter{Kind: RuleCIRed}); len(problems) != 1 || !problems[0].Active {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestPermissionsNotYetResolvedAreResolvedAtOnce(t *testing.T) {
	// A link written before Access existed is not trusted for a day.
	f := newAccessFixture(t, accessAccount{"first", repoAccess("acme/upstream", false)})
	f.collect()
	_, _ = f.store.UpdateProject(f.store.Projects()[0].ID, func(p *Project) error { p.GitHub.Access = ""; return nil })
	f.external.calls = nil
	f.collect()
	if link := f.github(t); link.Access != AccessRead {
		t.Fatalf("access = %q", link.Access)
	}
	if !f.called("search_repositories") {
		t.Fatal("access was not resolved again")
	}
}
