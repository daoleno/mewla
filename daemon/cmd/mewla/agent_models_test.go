package main

import (
	"reflect"
	"testing"

	"github.com/daoleno/mewla/daemon/agentevents/pi"
	"github.com/daoleno/mewla/daemon/codexctl"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/server"
)

// Each agent's own answer, in its shape, becomes the same menu.

func TestCodexMenuTakesItsListAndThreadEffort(t *testing.T) {
	models := []codexctl.Model{
		{ID: "gpt-6-luna", DisplayName: "GPT-6-Luna", DefaultEffort: "medium", Efforts: []string{"low", "medium", "high"}},
		{ID: "gpt-5.6-terra", DisplayName: "GPT-5.6-Terra", DefaultEffort: "medium", Efforts: []string{"low", "medium"}},
	}
	got := codexAgentModels(models, " gpt-5.6-terra ", "low")
	want := server.AgentModels{Label: "Codex", Current: "gpt-5.6-terra", Effort: "low", Models: []server.AgentModel{
		{ID: "gpt-6-luna", Name: "GPT-6-Luna", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
		{ID: "gpt-5.6-terra", Name: "GPT-5.6-Terra", Efforts: []string{"low", "medium"}, DefaultEffort: "medium"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("menu = %#v, want %#v", got, want)
	}
	// Codex says "none" when the thread keeps the model's own default.
	if got := codexAgentModels(models, "gpt-6-luna", "none"); got.Effort != "" {
		t.Fatalf("effort none = %q, want the model's default", got.Effort)
	}
}

// A `GET /api/model` answer of OpenCode 2, trimmed to the fields Mewla reads.
const opencodeModelList = `{"data":[
 {"id":"tidepool-alpha-free","modelID":"tidepool-alpha-free","providerID":"opencode","name":"Tidepool Alpha Free","status":"active","enabled":true,"variants":[]},
 {"id":"deepseek-v4.1-flash","modelID":"deepseek-v4.1-flash","providerID":"opencode-go","name":"DeepSeek V4.1 Flash","status":"active","enabled":true,
  "variants":[{"id":"default"},{"id":"low"},{"id":"high"}]},
 {"id":"old-model","modelID":"old-model","providerID":"opencode","name":"Old","status":"deprecated","enabled":true},
 {"id":"off-model","modelID":"off-model","providerID":"opencode","name":"Off","status":"active","enabled":false},
 {"id":"no-provider","modelID":"no-provider","name":"Nowhere"},
 {"id":"tidepool-alpha-free","modelID":"tidepool-alpha-free","providerID":"opencode","name":"Tidepool Alpha Free (second sign-in)","status":"active","enabled":true,"variants":[]}
]}`

func TestOpenCodeMenuKeepsUsableModelsAndItsVariants(t *testing.T) {
	models, err := opencodeapi.ParseModels([]byte(opencodeModelList))
	if err != nil {
		t.Fatal(err)
	}
	got := opencodeAgentModels(models, opencodeapi.ModelRef{ID: "deepseek-v4.1-flash", ProviderID: "opencode-go", Variant: "low"})
	want := server.AgentModels{Label: "OpenCode", Current: "opencode-go/deepseek-v4.1-flash", Effort: "low", Models: []server.AgentModel{
		{ID: "opencode/tidepool-alpha-free", Name: "Tidepool Alpha Free"},
		{ID: "opencode-go/deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash", Efforts: []string{"low", "high"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("menu = %#v, want %#v", got, want)
	}
	// A session on the model's own default reports variant "default"; sent
	// back as a variant OpenCode refuses it ("invalid activation selection").
	if got := opencodeAgentModels(models, opencodeapi.ModelRef{ID: "deepseek-v4.1-flash", ProviderID: "opencode-go", Variant: "default"}); got.Effort != "" {
		t.Fatalf("variant default = %q, want the model's default", got.Effort)
	}
	if _, err := opencodeapi.ParseModels([]byte(`{"data":`)); err == nil {
		t.Fatal("a broken answer parsed")
	}
}

// The "models" line Mewla's Pi extension sends back.
const piModelsLine = `{"t":"models","id":"m1","models":{"current":"opencode-go/glm-5.3-flash","thinking":"low","available":[
 {"provider":"opencode-go","id":"glm-5.3-flash","name":"GLM-5.3-Flash","thinking":["off","minimal","low","medium","high"]},
 {"provider":"opencode-go","id":"tiny","name":"Tiny","thinking":["off"]},
 {"provider":"","id":"stray","name":"Stray","thinking":[]}
]}}`

func TestPiMenuNamesModelsTheWayPiDoes(t *testing.T) {
	msg, err := pi.ParseMessage([]byte(piModelsLine))
	if err != nil || msg.Models == nil {
		t.Fatalf("message = %#v, %v", msg, err)
	}
	got := piAgentModels(*msg.Models)
	want := server.AgentModels{Label: "Pi", Current: "opencode-go/glm-5.3-flash", Effort: "low", Models: []server.AgentModel{
		// Pi always thinks at one of its levels: no "Default" is offered
		// (11-24: the sheet sent none, Pi kept its level, and the reply did
		// not echo the choice: "invalid activation selection").
		{ID: "opencode-go/glm-5.3-flash", Name: "GLM-5.3-Flash", Efforts: []string{"off", "minimal", "low", "medium", "high"}, EffortRequired: true},
		// A model whose only level is "off" has no reasoning levels to show.
		{ID: "opencode-go/tiny", Name: "Tiny"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("menu = %#v, want %#v", got, want)
	}
}
