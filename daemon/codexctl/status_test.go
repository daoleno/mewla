package codexctl

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestReadStatusGathersWhatTheAppServerKnows(t *testing.T) {
	fake := startFakeAppServer(t)
	client := openFake(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.ReadStatus(ctx, "t-main", "/work")
	if err != nil {
		t.Fatal(err)
	}
	if status.Thread == nil || status.Thread.ID != "t-main" || status.Thread.Model != "gpt-6" || status.Thread.State != "idle" || status.Thread.Name != "" {
		t.Fatalf("thread = %+v", status.Thread)
	}
	if status.Config == nil || status.Config.Approval != "on-request" || status.Config.Sandbox != "workspace-write" || status.Config.ContextWindow != 272000 || status.Config.Effort != "high" {
		t.Fatalf("config = %+v", status.Config)
	}
	if status.Account == nil || status.Account.Kind != "chatgpt" || status.Account.Plan != "pro" || status.Account.Email != "me@example.com" {
		t.Fatalf("account = %+v", status.Account)
	}
	if len(status.Limits) != 2 || status.Limits[0].ID != "codex" || len(status.Limits[0].Windows) != 2 || status.Limits[0].Windows[1].WindowMinutes != 10080 ||
		status.Limits[0].Credits == nil || status.Limits[0].Credits.Balance != "5.00" || status.Limits[1].Name != "Other" {
		t.Fatalf("limits = %+v", status.Limits)
	}
	if params := fake.lastParams("config/read"); params["cwd"] != "/work" {
		t.Fatalf("config/read params = %v", params)
	}
}

func TestReadStatusKeepsTheSectionsThatAnswered(t *testing.T) {
	fake := startFakeAppServer(t)
	fake.statusFails = map[string]bool{"config/read": true}
	client := openFake(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.ReadStatus(ctx, "t-main", "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Config != nil || status.Thread == nil || status.Account == nil {
		t.Fatalf("status = %+v", status)
	}
}

func TestParseTokenUsage(t *testing.T) {
	params := json.RawMessage(`{"threadId":"t","turnId":"u","tokenUsage":{"total":{"inputTokens":10,"cachedInputTokens":4,"outputTokens":5,"reasoningOutputTokens":2,"totalTokens":15},"last":{"inputTokens":1,"cachedInputTokens":0,"outputTokens":1,"reasoningOutputTokens":0,"totalTokens":2},"modelContextWindow":272000}}`)
	usage, ok := ParseTokenUsage(params)
	if !ok || usage.Total.Total != 15 || usage.Total.Cached != 4 || usage.Last.Output != 1 || usage.ContextWindow != 272000 {
		t.Fatalf("usage = %+v ok=%v", usage, ok)
	}
}

func TestThreadPathNamesTheRollout(t *testing.T) {
	fake := startFakeAppServer(t)
	client := openFake(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path, err := client.ThreadPath(ctx, "t-main")
	if err != nil || path != "/rollout.jsonl" {
		t.Fatalf("path = %q err=%v", path, err)
	}
}
