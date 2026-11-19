package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/work"
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
	for _, want := range []string{"<<<PERSON", "上传完图片输入框就没法工作了", "language the person writes their own messages in", "never a code name"} {
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
	f.service.requestChanged()
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
	f.service.requestChanged()
	f.service.progress.mu.Lock()
	_, pending = f.service.progress.pending[f.workID]
	f.service.progress.mu.Unlock()
	if pending {
		t.Fatal("a named Work was asked again")
	}
}

func TestAWorkTheJudgeTriedToNameWaitsForEvidence(t *testing.T) {
	f := newProgressJudgeFixture(t, answerShownBlank)
	f.service.requestChanged()
	f.clock = f.clock.Add(progressQuietGap + time.Second)
	if !f.judgeDue(t) {
		t.Fatal("not asked for a name")
	}
	f.clock = f.clock.Add(24 * time.Hour)
	f.service.requestChanged()
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

const (
	answerParked = `{"title":"Xcode 日志查看器不再卡住","line":"等明早 11:17 的清理跑完再验证，无需你操作","next":"清理跑完后收集数据","project":"Xcode Logs","measurable":true,"reason":"parked","reopened":[],"outcomes":[
		{"id":"fix","text":"Viewer fixed","weight":2,"state":"done"},
		{"id":"proof","text":"Proven after the retention run","weight":1,"state":"todo"}]}`
	answerClosed = `{"title":"Xcode 日志查看器不再卡住","line":"清理后查看器一直正常，已确认修好","next":"","project":"Xcode Logs","measurable":true,"reason":"closed","reopened":[],"outcomes":[
		{"id":"fix","text":"Viewer fixed","weight":2,"state":"done"},
		{"id":"proof","text":"Proven after the retention run","weight":1,"state":"done"}]}`
)

// parkedFixture is a Work with no Worker on it, as Brain parks one on a
// condition, judged by a fake model on a clock the test moves.
type parkedFixture struct {
	store   *Store
	service *Service
	model   *fakeProgressModel
	clock   time.Time
	id      string
}

func newParkedFixture(t *testing.T, answers ...string) *parkedFixture {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &parkedFixture{store: store, model: &fakeProgressModel{answers: answers}, clock: time.Now().UTC()}
	f.restart()
	item, _, err := store.EnsureWork(Work{ID: "w-xcode", Title: "XcodeLogsViewerHang", Objective: "Find why the Xcode Logs viewer hangs and fix it.", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	f.id = item.ID
	return f
}

// restart is a new daemon on the same store.
func (f *parkedFixture) restart() {
	f.service = NewService(f.store, nil, nil)
	f.service.SetProgressAsker(f.model.ask)
	f.service.now = func() time.Time { return f.clock }
	f.service.progress.loading = func(WorkTranscriptLink) (work.CodexConversation, error) { return work.CodexConversation{}, nil }
	f.service.seedProgressLastRun()
}

// scan reads Brain's dispositions, as the judge's ticker does, and runs every
// judgment that is due once the evidence has settled. It says how many ran.
func (f *parkedFixture) scan() int {
	f.service.requestChanged()
	f.clock = f.clock.Add(progressMinGap + time.Second)
	ran := 0
	for workID := f.service.nextProgressDue(); workID != ""; workID = f.service.nextProgressDue() {
		f.service.judgeProgress(context.Background(), workID)
		ran++
	}
	return ran
}

func (f *parkedFixture) work(t *testing.T) Work {
	t.Helper()
	item, err := f.store.Work(f.id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestBrainParkingAWorkIsEvidenceButARestartIsNot(t *testing.T) {
	f := newParkedFixture(t, answerParked)
	if ran := f.scan(); ran != 1 {
		t.Fatalf("a new Work was judged %d times, want 1 (named before its first report)", ran)
	}
	note := "11-20 03:17Z retention run; Trigger c57a7c00 pings the Worker after 04:40Z to collect repair numbers"
	waiting := WorkWaiting
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Status: &waiting, WaitFor: &note}); err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 1 {
		t.Fatalf("parking it was judged %d times, want 1", ran)
	}
	prompt := f.model.prompts[len(f.model.prompts)-1]
	for _, want := range []string{
		"Parked: nothing runs on it now",
		"Brain asks the person nothing.\n",
		"Brain's own note on what it waits for (Brain's shorthand, never words for the person): " + note,
		"The person's local time now: ",
		"begin the line by saying nothing is needed from the person",
		"never UTC",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the judge was not told %q", want)
		}
	}
	if shown := f.work(t).Shown; shown == nil || shown.Status != "waiting" || shown.Line != "等明早 11:17 的清理跑完再验证，无需你操作" {
		t.Fatalf("shown = %#v", shown)
	}
	// A restart replays nothing new: no judgment.
	f.restart()
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a restart judged %d times without new evidence", ran)
	}
	f.restart()
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a second restart judged %d times without new evidence", ran)
	}
	// Brain rewording the wait is evidence again.
	reworded := "the retention run tomorrow morning"
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Status: &waiting, WaitFor: &reworded}); err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 1 {
		t.Fatalf("a new wait note was judged %d times, want 1", ran)
	}
	if len(f.model.prompts) != 3 {
		t.Fatalf("%d model calls, want 3", len(f.model.prompts))
	}
}

func TestBrainsQuestionIsWhatAsksThePerson(t *testing.T) {
	f := newParkedFixture(t, answerParked)
	question := "Release Looper now, or wait for the review?"
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Question: &question, Choices: &[]string{"Release now", "Wait"}}); err != nil {
		t.Fatal(err)
	}
	f.scan()
	prompt := f.model.prompts[len(f.model.prompts)-1]
	if !strings.Contains(prompt, "Brain asks the person: "+question) || strings.Contains(prompt, "Brain asks the person nothing.\n") {
		t.Fatalf("the judge was not told Brain's question:\n%s", prompt)
	}
}

func TestAClosedWorkIsReadOnceForHowItEnded(t *testing.T) {
	f := newParkedFixture(t, answerParked, answerClosed)
	f.scan()
	before := f.work(t)
	done := WorkDone
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 1 {
		t.Fatalf("closing it was judged %d times, want 1", ran)
	}
	after := f.work(t)
	if after.Shown == nil || after.Shown.Status != "done" || after.Shown.Line != "清理后查看器一直正常，已确认修好" {
		t.Fatalf("shown = %#v", after.Shown)
	}
	if after.Progress.Percent != before.Progress.Percent || after.Progress.Judgments != before.Progress.Judgments {
		t.Fatalf("a closed Work's checklist moved: %d%% (%d) -> %d%% (%d)", before.Progress.Percent, before.Progress.Judgments, after.Progress.Percent, after.Progress.Judgments)
	}
	if !strings.Contains(f.model.prompts[1], "Closed by Brain") {
		t.Fatal("the judge was not told the Work is closed")
	}
	f.restart()
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a closed Work was judged %d more times", ran)
	}
	// Long closed: never read again.
	f.clock = f.clock.Add(progressClosedWindow + time.Hour)
	if ran := f.scan(); ran != 0 {
		t.Fatalf("an old closed Work was judged %d times", ran)
	}
}

func TestThePersonsLanguageIsReadFromTheirMessages(t *testing.T) {
	say := func(texts ...string) []personMessage {
		var out []personMessage
		for _, text := range texts {
			out = append(out, personMessage{Text: text})
		}
		return out
	}
	cases := []struct {
		messages []personMessage
		want     string
	}{
		{say("Re: LooperRequestBoard (work 2cc9c9eb-9b37-42d1) 这个也看不懂。", "seek alpha 这个直接用网页去抓，效率会更高吗", "set up a Router session for the user 这个的作用是啥？", "ok"), "Chinese"},
		{say("ship it", "what about the docs?", "继续"), ""},
		{say("このバグを直して", "ありがとう"), "Japanese"},
		{say("이거 고쳐줘", "고마워"), "Korean"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := personLanguage(c.messages); got != c.want {
			t.Errorf("%v: got %q, want %q", c.messages, got, c.want)
		}
	}
	if !strings.Contains(buildShownContext(Work{}, say("这个看不懂", "你需要我干啥"), nil, time.Now(), personLanguage(say("这个看不懂", "你需要我干啥"))), "The person writes in Chinese: write title, line and next in Chinese.") {
		t.Fatal("the judge is not told the person's language")
	}
}

func TestASweptClosedWorkIsNotReadAgain(t *testing.T) {
	f := newParkedFixture(t, answerClosed)
	// Closed without ever having a row: a sweep of old Work moving its updated_at is not a reason to read it.
	done := WorkDone
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Status: &done}); err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a closed Work with no row was judged %d times", ran)
	}
	// Its row written over a day ago: not in the list any more.
	err := f.store.updateWorkProgressLocked(f.id, func(item *Work) (*WorkEvent, bool) {
		item.Shown = &WorkShown{Title: "旧事", Line: "进行中", Status: "active", JudgedAt: f.clock.Add(-progressClosedWindow - time.Hour)}
		return nil, false
	})
	if err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a Work closed with a day-old row was judged %d times", ran)
	}
}

func TestEvidenceNotYetJudgedSurvivesARestart(t *testing.T) {
	f := newParkedFixture(t, answerParked)
	f.service.requestChanged() // seen, queued, not judged
	f.restart()                // the queue is gone
	if ran := f.scan(); ran != 1 {
		t.Fatalf("evidence seen before a restart was judged %d times after it, want 1", ran)
	}
	f.restart()
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a restart with everything judged judged %d times", ran)
	}
}

func TestTheLanguageIsThePersonsNotTheWorks(t *testing.T) {
	f := newParkedFixture(t, answerParked)
	// The person's newest messages are hours after the Work began, outside its own window.
	for i, text := range []string{"这个也看不懂。", "你看好多任务我都看不懂", "Dev 的代码最新的要往 main 合一下。"} {
		if _, err := f.store.AppendTimelineItem(TimelineItem{ID: fmt.Sprintf("u%d", i), ThreadID: "t", SessionID: "s", Role: "user", Kind: "user_message", Body: text, CreatedAt: f.clock.Add(10 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	f.clock = f.clock.Add(11 * time.Hour)
	f.scan()
	if prompt := f.model.prompts[len(f.model.prompts)-1]; !strings.Contains(prompt, "The person writes in Chinese: write title, line and next in Chinese.") {
		t.Fatalf("the judge was not told the person's language:\n%s", prompt[strings.Index(prompt, "<<<PERSON"):])
	}
}

func TestNoDailyCapStopsAJudgmentOfNewEvidence(t *testing.T) {
	f := newParkedFixture(t, answerParked)
	f.scan()
	// 80 judgments of this Work today and 700 in all (the old caps were 80 and 600).
	other, _, err := f.store.EnsureWork(Work{ID: "w-other", Title: "Another", Objective: "x", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	fill := func(id string, n int) {
		err := f.store.updateWorkProgressLocked(id, func(item *Work) (*WorkEvent, bool) {
			progress := WorkProgress{}
			if item.Progress != nil {
				progress = *cloneWorkProgress(item.Progress)
			}
			for i := 0; i < n; i++ {
				progress.Runs = append(progress.Runs, ProgressRun{At: f.clock.Add(-time.Duration(n-i) * time.Minute), USD: 0.002})
			}
			item.Progress = &progress
			return nil, false
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	fill(f.id, 80)
	fill(other.ID, 620)
	f.restart()
	f.scan() // the other Work's first naming
	before := len(f.model.prompts)
	note := "the release window tonight"
	waiting := WorkWaiting
	if _, err := f.store.UpdateWork(f.id, WorkUpdate{Status: &waiting, WaitFor: &note}); err != nil {
		t.Fatal(err)
	}
	if ran := f.scan(); ran != 1 || len(f.model.prompts) != before+1 {
		t.Fatalf("new evidence on the 81st judgment of the day (700 in all) was judged %d times, want 1", ran)
	}
	day, err := f.service.Context()
	if err != nil {
		t.Fatal(err)
	}
	if day.ProgressJudge.Judgments < 702 || day.ProgressJudge.USD < 1.4 || day.ProgressJudge.Works != 2 {
		t.Fatalf("Brain context shows %#v", day.ProgressJudge)
	}
	// And still nothing without new evidence after a restart.
	f.restart()
	if ran := f.scan(); ran != 0 {
		t.Fatalf("a restart judged %d times without new evidence", ran)
func TestAWordOrTwoIsNotAReading(t *testing.T) {
	prior := &WorkShown{Title: "Xcode Gemma 工具调用错乱根因", Line: "错乱出在 Bedrock 端，报告待出。", Next: "写出根因报告"}
	got, changed := applyShownVerdict(prior, shownVerdict{Title: "根因", Line: "矫正", Next: "ok"}, time.Now())
	if changed || got.Title != prior.Title || got.Line != prior.Line || got.Next != prior.Next {
		t.Fatalf("a degenerate answer replaced the row: %#v", got)
	}
	got, _ = applyShownVerdict(prior, shownVerdict{Title: "0G 旧版删除", Line: "旧版已删，测试站已过"}, time.Now())
	if got.Title != "0G 旧版删除" || got.Line != "旧版已删，测试站已过" {
		t.Fatalf("a short real answer was dropped: %#v", got)
	}
}
