package remote

import "testing"

func TestServiceURLDefaultsToHostedService(t *testing.T) {
	t.Setenv("MEWLA_REMOTE_SERVICE_URL", "")
	if got := ServiceURL(); got != "https://connect.mewla.ai" {
		t.Fatalf("default service = %q", got)
	}
	t.Setenv("MEWLA_REMOTE_SERVICE_URL", "http://127.0.0.1:18090/")
	if got := ServiceURL(); got != "http://127.0.0.1:18090" {
		t.Fatalf("override = %q", got)
	}
}
