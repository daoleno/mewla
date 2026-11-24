package gc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func noReads(t *testing.T) {
	t.Helper()
	saved := readsRecorded
	readsRecorded = func(string) bool { return false }
	t.Cleanup(func() { readsRecorded = saved })
}

func writeTree(t *testing.T, base string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func byBase(rows []Review) map[string]Review {
	out := map[string]Review{}
	for _, r := range rows {
		out[filepath.Base(r.Path)] = r
	}
	return out
}

// Where reads leave no trace, an old unmarked item holding files, new or
// from before the rule, goes to review and nothing is taken: not by mtime,
// not by guessing.
func TestWhereReadsLeaveNoTraceAnOldUnmarkedItemGoesToReviewNotToTheTrash(t *testing.T) {
	noReads(t)
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	writeTree(t, tdir, map[string]string{
		"before-cache/CACHEDIR.TAG": "Signature: 8a477f597d28d172789f06886806bc55",
		"before-cache/a.o":          "xxxx", "before-cache/b.o": "xx",
		"before-empty/sub/.keep": "",
	})
	if err := os.Remove(filepath.Join(tdir, "before-empty", "sub", ".keep")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	age(t, tdir, 200*time.Hour)
	// The first sweep lists what is there. The old item is put to review at once.
	moved, unproven, err := SweepUnmarked(root, Usage{}, now)
	if err != nil || len(moved) != 0 {
		t.Fatalf("first sweep = %v %v", moved, err)
	}
	if len(unproven) != 1 || filepath.Base(unproven[0].Path) != "before-cache" || !unproven[0].PreRule {
		t.Fatalf("the first sweep found %+v, want the one old item holding files, from before the rule", unproven)
	}
	writeTree(t, tdir, map[string]string{
		"target/debug/x.o": "12345678", "target/debug/y.o": "1", "target/debug/z.d": "1", "target/.git/HEAD": "ref",
		"fresh/f": "new",
		"app":     "\x7fELF....",
	})
	age(t, filepath.Join(tdir, "target"), 72*time.Hour)
	age(t, filepath.Join(tdir, "app"), 72*time.Hour)
	moved, unproven, err = SweepUnmarked(root, Usage{}, now)
	if err != nil || len(moved) != 0 {
		t.Fatalf("sweep moved %v (%v): where reads leave no trace nothing that holds a file is taken", moved, err)
	}
	SyncReviews(root, unproven, now)
	open, err := OpenReviews(root)
	if err != nil {
		t.Fatal(err)
	}
	rows := byBase(open)
	if len(rows) != 3 || rows["before-cache"].Path == "" || rows["target"].Path == "" || rows["app"].Path == "" {
		t.Fatalf("open reviews = %+v, want before-cache, target, app (fresh was written now, before-empty holds no file)", open)
	}
	if r := rows["before-cache"]; !r.PreRule || r.Looks != "a cache" || r.Bytes != int64(len("Signature: 8a477f597d28d172789f06886806bc55")+6) {
		t.Fatalf("before-cache = %+v", r)
	}
	if r := rows["target"]; r.PreRule || r.Looks != "a repository" || r.Bytes != int64(8+1+1+3) || r.FirstSeen.IsZero() {
		t.Fatalf("target = %+v", r)
	}
	if r := rows["app"]; r.Looks != "a binary" {
		t.Fatalf("app = %+v", r)
	}
	if open[0].Bytes < open[1].Bytes {
		t.Fatalf("open reviews are not the biggest first: %+v", open)
	}
	// Plain files say their commonest extensions.
	writeTree(t, tdir, map[string]string{"logs/a.log": "a", "logs/b.log": "b", "logs/c.txt": "c"})
	age(t, filepath.Join(tdir, "logs"), 72*time.Hour)
	_, unproven, _ = SweepUnmarked(root, Usage{}, now)
	for _, u := range unproven {
		if filepath.Base(u.Path) == "logs" && u.Looks != ".log×2 .txt×1" {
			t.Fatalf("logs looks %q", u.Looks)
		}
	}
	// A second pass with the same items leaves one row each, still unanswered.
	SyncReviews(root, unproven, now.Add(time.Hour))
	if open, _ = OpenReviews(root); len(open) != 4 {
		t.Fatalf("open reviews after a second pass = %d, want 4", len(open))
	}
	// Nothing was moved or deleted.
	if items, _ := List(root); len(items) != 0 {
		t.Fatalf("the trash holds %+v", items)
	}
}

// An item already in review is not walked again while its top is unchanged.
func TestAnItemAlreadyInReviewIsNotWalkedAgain(t *testing.T) {
	noReads(t)
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	writeTree(t, tdir, map[string]string{"old/deep/file.bin": "12345"})
	age(t, tdir, 200*time.Hour)
	now := time.Now()
	_, unproven, err := SweepUnmarked(root, Usage{}, now)
	if err != nil || len(unproven) != 1 {
		t.Fatalf("first sweep = %+v %v", unproven, err)
	}
	SyncReviews(root, unproven, now)
	// A nested file grows without the top's mtime moving: a walk would see it.
	big := filepath.Join(tdir, "old", "deep", "file.bin")
	if err := os.WriteFile(big, make([]byte, 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	age(t, big, 100*time.Hour)
	_, unproven, _ = SweepUnmarked(root, Usage{}, now.Add(time.Hour))
	if len(unproven) != 1 || unproven[0].Bytes != 5 {
		t.Fatalf("second sweep = %+v: an item in review is read from its row, not walked", unproven)
	}
	// Its top changing takes it out.
	if err := os.Chtimes(filepath.Join(tdir, "old"), now, now); err != nil {
		t.Fatal(err)
	}
	_, unproven, _ = SweepUnmarked(root, Usage{}, now.Add(time.Hour))
	if len(unproven) != 0 {
		t.Fatalf("a changed item is still in review: %+v", unproven)
	}
	SyncReviews(root, unproven, now.Add(time.Hour))
	if open, _ := OpenReviews(root); len(open) != 0 {
		t.Fatalf("open = %+v", open)
	}
	if row, ok := ReviewOf(root, filepath.Join(tdir, "old")); !ok || row.Answer != AnswerUsed {
		t.Fatalf("row = %+v %v, want answered used", row, ok)
	}
}

// Brain answers with the verbs that exist; the review table follows from what
// they leave behind. Where reads are recorded nothing goes to review.
func TestBrainsKeepAndReleaseAnswerAReviewAndNothingElseDoes(t *testing.T) {
	noReads(t)
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	now := time.Now()
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SweepUnmarked(root, Usage{}, now); err != nil { // an empty t/: the snapshot
		t.Fatal(err)
	}
	for _, name := range []string{"keepme", "dropme", "vanish", "touch", "idle"} {
		writeTree(t, tdir, map[string]string{name + "/data": "payload"})
		age(t, filepath.Join(tdir, name), 72*time.Hour)
	}
	_, unproven, _ := SweepUnmarked(root, Usage{}, now)
	if len(unproven) != 5 {
		t.Fatalf("unproven = %+v", unproven)
	}
	SyncReviews(root, unproven, now)
	later := now.Add(time.Hour)
	if _, err := AddKeep(root, filepath.Join(tdir, "keepme"), "brain", later.Add(48*time.Hour), "needed for the 12-01 run", later); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(root, KindKept, filepath.Join(tdir, "dropme"), "brain", "released by Brain", later); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(tdir, "vanish")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tdir, "touch", "data"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, unproven, err := SweepUnmarked(root, Usage{}, later)
	if err != nil || len(moved) != 0 {
		t.Fatalf("sweep = %v %v", moved, err)
	}
	SyncReviews(root, unproven, later)
	rows, _ := listReviews(root, false)
	got := map[string]string{}
	for _, r := range rows {
		got[filepath.Base(r.Path)] = r.Answer
	}
	want := map[string]string{"keepme": AnswerKeep, "dropme": AnswerRelease, "vanish": AnswerGone, "touch": AnswerUsed, "idle": ""}
	for name, answer := range want {
		if got[name] != answer {
			t.Fatalf("answers = %v, want %v", got, want)
		}
	}
	open, _ := OpenReviews(root)
	if len(open) != 1 || filepath.Base(open[0].Path) != "idle" {
		t.Fatalf("open = %+v, want idle only", open)
	}
	events, _ := Events(root, now.Add(-time.Hour))
	answered := 0
	for _, e := range events {
		if e.Action == ActionReviewed {
			answered++
		}
	}
	if answered != 4 {
		t.Fatalf("%d review answers in the ledger, want 4", answered)
	}
	// A kept item is not put to review at all, even from before the rule.
	writeTree(t, tdir, map[string]string{"later/data": "x"})
	age(t, filepath.Join(tdir, "later"), 72*time.Hour)
	if _, err := AddKeep(root, filepath.Join(tdir, "later"), "brain", later.Add(48*time.Hour), "kept first", later); err != nil {
		t.Fatal(err)
	}
	_, unproven, _ = SweepUnmarked(root, Usage{}, later)
	for _, u := range unproven {
		if filepath.Base(u.Path) == "later" {
			t.Fatalf("a kept item was put to review")
		}
	}
}

func TestWhereReadsAreRecordedNothingGoesToReview(t *testing.T) {
	saved := readsRecorded
	readsRecorded = func(string) bool { return true }
	t.Cleanup(func() { readsRecorded = saved })
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	writeTree(t, tdir, map[string]string{"before/data": "x"})
	age(t, tdir, 200*time.Hour)
	now := time.Now()
	if _, unproven, err := SweepUnmarked(root, Usage{}, now); err != nil || len(unproven) != 0 {
		t.Fatalf("first sweep = %+v %v", unproven, err)
	}
	writeTree(t, tdir, map[string]string{"after/data": "x"})
	age(t, filepath.Join(tdir, "after"), 72*time.Hour)
	moved, unproven, _ := SweepUnmarked(root, Usage{}, now)
	if len(unproven) != 0 || len(moved) != 1 {
		t.Fatalf("moved %v unproven %+v: on a file system that records reads the old rule stands", moved, unproven)
	}
}

// Brain is asked about new items, not about the same set twice, and at most
// once a day.
func TestBrainIsAskedAboutNewItemsOnceADayAndNeverAboutTheSameSetTwice(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	item := func(name string) Unproven {
		return Unproven{Path: filepath.Join(root, "t", name), Bytes: 10, LastWrite: now.Add(-100 * time.Hour)}
	}
	if _, due := ReviewsDue(root, now); due {
		t.Fatal("nothing in review, yet due")
	}
	SyncReviews(root, []Unproven{item("a"), item("b")}, now)
	open, due := ReviewsDue(root, now)
	if !due || len(open) != 2 {
		t.Fatalf("due = %v, open = %d", due, len(open))
	}
	askedAbout := func(rows []Review) []string {
		var paths []string
		for _, r := range rows {
			paths = append(paths, r.Path)
		}
		return paths
	}
	MarkReviewsAsked(root, askedAbout(open), now)
	if _, due := ReviewsDue(root, now.Add(30*time.Hour)); due {
		t.Fatal("the same set is due again")
	}
	SyncReviews(root, []Unproven{item("a"), item("b"), item("c")}, now.Add(time.Hour))
	if _, due := ReviewsDue(root, now.Add(2*time.Hour)); due {
		t.Fatal("a new item asked about within a day of the last asking")
	}
	open, due = ReviewsDue(root, now.Add(25*time.Hour))
	if !due || len(open) != 3 {
		t.Fatalf("a day later: due = %v open = %d", due, len(open))
	}
	// An item that arrives while the question is being written is not in it, and is not marked asked.
	SyncReviews(root, []Unproven{item("a"), item("b"), item("c"), item("late")}, now.Add(25*time.Hour))
	MarkReviewsAsked(root, askedAbout(open), now.Add(25*time.Hour))
	if rest, _ := OpenReviews(root); len(rest) != 4 {
		t.Fatalf("open = %+v", rest)
	}
	if row, _ := ReviewOf(root, item("late").Path); !row.AskedAt.IsZero() {
		t.Fatal("an item not in the question was marked asked")
	}
	if row, _ := ReviewOf(root, item("c").Path); row.AskedAt.IsZero() {
		t.Fatal("an item in the question was not marked asked")
	}
	SyncReviews(root, []Unproven{item("a"), item("b"), item("c")}, now.Add(26*time.Hour)) // late leaves
	// An answer is no new item.
	SyncReviews(root, []Unproven{item("a"), item("b")}, now.Add(50*time.Hour))
	if open, due := ReviewsDue(root, now.Add(100*time.Hour)); due || len(open) != 2 {
		t.Fatalf("after c left: due = %v open = %d", due, len(open))
	}
	// An item that comes back is a new question.
	SyncReviews(root, []Unproven{item("a"), item("b"), item("c")}, now.Add(60*time.Hour))
	if _, due := ReviewsDue(root, now.Add(100*time.Hour)); !due {
		t.Fatal("a returning item is not asked about")
	}
}

func TestGCWhySaysAnItemInReviewIsWaitingForBrain(t *testing.T) {
	noReads(t)
	root := t.TempDir()
	tdir := filepath.Join(root, "t")
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SweepUnmarked(root, Usage{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	writeTree(t, tdir, map[string]string{"old/data": "x"})
	age(t, filepath.Join(tdir, "old"), 72*time.Hour)
	now := time.Now()
	_, unproven, _ := SweepUnmarked(root, Usage{}, now)
	SyncReviews(root, unproven, now)
	path := filepath.Join(tdir, "old")
	state := func() string {
		why, err := WhyPath(WhyLookup{Root: root}, path)
		if err != nil || len(why.Chain) == 0 {
			t.Fatalf("why = %+v %v", why, err)
		}
		return why.Chain[0].State
	}
	if s := state(); !strings.Contains(s, "in review since") {
		t.Fatalf("state = %q", s)
	}
	MarkReviewsAsked(root, []string{path}, now)
	if s := state(); !strings.Contains(s, "asked about since") || !strings.Contains(s, "keep or release") {
		t.Fatalf("state = %q", s)
	}
}

// What waits for review is one of the kinds Resources shows, as it is now:
// no sample when nothing waits, and the day's other samples are still taken.
func TestWhatWaitsForReviewIsASizeSampleOfTheDay(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	day := now.Local().Format("2006-01-02")
	review := func() (SizeSample, bool) {
		samples, err := Sizes(root, day)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range samples {
			if s.Kind == SizeReview {
				return s, true
			}
		}
		return SizeSample{}, false
	}
	a := Unproven{Path: filepath.Join(root, "t", "a"), Bytes: 100, LastWrite: now.Add(-200 * time.Hour)}
	b := Unproven{Path: filepath.Join(root, "t", "b"), Bytes: 50, LastWrite: now.Add(-300 * time.Hour)}
	SyncReviews(root, []Unproven{a, b}, now)
	if s, ok := review(); !ok || s.Count != 2 || s.Bytes != 150 || s.Oldest.After(now.Add(-299*time.Hour)) {
		t.Fatalf("review sample = %+v %v", s, ok)
	}
	if measured, err := RecordSizes(root, now); err != nil || !measured {
		t.Fatalf("RecordSizes with a review sample in place = %v %v: the day's other kinds must still be measured", measured, err)
	}
	SyncReviews(root, []Unproven{a}, now)
	if s, ok := review(); !ok || s.Count != 1 || s.Bytes != 100 {
		t.Fatalf("after b left: %+v %v", s, ok)
	}
	SyncReviews(root, nil, now)
	if s, ok := review(); ok {
		t.Fatalf("nothing waits, yet a sample: %+v", s)
	}
}
