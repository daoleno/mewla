package server

import "testing"

func TestServesWebOriginForPluginSignIn(t *testing.T) {
	s := &Server{}
	s.SetAddressBook(testAddressBook(t, "https://mewla.example"))
	for origin, want := range map[string]bool{
		"https://mewla.example":      true,
		"http://127.0.0.1:9876":      true,
		"http://[::1]:9876":          true,
		"https://evil.example":       false,
		"http://mewla.example":       false,
		"http://localhost:9876":      true,
		"http://127.0.0.2:9876":      true,
		"http://LOCALHOST:9876":      false,
		"http://localhost":           false,
		"http://localhost.evil:9876": false,
		"http://192.168.1.20:9876":   false,
		"https://mewla.example/":     false,
		"https://mewla.example/x":    false,
		"https://MEWLA.example":      false,
		"https://user@mewla.example": false,
		"https://mewla.example?x=1":  false,
		"":                           false,
	} {
		if got := s.servesWebOrigin(origin); got != want {
			t.Errorf("servesWebOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}

// Once the daemon has bound its port, a loopback web UI is its own only on that
// port: another local program's page cannot take a sign-in's return (11-13).
func TestServesLoopbackWebOriginOnlyOnItsOwnPort(t *testing.T) {
	s := &Server{}
	s.SetAddressBook(testAddressBook(t, "https://mewla.example"))
	s.webPort.Store(9876)
	for origin, want := range map[string]bool{
		"http://localhost:9876": true,
		"http://127.0.0.1:9876": true,
		"http://[::1]:9876":     true,
		"http://localhost:3000": false,
		"http://127.0.0.1:9877": false,
		"https://mewla.example": true,
		"https://evil.example":  false,
	} {
		if got := s.servesWebOrigin(origin); got != want {
			t.Errorf("servesWebOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}
