package screen

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// FakeServer speaks the server half of an RFB handshake the way macOS Screen
// Sharing does: version 003.889, Apple's own types plus VNC password.
type FakeServer struct {
	Listener net.Listener
	Password string
	Offered  []byte
	Version  string
}

func startFakeServer(t *testing.T, password string, offered []byte) *FakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &FakeServer{Listener: ln, Password: password, Offered: offered, Version: "RFB 003.889\n"}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *FakeServer) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(conn, f.Version)
	var version [12]byte
	if _, err := io.ReadFull(conn, version[:]); err != nil || string(version[:]) != protocolVersion {
		return
	}
	_, _ = conn.Write(append([]byte{byte(len(f.Offered))}, f.Offered...))
	var chosen [1]byte
	if _, err := io.ReadFull(conn, chosen[:]); err != nil {
		return
	}
	if chosen[0] == securityVNCAuth {
		challenge := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		_, _ = conn.Write(challenge[:])
		var response [16]byte
		if _, err := io.ReadFull(conn, response[:]); err != nil {
			return
		}
		want, _ := vncAuthResponse(f.Password, challenge)
		if response != want {
			reason := "Authentication failed"
			_ = binary.Write(conn, binary.BigEndian, uint32(1))
			_ = binary.Write(conn, binary.BigEndian, uint32(len(reason)))
			_, _ = io.WriteString(conn, reason)
			return
		}
	}
	_ = binary.Write(conn, binary.BigEndian, uint32(0))
	var shared [1]byte
	if _, err := io.ReadFull(conn, shared[:]); err != nil {
		return
	}
	init := make([]byte, 20)
	binary.BigEndian.PutUint16(init[0:], 1512)
	binary.BigEndian.PutUint16(init[2:], 982)
	name := "Fixture Mac"
	_, _ = conn.Write(init)
	_ = binary.Write(conn, binary.BigEndian, uint32(len(name)))
	_, _ = io.WriteString(conn, name)
	// Echo whatever the viewer sends so bridges can be checked end to end.
	_, _ = io.Copy(conn, conn)
}

func enabledService(t *testing.T, address, password string) *Service {
	t.Helper()
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{Enabled: true, Address: address, Password: password}); err != nil {
		t.Fatal(err)
	}
	return NewService(dir)
}

func TestVNCAuthKnownAnswer(t *testing.T) {
	// Reference: openssl enc -des-ecb -nopad -K 0e86ceceeef64e26 (each byte
	// of "password" bit-mirrored) over the challenge 01..10.
	challenge := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got, err := vncAuthResponse("password", challenge)
	if err != nil {
		t.Fatal(err)
	}
	if want := "abd26395c6fb36af4213133396e381c4"; hex.EncodeToString(got[:]) != want {
		t.Fatalf("response = %x, want %s", got, want)
	}
	truncated, _ := vncAuthResponse("password-is-long", challenge)
	if truncated != got {
		t.Fatal("only the first eight bytes are part of the key")
	}
}

func TestStatusSignsInWithStoredPassword(t *testing.T) {
	server := startFakeServer(t, "s3cret", []byte{30, 33, 36, 35, securityVNCAuth})
	status := enabledService(t, server.Listener.Addr().String(), "s3cret").Status(context.Background())
	if !status.Available || status.Width != 1512 || status.Height != 982 || status.Name != "Fixture Mac" {
		t.Fatalf("status = %+v", status)
	}
}

func TestStatusExplainsWrongPasswordAndMissingPassword(t *testing.T) {
	server := startFakeServer(t, "s3cret", []byte{30, securityVNCAuth})
	status := enabledService(t, server.Listener.Addr().String(), "wrong").Status(context.Background())
	if status.Available || !strings.Contains(status.Reason, "Authentication failed") {
		t.Fatalf("wrong password status = %+v", status)
	}
	status = enabledService(t, server.Listener.Addr().String(), "").Status(context.Background())
	if status.Available || !strings.Contains(status.Reason, "mewla screen enable") {
		t.Fatalf("missing password status = %+v", status)
	}
	appleOnly := startFakeServer(t, "", []byte{30, 33})
	status = enabledService(t, appleOnly.Listener.Addr().String(), "x").Status(context.Background())
	if status.Available || !strings.Contains(status.Reason, "VNC viewers may control screen with password") {
		t.Fatalf("apple-only status = %+v", status)
	}
}

func TestDisabledByDefaultAndLoopbackOnly(t *testing.T) {
	service := NewService(t.TempDir())
	if service.Enabled() {
		t.Fatal("screen control must be off until enabled")
	}
	if _, err := service.Dial(context.Background()); err != ErrDisabled {
		t.Fatalf("dial disabled = %v", err)
	}
	for _, address := range []string{"127.0.0.1:5900", "[::1]:5900", "localhost:5900"} {
		if err := ValidateAddress(address); err != nil {
			t.Fatalf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:5900", "100.73.112.8:5900", "192.168.1.2:5900", "example.com:5900", "127.0.0.1"} {
		if err := ValidateAddress(address); err == nil {
			t.Fatalf("%s must be refused", address)
		}
		if err := SaveConfig(t.TempDir(), Config{Enabled: true, Address: address}); err == nil {
			t.Fatalf("saved non-loopback %s", address)
		}
	}
}

func TestAcceptClientOffersOnlyNone(t *testing.T) {
	viewer, daemon := net.Pipe()
	defer viewer.Close()
	defer daemon.Close()
	done := make(chan error, 1)
	go func() { done <- AcceptClient(daemon) }()
	var version [12]byte
	if _, err := io.ReadFull(viewer, version[:]); err != nil || string(version[:]) != protocolVersion {
		t.Fatalf("version %q %v", version, err)
	}
	_, _ = io.WriteString(viewer, protocolVersion)
	var types [2]byte
	_, _ = io.ReadFull(viewer, types[:])
	if types != [2]byte{1, securityNone} {
		t.Fatalf("types = %v", types)
	}
	_, _ = viewer.Write([]byte{securityNone})
	var result [4]byte
	_, _ = io.ReadFull(viewer, result[:])
	if result != [4]byte{} {
		t.Fatalf("result = %v", result)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
