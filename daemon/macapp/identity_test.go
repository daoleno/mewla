package macapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSecurity models `security` and openssl: create-keychain makes the
// file, import makes the identity findable.
type fakeSecurity struct {
	calls    []string
	imported bool
}

func (f *fakeSecurity) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch {
	case name == "security" && args[0] == "create-keychain":
		return nil, os.WriteFile(args[len(args)-1], nil, 0o600)
	case name == "security" && args[0] == "import":
		f.imported = true
	case name == "security" && args[0] == "find-certificate":
		if !f.imported {
			return []byte("security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain."), errors.New("exit status 44")
		}
		return []byte("SHA-1 hash: 6BDB705C42A0A3258558E5D2F5BA0E7629A5B750\nSHA-256 hash: 00\n"), nil
	case name == openssl:
		// The key and certificate are on disk only while being packed.
		for i, arg := range args {
			if arg == "-inkey" || arg == "-in" {
				if _, err := os.Stat(args[i+1]); err != nil {
					return nil, err
				}
			}
		}
	}
	return nil, nil
}

func (f *fakeSecurity) count(prefix string) int {
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

func TestLocalSignerCreatesTheIdentityOnce(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(IdentityKeychain(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSecurity{}
	signer, err := LocalSigner(home, fake.run)
	if err != nil {
		t.Fatal(err)
	}
	if signer.Identity != "6BDB705C42A0A3258558E5D2F5BA0E7629A5B750" || signer.Keychain != IdentityKeychain(home) || signer.Password == "" {
		t.Fatalf("signer = %+v", signer)
	}
	password := filepath.Join(IdentityDir(home), "keychain-password")
	if info, err := os.Stat(password); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("password file: %v %v", info, err)
	}
	if info, _ := os.Stat(IdentityDir(home)); info.Mode().Perm() != 0o700 {
		t.Fatalf("identity dir mode %v", info.Mode())
	}
	if entries, _ := os.ReadDir(IdentityDir(home)); len(entries) != 1 {
		t.Fatalf("key material left behind: %v", entries)
	}
	for _, want := range []string{
		"security create-keychain -p " + signer.Password + " " + signer.Keychain,
		"security import ",
		"security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k " + signer.Password + " " + signer.Keychain,
	} {
		if fake.count(want) != 1 {
			t.Fatalf("want one %q in %q", want, fake.calls)
		}
	}

	again, err := LocalSigner(home, fake.run)
	if err != nil || again.Identity != signer.Identity || again.Password != signer.Password {
		t.Fatalf("second LocalSigner = %+v, %v", again, err)
	}
	if fake.count("security create-keychain") != 1 || fake.count("security import") != 1 || fake.count(openssl) != 1 {
		t.Fatalf("identity was made twice: %q", fake.calls)
	}
}

func TestSignerUnlocksTheKeychain(t *testing.T) {
	bundle, binary, codesign := setup(t)
	signer := Signer{Identity: "ABC", Keychain: "/k.keychain-db", Password: "pw", Run: codesign.run}
	if _, err := Install(bundle, binary, "1", nil, signer); err != nil {
		t.Fatal(err)
	}
	unlock := "security unlock-keychain -p pw /k.keychain-db"
	sign := "codesign --force --sign ABC --identifier com.daoleno.mewla --keychain /k.keychain-db " + bundle + ".new"
	if len(codesign.calls) < 2 || codesign.calls[len(codesign.calls)-2] != unlock || codesign.calls[len(codesign.calls)-1] != sign {
		t.Fatalf("calls = %q", codesign.calls)
	}
}

func TestSigningFailureIsMarked(t *testing.T) {
	bundle, binary, codesign := setup(t)
	codesign.fail = true
	if _, err := Install(bundle, binary, "1", nil, Signer{Run: codesign.run}); !errors.Is(err, ErrSign) {
		t.Fatalf("err = %v", err)
	}
}
