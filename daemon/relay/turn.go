package relay

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

// TURN carries Screen's video when a phone and its computer have no direct
// path (mobile data, a strict NAT). It runs in the relay process, beside the
// relay it already is: UDP first, and TCP and TLS for networks that block UDP
// (TLS shares port 443 through a static SNI route, -sni-route). The media in
// it is DTLS-SRTP between the phone and the computer, so the relay forwards
// what it cannot read.
//
// Credentials are TURN REST style: "<expiry>:<route>" with an HMAC of it
// under a secret only this process knows, issued to a computer that proves
// it is a registered route (the signed turn_request), which hands them to the
// viewer it streams to. Nobody else can allocate here.

// TURNConfig configures the TURN server. Zero addresses are not served.
type TURNConfig struct {
	// PublicIP is the address allocations are reached at.
	PublicIP net.IP
	// UDPAddr, TCPAddr and TLSAddr are listen addresses (":3478",
	// "127.0.0.1:5349").
	UDPAddr string
	TCPAddr string
	TLSAddr string
	// TLS is the certificate for TLSAddr.
	TLS *tls.Config
	// URLs are what computers and viewers are told to use, such as
	// "turn:relay.example:3478?transport=udp" and
	// "turns:turn.relay.example:443?transport=tcp".
	URLs []string
	// TTL is how long a credential lasts (default 12 h).
	TTL time.Duration
	// Logf receives operational events.
	Logf func(format string, args ...any)
}

// TURNServer is a running TURN server and its credential issuer.
type TURNServer struct {
	secret string
	urls   []string
	ttl    time.Duration
	server *turn.Server
}

// StartTURN listens and serves TURN until Close.
func StartTURN(config TURNConfig) (*TURNServer, error) {
	if config.PublicIP == nil {
		return nil, errors.New("TURN needs the relay's public IP")
	}
	if len(config.URLs) == 0 {
		return nil, errors.New("TURN needs at least one URL to tell clients")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	t := &TURNServer{secret: hex.EncodeToString(raw), urls: append([]string(nil), config.URLs...), ttl: config.TTL}
	if t.ttl <= 0 {
		t.ttl = 12 * time.Hour
	}
	relayAddress := func() turn.RelayAddressGenerator {
		return &turn.RelayAddressGeneratorPortRange{RelayAddress: config.PublicIP, Address: "0.0.0.0", MinPort: 49152, MaxPort: 65535}
	}
	// Allocations reach only public addresses (and this relay's own, where
	// the other side's allocation is): never the relay host's loopback,
	// private network or cloud metadata.
	permit := func(_ net.Addr, peer net.IP) bool {
		if peer.Equal(config.PublicIP) {
			return true
		}
		return peer.IsGlobalUnicast() && !peer.IsPrivate()
	}
	var packetConns []turn.PacketConnConfig
	var listeners []turn.ListenerConfig
	closeAll := func() {
		for _, p := range packetConns {
			_ = p.PacketConn.Close()
		}
		for _, l := range listeners {
			_ = l.Listener.Close()
		}
	}
	if config.UDPAddr != "" {
		conn, err := net.ListenPacket("udp4", config.UDPAddr)
		if err != nil {
			return nil, fmt.Errorf("TURN UDP: %w", err)
		}
		packetConns = append(packetConns, turn.PacketConnConfig{PacketConn: conn, RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if config.TCPAddr != "" {
		l, err := net.Listen("tcp4", config.TCPAddr)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("TURN TCP: %w", err)
		}
		listeners = append(listeners, turn.ListenerConfig{Listener: l, RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if config.TLSAddr != "" {
		if config.TLS == nil {
			closeAll()
			return nil, errors.New("TURN TLS needs a certificate")
		}
		l, err := tls.Listen("tcp4", config.TLSAddr, config.TLS)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("TURN TLS: %w", err)
		}
		listeners = append(listeners, turn.ListenerConfig{Listener: l, RelayAddressGenerator: relayAddress(), PermissionHandler: permit})
	}
	if len(packetConns)+len(listeners) == 0 {
		return nil, errors.New("TURN has no address to listen on")
	}
	logs := logging.NewDefaultLoggerFactory()
	logs.DefaultLogLevel = logging.LogLevelWarn
	server, err := turn.NewServer(turn.ServerConfig{
		Realm:             "mewla",
		AuthHandler:       turn.LongTermTURNRESTAuthHandler(t.secret, logs.NewLogger("turn")),
		PacketConnConfigs: packetConns,
		ListenerConfigs:   listeners,
		LoggerFactory:     logs,
	})
	if err != nil {
		closeAll()
		return nil, err
	}
	t.server = server
	if config.Logf != nil {
		config.Logf("turn: serving %v", config.URLs)
	}
	return t, nil
}

// Credentials issues a credential for one route.
func (t *TURNServer) Credentials(route string) (urls []string, username, password string, expires time.Time, err error) {
	username, password, err = turn.GenerateLongTermTURNRESTCredentials(t.secret, route, t.ttl)
	if err != nil {
		return nil, "", "", time.Time{}, err
	}
	return append([]string(nil), t.urls...), username, password, time.Now().Add(t.ttl), nil
}

// Close stops the server.
func (t *TURNServer) Close() error {
	if t == nil || t.server == nil {
		return nil
	}
	return t.server.Close()
}
