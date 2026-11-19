package brain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	answerShownZh = `{"title":"0G 输入框上传图片后可用","line":"已修好并上线，Brain 在核对","next":"Brain 读结果后关闭","project":"0G App","measurable":true,"reason":"deployed","reopened":[],"outcomes":[
		{"id":"fix","text":"Fix merged on main","weight":2,"state":"done"},
		{"id":"release","text":"Released","weight":1,"state":"doing"}]}`
	answerShownLater = `{"title":"上传图片后输入框恢复","line":"移动端也验证通过","next":"关闭","project":"0G","measurable":true,"reason":"verified","reopened":[],"outcomes":[
		{"id":"fix","text":"Fix merged on main","weight":2,"state":"done"},
		{"id":"release","text":"Released","weight":1,"state":"done"}]}`
	answerShownBlank = `{"title":"","line":"  ","next":"","project":"","measurable":true,"reason":"no change","reopened":[],"outcomes":[
		{"id":"fix","text":"Fix merged on main","weight":2,"state":"done"},
		{"id":"release","text":"Released","weight":1,"state":"doing"}]}`
)

func (f *progressJudgeFixture) judgeNow(t *testing.T, summary string) {
	t.Helper()
	f.reportVia(t, "running", "working", summary)
	f.clock = f.clock.Add(progressMinGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatalf("not judged after %q", summary)
	}
}

func TestTheJudgeNamesTheWorkInItsSameCall(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh)
	if _, err := f.store.AppendTimelineItem(TimelineItem{ID: "u1", ThreadID: "t", SessionID: "s", Role: "user", Kind: "user_message", Body: "发现个严重的问题，上传完图片输入框就没法工作了", CreatedAt: f.clock.Add(-30 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	f.reportVia(t, "running", "working", "fix deployed")
	f.clock = f.clock.Add(progressMinGap + time.Second)
	before := f.work(t)
	if !f.judgeDue(t) {
		t.Fatal("not judged")
	}
	after := f.work(t)
	shown := after.Shown
	if shown == nil || shown.Title != "0G 输入框上传图片后可用" || shown.Line != "已修好并上线，Brain 在核对" || shown.Project != "0G App" || shown.Next == "" {
		t.Fatalf("shown = %#v", shown)
	}
	if shown.ChangedAt.IsZero() || shown.JudgedAt.IsZero() {
		t.Fatalf("times not set: %#v", shown)
	}
	if after.Revision != before.Revision {
		t.Fatalf("revision moved %d -> %d", before.Revision, after.Revision)
	}
	if len(f.model.prompts) != 1 {
		t.Fatalf("%d model calls, want 1 (one call judges progress and the name)", len(f.model.prompts))
	}
	prompt := f.model.prompts[0]
	for _, want := range []string{"<<<PERSON", "上传完图片输入框就没法工作了", "language of the person's own messages", "never a code name"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("judge prompt lacks %q", want)
		}
	}
	inventory, err := f.store.ProjectWorkInventory([]WorkerRef{{ID: f.sessionID, Delegated: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range inventory.Current {
		if current.ID == f.workID && (current.Shown == nil || current.Shown.Title != shown.Title || current.Shown.ByPerson != nil) {
			t.Fatalf("current_work shown = %#v", current.Shown)
		}
	}
}

func TestThePersonsTitleAndProjectStick(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh, answerShownLater)
	f.judgeNow(t, "fix deployed")
	if _, err := f.store.SetWorkShown(f.workID, ShownTitle, "  Composer after upload  ", f.clock); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SetWorkShown(f.workID, ShownProject, "0G App", f.clock); err != nil {
		t.Fatal(err)
	}
	f.judgeNow(t, "verified on mobile")
	shown := f.work(t).Shown
	if shown.Title != "Composer after upload" || shown.Project != "0G App" {
		t.Fatalf("the judge overwrote the person's fields: %#v", shown)
	}
	if shown.Line != "移动端也验证通过" {
		t.Fatalf("the line stopped following the judge: %#v", shown)
	}
	if !strings.Contains(f.model.prompts[1], "The person set: project, title") {
		t.Fatalf("the judge was not told what the person set:\n%s", f.model.prompts[1])
	}
	// Clearing hands the title back to the judge.
	cleared, err := f.store.SetWorkShown(f.workID, ShownTitle, "", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.ByPerson) != 1 || cleared.ByPerson[0] != "project" {
		t.Fatalf("by_person = %v", cleared.ByPerson)
	}
	if _, err := f.store.SetWorkShown(f.workID, "line", "x", f.clock); !errors.Is(err, ErrShownField) {
		t.Fatalf("set line err = %v", err)
	}
}

func TestAJudgeThatSaysNothingOrFailsKeepsTheLastGoodText(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh, answerShownBlank)
	f.judgeNow(t, "fix deployed")
	first := *f.work(t).Shown
	f.judgeNow(t, "nothing new")
	if got := f.work(t).Shown; got.Title != first.Title || got.Line != first.Line || got.Project != first.Project || !got.ChangedAt.Equal(first.ChangedAt) {
		t.Fatalf("a blank answer changed the row: %#v", got)
	}
	f.model.err = errors.New("401 authentication_error")
	f.reportVia(t, "running", "working", "again")
	f.clock = f.clock.Add(progressMinGap + time.Second)
	f.judgeDue(t)
	if got := f.work(t).Shown; got.Title != first.Title || got.Line != first.Line {
		t.Fatalf("a failed judgment changed the row: %#v", got)
	}
}

func TestAnOpenWorkWithNoNameIsAskedForOneOnce(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownZh)
	f.service.requestUnnamed()
	f.service.progress.mu.Lock()
	_, pending := f.service.progress.pending[f.workID]
	f.service.progress.mu.Unlock()
	if !pending {
		t.Fatal("an unnamed open Work was not asked for a name")
	}
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) || f.work(t).Shown == nil {
		t.Fatal("the first judgment did not name the Work")
	}
	f.service.requestUnnamed()
	f.service.progress.mu.Lock()
	_, pending = f.service.progress.pending[f.workID]
	f.service.progress.mu.Unlock()
	if pending {
		t.Fatal("a named Work was asked again")
	}
}

func TestAWorkTheJudgeTriedToNameWaitsForEvidence(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownBlank)
	f.service.requestUnnamed()
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("not asked for a name")
	}
	f.clock = f.clock.Add(24 * time.Hour)
	f.service.requestUnnamed()
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if f.judgeDue(t) {
		t.Fatal("asked again with no new evidence")
	}
}

func TestOnlyWhatThePersonTypedReachesTheJudge(t *testing.T) {
	cases := []struct {
		body string
		want string
		ok   bool
	}{
		{`{"role":"user","kind":"user_message","body":"0g 的 dev 和 main 弄好了吗","created_at":"2026-11-19T03:41:14Z"}`, "0g 的 dev 和 main 弄好了吗", true},
		{`{"role":"user","body":"看看这个 <mewla_attachments>{\"files\":[]}</mewla_attachments>","created_at":"2026-11-19T03:41:14Z"}`, "看看这个", true},
		{`{"role":"user","body":"<mewla_computer_message from=\"mac\">done</mewla_computer_message>","created_at":"2026-11-19T03:41:14Z"}`, "", false},
		{`{"role":"user","body":"<mewla_watch kind=\"triage\"></mewla_watch>","created_at":"2026-11-19T03:41:14Z"}`, "", false},
		{`{"role":"assistant","kind":"assistant_message","body":"I fixed it","created_at":"2026-11-19T03:41:14Z"}`, "", false},
		{`{"role":"user","body":"no time"}`, "", false},
	}
	for _, c := range cases {
		got, ok := personMessageOf([]byte(c.body))
		if ok != c.ok || got.Text != c.want {
			t.Errorf("%s: got %q %v", c.body, got.Text, ok)
		}
	}
}

func TestTheJudgeReadsMessagesNearTheWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 19, 6, 0, 0, 0, time.UTC)
	add := func(id string, at time.Time, role, body string) {
		if _, err := store.AppendTimelineItem(TimelineItem{ID: id, ThreadID: "t", SessionID: "s", Role: role, Body: body, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	add("old", start.Add(-5*time.Hour), "user", "an ask from the morning")
	add("ask", start.Add(-20*time.Minute), "user", "the ask")
	add("brain", start.Add(-19*time.Minute), "assistant", "on it")
	add("later", start.Add(2*time.Hour), "user", "how is it going")
	got := store.personMessagesFor(Work{CreatedAt: start}, start.Add(3*time.Hour))
	if len(got) != 2 || got[0].Text != "the ask" || got[1].Text != "how is it going" {
		t.Fatalf("messages = %#v", got)
	}
}

func TestProjectsInUseKeepOneNamePerProject(t *testing.T) {
	items := []Work{
		{ID: "a", Status: WorkRunning, Shown: &WorkShown{Project: "0G"}},
		{ID: "b", Status: WorkRunning, Shown: &WorkShown{Project: "0G"}},
		{ID: "c", Status: WorkWaiting, Shown: &WorkShown{Project: "Mewla"}},
		{ID: "d", Status: WorkDone, Shown: &WorkShown{Project: "Old"}},
		{ID: "e", Status: WorkRunning},
	}
	if got := strings.Join(projectsInUse(items, "c"), ","); got != "0G" {
		t.Fatalf("projects = %s", got)
	}
	if got := strings.Join(projectsInUse(items, ""), ","); got != "0G,Mewla" {
		t.Fatalf("projects = %s", got)
	}
}

func TestClipShownMakesOneLine(t *testing.T) {
	if got := clipShown("a\n  b\tc", 10); got != "a b c" {
		t.Fatalf("got %q", got)
	}
	if got := clipShown(strings.Repeat("长", 30), 10); got != strings.Repeat("长", 9)+"…" {
		t.Fatalf("got %q", got)
	}
}

func TestOneProjectKeepsOneName(t *testing.T) {
	inUse := []string{"0G App", "Mewla", "data-platform"}
	for in, want := range map[string]string{"0G app": "0G App", "0G": "0G App", "mewla": "Mewla", "Data Platform": "data-platform", "Looper": "Looper", "": ""} {
		if got := sameProject(in, inUse); got != want {
			t.Errorf("sameProject(%q) = %q, want %q", in, got, want)
		}
	}
}
