package screencast

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/relay"
	"github.com/pion/webrtc/v4"
)

type fakeSource struct {
	mu   sync.Mutex
	n    int
	w, h int
	ch   chan struct{}
}

func (f *fakeSource) Next(ctx context.Context, after uint64) (Picture, error) {
	for {
		f.mu.Lock()
		n := f.n
		f.mu.Unlock()
		if uint64(n+1) > after {
			return Picture{Pix: testPicture(f.w, f.h, n), Width: f.w, Height: f.h, Stride: f.w * 4, Seq: uint64(n + 1)}, nil
		}
		select {
		case <-f.ch:
		case <-ctx.Done():
			return Picture{}, ctx.Err()
		}
	}
}

func (f *fakeSource) change() {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

// A viewer peer (Pion standing in for the browser) gets the offer, answers,
// receives H.264 and sends input on both channels.
func TestSessionVideoAndInput(t *testing.T) {
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, viewer, frames, channels, gotInput, inputs, inMu := startViewer(ctx, t, webrtc.Configuration{}, nil)
	defer session.Close()
	defer viewer.Close()
	_ = frames
	testInput(ctx, t, session, frames, channels, gotInput, inputs, inMu)
}

func startViewer(ctx context.Context, t *testing.T, viewerConfig webrtc.Configuration, ice []webrtc.ICEServer, opts ...func(*Config)) (*Session, *webrtc.PeerConnection, chan int, chan *webrtc.DataChannel, chan struct{}, *[][]byte, *sync.Mutex) {
	return startViewerWith(ctx, t, nil, viewerConfig, ice, opts...)
}

// startViewerWith is startViewer with the viewer made by api (nil: the default).
func startViewerWith(ctx context.Context, t *testing.T, api *webrtc.API, viewerConfig webrtc.Configuration, ice []webrtc.ICEServer, opts ...func(*Config)) (*Session, *webrtc.PeerConnection, chan int, chan *webrtc.DataChannel, chan struct{}, *[][]byte, *sync.Mutex) {
	newPeer := webrtc.NewPeerConnection
	if api != nil {
		newPeer = api.NewPeerConnection
	}
	viewer, err := newPeer(viewerConfig)
	if err != nil {
		t.Fatal(err)
	}

	toViewer := make(chan Signal, 64)
	inputs := &[][]byte{}
	inMu := &sync.Mutex{}
	gotInput := make(chan struct{}, 16)
	src := &fakeSource{w: 321, h: 241, ch: make(chan struct{}, 1)} // odd: the stream is cropped to even
	cfg := Config{
		Source: src,
		Input: func(msg []byte) error {
			inMu.Lock()
			*inputs = append(*inputs, append([]byte(nil), msg...))
			inMu.Unlock()
			gotInput <- struct{}{}
			return nil
		},
		Signal:       func(v any) { toViewer <- v.(Signal) },
		LoopbackOnly: true,
		ICEServers:   ice,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	session, err := Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	frames := make(chan int, 64)
	viewer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Codec().MimeType != webrtc.MimeTypeH264 {
			t.Errorf("codec %s", track.Codec().MimeType)
		}
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			if pkt.Marker {
				frames <- len(pkt.Payload)
			}
		}
	})
	channels := make(chan *webrtc.DataChannel, 2)
	viewer.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() { channels <- dc })
	})
	viewer.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		raw, _ := json.Marshal(Signal{Type: "ice", Candidate: &init})
		_ = session.HandleSignal(raw)
	})
	go func() {
		for sig := range toViewer {
			switch sig.Type {
			case "rtc":
				if err := viewer.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sig.SDP}); err != nil {
					t.Error(err)
					return
				}
				answer, _ := viewer.CreateAnswer(nil)
				_ = viewer.SetLocalDescription(answer)
				raw, _ := json.Marshal(Signal{Type: "rtc", SDPType: "answer", SDP: answer.SDP})
				if err := session.HandleSignal(raw); err != nil {
					t.Error(err)
				}
			case "ice":
				_ = viewer.AddICECandidate(*sig.Candidate)
			}
		}
	}()

	session.cfg.Source = src
	return session, viewer, frames, channels, gotInput, inputs, inMu
}

func testInput(ctx context.Context, t *testing.T, session *Session, frames chan int, channels chan *webrtc.DataChannel, gotInput chan struct{}, inputsPtr *[][]byte, inMu *sync.Mutex) {
	src := session.cfg.Source.(*fakeSource)
	// The first picture comes without any change: a key frame once connected.
	select {
	case <-frames:
	case <-ctx.Done():
		t.Fatal("no picture")
	}
	src.change()
	select {
	case <-frames:
	case <-ctx.Done():
		t.Fatal("no picture after a change")
	}

	byLabel := map[string]*webrtc.DataChannel{}
	for len(byLabel) < 2 {
		select {
		case dc := <-channels:
			byLabel[dc.Label()] = dc
		case <-ctx.Done():
			t.Fatal("data channels did not open")
		}
	}
	reliable := func(seq uint32, msg ...byte) {
		b := binary.BigEndian.AppendUint32(nil, seq)
		_ = byLabel["input"].Send(append(b, msg...))
	}
	move := func(seq uint32, x, y uint16) {
		b := binary.BigEndian.AppendUint32(nil, seq)
		b = binary.BigEndian.AppendUint16(b, x)
		_ = byLabel["pointer"].Send(binary.BigEndian.AppendUint16(b, y))
	}
	reliable(1, 5, 1, 0, 10, 0, 20) // press at 10,20
	<-gotInput
	move(3, 30, 40) // a move while pressed carries the button
	<-gotInput
	move(2, 99, 99) // unordered: arrives after seq 3 but is newer than the last reliable one, so it applies
	<-gotInput
	reliable(5, 5, 0, 0, 30, 0, 40, 4, 1, 0, 0, 0, 0, 0, 0x61) // release, then 'a' down
	<-gotInput
	<-gotInput
	move(4, 1, 1) // sent before the release: dropped
	reliable(6, 4, 0, 0, 0, 0, 0, 0, 0x61)
	<-gotInput

	inMu.Lock()
	defer inMu.Unlock()
	inputs := *inputsPtr
	want := [][]byte{
		{5, 1, 0, 10, 0, 20},
		{5, 1, 0, 30, 0, 40},
		{5, 1, 0, 99, 0, 99},
		{5, 0, 0, 30, 0, 40},
		{4, 1, 0, 0, 0, 0, 0, 0x61},
		{4, 0, 0, 0, 0, 0, 0, 0x61},
	}
	if len(inputs) != len(want) {
		t.Fatalf("inputs %v, want %v", inputs, want)
	}
	for i := range want {
		if string(inputs[i]) != string(want[i]) {
			t.Fatalf("input %d: %v, want %v", i, inputs[i], want[i])
		}
	}
}

// With no direct path the picture and input go through the relay's TURN: the
// viewer may use relay candidates only, over UDP, over TCP (a network that
// blocks UDP) and over TLS (one that lets only 443 out: the hosted relay's
// turns: URL, here with a test certificate the viewer trusts).
func TestSessionThroughTURN(t *testing.T) {
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	for _, transport := range []string{"udp", "tcp", "tls"} {
		t.Run(transport, func(t *testing.T) {
			secret := []byte("0123456789abcdef0123456789abcdef")
			cert, roots := testCertificate(t, "turn.relay.test")
			turnServer, err := relay.StartTURN(relay.TURNConfig{
				PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: "127.0.0.1:0", TCPAddr: "127.0.0.1:0", Secret: secret,
				TLSAddr: "127.0.0.1:0", TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer turnServer.Close()
			udp, tcp, tlsAddr := turnServer.Addrs() // where the system put them
			issuer, err := relay.NewTURNIssuer(secret, []string{"turn:" + udp + "?transport=udp", "turn:" + tcp + "?transport=tcp"}, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			urls, user, pass, _, err := issuer.Credentials("0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			ice := ICEServersFor(urls, user, pass)
			viewerURL := "turn:" + udp + "?transport=udp"
			var api *webrtc.API
			switch transport {
			case "tcp":
				viewerURL = "turn:" + tcp + "?transport=tcp"
			case "tls":
				_, port, _ := net.SplitHostPort(tlsAddr)
				viewerURL = "turns:turn.relay.test:" + port + "?transport=tcp"
				// The name resolves to this relay, and its certificate is checked as a browser checks it.
				var se webrtc.SettingEngine
				se.SetICEProxyDialer(tlsDialer{addr: tlsAddr, config: &tls.Config{ServerName: "turn.relay.test", RootCAs: roots, MinVersion: tls.VersionTLS12}})
				api = webrtc.NewAPI(webrtc.WithSettingEngine(se))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			stats := make(chan Stats, 64)
			session, viewer, frames, channels, gotInput, inputs, inMu := startViewerWith(ctx, t, api, webrtc.Configuration{
				ICETransportPolicy: webrtc.ICETransportPolicyRelay,
				ICEServers:         []webrtc.ICEServer{{URLs: []string{viewerURL}, Username: user, Credential: pass}},
			}, ice, func(c *Config) {
				c.RelayMaxKbps = 1000
				c.Stats = func(s Stats) {
					select {
					case stats <- s:
					default:
					}
				}
			})
			defer session.Close()
			defer viewer.Close()
			testInput(ctx, t, session, frames, channels, gotInput, inputs, inMu)
			if route, _ := session.route(); route != "relay" {
				t.Fatalf("route %q, want relay", route)
			}
			// Through the relay the video stays under its ceiling (with room for headers).
			for {
				select {
				case s := <-stats:
					if s.Route != "relay" || s.Kbps > 850 {
						continue
					}
				case <-ctx.Done():
					t.Fatal("the rate never came under the relay's ceiling")
				}
				break
			}
		})
	}
}

// Two views of one computer, every peer relay-only through the recipe's three
// TURN addresses (UDP, TCP, TLS), both get pictures: each peer of each view
// has a credential of its own (screenICE), the offer hands the viewer its own,
// and every peer allocates on all three addresses under the relay's default
// cap of 4 per credential. One credential shared by the computer and every
// view (the re-review's RR2) filled that cap with the first view.
func TestTwoRelayedViewsThroughThreeTURNAddresses(t *testing.T) {
	if _, _, err := findEncoder(); err != nil {
		t.Skip(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	cert, roots := testCertificate(t, "turn.relay.test")
	turnServer, err := relay.StartTURN(relay.TURNConfig{
		PublicIP: net.IPv4(127, 0, 0, 1), UDPAddr: "127.0.0.1:0", TCPAddr: "127.0.0.1:0", Secret: secret,
		TLSAddr: "127.0.0.1:0", TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer turnServer.Close()
	udp, tcp, tlsAddr := turnServer.Addrs() // where the system put them
	_, port, _ := net.SplitHostPort(tlsAddr)
	issuer, err := relay.NewTURNIssuer(secret, []string{
		"turn:" + udp + "?transport=udp",
		"turn:" + tcp + "?transport=tcp",
		"turns:turn.relay.test:" + port + "?transport=tcp",
	}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	const route = "0123456789abcdef0123456789abcdef"
	fresh := func() []webrtc.ICEServer {
		urls, user, pass, _, err := issuer.Credentials(route)
		if err != nil {
			t.Fatal(err)
		}
		return ICEServersFor(urls, user, pass)
	}
	// turns:turn.relay.test resolves to this relay, its certificate checked as
	// a browser checks it; plain TCP goes where it says.
	dial := relayDialer{name: "turn.relay.test", tlsAddr: tlsAddr, config: &tls.Config{ServerName: "turn.relay.test", RootCAs: roots, MinVersion: tls.VersionTLS12}}
	var se webrtc.SettingEngine
	se.SetICEProxyDialer(dial)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se))
	for view := 1; view <= 2; view++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		computer, viewer := fresh(), fresh()
		offered := make(chan []webrtc.ICEServer, 1)
		session, peer, frames, _, _, _, _ := startViewerWith(ctx, t, api, webrtc.Configuration{
			ICETransportPolicy: webrtc.ICETransportPolicyRelay, ICEServers: viewer,
		}, computer, func(c *Config) {
			c.ViewerICEServers = viewer
			c.dialTURN = dial
			signal := c.Signal
			c.Signal = func(v any) {
				if sig := v.(Signal); sig.Type == "rtc" {
					offered <- sig.ICEServers
				}
				signal(v)
			}
		})
		defer session.Close()
		defer peer.Close()
		if got := <-offered; len(got) == 0 || got[0].Username != viewer[0].Username {
			t.Fatalf("view %d: the offer gave the viewer %+v, not its own credential", view, got)
		}
		select {
		case <-frames:
		case <-ctx.Done():
			t.Fatalf("view %d: no picture (viewer %s)", view, peer.ConnectionState())
		}
		if r, _ := session.route(); r != "relay" {
			t.Fatalf("view %d: route %q", view, r)
		}
		// Every peer so far holds an allocation at each of the three addresses.
		want := 6 * view
		deadline := time.Now().Add(5 * time.Second)
		for total, _ := turnServer.Allocations(route); total != want; total, _ = turnServer.Allocations(route) {
			if time.Now().After(deadline) {
				t.Fatalf("view %d: %d allocations, want %d", view, total, want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// relayDialer dials TURN over TCP for a peer: a turns: URL naming name goes
// over TLS to tlsAddr, anything else as plain TCP where it says.
type relayDialer struct {
	name, tlsAddr string
	config        *tls.Config
}

func (d relayDialer) Dial(network, addr string) (net.Conn, error) {
	if host, _, _ := net.SplitHostPort(addr); host == d.name {
		return tls.Dial(network, d.tlsAddr, d.config)
	}
	return net.Dial(network, addr)
}

// tlsDialer dials the TURN server over TLS for a viewer whose turns: URL
// names it (Pion uses the connection as it comes).
type tlsDialer struct {
	addr   string
	config *tls.Config
}

func (d tlsDialer) Dial(network, _ string) (net.Conn, error) {
	return tls.Dial(network, d.addr, d.config)
}

// testCertificate is a self-signed certificate for host and the pool that
// trusts it.
func testCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}
