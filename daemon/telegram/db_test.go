package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/sqlitedb"
)

// requireSameState compares two states by what they serialize to: the
// database stores each entry as JSON, so an empty list and a missing one are
// the same state.
func requireSameState(t *testing.T, got, want durableState) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("state differs\nwant %.300s\ngot  %.300s", wantRaw, gotRaw)
	}
}

// legacyState fills every kind of collection durableState has, so the move
// has an entry of each kind (string and integer keyed maps, lists of structs,
// of ints and of strings) to carry.
func legacyState() durableState {
	now := time.Date(2026, 10, 1, 12, 0, 0, 123456789, time.UTC)
	state := newDurableState()
	ensureDurableMaps(&state)
	state.Enabled, state.BotID, state.OwnerID, state.ChatID, state.NextOffset = true, 7001, 99, 99, 4000
	state.BotUsername, state.LastError = "mewla_bot", "none"
	state.LastSendAt = &now
	state.BrainTopics = []int64{3, 5, 8}
	state.SessionChoices = []string{"s1", "s2"}
	for index := 0; index < 600; index++ {
		key := fmt.Sprint(index)
		state.Processed[key] = updateRecord{Disposition: "accepted", HandledAt: now, MessageThreadID: int64(index)}
		state.Projection["p:"+key] = "digest-" + key
		state.TopicProjection["t:"+key] = "digest-" + key
		state.TopicMessages["m:"+key] = int64(index)
		state.WorkMessages["w:"+key] = int64(index)
		state.CallbackRoutes["c:"+key] = "route"
		state.CallbackIDs["i:"+key] = int64(index)
		state.ReplySessions[int64(index)] = "session-" + key
		state.MessageSources[int64(index)] = messageSource{}
		state.Feedback[int64(index)] = feedbackRecord{}
	}
	state.MediaInputs["update:3"] = mediaInput{State: "failed"}
	state.Outbox = []outboxRecord{
		{ID: "one", Kind: "send", Text: "one", PlainText: "one", State: "sent", CreatedAt: now, Entities: []MessageEntity{{Type: "bold", Length: 3}}},
		{ID: "two", Kind: "send", Text: "two", PlainText: "two", State: "pending", CreatedAt: now},
	}
	state.Topics = []topicMapping{{SessionID: "session-1", ChatID: 99, MessageThreadID: 42, State: topicStateActive, CreatedAt: now, UpdatedAt: now}}
	state.TopicOps = []topicOpRecord{{ID: "op-1", Kind: topicOpCreate, State: "sent", CreatedAt: now}}
	return state
}

func TestMigrationCarriesEveryEntryAndSetsStateJSONAside(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := legacyState()
	raw, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	requireSameState(t, opened.snapshot(), want)
	record, found, err := sqlitedb.ReadRecord(opened.db)
	if err != nil || !found {
		t.Fatalf("record found=%v err=%v", found, err)
	}
	var details MigrationDetails
	if err := json.Unmarshal(record.Details, &details); err != nil {
		t.Fatal(err)
	}
	digest, err := digestState(want)
	if err != nil || details != digest || details.Entries < 6000 {
		t.Fatalf("details=%+v want %+v err=%v", details, digest, err)
	}
	if kept, err := os.ReadFile(filepath.Join(record.BackupDir, "telegram", legacyFile)); err != nil || string(kept) != string(raw) {
		t.Fatalf("state.json was not set aside intact: %v", err)
	}
	if token, err := os.ReadFile(filepath.Join(dir, "token")); err != nil || string(token) != "secret\n" {
		t.Fatalf("token moved or changed: %q %v", token, err)
	}
	// The next start reads the database and finds nothing to migrate.
	again, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	requireSameState(t, again.snapshot(), want)
}

func TestOneMutationWritesOneEntryHoweverLargeTheStateIs(t *testing.T) {
	root := t.TempDir()
	s, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.mutate(func(state *durableState) error {
		*state = legacyState()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	written := countWrites(t, s)
	if err := s.mutate(func(state *durableState) error {
		state.TopicMessages["m:new"] = 7
		delete(state.Projection, "p:3")
		state.Outbox[1].State = "sent"
		state.NextOffset++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Four changes: one added entry, one removed, one list element, one scalar.
	if n := written(); n != 4 {
		t.Fatalf("mutation wrote %d rows, want 4", n)
	}
	reopened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reopened.snapshot(), s.snapshot()) {
		t.Fatal("restart did not read back what was committed")
	}
}

// TestMigrationOfARealTelegramState migrates a copy of an actual
// telegram/state.json. Set MEWLA_REAL_TELEGRAM_COPY to the telegram
// directory; the test works on a copy and never writes to it.
func TestMigrationOfARealTelegramState(t *testing.T) {
	source := os.Getenv("MEWLA_REAL_TELEGRAM_COPY")
	if source == "" {
		t.Skip("MEWLA_REAL_TELEGRAM_COPY is not set")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "telegram")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(source, legacyFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, legacyFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	record, _, _ := sqlitedb.ReadRecord(opened.db)
	t.Logf("migrated %s", record.Details)
	want, _, err := decodeLegacy(raw)
	if err != nil {
		t.Fatal(err)
	}
	requireSameState(t, opened.snapshot(), want)
}
