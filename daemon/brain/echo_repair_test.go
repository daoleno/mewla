package brain

import (
	"strings"
	"testing"
	"time"
)

type storedEchoFixture struct {
	store     *Store
	threadID  string
	admission BrainInputAdmission
	echo      TimelineItem
}

// storedEchoDuplicate stores an admission row and the provider echo row the
// automatic claim missed, in the order the live store holds them.
func storedEchoDuplicate(t *testing.T, requestID, sessionID, body, echoBody string, prepared, accepted, echoAt time.Time, echoFirst bool) storedEchoFixture {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_1793797903668055662"
	admission := acceptEchoTestAdmission(t, store, requestID, threadID, sessionID, body, prepared, accepted)
	echo := TimelineItem{
		ID: sessionID + ":echo", ThreadID: threadID, SessionID: sessionID, Role: "user", Body: echoBody,
		CreatedAt: echoAt, Kind: timelineKindUserMessage, Origin: TimelineOriginTranscript,
		ProviderInputSHA256: []string{AdmissionDigest("the provider's wrapped bytes")},
	}
	rows := []TimelineItem{brainInputAdmissionTimelineItem(admission), echo}
	if echoFirst {
		rows[0], rows[1] = rows[1], rows[0]
	}
	for _, row := range rows {
		if _, err := store.AppendTimelineItem(row); err != nil {
			t.Fatal(err)
		}
	}
	return storedEchoFixture{store: store, threadID: threadID, admission: admission, echo: echo}
}

// The two leftover shapes in the owner's thread: a Claude queued record kept
// before its envelope was decoded (tab as four spaces, blank-line run folded),
// and a Codex echo stored before its admission row (display-cleaned).
func TestRepairStoredEchoClaimsTheNamedLeftover(t *testing.T) {
	cases := []struct {
		name, requestID, sessionID, body, echoBody, transform string
		echoFirst                                             bool
		prepared, accepted, echoAt                            time.Time
	}{
		{
			name: "claude queued 36812", requestID: "mwm45gry_urqoln", sessionID: "e976f820-9dde-40e6-9a3e-ab6a5768effb",
			body:     "方案选 A（买 RunsOn，€300/年）还是 B（免费自建）。\t1.\n在 agents 的 Chrome 里登录一次 paul-freeride 的 GitHub。\n\n\n这个是关于啥的问题",
			echoBody: "方案选 A（买 RunsOn，€300/年）还是 B（免费自建）。    1.\n在 agents 的 Chrome 里登录一次 paul-freeride 的 GitHub。\n\n这个是关于啥的问题",
			prepared: time.Date(2026, 11, 18, 12, 44, 34, 738609304, time.UTC), accepted: time.Date(2026, 11, 18, 12, 44, 35, 104069033, time.UTC),
			echoAt: time.Date(2026, 11, 18, 12, 44, 47, 525000000, time.UTC), transform: "claude+display",
		},
		{
			name: "codex 40873", requestID: "mwrwhx36_ys2vum", sessionID: "01a202bb-2d32-7052-afce-3d2b073be54b",
			body:     "Mewla Windows 版有一个自动更新后频繁弹终端窗口、抢占焦点的问题。\n手动 restart 能恢复。\n\n\n这是人家的 AI 给我们的反馈。",
			echoBody: "Mewla Windows 版有一个自动更新后频繁弹终端窗口、抢占焦点的问题。\n手动 restart 能恢复。\n\n这是人家的 AI 给我们的反馈。",
			prepared: time.Date(2026, 11, 22, 13, 56, 56, 141599173, time.UTC), accepted: time.Date(2026, 11, 22, 13, 56, 58, 401204155, time.UTC),
			echoAt: time.Date(2026, 11, 22, 13, 56, 58, 269000000, time.UTC), transform: "display", echoFirst: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := storedEchoDuplicate(t, tc.requestID, tc.sessionID, tc.body, tc.echoBody, tc.prepared, tc.accepted, tc.echoAt, tc.echoFirst)
			dry, err := f.store.RepairStoredEcho(tc.requestID, f.echo.ID, false)
			if err != nil || dry.Applied || dry.Transform != tc.transform {
				t.Fatalf("dry run = %+v, %v", dry, err)
			}
			if users := threadUserRows(t, f.store, f.threadID); len(users) != 2 {
				t.Fatalf("a dry run must not write: %+v", users)
			}
			applied, err := f.store.RepairStoredEcho(tc.requestID, f.echo.ID, true)
			if err != nil || !applied.Applied {
				t.Fatalf("apply = %+v, %v", applied, err)
			}
			users := threadUserRows(t, f.store, f.threadID)
			if len(users) != 1 || users[0].ID != tc.requestID || users[0].AdmissionEchoEventID != f.echo.ID ||
				users[0].AdmissionEchoSessionID != tc.sessionID || users[0].Body != tc.body {
				t.Fatalf("the admission must claim the echo and the echo row go: %+v", users)
			}
			if _, err := f.store.RepairStoredEcho(tc.requestID, f.echo.ID, true); err == nil {
				t.Fatal("a second repair must be refused")
			}
		})
	}
}

func TestRepairStoredEchoRefusesAnythingElse(t *testing.T) {
	prepared := time.Date(2026, 11, 22, 13, 56, 56, 0, time.UTC)
	accepted, echoAt := prepared.Add(2*time.Second), prepared.Add(2*time.Second)
	body := "line one\n\n\nline two"
	cases := []struct {
		name, want string
		mutate     func(t *testing.T, f storedEchoFixture) (admissionID, echoID string)
	}{
		{"different text", "any known transform", func(t *testing.T, f storedEchoFixture) (string, string) {
			return f.admission.RequestID, f.echo.ID
		}},
		{"a user row between", "not adjacent", func(t *testing.T, f storedEchoFixture) (string, string) {
			if _, err := f.store.AppendTimelineItem(TimelineItem{ID: "typed-later", ThreadID: f.threadID, SessionID: "session", Role: "user",
				Body: "something else", CreatedAt: echoAt.Add(time.Second), Kind: timelineKindUserMessage, Origin: TimelineOriginTranscript}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.AppendTimelineItem(TimelineItem{ID: "late:echo", ThreadID: f.threadID, SessionID: "session", Role: "user",
				Body: "line one\n\nline two", CreatedAt: echoAt.Add(2 * time.Second), Kind: timelineKindUserMessage, Origin: TimelineOriginTranscript}); err != nil {
				t.Fatal(err)
			}
			return f.admission.RequestID, "late:echo"
		}},
		{"not a transcript row", "not a transcript user row", func(t *testing.T, f storedEchoFixture) (string, string) {
			if _, err := f.store.AppendTimelineItem(TimelineItem{ID: "provider:row", ThreadID: f.threadID, SessionID: "session", Role: "user",
				Body: "line one\n\nline two", CreatedAt: echoAt, Kind: timelineKindUserMessage, Origin: TimelineOriginProvider}); err != nil {
				t.Fatal(err)
			}
			return f.admission.RequestID, "provider:row"
		}},
		{"not an admission", "not a Brain admission", func(t *testing.T, f storedEchoFixture) (string, string) {
			return f.echo.ID, f.echo.ID
		}},
		{"recorded before the send", "before admission", func(t *testing.T, f storedEchoFixture) (string, string) {
			if _, err := f.store.AppendTimelineItem(TimelineItem{ID: "early:echo", ThreadID: f.threadID, SessionID: "session", Role: "user",
				Body: "line one\n\nline two", CreatedAt: prepared.Add(-time.Minute), Kind: timelineKindUserMessage, Origin: TimelineOriginTranscript}); err != nil {
				t.Fatal(err)
			}
			return f.admission.RequestID, "early:echo"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			echoBody := "line one\n\nline two"
			if tc.name == "different text" {
				echoBody = "line one\n\nline three"
			}
			f := storedEchoDuplicate(t, "refused-send", "session", body, echoBody, prepared, accepted, echoAt, false)
			admissionID, echoID := tc.mutate(t, f)
			before := threadUserRows(t, f.store, f.threadID)
			_, err := f.store.RepairStoredEcho(admissionID, echoID, true)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if after := threadUserRows(t, f.store, f.threadID); len(after) != len(before) {
				t.Fatalf("a refused repair must not write: %d -> %d", len(before), len(after))
			}
		})
	}
}
