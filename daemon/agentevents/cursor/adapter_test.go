package cursor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const chat = "c3af51c5-c8d4-4bdf-ae2c-d224c516d18e"

func fake(id string, err error, calls *int) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) {
		*calls++
		return id, err
	}
}

func TestAFreshLaunchResumesAChatCreatedForIt(t *testing.T) {
	calls := 0
	launch, err := Adapter{NewChat: fake(chat, nil, &calls)}.PrepareLaunch("cursor-agent --force")
	if err != nil || launch.Command != "cursor-agent --force --resume "+chat || calls != 1 {
		t.Fatalf("launch = %+v, %v (%d calls)", launch, err, calls)
	}
}

func TestALaunchThatAlreadyChoosesItsChatOrIsNotAChatIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"cursor-agent --resume " + chat,
		"cursor-agent --resume=" + chat,
		"cursor-agent --continue",
		"cursor-agent resume",
		"cursor-agent ls",
		"cursor-agent create-chat",
		"cursor-agent login",
		"claude --model x",
		"",
	} {
		calls := 0
		launch, _ := Adapter{NewChat: fake(chat, nil, &calls)}.PrepareLaunch(command)
		if launch.Command != command || calls != 0 {
			t.Errorf("%q became %q with %d chats created", command, launch.Command, calls)
		}
	}
}

func TestALaunchIsLeftAloneWhenNoChatCanBeCreated(t *testing.T) {
	calls := 0
	for _, newChat := range []func(context.Context, string) (string, error){
		fake("", errors.New("not signed in"), &calls),
		fake("not-a-uuid", nil, &calls),
		fake(strings.ToUpper(chat), nil, &calls),
	} {
		launch, err := Adapter{NewChat: newChat}.PrepareLaunch("agent")
		if err != nil || launch.Command != "agent" {
			t.Fatalf("launch = %+v, %v", launch, err)
		}
	}
}
