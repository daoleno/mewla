package watcher

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"
)

func TestParseLsofListeningSockets(t *testing.T) {
	// `lsof -nP -iTCP -sTCP:LISTEN -F pfdn`: a process line, then fd, device and
	// name lines per listening socket.
	output := "p1410617\nf22\nd0x2f1c\nn*:3000\np888\nf9\nd0x77a1\nn127.0.0.1:9876\nf10\nd0x77a2\nn[::1]:9877\n" +
		"pnotapid\nf3\nd0x1\nn*:1111\n" +
		"p77\nf18\nd0x99\nn[::]:5173\nf19\nd0x9a\nn*:bad\nf20\nn*:6006\n"
	got := parseLsofListeningSockets(output)
	want := []listeningSocket{
		{pid: 1410617, port: 3000, bind: "*", inode: "0x2f1c"},
		{pid: 888, port: 9876, bind: "127.0.0.1", inode: "0x77a1"},
		{pid: 888, port: 9877, bind: "::1", inode: "0x77a2"},
		{pid: 77, port: 5173, bind: "::", inode: "0x99"},
		// No device field: it must not inherit the previous socket's.
		{pid: 77, port: 6006, bind: "*", inode: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLsofListeningSockets() = %#v, want %#v", got, want)
	}
}

func TestSplitAddressPort(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		wantAddr string
		wantPort int
		wantOK   bool
	}{
		{name: "wildcard", value: "*:3000", wantAddr: "*", wantPort: 3000, wantOK: true},
		{name: "ipv4", value: "127.0.0.1:9876", wantAddr: "127.0.0.1", wantPort: 9876, wantOK: true},
		{name: "bracketed ipv6", value: "[::]:5173", wantAddr: "::", wantPort: 5173, wantOK: true},
		{name: "missing port", value: "127.0.0.1", wantOK: false},
		{name: "bad port", value: "*:abc", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAddr, gotPort, gotOK := splitAddressPort(tt.value)
			if gotAddr != tt.wantAddr || gotPort != tt.wantPort || gotOK != tt.wantOK {
				t.Fatalf("splitAddressPort(%q) = %q, %d, %v; want %q, %d, %v", tt.value, gotAddr, gotPort, gotOK, tt.wantAddr, tt.wantPort, tt.wantOK)
			}
		})
	}
}

func TestBuildServiceURLsForWildcardBind(t *testing.T) {
	interfaces := []SessionServiceInterface{
		{Name: "eth0", Address: "192.168.1.42", Kind: "lan"},
		{Name: "tailscale0", Address: "100.88.1.2", Kind: "tailscale"},
	}

	got := buildServiceURLs([]string{"0.0.0.0"}, 3000, interfaces)
	want := []SessionServiceURL{
		{Label: "LAN", URL: "http://192.168.1.42:3000", Address: "192.168.1.42", Kind: "lan"},
		{Label: "Tailscale", URL: "http://100.88.1.2:3000", Address: "100.88.1.2", Kind: "tailscale"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildServiceURLs() = %#v, want %#v", got, want)
	}
}

func TestBuildServiceURLsSkipsLocalhostOnlyBind(t *testing.T) {
	got := buildServiceURLs(
		[]string{"127.0.0.1"},
		3000,
		[]SessionServiceInterface{{Name: "eth0", Address: "192.168.1.42", Kind: "lan"}},
	)
	if len(got) != 0 {
		t.Fatalf("buildServiceURLs(localhost) = %#v, want empty", got)
	}
}

func TestSessionServiceJSONUsesEmptyArrays(t *testing.T) {
	service := SessionService{
		ID:    "main:@1:123:3000",
		Binds: []string{},
		URLs:  []SessionServiceURL{},
	}

	data, err := json.Marshal(service)
	if err != nil {
		t.Fatalf("json.Marshal(SessionService) error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(SessionService) error = %v", err)
	}
	if _, ok := payload["binds"].([]any); !ok {
		t.Fatalf("SessionService binds JSON = %#v, want array", payload["binds"])
	}
	if _, ok := payload["urls"].([]any); !ok {
		t.Fatalf("SessionService urls JSON = %#v, want array", payload["urls"])
	}
}

func TestPreferServiceInterfacesKeepsTailscaleIPv4(t *testing.T) {
	input := []SessionServiceInterface{
		{Name: "eth0", Address: "192.168.1.42", Kind: "lan"},
		{Name: "tailscale0", Address: "100.88.1.2", Kind: "tailscale"},
		{Name: "tailscale0", Address: "fd7a:115c:a1e0::1", Kind: "tailscale"},
	}

	got := preferServiceInterfaces(input)
	want := []SessionServiceInterface{
		{Name: "eth0", Address: "192.168.1.42", Kind: "lan"},
		{Name: "tailscale0", Address: "100.88.1.2", Kind: "tailscale"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preferServiceInterfaces() = %#v, want %#v", got, want)
	}
}

func TestClassifyServiceAddress(t *testing.T) {
	tests := []struct {
		name  string
		iface string
		ip    string
		want  string
	}{
		{name: "private lan", iface: "eth0", ip: "192.168.1.42", want: "lan"},
		{name: "tailscale address range", iface: "eth0", ip: "100.90.1.2", want: "tailscale"},
		{name: "tailscale interface name", iface: "tailscale0", ip: "fd7a:115c:a1e0::1", want: "tailscale"},
		{name: "public address skipped", iface: "eth0", ip: "8.8.8.8", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyServiceAddress(tt.iface, net.ParseIP(tt.ip))
			if got != tt.want {
				t.Fatalf("classifyServiceAddress(%q, %q) = %q, want %q", tt.iface, tt.ip, got, tt.want)
			}
		})
	}
}

func TestPanesByProcessIncludesDescendants(t *testing.T) {
	processes := map[int]processInfo{
		10: {pid: 10, ppid: 1},
		11: {pid: 11, ppid: 10},
		12: {pid: 12, ppid: 11},
		20: {pid: 20, ppid: 1},
	}
	panes := []servicePane{
		{target: "main:@1", panePID: 10, active: true},
		{target: "main:@2", panePID: 20, active: true},
	}

	got := panesByProcess(processes, panes)
	for _, pid := range []int{10, 11, 12} {
		if got[pid].target != "main:@1" {
			t.Fatalf("panesByProcess()[%d] = %q, want main:@1", pid, got[pid].target)
		}
	}
	if got[20].target != "main:@2" {
		t.Fatalf("panesByProcess()[20] = %q, want main:@2", got[20].target)
	}
}
