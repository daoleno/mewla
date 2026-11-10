package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type setupRemoteRecorder struct {
	calls    []string
	signedIn bool
	phones   []int // one count per call
	agent    bool
	failAt   string
	clock    time.Time
}

func (r *setupRemoteRecorder) steps() setupRemoteSteps {
	call := 0
	fail := func(name string) error {
		r.calls = append(r.calls, name)
		if r.failAt == name {
			return errors.New(name + " failed")
		}
		return nil
	}
	return setupRemoteSteps{
		signedIn: func() bool { return r.signedIn },
		login:    func(context.Context) error { return fail("login") },
		service: func() (string, error) {
			err := fail("service")
			return "service note", err
		},
		pair: func() error { return fail("pair") },
		phones: func() (int, error) {
			r.calls = append(r.calls, "phones")
			index := call
			if index >= len(r.phones) {
				index = len(r.phones) - 1
			}
			call++
			return r.phones[index], nil
		},
		agentReady: func() bool { return r.agent },
		sleep:      func(d time.Duration) { r.clock = r.clock.Add(d) },
		now:        func() time.Time { return r.clock },
	}
}

func TestSetupRemoteRunsStepsInOrderAndWaitsForPhone(t *testing.T) {
	r := &setupRemoteRecorder{phones: []int{0, 0, 0, 1}, agent: true, clock: time.Unix(0, 0)}
	var out bytes.Buffer
	err := setupRemote(context.Background(), setupRemoteConfig{wait: true, waitFor: time.Minute}, r.steps(), &out)
	if err != nil {
		t.Fatalf("setupRemote: %v\n%s", err, out.String())
	}
	if got := strings.Join(r.calls, ","); !strings.HasPrefix(got, "login,service,phones,pair,phones") {
		t.Fatalf("order = %s; want login, service, count phones, pair, then poll", got)
	}
	if !strings.Contains(out.String(), "Phone paired") || !strings.Contains(out.String(), "first message") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestSetupRemoteSkipsSignInWhenAlreadyOn(t *testing.T) {
	r := &setupRemoteRecorder{signedIn: true, phones: []int{0}, agent: true, clock: time.Unix(0, 0)}
	var out bytes.Buffer
	if err := setupRemote(context.Background(), setupRemoteConfig{wait: false}, r.steps(), &out); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.calls {
		if call == "login" {
			t.Fatalf("signed-in computer asked to sign in again: %v", r.calls)
		}
	}
}

func TestSetupRemoteStopsOnFirstFailureAndSaysHowToResume(t *testing.T) {
	for _, failAt := range []string{"login", "service", "pair"} {
		r := &setupRemoteRecorder{phones: []int{0}, failAt: failAt, clock: time.Unix(0, 0)}
		var out bytes.Buffer
		if err := setupRemote(context.Background(), setupRemoteConfig{wait: true, waitFor: time.Minute}, r.steps(), &out); err == nil {
			t.Fatalf("%s failure was swallowed", failAt)
		}
		if last := r.calls[len(r.calls)-1]; last != failAt {
			t.Fatalf("%s: steps continued past the failure: %v", failAt, r.calls)
		}
		if !strings.Contains(out.String(), "Setup stopped") {
			t.Fatalf("%s: no resume hint:\n%s", failAt, out.String())
		}
	}
}

func TestSetupRemoteTimesOutWithoutPhoneAndNamesMissingAgent(t *testing.T) {
	r := &setupRemoteRecorder{signedIn: true, phones: []int{0}, agent: false, clock: time.Unix(0, 0)}
	var out bytes.Buffer
	err := setupRemote(context.Background(), setupRemoteConfig{wait: true, waitFor: 10 * time.Second}, r.steps(), &out)
	if !errors.Is(err, errSetupRemoteIncomplete) {
		t.Fatalf("err = %v; want errSetupRemoteIncomplete", err)
	}
	if !strings.Contains(out.String(), "expired") {
		t.Fatalf("output:\n%s", out.String())
	}

	r = &setupRemoteRecorder{signedIn: true, phones: []int{0, 1}, agent: false, clock: time.Unix(0, 0)}
	out.Reset()
	if err := setupRemote(context.Background(), setupRemoteConfig{wait: true, waitFor: time.Minute}, r.steps(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "deps") || strings.Contains(out.String(), "-only") {
		t.Fatalf("a computer with no signed-in agent was not told the next step:\n%s", out.String())
	}
}
