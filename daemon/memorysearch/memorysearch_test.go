package memorysearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

type fixtureRow struct {
	id, kind, body, at, workID string
	admission                  bool
}

func writeRows(t *testing.T, path string, rows ...fixtureRow) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, row := range rows {
		role := "assistant"
		if row.kind == "user_message" {
			role = "user"
		}
		raw, _ := json.Marshal(map[string]any{
			"id": row.id, "role": role, "kind": row.kind, "body": row.body,
			"created_at": row.at, "work_id": row.workID, "session_id": "s",
			"brain_admission": row.admission,
		})
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

var fixtureSourceName = regexp.MustCompile(`^messages(-[0-9]+)?\.jsonl$`)

// fileSource serves the fixture jsonl files as chat sources: archives sorted
// by name, then messages.jsonl; a row's Seq is its 1-based line number, and a
// trailing line without a newline is not a row yet.
type fileSource struct{ dir string }

func (f fileSource) ChatSources() []string {
	entries, _ := os.ReadDir(f.dir)
	var archives []string
	live := false
	for _, entry := range entries {
		switch name := entry.Name(); {
		case name == "messages.jsonl":
			live = true
		case fixtureSourceName.MatchString(name):
			archives = append(archives, name)
		}
	}
	sort.Strings(archives)
	if live {
		archives = append(archives, "messages.jsonl")
	}
	return archives
}

func (f fileSource) ChatRows(name string, after int64, limit int) ([]ChatRow, error) {
	raw, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		return nil, err
	}
	var rows []ChatRow
	for index, line := range strings.SplitAfter(string(raw), "\n") {
		if !strings.HasSuffix(line, "\n") || int64(index+1) <= after {
			continue
		}
		rows = append(rows, ChatRow{Seq: int64(index + 1), Body: []byte(line)})
		if len(rows) == limit {
			break
		}
	}
	return rows, nil
}

// memSource is a source whose rows the test sets directly.
type memSource struct{ rows []ChatRow }

func (m *memSource) ChatSources() []string { return []string{"live"} }

func (m *memSource) ChatRows(_ string, after int64, limit int) ([]ChatRow, error) {
	var out []ChatRow
	for _, row := range m.rows {
		if row.Seq > after && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func row(seq int64, id, at, body string) ChatRow {
	raw, _ := json.Marshal(map[string]any{
		"id": id, "role": "user", "kind": "user_message", "body": body, "created_at": at, "session_id": "s",
	})
	return ChatRow{Seq: seq, Body: raw}
}

func openFixture(t *testing.T) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	ix, err := Open(filepath.Join(dir, "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix, dir
}

func refresh(t *testing.T, ix *Index, dir string) RefreshStats {
	t.Helper()
	stats, err := ix.Refresh(context.Background(), fileSource{dir})
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func search(t *testing.T, ix *Index, opts SearchOptions) []Hit {
	t.Helper()
	hits, err := ix.Search(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func ids(hits []Hit) string {
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = hit.ID
	}
	return strings.Join(out, ",")
}

func TestChineseQueries(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "a", kind: "user_message", at: "2026-10-05T17:55:56Z",
			body: "我担心丢信息，不要用写死的规则去优化 handoff，Claude 原生压缩不要过度干预。"},
		fixtureRow{id: "b", kind: "assistant_message", at: "2026-10-05T17:56:08.5Z",
			body: "handoff 那条我撤回——不动模型的压缩，交给它自己判断。"},
		fixtureRow{id: "c", kind: "user_message", at: "2026-10-06T01:00:00Z", body: "无关的一句话"},
	)
	refresh(t, ix, dir)

	for name, tc := range map[string]struct {
		query string
		want  string
	}{
		"whole sentence":   {"不要用写死的规则去优化 handoff，Claude 原生压缩不要过度干预。", "a"},
		"partial phrase":   {"写死的规则", "a"},
		"two characters":   {"压缩", "b,a"},
		"one character":    {"撤", "b"},
		"case-insensitive": {"CLAUDE", "a"},
		"latin word":       {"handoff", "b,a"},
		"no match":         {"不存在的短语", ""},
	} {
		if got := ids(search(t, ix, SearchOptions{Query: tc.query})); got != tc.want {
			t.Errorf("%s (%q): got %q want %q", name, tc.query, got, tc.want)
		}
	}
}

func TestLikeFallbackEscapesWildcards(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "a", kind: "user_message", at: "2026-10-05T10:00:00Z", body: "100% done"},
		fixtureRow{id: "b", kind: "user_message", at: "2026-10-05T10:01:00Z", body: "nothing here"},
	)
	refresh(t, ix, dir)
	if got := ids(search(t, ix, SearchOptions{Query: "%"})); got != "a" {
		t.Fatalf("%% matched %q, want only a", got)
	}
	if got := ids(search(t, ix, SearchOptions{Query: `a" OR *`})); got != "" {
		t.Fatalf("operator characters must be literal, got %q", got)
	}
}

func TestRankingPhraseBeforeTermsThenNewest(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "old-phrase", kind: "user_message", at: "2026-10-01T00:00:00Z", body: "release the beta build"},
		fixtureRow{id: "new-terms", kind: "user_message", at: "2026-10-03T00:00:00Z", body: "the build was a beta of release"},
		fixtureRow{id: "new-phrase", kind: "assistant_message", at: "2026-10-02T00:00:00Z", body: "Release the beta build now"},
	)
	refresh(t, ix, dir)
	got := ids(search(t, ix, SearchOptions{Query: "release the beta"}))
	if got != "new-phrase,old-phrase,new-terms" {
		t.Fatalf("phrase hits = %q", got)
	}
	got = ids(search(t, ix, SearchOptions{Query: "release beta"}))
	if got != "new-terms,new-phrase,old-phrase" && got != "new-terms,old-phrase,new-phrase" {
		t.Fatalf("terms hits = %q", got)
	}
}

func TestFiltersAndLimit(t *testing.T) {
	ix, dir := openFixture(t)
	var rows []fixtureRow
	for i := range 5 {
		rows = append(rows,
			fixtureRow{id: fmt.Sprintf("u%d", i), kind: "user_message", at: fmt.Sprintf("2026-10-0%dT00:00:00Z", i+1), body: fmt.Sprintf("deploy step %d", i)},
			fixtureRow{id: fmt.Sprintf("b%d", i), kind: "assistant_message", at: fmt.Sprintf("2026-10-0%dT00:00:01Z", i+1), body: fmt.Sprintf("deploy reply %d", i)})
	}
	writeRows(t, filepath.Join(dir, "messages.jsonl"), rows...)
	refresh(t, ix, dir)

	if got := ids(search(t, ix, SearchOptions{Query: "deploy", From: "user", Limit: 2})); got != "u4,u3" {
		t.Fatalf("user limit 2 = %q", got)
	}
	if got := ids(search(t, ix, SearchOptions{Query: "deploy", From: "brain", Limit: 1})); got != "b4" {
		t.Fatalf("brain = %q", got)
	}
	since := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if got := ids(search(t, ix, SearchOptions{Query: "deploy", Since: since, Until: until, From: "user"})); got != "u2,u1" {
		t.Fatalf("since/until = %q", got)
	}
	if _, err := ix.Search(context.Background(), SearchOptions{Query: "deploy", From: "robot"}); err == nil {
		t.Fatal("bad --from accepted")
	}
}

func TestFractionalTimestampsOrderCorrectly(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "later", kind: "user_message", at: "2026-10-05T10:00:00.5Z", body: "same second note"},
		fixtureRow{id: "earlier", kind: "assistant_message", at: "2026-10-05T10:00:00.25879Z", body: "same second note again"},
	)
	refresh(t, ix, dir)
	if got := ids(search(t, ix, SearchOptions{Query: "same second note"})); got != "later,earlier" {
		t.Fatalf("order = %q", got)
	}
}

func TestIncrementalCursor(t *testing.T) {
	ix, dir := openFixture(t)
	path := filepath.Join(dir, "messages.jsonl")
	writeRows(t, path, fixtureRow{id: "1", kind: "user_message", at: "2026-10-05T10:00:00Z", body: "first alpha"})
	if stats := refresh(t, ix, dir); stats.Messages != 1 {
		t.Fatalf("first refresh = %+v", stats)
	}
	if stats := refresh(t, ix, dir); stats.Messages != 0 {
		t.Fatalf("idle refresh = %+v", stats)
	}

	// A half-written trailing line is not consumed, then completes later.
	file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString(`{"id":"2","role":"user","kind":"user_message","body":"second be`)
	_ = file.Close()
	if stats := refresh(t, ix, dir); stats.Messages != 0 {
		t.Fatalf("partial line indexed: %+v", stats)
	}
	file, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString("ta\",\"created_at\":\"2026-10-05T10:01:00Z\"}\n")
	_ = file.Close()
	if stats := refresh(t, ix, dir); stats.Messages != 1 {
		t.Fatalf("completed line refresh = %+v", stats)
	}
	if got := ids(search(t, ix, SearchOptions{Query: "second beta"})); got != "2" {
		t.Fatalf("appended message not found: %q", got)
	}
}

func TestRewrittenRowsAreReadAgainWithoutDuplicates(t *testing.T) {
	ix, _ := openFixture(t)
	src := &memSource{rows: []ChatRow{
		row(1, "1", "2026-10-05T10:00:00Z", "keep gamma one"),
		row(2, "2", "2026-10-05T10:01:00Z", "keep gamma two"),
	}}
	if _, err := ix.Refresh(context.Background(), src); err != nil {
		t.Fatal(err)
	}

	// The timeline rewrote a row: the old one is gone and a new one took a
	// later position, as moving a Work card does.
	src.rows = []ChatRow{
		row(3, "2", "2026-10-05T10:01:00Z", "keep gamma two"),
		row(4, "3", "2026-10-05T10:02:00Z", "keep gamma three"),
		row(5, "4", "2026-10-05T10:03:00Z", "keep gamma four"),
	}
	stats, err := ix.Refresh(context.Background(), src)
	if err != nil || stats.Messages != 2 {
		t.Fatalf("rewrite refresh = %+v %v", stats, err)
	}
	if got := ids(search(t, ix, SearchOptions{Query: "keep gamma", Limit: 10})); got != "4,3,2,1" {
		t.Fatalf("after rewrite = %q", got)
	}
	if stats, err := ix.Refresh(context.Background(), src); err != nil || stats.Messages != 0 {
		t.Fatalf("idle refresh = %+v %v", stats, err)
	}
}

func TestRefreshReadsMoreThanOneBatch(t *testing.T) {
	ix, _ := openFixture(t)
	src := &memSource{}
	for i := int64(1); i <= refreshBatch*2+7; i++ {
		src.rows = append(src.rows, row(i, fmt.Sprintf("m%d", i), "2026-10-05T10:00:00Z", fmt.Sprintf("batch row %d", i)))
	}
	stats, err := ix.Refresh(context.Background(), src)
	if err != nil || stats.Messages != refreshBatch*2+7 {
		t.Fatalf("refresh = %+v %v", stats, err)
	}
}

func TestArchivesAreIndexedOnceAndLiveFileWins(t *testing.T) {
	ix, dir := openFixture(t)
	shared := fixtureRow{id: "shared", kind: "user_message", at: "2026-05-26T06:52:26Z", body: "remember the delta test"}
	writeRows(t, filepath.Join(dir, "messages-3115074017.jsonl"), shared,
		fixtureRow{id: "arch", kind: "assistant_message", at: "2026-06-01T00:00:00Z", body: "archived delta reply"})
	writeRows(t, filepath.Join(dir, "messages.jsonl"), shared,
		fixtureRow{id: "live", kind: "user_message", at: "2026-10-01T00:00:00Z", body: "live delta message"})
	// Not chat sources.
	writeRows(t, filepath.Join(dir, "messages.jsonl.pre-digest-repair-20261008-153901"),
		fixtureRow{id: "backup", kind: "user_message", at: "2026-10-02T00:00:00Z", body: "backup delta copy"})

	stats := refresh(t, ix, dir)
	if stats.Messages != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if got := ids(search(t, ix, SearchOptions{Query: "delta"})); got != "live,arch,shared" {
		t.Fatalf("archive search = %q", got)
	}
}

func TestDuplicateCollapse(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "1", kind: "user_message", at: "2026-10-01T00:00:00Z", body: "please  retry\nthe job"},
		fixtureRow{id: "2", kind: "user_message", at: "2026-10-02T00:00:00Z", body: "please retry the job"},
		fixtureRow{id: "3", kind: "assistant_message", at: "2026-10-03T00:00:00Z", body: "please retry the job"},
		fixtureRow{id: "4", kind: "user_message", at: "2026-10-04T00:00:00Z", body: "please retry the job"})
	hits := search(t, ix, SearchOptions{Query: "retry the job"})
	if len(hits) != 0 {
		t.Fatal("search before refresh must see nothing")
	}
	refresh(t, ix, dir)
	hits = search(t, ix, SearchOptions{Query: "retry the job"})
	if ids(hits) != "4,3" || hits[0].Duplicates != 2 || hits[1].Duplicates != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestWorkCardsAndSkippedKinds(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "card", kind: "work_card", at: "2026-10-01T00:00:00Z", body: "epsilon result ready", workID: "w-1"},
		fixtureRow{id: "cal", kind: "calendar_result", at: "2026-10-01T00:01:00Z", body: "epsilon calendar"},
		fixtureRow{id: "empty", kind: "user_message", at: "2026-10-01T00:02:00Z", body: "   "})
	refresh(t, ix, dir)
	hits := search(t, ix, SearchOptions{Query: "epsilon"})
	if len(hits) != 1 || hits[0].Who != WhoWork || hits[0].WorkID != "w-1" {
		t.Fatalf("hits = %+v", hits)
	}
	if got := search(t, ix, SearchOptions{Query: "epsilon", From: "brain"}); len(got) != 0 {
		t.Fatalf("work card leaked into --from brain: %+v", got)
	}
}

func TestShowAround(t *testing.T) {
	ix, dir := openFixture(t)
	var rows []fixtureRow
	for i := range 9 {
		rows = append(rows, fixtureRow{id: fmt.Sprintf("m%d", i), kind: "user_message",
			at: fmt.Sprintf("2026-10-01T00:00:0%dZ", i), body: fmt.Sprintf("message number %d", i)})
	}
	writeRows(t, filepath.Join(dir, "messages.jsonl"), rows...)
	refresh(t, ix, dir)
	ctx := context.Background()

	got, err := ix.Show(ctx, "m4", 2)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range got {
		order = append(order, m.ID)
		if (m.ID == "m4") != m.Match {
			t.Fatalf("match flag wrong on %s", m.ID)
		}
	}
	if strings.Join(order, ",") != "m2,m3,m4,m5,m6" {
		t.Fatalf("around 2 = %v", order)
	}

	edge, err := ix.Show(ctx, "m0", 3)
	if err != nil || len(edge) != 4 || edge[0].ID != "m0" {
		t.Fatalf("edge = %+v err=%v", edge, err)
	}
	alone, err := ix.Show(ctx, "m8", 0)
	if err != nil || len(alone) != 1 || alone[0].Text != "message number 8" {
		t.Fatalf("alone = %+v err=%v", alone, err)
	}
	if _, err := ix.Show(ctx, "nope", 1); err == nil {
		t.Fatal("missing id accepted")
	}
}

func TestShowIDPrefix(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "07ac686b-aaaa:1", kind: "assistant_message", at: "2026-10-01T00:00:00Z", body: "one"},
		fixtureRow{id: "07ac686b-bbbb:1", kind: "assistant_message", at: "2026-10-01T00:00:01Z", body: "two"},
		fixtureRow{id: "ffff0000", kind: "user_message", at: "2026-10-01T00:00:02Z", body: "three"})
	refresh(t, ix, dir)
	ctx := context.Background()
	if got, err := ix.Show(ctx, "ffff", 0); err != nil || got[0].ID != "ffff0000" {
		t.Fatalf("unique prefix: %+v %v", got, err)
	}
	if _, err := ix.Show(ctx, "07ac686b", 0); err == nil {
		t.Fatal("ambiguous prefix accepted")
	}
}

func TestSnippetIsRuneSafe(t *testing.T) {
	body := strings.Repeat("字", 200) + "目标短语" + strings.Repeat("尾", 200)
	got := snippet(body, []string{"目标短语"})
	if !strings.Contains(got, "目标短语") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("snippet = %q", got)
	}
	if n := len([]rune(got)); n > snippetBefore+snippetAfter+2 {
		t.Fatalf("snippet too long: %d", n)
	}
	if got := snippet("line one\n\nline   two", []string{"zzz"}); got != "line one line two" {
		t.Fatalf("whitespace = %q", got)
	}
}

func TestIndexStoresRedactedTextOnly(t *testing.T) {
	ix, dir := openFixture(t)
	secret := "sk-proj-AbCdEf0123456789xyzXYZ0123456789"
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "1", kind: "user_message", at: "2026-10-01T00:00:00Z",
			body: "rotate the key " + secret + " today"})
	refresh(t, ix, dir)
	if got := search(t, ix, SearchOptions{Query: "AbCdEf0123456789"}); len(got) != 0 {
		t.Fatalf("secret is searchable: %+v", got)
	}
	got, err := ix.Show(context.Background(), "1", 0)
	if err != nil || strings.Contains(got[0].Text, "AbCdEf") || !strings.Contains(got[0].Text, "rotate the key") {
		t.Fatalf("stored text = %+v err=%v", got, err)
	}
}

func TestSchemaVersionMismatchRebuilds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.sqlite")
	ix, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	writeRows(t, filepath.Join(dir, "messages.jsonl"), fixtureRow{id: "1", kind: "user_message", at: "2026-10-01T00:00:00Z", body: "stale row"})
	if _, err := ix.Refresh(context.Background(), fileSource{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := ix.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	_ = ix.Close()

	ix, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if got := search(t, ix, SearchOptions{Query: "stale row"}); len(got) != 0 {
		t.Fatalf("old index survived: %+v", got)
	}
	if stats, err := ix.Refresh(context.Background(), fileSource{dir}); err != nil || stats.Messages != 1 {
		t.Fatalf("rebuild = %+v %v", stats, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("index mode = %v err=%v", info.Mode(), err)
	}
}

func TestBackfilledNonTypingRowsAreContext(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		fixtureRow{id: "typed", kind: "user_message", at: "2026-10-01T00:00:00Z", admission: true, body: "zeta rollout plan: ship Friday"},
		fixtureRow{id: "s:t1:user", kind: "user_message", at: "2026-10-02T00:00:00Z", body: "This session is being continued from a previous conversation. zeta rollout plan was Friday."},
		fixtureRow{id: "s:t2:user", kind: "user_message", at: "2026-10-03T00:00:00Z", body: "<task-notification>\n<task-id>x</task-id>zeta finished</task-notification>"},
		fixtureRow{id: "s:t3:user", kind: "user_message", at: "2026-10-04T00:00:00Z", body: "<pasted_content id=\"1\">\n<zen_work_event>{\"summary\":\"zeta done\"}</zen_work_event>\n</pasted_content id=\"1\">"},
		fixtureRow{id: "s:t4:user", kind: "user_message", at: "2026-10-05T00:00:00Z", body: "[Request interrupted by user] zeta"},
		// What the user pasted or typed in Brain's terminal is still theirs.
		fixtureRow{id: "s:t5:user", kind: "user_message", at: "2026-10-06T00:00:00Z", body: "<pasted_content id=\"2\">\n~ zeta ping ok\n</pasted_content id=\"2\">"},
		fixtureRow{id: "s:t6:user", kind: "user_message", at: "2026-10-07T00:00:00Z", body: "typed zeta first <zen_work_event>{}</zen_work_event>"},
		fixtureRow{id: "s:t7:user", kind: "user_message", at: "2026-10-08T00:00:00Z", body: "zeta 继续"},
		fixtureRow{id: "s:a1:assistant:1", kind: "assistant_message", at: "2026-10-09T00:00:00Z", body: "zeta reply from brain"},
	)
	refresh(t, ix, dir)

	whoOfHit := func(from string) map[string]string {
		out := map[string]string{}
		for _, hit := range search(t, ix, SearchOptions{Query: "zeta", From: from, Limit: 50}) {
			out[hit.ID] = hit.Who
		}
		return out
	}
	user := whoOfHit("user")
	for _, id := range []string{"typed", "s:t5:user", "s:t6:user", "s:t7:user"} {
		if user[id] != WhoUser {
			t.Errorf("%s should be user, --from user gave %v", id, user)
		}
	}
	for _, id := range []string{"s:t1:user", "s:t2:user", "s:t3:user", "s:t4:user", "s:a1:assistant:1"} {
		if _, leaked := user[id]; leaked {
			t.Errorf("%s leaked into --from user", id)
		}
	}
	ctx := whoOfHit("context")
	if len(ctx) != 4 || ctx["s:t1:user"] != WhoContext || ctx["s:t4:user"] != WhoContext {
		t.Fatalf("--from context = %v", ctx)
	}

	// --from all keeps them, after every real message (both match tiers).
	all := search(t, ix, SearchOptions{Query: "zeta", Limit: 50})
	seenContext := false
	for _, hit := range all {
		if hit.Who == WhoContext {
			seenContext = true
		} else if seenContext {
			t.Fatalf("real message %s ranked after a context row: %s", hit.ID, ids(all))
		}
	}
	if len(all) != 9 {
		t.Fatalf("--from all = %s", ids(all))
	}
	if _, err := ix.Search(context.Background(), SearchOptions{Query: "zeta", From: "robot"}); err == nil {
		t.Fatal("bad --from accepted")
	}
}

func TestBackfilledCopyCollapsesIntoRealMessage(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		// The backfilled copy is NEWER than the real message: the real one wins.
		fixtureRow{id: "real", kind: "user_message", at: "2026-10-01T00:00:00Z", admission: true, body: "please retry the job"},
		fixtureRow{id: "s:t1:user", kind: "user_message", at: "2026-10-01T00:00:02Z", body: "please retry the job"},
		// No real counterpart: the backfilled typing stays a user hit.
		fixtureRow{id: "s:t2:user", kind: "user_message", at: "2026-10-02T00:00:00Z", body: "only typed in the terminal"},
		// A context row that repeats a real message folds in as well.
		fixtureRow{id: "s:t3:user", kind: "user_message", at: "2026-10-03T00:00:00Z", body: "<task-notification>pinned</task-notification>"},
		fixtureRow{id: "real2", kind: "user_message", at: "2026-10-04T00:00:00Z", admission: true, body: "<task-notification>pinned</task-notification>"},
	)
	refresh(t, ix, dir)

	hits := search(t, ix, SearchOptions{Query: "retry the job"})
	if len(hits) != 1 || hits[0].ID != "real" || hits[0].Who != WhoUser || hits[0].Duplicates != 1 {
		t.Fatalf("retry hits = %+v", hits)
	}
	hits = search(t, ix, SearchOptions{Query: "only typed", From: "user"})
	if len(hits) != 1 || hits[0].ID != "s:t2:user" || hits[0].Who != WhoUser {
		t.Fatalf("backfilled typing = %+v", hits)
	}
	hits = search(t, ix, SearchOptions{Query: "pinned"})
	if len(hits) != 1 || hits[0].ID != "real2" || hits[0].Duplicates != 1 {
		t.Fatalf("context duplicate = %+v", hits)
	}
}

func TestContextRanksAfterRealMessagesAcrossTiers(t *testing.T) {
	ix, dir := openFixture(t)
	writeRows(t, filepath.Join(dir, "messages.jsonl"),
		// Exact phrase, but context (newest).
		fixtureRow{id: "s:c:user", kind: "user_message", at: "2026-10-09T00:00:00Z", body: "This session is being continued: rollout plan for friday"},
		// Terms only, but real.
		fixtureRow{id: "real-terms", kind: "assistant_message", at: "2026-10-01T00:00:00Z", body: "friday is the day we do the rollout, per the plan"},
		fixtureRow{id: "real-phrase", kind: "user_message", at: "2026-10-02T00:00:00Z", admission: true, body: "rollout plan: friday"},
	)
	refresh(t, ix, dir)
	if got := ids(search(t, ix, SearchOptions{Query: "rollout plan", Limit: 10})); got != "real-phrase,real-terms,s:c:user" {
		t.Fatalf("order = %q", got)
	}
	// The limit never lets context displace a real message.
	if got := ids(search(t, ix, SearchOptions{Query: "rollout plan", Limit: 2})); got != "real-phrase,real-terms" {
		t.Fatalf("limited = %q", got)
	}
}
