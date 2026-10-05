package enrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestRequestRequiresMatchingSecretAndNumber(t *testing.T) {
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	pub := key.Public().(ed25519.PublicKey)
	req, secret, err := m.Create("browser-1", "Firefox", "web", "https://zen.example", fmtHex(pub))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Status(req.ID, "bad"); ok {
		t.Fatal("wrong secret disclosed request")
	}
	if _, err := m.Decide(req.ID, "000", "phone", true); err == nil {
		t.Fatal("wrong number approved request")
	}
	if _, err := m.Decide(req.ID, req.Number, "phone", true); err != nil {
		t.Fatal(err)
	}
	status, ok := m.Status(req.ID, secret)
	if !ok || status.Status != Approved {
		t.Fatalf("status=%#v ok=%v", status, ok)
	}
}

func fmtHex(value []byte) string {
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2], out[i*2+1] = hexChars[b>>4], hexChars[b&15]
	}
	return string(out)
}
