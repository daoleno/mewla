package connections

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloudListReturnsCachedRowsWithoutWaitingForCLI(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	defer m.Close()
	os.WriteFile(config, []byte("[profile cached]\nregion=us-east-1\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	started, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	m.cloud.run = func(ctx context.Context, _ string, args, env []string) (cloudOutput, error) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return cloudOutput{}, ctx.Err()
		}
		if args[0] == "sts" {
			return cloudOutput{Stdout: `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:root"}`}, nil
		}
		return cloudOutput{Stdout: `{"AccountAliases":["Updated"]}`}, nil
	}
	m.snapshots.checked = map[string]time.Time{}
	m.cloud.checked = map[string]time.Time{}
	pushes := make(chan Response, 10)
	m.SetChanged(func(r Response) { pushes <- r })
	start := time.Now()
	response, err := m.Handle(context.Background(), Request{Action: "list"})
	if err != nil || time.Since(start) > 150*time.Millisecond {
		t.Fatal("list synchronously waited for CLI", err, time.Since(start))
	}
	if len(response.Accounts) != 1 || !response.Accounts[0].Checking {
		t.Fatal("cached row must show Checking", response.Accounts)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not run")
	}
	// A CLI deliberately remains blocked. Listing and unrelated reads still finish.
	for i := 0; i < 10; i++ {
		start = time.Now()
		if _, err = m.Handle(context.Background(), Request{Action: "list"}); err != nil || time.Since(start) > 150*time.Millisecond {
			t.Fatal("background CLI held list lock")
		}
	}
	start = time.Now()
	if _, err = m.Handle(context.Background(), Request{Action: "search", Query: "nothing"}); err != nil || time.Since(start) > 150*time.Millisecond {
		t.Fatal("cloud refresh blocked other plugins")
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent lists duplicated CLI work", calls.Load())
	}
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case pushed := <-pushes:
			if len(pushed.Accounts) == 1 && !pushed.Accounts[0].Checking {
				if pushed.Accounts[0].AWS.AccountName != "Updated" {
					t.Fatal("stale status pushed")
				}
				return
			}
		case <-deadline:
			t.Fatal("completed statuses were not pushed")
		}
	}
}
func TestCloudRefreshCannotResurrectDeletedOrOverwritePermissions(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	defer m.Close()
	os.WriteFile(config, []byte("[profile deleted]\n[profile changed]\n"), 0600)
	mustHandle(t, m, Request{Action: "list"})
	started, release := make(chan struct{}, 2), make(chan struct{})
	m.cloud.run = func(ctx context.Context, _ string, args, env []string) (cloudOutput, error) {
		if args[0] == "sts" {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return cloudOutput{}, ctx.Err()
			}
		}
		if args[0] == "sts" {
			return cloudOutput{Stdout: `{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:root"}`}, nil
		}
		return cloudOutput{Stdout: `{"AccountAliases":["Updated"]}`}, nil
	}
	m.snapshots.checked = map[string]time.Time{}
	m.cloud.checked = map[string]time.Time{}
	m.Handle(context.Background(), Request{Action: "list"})
	<-started
	<-started
	m.mu.Lock()
	delete(m.records, awsProfileID("deleted"))
	m.records[awsProfileID("changed")].Account.Enabled = true
	m.records[awsProfileID("changed")].Grants = map[string]string{}
	m.mu.Unlock()
	close(release)
	m.snapshots.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records[awsProfileID("deleted")] != nil {
		t.Fatal("background refresh resurrected deleted profile")
	}
	if !m.records[awsProfileID("changed")].Account.Enabled || len(m.records[awsProfileID("changed")].Grants) != 0 {
		t.Fatal("background refresh changed agent permissions")
	}
}
func TestCloudRefreshConcurrencyIsBounded(t *testing.T) {
	m, config, _, _ := testCloudManager(t)
	defer m.Close()
	var profiles strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&profiles, "[profile p%d]\n", i)
	}
	os.WriteFile(config, []byte(profiles.String()), 0600)
	var active, max atomic.Int32
	m.cloud.run = func(ctx context.Context, _ string, args, env []string) (cloudOutput, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for prior := max.Load(); n > prior; prior = max.Load() {
			if max.CompareAndSwap(prior, n) {
				break
			}
		}
		timer := time.NewTimer(5 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
		}
		return cloudOutput{ExitCode: 1}, nil
	}
	m.Handle(context.Background(), Request{Action: "list"})
	m.snapshots.wg.Wait()
	if max.Load() > 4 || max.Load() < 2 {
		t.Fatal("unexpected CLI parallelism", max.Load())
	}
	if len(m.cachedList().Accounts) != 12 {
		t.Fatal("discovered rows not retained")
	}
}
