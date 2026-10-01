package telegram

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestUnchangedProjectionDoesNotRewriteDurableState(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.mutate(func(v *durableState) error {
		v.Outbox = []outboxRecord{{ID: "one", Entities: []MessageEntity{{Type: "bold"}}, ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "original"}}}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(s.statePath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err = s.mutate(func(v *durableState) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.Stat(s.statePath)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("no-op rewrote state")
	}
	snapshot := s.snapshot()
	snapshot.Outbox[0].ReplyMarkup.InlineKeyboard[0][0].Text = "external"
	snapshot.Outbox[0].Entities[0].Type = "external"
	snapshot.Projection["key"] = "external"
	if s.snapshot().Outbox[0].ReplyMarkup.InlineKeyboard[0][0].Text != "original" || len(s.snapshot().Projection) != 0 {
		t.Fatal("snapshot aliases store")
	}
	original := s.snapshot()
	_ = s.mutate(func(v *durableState) error { v.Outbox[0].Entities[0].Type = "failed"; return errors.New("abort") })
	if !reflect.DeepEqual(original, s.snapshot()) {
		t.Fatal("failed mutation leaked")
	}
}
