package modelprofiles

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles/translate"
)

func TestFailoverRest(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		header http.Header
		body   string
		err    error
		base   time.Duration
		ok     bool
	}{
		{"unreachable", 0, nil, "", syscall.ECONNREFUSED, restBusy, true},
		{"agent left", 0, nil, "", errors.Join(errors.New("x"), contextCanceled()), 0, false},
		{"busy", 503, nil, "", nil, restBusy, true},
		{"overloaded", 529, nil, "", nil, restBusy, true},
		{"rate limit says when", 429, http.Header{"Retry-After": {"90"}}, "", nil, 90 * time.Second, true},
		{"rate limit", 429, nil, "", nil, restRate, true},
		{"balance", 400, nil, `{"error":{"message":"余额不足"}}`, nil, restQuota, true},
		{"payment", 402, nil, "", nil, restQuota, true},
		{"key refused", 401, nil, "", nil, restRefused, true},
		{"bad request", 400, nil, `{"error":{"message":"messages: field required"}}`, nil, 0, false},
		{"too long", 413, nil, "", nil, 0, false},
	} {
		base, _, ok := failoverRest(c.status, c.header, []byte(c.body), c.err)
		if ok != c.ok || base != c.base {
			t.Errorf("%s: got %v %v, want %v %v", c.name, base, ok, c.base, c.ok)
		}
	}
}

func contextCanceled() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx.Err()
}

func TestConnectionRestsGrowAndClear(t *testing.T) {
	now := time.Unix(1000, 0)
	r := newConnectionRests()
	r.now = func() time.Time { return now }
	if got := r.fail("a", restBusy, restLongest); got != 30*time.Second {
		t.Fatalf("first rest %v", got)
	}
	// A request that was already on its way fails during the rest: the same
	// outage, so the rest runs from it but does not double.
	now = now.Add(2 * time.Second)
	if got := r.fail("a", restBusy, restLongest); got != restBusy {
		t.Fatalf("a failure during the rest made it %v", got)
	}
	now = now.Add(31 * time.Second)
	if got := r.fail("a", restBusy, restLongest); got != time.Minute {
		t.Fatalf("second rest %v", got)
	}
	for i := 0; i < 10; i++ {
		now = now.Add(restLongest)
		r.fail("a", restBusy, restLongest)
	}
	now = now.Add(restLongest)
	if got := r.fail("a", restBusy, restLongest); got != restLongest {
		t.Fatalf("rest grew to %v, want at most %v", got, restLongest)
	}
	// A rate limit naming a longer wait than the rest left lengthens it.
	if got := r.fail("a", 20*time.Minute, 20*time.Minute); got != 20*time.Minute {
		t.Fatalf("a longer Retry-After during the rest gave %v", got)
	}
	if !r.resting("A") {
		t.Fatalf("connection ids are not case-sensitive")
	}
	r.ok("a")
	if r.resting("a") || r.fail("a", restBusy, restLongest) != restBusy {
		t.Fatalf("a success must clear the rest and its count")
	}
	now = now.Add(31 * time.Second)
	if r.resting("a") {
		t.Fatalf("the rest is over")
	}
}

// failingUpstream answers every request to its API path with one status,
// and lists models.
type failingUpstream struct {
	status int
	models string
	mu     sync.Mutex
	calls  int
}

func (u *failingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		_, _ = io.WriteString(w, u.models)
		return
	}
	body, _ := io.ReadAll(r.Body)
	if strings.TrimSpace(string(body)) == "{}" {
		http.Error(w, `{"error":{"message":"model: field required"}}`, http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.calls++
	u.mu.Unlock()
	http.Error(w, `{"error":{"message":"down for maintenance"}}`, u.status)
}

func (u *failingUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func chatReply(text string) string {
	return "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" + text + "\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
}

func TestAClaudeSessionMovesToItsFallbackAndBack(t *testing.T) {
	stubRetrySleep(t)
	primary := &failingUpstream{status: 503, models: `{"data":[{"id":"claude-opus-5-5"},{"id":"claude-haiku-5-5"}]}`}
	psrv := httptest.NewServer(primary)
	defer psrv.Close()
	fallback := &oneAPIUpstream{path: "/v1/chat/completions", models: `{"data":[{"id":"glm-5"}]}`, reply: chatReply("from fallback")}
	fsrv := httptest.NewServer(fallback)
	defer fsrv.Close()

	owner := detectingOwner(t)
	now := time.Unix(5000, 0)
	owner.failover.rests.now = func() time.Time { return now }
	fb := addConnection(t, owner, ClientClaude, "Chat Spare", fsrv.URL+"/v1", "glm-5")
	main := addConnection(t, owner, ClientClaude, "Main Relay", psrv.URL, "claude-opus-5-5")
	if _, err := owner.SetProviderFallback(main.ID, fb.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunchModel(ExecutorClaude, "", "claude-opus-5-5", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "failover-fixture"); err != nil {
		t.Fatal(err)
	}
	root := plan.Env[EnvAnthropicBaseURL]
	ask := func() (int, string) {
		return post(t, root+"/v1/messages", `{"model":"claude-opus-5-5","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"ping"}]}`, nil)
	}

	status, body := ask()
	if status != 200 || !strings.Contains(body, "from fallback") || !strings.Contains(body, "event: message_start") {
		t.Fatalf("status=%d body=%s: want the fallback's answer in Claude's shape", status, body)
	}
	if primary.count() != 3 {
		t.Fatalf("primary asked %d times, want 3 (the first try and two retries)", primary.count())
	}
	if sent, _ := fallback.last(); !strings.Contains(sent, `"model":"glm-5"`) {
		t.Fatalf("fallback got %s: a Claude id it does not list goes as a model it serves", sent)
	}

	// While it rests, its requests go straight to the fallback.
	if status, _ := ask(); status != 200 || primary.count() != 3 {
		t.Fatalf("status=%d primary=%d: a resting connection is not asked", status, primary.count())
	}
	// When the rest is over it is asked again.
	now = now.Add(restBusy + time.Second)
	_, _ = ask()
	if primary.count() != 6 {
		t.Fatalf("primary asked %d times after its rest, want 6", primary.count())
	}
}

func TestAFailureThatIsTheRequestsOwnStays(t *testing.T) {
	stubRetrySleep(t)
	primary := &failingUpstream{status: 400, models: `{"data":[{"id":"claude-opus-5-5"}]}`}
	psrv := httptest.NewServer(primary)
	defer psrv.Close()
	fallback := &oneAPIUpstream{path: "/v1/chat/completions", models: `{"data":[{"id":"glm-5"}]}`, reply: chatReply("x")}
	fsrv := httptest.NewServer(fallback)
	defer fsrv.Close()
	owner := detectingOwner(t)
	fb := addConnection(t, owner, ClientClaude, "Chat Spare", fsrv.URL+"/v1", "glm-5")
	main := addConnection(t, owner, ClientClaude, "Main Relay", psrv.URL, "claude-opus-5-5")
	if _, err := owner.SetProviderFallback(main.ID, fb.ID); err != nil {
		t.Fatal(err)
	}
	plan, _ := owner.PrepareLaunchModel(ExecutorClaude, "", "claude-opus-5-5", "claude")
	_, _, _, _ = owner.CommitLaunch(plan.ProvisionalID, "stay-fixture")
	status, body := post(t, plan.Env[EnvAnthropicBaseURL]+"/v1/messages", `{"model":"claude-opus-5-5","max_tokens":5,"messages":[{"role":"user","content":"x"}]}`, nil)
	if status != 400 || !strings.Contains(body, "down for maintenance") || len(fallback.bodies) != 0 || owner.failover.rests.resting(main.ID) {
		t.Fatalf("status=%d body=%s fallback=%d: a bad request fails everywhere and is not moved", status, body, len(fallback.bodies))
	}
}

func TestTwoEndpointsOfTheAgentsOwnAPIAreNotPaired(t *testing.T) {
	stubRetrySleep(t)
	primary := &failingUpstream{status: 503, models: `{"data":[{"id":"claude-opus-5-5"}]}`}
	psrv := httptest.NewServer(primary)
	defer psrv.Close()
	other := &failingUpstream{status: 200, models: `{"data":[{"id":"claude-opus-5-5"}]}`}
	osrv := httptest.NewServer(other)
	defer osrv.Close()
	owner := detectingOwner(t)
	fb := addConnection(t, owner, ClientClaude, "Other Anthropic", osrv.URL, "claude-opus-5-5")
	main := addConnection(t, owner, ClientClaude, "Main Relay", psrv.URL, "claude-opus-5-5")
	if _, err := owner.SetProviderFallback(main.ID, fb.ID); err == nil {
		t.Fatalf("the pair was accepted")
	}
	// A pair saved while one of them still looked translated (its API was
	// found again later) is not moved either.
	saved, err := owner.store.Get(main.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.Fallback = fb.ID
	if _, err := owner.store.Update(saved, owner.store.Revision()); err != nil {
		t.Fatal(err)
	}
	plan, _ := owner.PrepareLaunchModel(ExecutorClaude, "", "claude-opus-5-5", "claude")
	_, _, _, _ = owner.CommitLaunch(plan.ProvisionalID, "pair-fixture")
	status, _ := post(t, plan.Env[EnvAnthropicBaseURL]+"/v1/messages", `{"model":"claude-opus-5-5","max_tokens":5,"messages":[{"role":"user","content":"x"}]}`, nil)
	if status != 503 || other.count() != 0 {
		t.Fatalf("status=%d other=%d: each would refuse the thinking the other signed", status, other.count())
	}
}

func TestCodexOnTheGatewayMovesToAnAnthropicFallback(t *testing.T) {
	stubRetrySleep(t)
	anth := &oneAPIUpstream{
		path:   "/v1/messages",
		models: `{"data":[{"id":"claude-sonnet-5"}]}`,
		reply: "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n" +
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"spare\"}}\n\n" +
			"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"data: {\"type\":\"message_stop\"}\n\n",
	}
	asrv := httptest.NewServer(anth)
	defer asrv.Close()
	owner := detectingOwner(t)
	fb := addConnection(t, owner, ClientCodex, "Anthropic Spare", asrv.URL+"/v1", "claude-sonnet-5")
	// Nothing listens here: the main connection cannot be reached.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	cat, err := owner.UpsertProviderConnection(ProviderConnectionInput{Name: "Main", Client: ClientCodex, PresetID: ProviderPresetCustom, BaseURL: deadURL + "/v1", ModelID: "gpt-5.5", Advanced: true}, "fixture-key-not-real", owner.Catalog().Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	var mainID string
	for _, c := range cat.Connections {
		if c.Name == "Main" {
			mainID = c.ID
		}
	}
	if _, err := owner.SetProviderConnection(ClientCodex, mainID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderFallback(mainID, fb.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3425/v1/responses",
		strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ping"}]}]}`))
	req.RemoteAddr = "127.0.0.1:1234"
	res := httptest.NewRecorder()
	owner.Gateway().ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"delta":"spare"`) || !strings.Contains(res.Body.String(), "response.completed") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if sent, _ := anth.last(); !strings.Contains(sent, `"model":"claude-sonnet-5"`) {
		t.Fatalf("fallback got %s: Codex's model goes as the fallback's own", sent)
	}
}

func TestSetProviderFallbackRules(t *testing.T) {
	models := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"m"}]}`)
	}))
	defer models.Close()
	owner, _ := newDurableOwner(t, t.TempDir())
	defer owner.Close()
	a := addConnection(t, owner, ClientClaude, "A", models.URL, "m")
	b := addConnection(t, owner, ClientClaude, "B", models.URL, "m")
	c := addConnection(t, owner, ClientCodex, "C", models.URL+"/v1", "m")
	if _, err := owner.SetProviderFallback(a.ID, a.ID); err == nil {
		t.Fatalf("a connection was made its own fallback")
	}
	if _, err := owner.SetProviderFallback(a.ID, c.ID); err == nil {
		t.Fatalf("a Codex connection was made a Claude connection's fallback")
	}
	// Two Anthropic endpoints for Claude Code: send would never move a
	// request between them, so the pair is refused when it is set.
	if _, err := owner.SetProviderFallback(a.ID, b.ID); err == nil || !strings.Contains(err.Error(), "own API") {
		t.Fatalf("two connections of the agent's own API were paired: %v", err)
	}
	// B turns out to speak Chat: now it may take A's requests.
	chat, err := owner.store.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	chat.UpstreamAPI = ProtocolOpenAIChat
	if _, err := owner.store.Update(chat, owner.store.Revision()); err != nil {
		t.Fatal(err)
	}
	proj, err := owner.SetProviderFallback(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range proj.Connections {
		if conn.ID == a.ID && conn.FallbackID != b.ID {
			t.Fatalf("projection fallback = %q", conn.FallbackID)
		}
	}
	// An edit keeps it; deleting the fallback clears it.
	if _, err := owner.UpsertProviderConnection(ProviderConnectionInput{ID: a.ID, Name: "A2", Client: ClientClaude, PresetID: ProviderPresetCustom, BaseURL: models.URL, ModelID: "m", Advanced: true}, "", owner.Catalog().Revision, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := owner.store.Get(a.ID); p.Fallback != b.ID {
		t.Fatalf("an edit lost the fallback: %q", p.Fallback)
	}
	if _, err := owner.SetProviderConnection(ClientClaude, a.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.DeleteProviderConnection(b.ID, owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if p, _ := owner.store.Get(a.ID); p.Fallback != "" {
		t.Fatalf("fallback %q outlived its connection", p.Fallback)
	}
}

// A fallback with no model of the person's and no model list yet takes the
// agent's own model, not its client default (gpt-5 for Codex): seen on the
// owner's Mac, where a fresh daemon had not listed the fallback's models.
func TestAFallbackWithoutAModelKeepsTheAgents(t *testing.T) {
	owner, _ := newDurableOwner(t, t.TempDir())
	defer owner.Close()
	ids := map[string]string{}
	for _, name := range []string{"Main", "Spare"} {
		cat, err := owner.UpsertProviderConnection(ProviderConnectionInput{
			Name: name, Client: ClientCodex, PresetID: ProviderPresetCustom, BaseURL: "http://127.0.0.1:9/v1", Advanced: true,
		}, "fixture-key-not-real", owner.Catalog().Revision, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cat.Connections {
			if c.Name == name {
				ids[name] = c.ID
			}
		}
	}
	spare, err := owner.store.Get(ids["Spare"])
	if err != nil {
		t.Fatal(err)
	}
	spare.UpstreamAPI = ProtocolAnthropicMessages
	if _, err := owner.store.Update(spare, owner.store.Revision()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderFallback(ids["Main"], ids["Spare"]); err != nil {
		t.Fatal(err)
	}
	target, ok := owner.failoverFor(ids["Main"])
	if !ok {
		t.Fatal("no fallback")
	}
	if target.Model != "" || len(target.Listed) != 0 {
		t.Fatalf("model=%q listed=%v", target.Model, target.Listed)
	}
	if got := failoverModel(translate.APIResponses, "claude-sonnet-5", target); got != "claude-sonnet-5" {
		t.Fatalf("the request went as %q", got)
	}
}

// A fallback that lists models but not the one asked for gets one it serves.
func TestFailoverModelIsOneTheFallbackServes(t *testing.T) {
	glm := failoverTarget{Listed: []string{"glm-5", "glm-5-air"}}
	for _, tc := range []struct {
		api, asked string
		target     failoverTarget
		want       string
	}{
		{translate.APIResponses, "glm-5-air", glm, "glm-5-air"},                                                // listed
		{translate.APIResponses, "gpt-6", glm, "glm-5"},                                                        // not listed, nothing set
		{translate.APIResponses, "gpt-6", failoverTarget{Model: "glm-5-air", Listed: glm.Listed}, "glm-5-air"}, // the person's
		{translate.APIAnthropic, "claude-opus-5-5", glm, "glm-5"},                                              // a Claude stand-in
		{translate.APIAnthropic, "claude-haiku-5-5", failoverTarget{Listed: []string{"claude-haiku-4-5", "glm-5"}}, "claude-haiku-4-5"},
		{translate.APIResponses, "claude-sonnet-5", failoverTarget{}, "claude-sonnet-5"}, // nothing listed yet
	} {
		if got := failoverModel(tc.api, tc.asked, tc.target); got != tc.want {
			t.Errorf("%s %q → %q, want %q", tc.api, tc.asked, got, tc.want)
		}
	}
}
