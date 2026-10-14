package macapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// macOS keeps Screen Recording and Accessibility grants for as long as an
// app's designated requirement stays the same. An ad hoc signature pins it to
// one build, so every update would ask again. A code-signing identity that
// lives only on this Mac keeps the requirement fixed without an Apple
// developer account: macOS compares the certificate's hash and does not need
// to trust it.
//
// The identity is a self-signed certificate and private key in a dedicated
// keychain. Its random password sits next to it in Application Support, so
// signing never asks the person for anything. `mewla boot uninstall` keeps
// both: deleting them makes the next install a new app to macOS, which then
// asks for the permissions again.

const (
	// IdentityName is the certificate's common name.
	IdentityName = "Mewla Local Signing"
	keychainName = "mewla-signing.keychain-db"
	// openssl is macOS's own LibreSSL: its PKCS#12 files are the kind
	// `security import` reads. Homebrew's OpenSSL 3 writes ones it can't.
	openssl = "/usr/bin/openssl"
)

// IdentityKeychain is the keychain that holds the local signing identity.
func IdentityKeychain(home string) string {
	return filepath.Join(home, "Library", "Keychains", keychainName)
}

// IdentityDir holds the keychain password.
func IdentityDir(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Mewla", "signing")
}

// Runner runs a command and returns its combined output.
type Runner func(name string, args ...string) ([]byte, error)

var sha1Line = regexp.MustCompile(`(?m)^SHA-1 hash: ([0-9A-F]{40})$`)

// LocalSigner returns a Signer for this Mac's local identity, creating the
// keychain and identity the first time. Every step is safe to repeat.
func LocalSigner(home string, run Runner) (Signer, error) {
	dir := IdentityDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Signer{}, err
	}
	password, err := keychainPassword(filepath.Join(dir, "keychain-password"))
	if err != nil {
		return Signer{}, err
	}
	keychain := IdentityKeychain(home)
	if _, err := os.Stat(keychain); os.IsNotExist(err) {
		if err := security(run, "create-keychain", "-p", password, keychain); err != nil {
			return Signer{}, err
		}
		// No timeout: unlocked stays unlocked until logout.
		if err := security(run, "set-keychain-settings", keychain); err != nil {
			return Signer{}, err
		}
	}
	if err := security(run, "unlock-keychain", "-p", password, keychain); err != nil {
		return Signer{}, err
	}
	hash, err := identityHash(run, keychain)
	if err != nil {
		if err := createIdentity(run, dir, keychain, password); err != nil {
			return Signer{}, err
		}
		if hash, err = identityHash(run, keychain); err != nil {
			return Signer{}, err
		}
	}
	return Signer{Identity: hash, Keychain: keychain, Password: password, Run: run}, nil
}

func keychainPassword(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		return strings.TrimSpace(string(data)), nil
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		return "", err
	}
	return password, nil
}

func security(run Runner, args ...string) error {
	if output, err := run("security", args...); err != nil {
		return fmt.Errorf("security %s: %v: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func identityHash(run Runner, keychain string) (string, error) {
	output, err := run("security", "find-certificate", "-c", IdentityName, "-Z", keychain)
	if err != nil {
		return "", fmt.Errorf("no %q certificate in %s", IdentityName, keychain)
	}
	match := sha1Line.FindSubmatch(output)
	if match == nil {
		return "", fmt.Errorf("no %q certificate in %s", IdentityName, keychain)
	}
	return string(match[1]), nil
}

// createIdentity makes the key and certificate, imports them for codesign
// only, and deletes the files; the private key then exists only inside the
// keychain.
func createIdentity(run Runner, dir, keychain, password string) error {
	work, err := os.MkdirTemp(dir, "new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	keyPEM, certPEM, err := newCodeSigningCertificate()
	if err != nil {
		return err
	}
	key, cert, p12 := filepath.Join(work, "key.pem"), filepath.Join(work, "cert.pem"), filepath.Join(work, "identity.p12")
	if err := os.WriteFile(key, keyPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(cert, certPEM, 0o600); err != nil {
		return err
	}
	if output, err := run(openssl, "pkcs12", "-export", "-inkey", key, "-in", cert, "-out", p12, "-passout", "pass:"+password); err != nil {
		return fmt.Errorf("openssl pkcs12: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if err := security(run, "import", p12, "-k", keychain, "-P", password, "-T", "/usr/bin/codesign"); err != nil {
		return err
	}
	// Lets codesign use the key without a prompt.
	return security(run, "set-key-partition-list", "-S", "apple-tool:,apple:,codesign:", "-s", "-k", password, keychain)
}

func newCodeSigningCertificate() (keyPEM, certPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: IdentityName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
