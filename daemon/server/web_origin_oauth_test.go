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
		"http://localhost:9876":      false,
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
