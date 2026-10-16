package computers

import "testing"

func TestPrettyHostname(t *testing.T) {
	for _, tc := range []struct{ contents, want string }{
		{"PRETTY_HOSTNAME=\"daoleno's Laptop\"\n", "daoleno's Laptop"},
		{"CHASSIS=server\nPRETTY_HOSTNAME='Home Server'\n", "Home Server"},
		{"PRETTY_HOSTNAME=Studio\n", "Studio"},
		{"PRETTY_HOSTNAME=\"Say \\\"hi\\\"\"", `Say "hi"`},
		{"  PRETTY_HOSTNAME = \"Spaced\"  ", "Spaced"},
		{"CHASSIS=server\n", ""},
		{"PRETTY_HOSTNAME=\"\"\n", ""},
		{"", ""},
	} {
		if got := prettyHostname(tc.contents); got != tc.want {
			t.Errorf("prettyHostname(%q) = %q, want %q", tc.contents, got, tc.want)
		}
	}
}
