package watch

import (
	"strings"
	"testing"
	"time"
)

func TestLanguageOfLocale(t *testing.T) {
	for locale, want := range map[string]string{
		"zh-Hans-CN": "zh", "zh_TW": "zh", "en-US": "en", "pt_BR": "pt", "ja": "ja", "": "", "nonsense!!": "", "und": "",
	} {
		if got := languageOf(locale); got != want {
			t.Errorf("languageOf(%q) = %q, want %q", locale, got, want)
		}
	}
	if got := languageName("zh"); got != "Chinese" {
		t.Errorf("languageName(zh) = %q", got)
	}
	if got := languageName("ja"); got != "Japanese" {
		t.Errorf("languageName(ja) = %q", got)
	}
	if phrasesFor("ja").briefTitle != "Morning brief" || phrasesFor("zh").urgentTitle == phrasesFor("en").urgentTitle {
		t.Fatal("phrases fall back to English only for languages without a translation")
	}
}

func TestSetDeviceKeepsTheLocaleAndIgnoresJunk(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 10, 22, 4, 0, 0, 0, time.UTC))
	f.n.SetDevice(Device{Locale: "zh-Hans-CN"})
	if f.n.state.Device.TimeZone != "Asia/Shanghai" || f.n.state.Device.Locale != "zh-Hans-CN" {
		t.Fatalf("device = %+v: a locale alone must not erase the time zone", f.n.state.Device)
	}
	f.n.SetDevice(Device{TimeZone: "Not/AZone", Locale: "%%%"})
	if f.n.state.Device.TimeZone != "Asia/Shanghai" || f.n.state.Device.Locale != "zh-Hans-CN" {
		t.Fatalf("junk replaced the device: %+v", f.n.state.Device)
	}
	reopened, err := newNotifier(f.store, f.brain, nil, nil)
	if err != nil || reopened.deviceLanguage() != "zh" {
		t.Fatalf("locale not persisted: %v %q", err, reopened.deviceLanguage())
	}
}

func TestLanguageFollowsTheAppThenTheMessagesThenEnglish(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 10, 22, 4, 0, 0, 0, time.UTC))
	c := &Collector{store: f.store, judge: newJudge(f.store, t.TempDir()), notify: f.n}
	if got := c.language(); got != "en" {
		t.Fatalf("nothing known: %q", got)
	}
	for i := 0; i < 12; i++ {
		if _, err := f.store.Append([]Signal{{ID: "s" + string(rune('a'+i)), Source: SourceHuman, Kind: kindUserMessage, Excerpt: "做好没", Refs: map[string]string{"author": "user", "session_role": "direct"}, Time: f.clock}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.language(); got != "zh" {
		t.Fatalf("messages in Chinese, no locale: %q", got)
	}
	f.n.SetDevice(Device{Locale: "ja-JP"})
	if got := c.language(); got != "ja" {
		t.Fatalf("the app says Japanese: %q", got)
	}
	c.judge.language = c.language
	if prompt := c.judge.playbook(); !strings.Contains(prompt, "The person reads Japanese. Write every title, cost and reason in Japanese.") {
		t.Fatalf("playbook does not follow the language:\n%s", prompt[len(prompt)-300:])
	}
}

func TestBriefTitleAndUrgentPushFollowTheAppsLanguage(t *testing.T) {
	f := newNotifyFixture(t, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
	cal := &fakeCalendar{}
	f.n.calendar = cal
	// The collector's own language function, as in the daemon: tick holds the
	// notifier's lock while it asks, so asking must not take that lock.
	f.n.language = (&Collector{store: f.store, judge: newJudge(f.store, t.TempDir()), notify: f.n}).language
	f.n.SetDevice(Device{Locale: "zh-Hans-CN"})
	ticked := make(chan struct{})
	go func() { f.n.tick(); close(ticked) }()
	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("tick deadlocked asking for the language")
	}
	if len(cal.items) != 1 || cal.items[0].Title != "早报" || !strings.Contains(cal.items[0].Notes, "早报") {
		t.Fatalf("items = %+v", cal.items)
	}
	f.problem("ci", "CI red", 90, func(p *Problem) { p.Rule = RuleCIRed; p.Severe = true })
	f.n.prevUrgent = map[string]bool{}
	f.n.tick()
	f.problem("urgent", "Costly", 95)
	f.n.tick()
	var pushed string
	for _, p := range f.pushes {
		pushed += p + "\n"
	}
	if !strings.Contains(pushed, "现在需要你") || strings.Contains(pushed, "Needs you now") {
		t.Fatalf("pushes = %v", f.pushes)
	}
}
