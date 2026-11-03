package modelprofiles

import "testing"

func TestSeparateSignInBypassesTheMachineProviderRoute(t *testing.T) {
	for command, want := range map[string]bool{
		"claude":                                false,
		"env CLAUDE_CONFIG_DIR=/a/b claude":     true,
		"env CODEX_HOME=/a/b codex":             true,
		"env CLAUDE_CONFIG_DIR=/a/b claude --x": true,
	} {
		if got := shouldBypassProfiles("claude", "", command); got != want {
			t.Errorf("%q bypass = %v, want %v", command, got, want)
		}
	}
	if shouldBypassProfiles("claude", "chosen-profile", "env CLAUDE_CONFIG_DIR=/a/b claude") {
		t.Error("an explicit profile choice must still apply to a separate sign-in")
	}
}
