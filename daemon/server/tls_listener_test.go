package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestTLSListenerKeepsServingWhenConnectionClosesBeforeFirstByte(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &tlsHTTPListener{Listener: tcp, config: testServerTLSConfig(t)}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}),
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	t.Cleanup(func() { _ = server.Close() })

	addr := tcp.Addr().String()
	for range 2 {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}

	plaintext := &http.Client{Timeout: 3 * time.Second}
	assertHTTPResponse(t, plaintext, "http://"+addr+"/")

	encrypted := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	assertHTTPResponse(t, encrypted, "https://"+addr+"/")

	if closeErr := server.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after close")
	}
}

func assertHTTPResponse(t *testing.T, client *http.Client, rawURL string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(rawURL)
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK || string(body) != "ok" {
			t.Fatalf("%s response = %d %q", rawURL, response.StatusCode, body)
		}
		return
	}
	t.Fatalf("%s failed: %v", rawURL, lastErr)
}

func testServerTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}},
	}
}
