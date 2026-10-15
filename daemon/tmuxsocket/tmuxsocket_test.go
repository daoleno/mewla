package tmuxsocket

import (
	"reflect"
	"testing"
)

func TestArgsPinExactServerWithoutAutostart(t *testing.T) {
	if got := Args(""); got != nil {
		t.Fatalf("empty socket args = %#v, want nil (default server)", got)
	}
	if got := Args("  "); got != nil {
		t.Fatalf("blank socket args = %#v, want nil (default server)", got)
	}
	socket := "/run/user/1000/tmux-1000/default"
	if got := Args(socket); !reflect.DeepEqual(got, []string{"-S", socket, "-N"}) {
		t.Fatalf("socket args = %#v, want [-S socket -N]", got)
	}
}

func TestClientArgsAlwaysForceUTF8Client(t *testing.T) {
	if got := ClientArgs(""); !reflect.DeepEqual(got, []string{"-u"}) {
		t.Fatalf("default server client args = %#v, want [-u]", got)
	}
	socket := "/run/user/1000/tmux-1000/default"
	if got := ClientArgs(socket); !reflect.DeepEqual(got, []string{"-u", "-S", socket, "-N"}) {
		t.Fatalf("socket client args = %#v, want [-u -S socket -N]", got)
	}
}
