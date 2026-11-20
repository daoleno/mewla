package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/addressbook"
	"rsc.io/qr/coding"
)

func TestPairJSONGivesThePhoneQRAndThisComputersLink(t *testing.T) {
	stateDir := shortControlStateDir(t)
	book, err := addressbook.New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"http://127.0.0.1:9876", "http://192.168.1.30:9876"} {
		if _, err := book.Add(address, addressbook.SourceDiscovered); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	if err := runPair(pairConfig{stateDir: stateDir, endpoint: "http://192.168.1.20:9876", json: true, stdout: &stdout}, io.Discard); err != nil {
		t.Fatalf("pair -json: %v", err)
	}
	var out pairingJSON
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("pair -json printed %q: %v", stdout.String(), err)
	}
	if !strings.HasPrefix(out.PhoneLink, "mewla://settings?p=") || out.PhoneAddress != "http://192.168.1.20:9876" {
		t.Fatalf("phone link %q at %q", out.PhoneLink, out.PhoneAddress)
	}
	if !strings.HasPrefix(out.LocalLink, "mewla://settings?p=") || out.LocalLink == out.PhoneLink {
		t.Fatalf("local link %q", out.LocalLink)
	}
	if out.DaemonID == "" || out.DaemonPublicKey == "" || out.ExpiresAt.IsZero() {
		t.Fatalf("identity %+v", out)
	}
	if out.WifiAddress != "http://192.168.1.30:9876" || !strings.HasPrefix(out.WifiLink, "mewla://settings?p=") || out.WifiQR == nil {
		t.Fatalf("wi-fi %q %q", out.WifiAddress, out.WifiLink)
	}
	code, err := encodeQRAt(out.PhoneLink, coding.Q)
	if err != nil {
		t.Fatal(err)
	}
	if out.PhoneQR == nil || out.PhoneQR.Size != code.Size || len(out.PhoneQR.Rows) != code.Size {
		t.Fatalf("qr %+v, want %d modules", out.PhoneQR, code.Size)
	}
	for y, row := range out.PhoneQR.Rows {
		for x, module := range row {
			if (module == '1') != code.Black(x, y) {
				t.Fatalf("module %d,%d differs", x, y)
			}
		}
	}
}

func TestAppCLIDirIsOnlyTheMacAppsHelpers(t *testing.T) {
	for executable, want := range map[string]string{
		"/Applications/Mewla.app/Contents/Helpers/mewla":          "/Applications/Mewla.app/Contents/Helpers",
		"/Users/me/Applications/Mewla.app/Contents/MacOS/mewla":   "",
		"/Users/me/.local/bin/mewla":                              "",
		"/Applications/Mewla.app/Contents/Helpers/mewla-launcher": "",
	} {
		if got := appCLIDir(executable); got != want {
			t.Fatalf("appCLIDir(%q) = %q, want %q", executable, got, want)
		}
	}
}
