package brain

import (
	"strings"
	"testing"
	"time"
)

func TestQuickRepliesBelongToTheCurrentThreadAndExpire(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	if got, err := store.DisplayedQuickReplies(); err != nil || got != nil {
		t.Fatalf("empty store shows %#v, %v", got, err)
	}
	set, err := store.SetQuickReplies([][]string{{" 1  处理 ", "1 不是问题", "1 稍后", "1 处理"}, {}, {"2 处理"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rows) != 2 || len(set.Rows[0]) != 3 || set.Rows[0][0] != "1 处理" {
		t.Fatalf("rows = %#v", set.Rows)
	}
	if got, _ := store.DisplayedQuickReplies(); got == nil || len(got.Rows) != 2 {
		t.Fatalf("displayed = %#v", got)
	}

	// A new chat thread starts without the previous thread's chips.
	if err := store.SetChatState(ChatState{ThreadID: "thread-b", ThreadIDs: []string{"thread-a", "thread-b"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.DisplayedQuickReplies(); got != nil {
		t.Fatalf("chips followed the user to another thread: %#v", got)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-a", ThreadIDs: []string{"thread-a", "thread-b"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.DisplayedQuickReplies(); got == nil {
		t.Fatal("chips for the current thread were lost")
	}

	store.now = func() time.Time { return time.Now().Add(quickRepliesTTL + time.Minute) }
	if got, _ := store.DisplayedQuickReplies(); got != nil {
		t.Fatalf("expired chips still shown: %#v", got)
	}
	if err := store.ClearQuickReplies(); err != nil {
		t.Fatal(err)
	}
}

func TestQuickRepliesRejectWhatAPhoneCannotShow(t *testing.T) {
	store := newObjectiveStore(t, "thread-a")
	for name, rows := range map[string][][]string{
		"none":      nil,
		"blank":     {{"  "}},
		"long chip": {{strings.Repeat("字", maxQuickReplyRunes+1)}},
		"wide row":  {{"a", "b", "c", "d", "e"}},
		"too many":  {{"1"}, {"2"}, {"3"}, {"4"}, {"5"}, {"6"}, {"7"}},
	} {
		if _, err := store.SetQuickReplies(rows); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
