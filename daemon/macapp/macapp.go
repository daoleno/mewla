// Package macapp assembles Mewla.app, the bundle the macOS daemon runs from.
// macOS names an app in privacy settings, prompts and dialogs after its
// bundle, so a bare executable shows up as a lowercase "mewla" with a generic
// icon. `mewla boot install` copies this executable into the bundle, signs it
// on the Mac and points launchd at it; the CLI keeps running from wherever it
// was installed.
package macapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BundleID   = "com.daoleno.mewla"
	Name       = "Mewla"
	executable = "mewla"
	iconName   = "Mewla"
	// managedKey marks a bundle this package created; anything else at the
	// same path is never replaced or removed.
	managedKey = "MewlaManagedBundle"
	// stampKey records what the bundle was built from: codesign rewrites
	// the executable, so it can't be compared with the source binary.
	stampKey = "MewlaInstallStamp"
)

// Path is where the bundle lives: the user's own Applications folder, so no
// administrator rights are needed and the privacy settings' file picker can
// find it.
func Path(home string) string { return filepath.Join(home, "Applications", Name+".app") }

// Executable is the daemon executable inside a bundle.
func Executable(bundle string) string { return filepath.Join(bundle, "Contents", "MacOS", executable) }

// IconPath is the bundle's icon file.
func IconPath(bundle string) string {
	return filepath.Join(bundle, "Contents", "Resources", iconName+".icns")
}

// Signer signs the assembled bundle with codesign. An empty Identity signs
// ad hoc, which macOS ties to this exact build; a fixed certificate keeps
// privacy grants across updates.
type Signer struct {
	Identity string
	Keychain string
	Run      func(name string, args ...string) ([]byte, error)
}

// SignerFromEnv reads MEWLA_CODESIGN_IDENTITY and MEWLA_CODESIGN_KEYCHAIN.
func SignerFromEnv(run func(name string, args ...string) ([]byte, error)) Signer {
	return Signer{
		Identity: strings.TrimSpace(os.Getenv("MEWLA_CODESIGN_IDENTITY")),
		Keychain: strings.TrimSpace(os.Getenv("MEWLA_CODESIGN_KEYCHAIN")),
		Run:      run,
	}
}

func (s Signer) sign(bundle string) error {
	identity := s.Identity
	if identity == "" {
		identity = "-"
	}
	args := []string{"--force", "--sign", identity, "--identifier", BundleID}
	if s.Keychain != "" {
		args = append(args, "--keychain", s.Keychain)
	}
	args = append(args, bundle)
	if output, err := s.Run("codesign", args...); err != nil {
		return fmt.Errorf("codesign %s: %v: %s", bundle, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s Signer) verify(bundle string) bool {
	_, err := s.Run("codesign", "--verify", "--strict", bundle)
	return err == nil
}

// InfoPlist is the bundle's Info.plist. LSUIElement keeps the daemon out of
// the Dock and the app switcher.
func InfoPlist(version, stamp string) []byte {
	var b bytes.Buffer
	str := func(key, value string) {
		b.WriteString("\t<key>" + key + "</key>\n\t<string>")
		_ = xml.EscapeText(&b, []byte(value))
		b.WriteString("</string>\n")
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	str("CFBundleIdentifier", BundleID)
	str("CFBundleName", Name)
	str("CFBundleDisplayName", Name)
	str("CFBundleExecutable", executable)
	str("CFBundleIconFile", iconName)
	str("CFBundlePackageType", "APPL")
	str("CFBundleInfoDictionaryVersion", "6.0")
	str("CFBundleShortVersionString", version)
	str("CFBundleVersion", version)
	str("LSMinimumSystemVersion", "13.0")
	str(stampKey, stamp)
	b.WriteString("\t<key>LSUIElement</key>\n\t<true/>\n")
	b.WriteString("\t<key>" + managedKey + "</key>\n\t<true/>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

// ErrNotManaged means something other than Mewla's own bundle is at the path.
var ErrNotManaged = errors.New("a Mewla.app that mewla boot did not create is in the way")

// Managed reports whether the bundle at path was created by this package.
func Managed(bundle string) bool {
	data, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	return err == nil && bytes.Contains(data, []byte("<key>"+managedKey+"</key>"))
}

// Install puts binary, icon and Info.plist into the bundle and signs it. An
// unchanged, validly signed bundle is left alone; otherwise a complete new
// bundle is assembled next to it and swapped in, so a running daemon keeps
// the files it started from. It reports whether the bundle changed.
func Install(bundle, binary, version string, icon []byte, signer Signer) (bool, error) {
	if _, err := os.Stat(bundle); err == nil && !Managed(bundle) {
		return false, fmt.Errorf("%w: %s", ErrNotManaged, bundle)
	}
	program, err := os.ReadFile(binary)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", binary, err)
	}
	stamp := sha256.New()
	stamp.Write(program)
	stamp.Write(icon)
	stamp.Write([]byte(signer.Identity))
	info := InfoPlist(version, hex.EncodeToString(stamp.Sum(nil)))
	if current, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist")); err == nil && bytes.Equal(current, info) && signer.verify(bundle) {
		return false, nil
	}

	next := bundle + ".new"
	if err := os.RemoveAll(next); err != nil {
		return false, err
	}
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{filepath.Join(next, "Contents", "Info.plist"), info, 0o644},
		{Executable(next), program, 0o755},
		{IconPath(next), icon, 0o644},
	}
	for _, f := range files {
		if f.data == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
			return false, err
		}
	}
	if err := signer.sign(next); err != nil {
		_ = os.RemoveAll(next)
		return false, err
	}

	old := bundle + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(bundle); err == nil {
		if err := os.Rename(bundle, old); err != nil {
			return false, err
		}
	}
	if err := os.Rename(next, bundle); err != nil {
		_ = os.Rename(old, bundle)
		return false, err
	}
	_ = os.RemoveAll(old)
	return true, nil
}

// Remove deletes the bundle if this package created it.
func Remove(bundle string) error {
	if _, err := os.Stat(bundle); os.IsNotExist(err) {
		return nil
	}
	if !Managed(bundle) {
		return fmt.Errorf("%w: %s", ErrNotManaged, bundle)
	}
	return os.RemoveAll(bundle)
}
