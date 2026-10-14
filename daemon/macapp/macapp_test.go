package macapp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCodesign struct {
	calls  []string
	signed map[string]bool
	fail   bool
}

func (f *fakeCodesign) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if name != "codesign" {
		return nil, nil
	}
	bundle := args[len(args)-1]
	if args[0] == "--verify" {
		if f.signed[bundle] {
			return nil, nil
		}
		return []byte("not signed"), errors.New("exit status 1")
	}
	if f.fail {
		return []byte("no identity"), errors.New("exit status 1")
	}
	// codesign embeds the signature in the main executable.
	program, err := os.OpenFile(Executable(bundle), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	defer program.Close()
	if _, err := program.WriteString(" signed"); err != nil {
		return nil, err
	}
	f.signed[bundle] = true
	return nil, nil
}

func setup(t *testing.T) (string, string, *fakeCodesign) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "mewla")
	if err := os.WriteFile(binary, []byte("build one"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Path(dir), binary, &fakeCodesign{signed: map[string]bool{}}
}

func TestInstallAssemblesAndSignsBundle(t *testing.T) {
	bundle, binary, codesign := setup(t)
	signer := Signer{Identity: "Mewla Dev", Keychain: "/k.keychain-db", Run: codesign.run}

	changed, err := Install(bundle, binary, "1.2.3", []byte("icon"), signer)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	program, _ := os.ReadFile(Executable(bundle))
	if string(program) != "build one signed" {
		t.Fatalf("executable = %q", program)
	}
	if info, _ := os.Stat(Executable(bundle)); info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode = %v", info.Mode())
	}
	if icon, _ := os.ReadFile(IconPath(bundle)); string(icon) != "icon" {
		t.Fatalf("icon = %q", icon)
	}
	info, _ := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	for _, want := range []string{
		"<string>com.daoleno.mewla</string>",
		"<key>CFBundleName</key>\n\t<string>Mewla</string>",
		"<key>CFBundleExecutable</key>\n\t<string>mewla</string>",
		"<key>CFBundleIconFile</key>\n\t<string>Mewla</string>",
		"<string>1.2.3</string>",
		"<key>LSUIElement</key>\n\t<true/>",
		"<key>MewlaInstallStamp</key>",
	} {
		if !bytes.Contains(info, []byte(want)) {
			t.Fatalf("Info.plist lacks %q:\n%s", want, info)
		}
	}
	sign := "codesign --force --sign Mewla Dev --identifier com.daoleno.mewla --keychain /k.keychain-db " + bundle + ".new"
	if !contains(codesign.calls, sign) {
		t.Fatalf("codesign calls = %q", codesign.calls)
	}
	for _, leftover := range []string{bundle + ".new", bundle + ".old"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", leftover)
		}
	}
}

func TestInstallSkipsUnchangedSignedBundle(t *testing.T) {
	bundle, binary, codesign := setup(t)
	signer := Signer{Run: codesign.run}
	if _, err := Install(bundle, binary, "1", nil, signer); err != nil {
		t.Fatal(err)
	}
	codesign.signed[bundle] = true

	changed, err := Install(bundle, binary, "1", nil, signer)
	if err != nil || changed {
		t.Fatalf("second Install = %v, %v", changed, err)
	}

	// A different signing identity re-signs the bundle.
	changed, err = Install(bundle, binary, "1", nil, Signer{Identity: "Mewla Dev", Run: codesign.run})
	if err != nil || !changed {
		t.Fatalf("Install with a new identity = %v, %v", changed, err)
	}
}

func TestInstallReplacesChangedBinary(t *testing.T) {
	bundle, binary, codesign := setup(t)
	signer := Signer{Run: codesign.run}
	if _, err := Install(bundle, binary, "1", nil, signer); err != nil {
		t.Fatal(err)
	}
	codesign.signed[bundle] = true
	if err := os.WriteFile(binary, []byte("build two"), 0o755); err != nil {
		t.Fatal(err)
	}

	changed, err := Install(bundle, binary, "1", nil, signer)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	if program, _ := os.ReadFile(Executable(bundle)); string(program) != "build two signed" {
		t.Fatalf("executable = %q", program)
	}
	if !contains(codesign.calls, "codesign --force --sign - --identifier com.daoleno.mewla "+bundle+".new") {
		t.Fatalf("ad hoc signing missing: %q", codesign.calls)
	}
}

func TestInstallKeepsOldBundleWhenSigningFails(t *testing.T) {
	bundle, binary, codesign := setup(t)
	if _, err := Install(bundle, binary, "1", nil, Signer{Run: codesign.run}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("build two"), 0o755); err != nil {
		t.Fatal(err)
	}
	codesign.fail = true

	if _, err := Install(bundle, binary, "1", nil, Signer{Run: codesign.run}); err == nil {
		t.Fatal("Install succeeded with failing codesign")
	}
	if program, _ := os.ReadFile(Executable(bundle)); string(program) != "build one signed" {
		t.Fatalf("executable = %q", program)
	}
	if _, err := os.Stat(bundle + ".new"); !os.IsNotExist(err) {
		t.Fatal("partial bundle left behind")
	}
}

func TestForeignBundleIsLeftAlone(t *testing.T) {
	bundle, binary, codesign := setup(t)
	foreign := filepath.Join(bundle, "Contents", "Info.plist")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("<plist><dict/></plist>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(bundle, binary, "1", nil, Signer{Run: codesign.run}); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("Install err = %v", err)
	}
	if err := Remove(bundle); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("Remove err = %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign bundle was touched")
	}
}

func TestRemoveDeletesManagedBundle(t *testing.T) {
	bundle, binary, codesign := setup(t)
	if _, err := Install(bundle, binary, "1", nil, Signer{Run: codesign.run}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(bundle); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundle); !os.IsNotExist(err) {
		t.Fatal("bundle still exists")
	}
	if err := Remove(bundle); err != nil {
		t.Fatalf("Remove of missing bundle = %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
