// Package screencast sends a screen to a viewer as pushed video over WebRTC.
//
// The computer encodes the picture as one low-latency H.264 stream and sends
// each picture the moment it changes, without waiting to be asked; the viewer
// plays it in a <video> element with the platform's decoder. Media and input
// travel as SRTP and SCTP over UDP (DTLS-encrypted end to end, so a TURN relay
// in the middle cannot read them); only the signalling (the offer, the answer
// and the candidates) rides the authenticated Mewla link, which binds the
// DTLS fingerprints in them to the paired device.
//
// The person's input comes back on two data channels as RFB client messages,
// the same bytes every screen backend already takes: "input" (reliable,
// ordered) carries keys, button changes, the wheel and the clipboard;
// "pointer" (unordered, never retransmitted) carries pointer moves only.
package screencast

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/interceptor/pkg/gcc"
	"github.com/pion/rtcp"
	"github.com/pion/transport/v5"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"golang.org/x/net/proxy"
)

// Config is what a session needs from its caller.
type Config struct {
	Source Source
	// NewEncoder makes this computer's encoder (VideoToolbox on a Mac); nil
	// uses ffmpeg.
	NewEncoder func(kbps int) (Encoder, error)
	// Input takes the person's RFB client messages (KeyEvent, PointerEvent,
	// ClientCutText), whole and in order. Nil: the view is for watching.
	Input func(msg []byte) error
	// Scroll takes the person's scrolls by picture pixels (Mewla's client
	// message 250: x, y, then signed dx and dy; +dy scrolls down, as a wheel
	// turned toward the person). Non-nil, the offer says fine_scroll and the
	// viewer sends them instead of wheel notches.
	Scroll func(x, y, dx, dy int) error
	// Cursor says the video carries the computer's own cursor: the offer
	// tells the viewer, which then draws no arrow of its own.
	Cursor bool
	// Signal sends a signalling message to the viewer over the Mewla link.
	Signal func(v any)
	// Stats hears what the stream did, about once a second.
	Stats func(Stats)
	// LoopbackOnly offers this computer's loopback address only (a daemon
	// reachable from this computer alone).
	LoopbackOnly bool
	// ICEServers are STUN and TURN servers to gather through, when any.
	ICEServers []webrtc.ICEServer
	// ViewerICEServers are the servers the offer gives the viewer (nil:
	// ICEServers): a TURN credential of its own, since one holds only a few
	// allocations and each peer takes one per TURN address.
	ViewerICEServers []webrtc.ICEServer
	// RelayMaxKbps is the most the relay's TURN carries for one allocation
	// (0: no cap): while the picture goes through it, the video stays under.
	RelayMaxKbps int
	// SignOffer and VerifyAnswer bind the DTLS fingerprints to identities:
	// SignOffer signs the offer's (as this computer; the viewer checks it
	// with the computer's key from pairing) and VerifyAnswer checks the
	// viewer's signature over the answer's (as its paired device) before the
	// answer is used. The DTLS handshake then holds each side to them.
	SignOffer    func(fingerprints []string) (any, error)
	VerifyAnswer func(fingerprints []string, authorization string) error

	// net replaces the computer's network (tests: a virtual, lossy one).
	net transport.Net
	// dialTURN dials TURN over TCP and TLS (tests: a turns: name that
	// resolves to a local relay with a test certificate).
	dialTURN proxy.Dialer
}

// Stats is one second of the stream, in the fields the viewer's Connection
// panel reads (the RFB stream's names where they mean the same).
type Stats struct {
	Transport string  `json:"transport"`
	Route     string  `json:"route,omitempty"`
	Encoder   string  `json:"encoder,omitempty"`
	Updates   int     `json:"updates"`
	Video     int     `json:"video"`
	Bytes     int     `json:"bytes"`
	EncodeMs  float64 `json:"encode_ms"`
	EncodeMax float64 `json:"encode_max_ms"`
	RTTMs     float64 `json:"rtt_ms"`
	Kbps      int     `json:"kbps"`
	Keys      int     `json:"key_frames"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
}

// Signal is a signalling message, in either direction.
type Signal struct {
	Type      string                   `json:"type"` // "rtc" or "ice"
	SDPType   string                   `json:"sdp_type,omitempty"`
	SDP       string                   `json:"sdp,omitempty"`
	Candidate *webrtc.ICECandidateInit `json:"candidate,omitempty"`
	// ICEServers, on the offer, are the STUN and TURN servers the viewer
	// gathers through too (the relay's TURN, when there is one).
	ICEServers []webrtc.ICEServer `json:"ice_servers,omitempty"`
	// Assertion, on the offer, is the computer's signature over the offer's
	// DTLS fingerprints (Config.SignOffer); Authorization, on the answer, the
	// viewer device's over the answer's (Config.VerifyAnswer).
	Assertion     any    `json:"assertion,omitempty"`
	Authorization string `json:"authorization,omitempty"`
	// FineScroll and Cursor, on the offer: the computer takes pixel scrolls
	// (Config.Scroll), and its video carries its own cursor (Config.Cursor).
	FineScroll bool `json:"fine_scroll,omitempty"`
	Cursor     bool `json:"cursor,omitempty"`
}

const (
	minInterval = time.Second / 60 // at most 60 pictures a second
	// stillFor is how long a still picture keeps being sent after the last
	// change, so the encoder sharpens it (each repeat refines what the last
	// one left blurred); stillEvery is how often in that time.
	stillFor   = time.Second
	stillEvery = 100 * time.Millisecond
	// Bit rates in kbit/s: where a session starts and its bounds.
	startKbps = 6000
	minKbps   = 300
	maxKbps   = 20000
	// blindBytes is a second's video that the estimator should have heard
	// about (a still screen sends less).
	blindBytes = 20_000
)

// Session is one viewer's stream.
type Session struct {
	cfg   Config
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample
	enc   Encoder
	bwe   chan cc.BandwidthEstimator

	ctx    context.Context
	cancel context.CancelCauseFunc

	mu        sync.Mutex
	remoteSet bool
	pending   []webrtc.ICECandidateInit
	needKey   bool
	lastKey   time.Time

	// kick asks for a picture now (the viewer connected and needs a key frame).
	kick chan struct{}

	inMu    sync.Mutex
	mask    byte
	lastSeq uint32
}

// Start makes the peer, its video track and data channels, and sends the
// offer through cfg.Signal. The session ends with ctx, Close, or a lost link.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	var enc Encoder
	var err error
	if cfg.NewEncoder != nil {
		enc, err = cfg.NewEncoder(startKbps)
	} else {
		enc, err = newFFmpegEncoder(startKbps)
	}
	if err != nil {
		return nil, err
	}
	s := &Session{cfg: cfg, enc: enc, bwe: make(chan cc.BandwidthEstimator, 1), kick: make(chan struct{}, 1)}
	s.ctx, s.cancel = context.WithCancelCause(ctx)
	if err := s.setup(); err != nil {
		enc.Close()
		s.cancel(err)
		if s.pc != nil {
			_ = s.pc.Close()
		}
		return nil, err
	}
	go func() {
		<-s.ctx.Done()
		_ = s.pc.Close()
	}()
	go s.run()
	return s, nil
}

// Done is closed when the session ends; Err says why.
func (s *Session) Done() <-chan struct{} { return s.ctx.Done() }

func (s *Session) Err() error { return context.Cause(s.ctx) }

// rateCostsKey reports whether enc changes its bit rate only with a key frame.
func rateCostsKey(enc Encoder) bool {
	r, ok := enc.(interface{ RateCostsKeyFrame() bool })
	return ok && r.RateCostsKeyFrame()
}

// ErrEncoder ends a session whose encoder failed (with every fallback).
var ErrEncoder = errors.New("The computer could not encode its screen")

// Close ends the session.
func (s *Session) Close() { s.cancel(errors.New("closed")) }

func (s *Session) setup() error {
	m := &webrtc.MediaEngine{}
	feedback := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}, {Type: "ccm", Parameter: "fir"}, {Type: "transport-cc"}}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f", RTCPFeedback: feedback},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return err
	}
	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: playoutDelayURI}, webrtc.RTPCodecTypeVideo); err != nil {
		return err
	}
	reg := &interceptor.Registry{}
	reg.Add(playoutDelayFactory{})
	if err := webrtc.ConfigureRTCPReports(reg); err != nil {
		return err
	}
	// The link's capacity, from the viewer's transport-wide feedback. No
	// pacer: a picture leaves at once, the encoder's rate is what follows the
	// link.
	congestion, err := cc.NewInterceptor(func() (cc.BandwidthEstimator, error) {
		return gcc.NewSendSideBWE(gcc.SendSideBWEInitialBitrate(startKbps*1000), gcc.SendSideBWEMinBitrate(minKbps*1000),
			gcc.SendSideBWEMaxBitrate(maxKbps*1000), gcc.SendSideBWEPacer(gcc.NewNoOpPacer()))
	})
	if err != nil {
		return err
	}
	congestion.OnNewPeerConnection(func(_ string, e cc.BandwidthEstimator) {
		select {
		case s.bwe <- e:
		default:
		}
	})
	reg.Add(congestion)
	// After the estimator, so its packets already carry the sequence numbers it reads.
	if err := webrtc.ConfigureTWCCHeaderExtensionSender(m, reg); err != nil {
		return err
	}
	// Outermost: a resent packet takes a new sequence number and passes the
	// estimator, which then counts it. Inside them, a resend kept its first
	// number, came a queue late, and read to the estimator as a link always
	// overused (it held a thin link's rate at its floor).
	if err := webrtc.ConfigureNack(m, reg); err != nil {
		return err
	}

	var se webrtc.SettingEngine
	// UDP, and TCP to a TURN server for networks that block UDP.
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6, webrtc.NetworkTypeTCP4})
	se.SetICETimeouts(4*time.Second, 10*time.Second, time.Second)
	if s.cfg.dialTURN != nil {
		se.SetICEProxyDialer(s.cfg.dialTURN)
	}
	if s.cfg.net != nil {
		se.SetNet(s.cfg.net)
	} else if s.cfg.LoopbackOnly {
		se.SetIncludeLoopbackCandidate(true)
		se.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	} else {
		se.SetInterfaceFilter(func(name string) bool {
			for _, skip := range []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet"} {
				if strings.HasPrefix(name, skip) {
					return false
				}
			}
			return true
		})
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(reg), webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: s.cfg.ICEServers})
	if err != nil {
		return err
	}
	s.pc = pc
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			// Pictures sent before this went nowhere: start with a key frame.
			s.mu.Lock()
			s.needKey = true
			s.lastKey = time.Time{}
			s.mu.Unlock()
			select {
			case s.kick <- struct{}{}:
			default:
			}
		case webrtc.PeerConnectionStateFailed:
			s.cancel(errors.New("the connection to the viewer failed"))
		case webrtc.PeerConnectionStateClosed:
			s.cancel(errors.New("closed"))
		}
	})
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		s.cfg.Signal(Signal{Type: "ice", Candidate: &init})
	})

	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "video", "screen")
	if err != nil {
		return err
	}
	s.track = track
	sender, err := pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
	if err != nil {
		return err
	}
	go s.readRTCP(sender.Sender())

	if s.cfg.Input != nil {
		ordered := true
		input, err := pc.CreateDataChannel("input", &webrtc.DataChannelInit{Ordered: &ordered})
		if err != nil {
			return err
		}
		input.OnMessage(func(msg webrtc.DataChannelMessage) { s.reliable(msg.Data) })
		unordered, none := false, uint16(0)
		pointer, err := pc.CreateDataChannel("pointer", &webrtc.DataChannelInit{Ordered: &unordered, MaxRetransmits: &none})
		if err != nil {
			return err
		}
		pointer.OnMessage(func(msg webrtc.DataChannelMessage) { s.move(msg.Data) })
	}

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	offerSDP := pc.LocalDescription().SDP
	var assertion any
	if s.cfg.SignOffer != nil {
		if assertion, err = s.cfg.SignOffer(Fingerprints(offerSDP)); err != nil {
			return err
		}
	}
	viewerServers := s.cfg.ViewerICEServers
	if viewerServers == nil {
		viewerServers = s.cfg.ICEServers
	}
	s.cfg.Signal(Signal{Type: "rtc", SDPType: "offer", SDP: offerSDP, ICEServers: viewerServers, Assertion: assertion, FineScroll: s.cfg.Scroll != nil && s.cfg.Input != nil, Cursor: s.cfg.Cursor})
	return nil
}

// HandleSignal takes the viewer's answer and candidates.
func (s *Session) HandleSignal(raw []byte) error {
	var msg Signal
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	switch msg.Type {
	case "rtc":
		if msg.SDPType != "answer" {
			return errors.New("expected an answer")
		}
		s.mu.Lock()
		answered := s.remoteSet
		s.mu.Unlock()
		if answered {
			return errors.New("the viewer has answered already")
		}
		if s.cfg.VerifyAnswer != nil {
			if err := s.cfg.VerifyAnswer(Fingerprints(msg.SDP), msg.Authorization); err != nil {
				s.cancel(err)
				return err
			}
		}
		if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}); err != nil {
			return err
		}
		s.mu.Lock()
		s.remoteSet = true
		pending := s.pending
		s.pending = nil
		s.mu.Unlock()
		for _, c := range pending {
			_ = s.pc.AddICECandidate(c)
		}
	case "ice":
		if msg.Candidate == nil {
			return nil
		}
		s.mu.Lock()
		if !s.remoteSet {
			s.pending = append(s.pending, *msg.Candidate)
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		return s.pc.AddICECandidate(*msg.Candidate)
	}
	return nil
}

// readRTCP turns the viewer's requests for a picture it can decode from (a
// lost one) into a key frame.
func (s *Session) readRTCP(sender *webrtc.RTPSender) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			switch p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				s.mu.Lock()
				s.needKey = true
				s.mu.Unlock()
			}
		}
	}
}

// ICEServersFor turns a relay's TURN URLs and credential into the servers both
// peers gather through: TURN as given, and STUN on each UDP TURN address (a
// direct path through NAT when one exists).
func ICEServersFor(urls []string, username, password string) []webrtc.ICEServer {
	if len(urls) == 0 {
		return nil
	}
	var stun []string
	for _, u := range urls {
		if strings.HasPrefix(u, "turn:") && !strings.Contains(u, "transport=tcp") {
			host := strings.TrimPrefix(u, "turn:")
			if i := strings.IndexByte(host, '?'); i >= 0 {
				host = host[:i]
			}
			stun = append(stun, "stun:"+host)
		}
	}
	servers := []webrtc.ICEServer{{URLs: urls, Username: username, Credential: password}}
	if len(stun) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: stun})
	}
	return servers
}

// ---- input --------------------------------------------------------------

// reliable takes a sequence number and whole RFB messages.
func (s *Session) reliable(data []byte) {
	if len(data) < 4 {
		return
	}
	seq := binary.BigEndian.Uint32(data)
	msgs := data[4:]

	s.inMu.Lock()
	defer s.inMu.Unlock()
	s.lastSeq = seq
	for len(msgs) > 0 {
		n := rfbLength(msgs)
		if n <= 0 || n > len(msgs) {
			return // not a message this channel carries: drop the rest
		}
		msg := msgs[:n]
		msgs = msgs[n:]
		if msg[0] == 5 {
			s.mask = msg[1]
		}
		if msg[0] == scrollMessage {
			// Mewla's own: never passed on as an RFB message.
			if s.cfg.Scroll == nil {
				continue
			}
			x, y := int(binary.BigEndian.Uint16(msg[2:])), int(binary.BigEndian.Uint16(msg[4:]))
			dx, dy := int(int16(binary.BigEndian.Uint16(msg[6:]))), int(int16(binary.BigEndian.Uint16(msg[8:])))
			if err := s.cfg.Scroll(x, y, dx, dy); err != nil {
				s.cancel(err)
				return
			}
			continue
		}
		if err := s.cfg.Input(msg); err != nil {
			s.cancel(err)
			return
		}
	}
}

// move takes a pointer move: a sequence number, x and y. One sent before the
// last reliable message arrived late and is dropped; the buttons are the ones
// the reliable channel last set, never the move's own.
func (s *Session) move(data []byte) {
	if len(data) != 8 {
		return
	}
	seq := binary.BigEndian.Uint32(data)
	s.inMu.Lock()
	defer s.inMu.Unlock()
	if int32(seq-s.lastSeq) < 0 {
		return
	}
	msg := []byte{5, s.mask, data[4], data[5], data[6], data[7]}
	if err := s.cfg.Input(msg); err != nil {
		s.cancel(err)
	}
}

// scrollMessage is Mewla's client message for a scroll by picture pixels:
// type, padding, x, y (uint16), dx, dy (int16), big-endian. RFB leaves message
// types from 128 to vendors; a viewer sends it only when the offer says
// fine_scroll.
const scrollMessage = 250

// rfbLength is the length of the RFB client message msg starts with, or 0.
func rfbLength(msg []byte) int {
	switch msg[0] {
	case scrollMessage:
		return 10
	case 4: // KeyEvent
		return 8
	case 5: // PointerEvent
		return 6
	case 6: // ClientCutText
		if len(msg) < 8 {
			return 0
		}
		n := binary.BigEndian.Uint32(msg[4:])
		if n > 1<<20 {
			return 0
		}
		return 8 + int(n)
	}
	return 0
}

// ---- pictures -------------------------------------------------------------

func (s *Session) run() {
	defer s.enc.Close()
	// The newest picture, pulled from the source as it changes; an older one
	// not yet sent is replaced, never queued.
	pictures := make(chan Picture, 1)
	go func() {
		var after uint64
		for {
			p, err := s.cfg.Source.Next(s.ctx, after)
			if err != nil {
				if s.ctx.Err() == nil {
					s.cancel(err)
				}
				return
			}
			after = p.Seq
			p.Pix = append([]byte(nil), p.Pix[:p.Height*p.Stride]...)
			select {
			case <-pictures:
			default:
			}
			pictures <- p
		}
	}()
	var (
		last      *Picture
		pending   *Picture
		lastSent  time.Time
		changedAt time.Time
		estimator cc.BandwidthEstimator
		kbps      = startKbps
		ceiling   = maxKbps
		rateAt    time.Time
		st        = statsWindow{start: time.Now()}
		// blindCap holds the rate under what the link took while the
		// estimator could not see it (see the stats tick).
		blindCap = maxKbps
		heard    string
		minRTT   float64
		deaf     bool // the estimator heard nothing new in the last second of video
		// budget is what the link may take now, in bytes, at the rate the
		// video is held to; below zero, what went over it. An encoder over
		// its rate (VA-API at its coarsest on a screen in full motion) then
		// sends fewer pictures, never more than the link carries.
		budget   float64
		budgetAt time.Time
	)
	// held is the rate the link is held to: what the estimator says it
	// carries (an encoder's own rate may lag it, or overshoot), under the
	// ceiling and blindCap.
	held := func() int {
		if estimator == nil {
			return min(kbps, ceiling)
		}
		if deaf {
			return max(minKbps, min(ceiling, blindCap))
		}
		return max(minKbps, min(ceiling, blindCap, estimator.GetTargetBitrate()/1000))
	}
	refill := func(now time.Time) {
		rate := float64(held()) * 125 // bytes a second
		if budgetAt.IsZero() {
			budget = rate / 4
		} else {
			budget = min(rate/4, budget+now.Sub(budgetAt).Seconds()*rate)
		}
		budgetAt = now
	}
	// overBudget is how long until the link has taken what went over it.
	overBudget := func(now time.Time) time.Duration {
		refill(now)
		if budget >= 0 {
			return 0
		}
		return time.Duration(-budget / (float64(held()) * 125) * float64(time.Second))
	}
	tick := time.NewTicker(stillEvery)
	defer tick.Stop()
	statsTick := time.NewTicker(time.Second)
	defer statsTick.Stop()
	send := func(p *Picture) {
		now := time.Now()
		s.mu.Lock()
		// A lost picture is answered with a key frame at most once a second:
		// on a lossy link more would only add to the loss.
		key := s.needKey && now.Sub(s.lastKey) > time.Second
		if key {
			s.needKey = false
			s.lastKey = now
		}
		s.mu.Unlock()
		// Follow the link: a lower rate at once, a higher one when it holds;
		// never over the ceiling (the relay's, when the picture goes through it).
		if kbps > ceiling {
			kbps = ceiling
			rateAt = now
			s.enc.SetBitrate(kbps)
		}
		if estimator != nil {
			target := held()
			// An encoder that changes its rate only with a key frame (ffmpeg)
			// drops at most every 2 s, and rises only when a key frame is due
			// anyway or the screen is still: a big key frame in the middle of
			// motion on a link that just lost packets loses more.
			restarts := rateCostsKey(s.enc)
			dropAfter, quiet := 500*time.Millisecond, true
			if restarts {
				dropAfter, quiet = 2*time.Second, key || now.Sub(changedAt) > stillFor
			}
			drop := target < kbps*6/10 && now.Sub(rateAt) > dropAfter
			rise := target > kbps*14/10 && now.Sub(rateAt) > 3*time.Second && quiet
			if drop || rise {
				kbps = target
				rateAt = now
				s.enc.SetBitrate(kbps)
				if restarts {
					// Its next picture is a key frame: it answers a lost one too.
					s.mu.Lock()
					s.needKey, s.lastKey = false, now
					s.mu.Unlock()
				}
			}
		}
		start := time.Now()
		au, err := s.enc.Encode(*p, key)
		took := time.Since(start)
		if err != nil {
			log.Printf("screencast: encode: %v", err)
			s.cancel(fmt.Errorf("%w: %v", ErrEncoder, err))
			return
		}
		if len(au) == 0 {
			return // the encoder skipped it; the stream is as it was
		}
		var dur time.Duration
		if !lastSent.IsZero() {
			dur = now.Sub(lastSent)
		}
		lastSent = now
		last = p
		if err := s.track.WriteSample(media.Sample{Data: au, Duration: dur}); err != nil {
			return
		}
		// A key frame may go over, by a second's worth at most: a lost
		// picture is always repaired, and the pictures after it wait.
		refill(time.Now())
		budget = max(budget-float64(len(au)), -float64(held())*125)
		st.add(len(au), took, key || nalStartsKey(au))
	}
	var wait <-chan time.Time
	for {
		select {
		case <-s.ctx.Done():
			return
		case e := <-s.bwe:
			estimator = e
		case <-statsTick.C:
			// The estimator learns the link from the viewer's feedback on
			// the last few hundred packets. A link that is thin from the start
			// queues the video until the feedback comes too late for it to
			// read, and after heavy loss it can stop reading for seconds: it
			// then holds its rate while the picture freezes. While it hears
			// nothing new as the video goes out, the round trip says instead:
			// well over its lowest (a queue), the rate halves each second;
			// near it, it rises a tenth. Once the estimator hears again, the
			// rate rises a quarter a second and the estimator, which now sees
			// a queue grow, takes over.
			route, rtt := s.route()
			if rtt > 0 && (minRTT == 0 || rtt < minRTT) {
				minRTT = rtt
			}
			if estimator != nil {
				m := estimator.GetStats()
				now := fmt.Sprint(m["delayMeasurement"], m["delayEstimate"], m["averageLoss"])
				deaf = now == heard && st.bytes > blindBytes
				switch {
				case now != heard:
					blindCap = min(maxKbps, blindCap*5/4+1)
				case !deaf:
				case rtt > 0 && rtt < minRTT+max(30, minRTT/4):
					blindCap = min(maxKbps, blindCap*11/10+1)
				default:
					blindCap = max(minKbps, held()/2)
				}
				heard = now
			}
			ceiling = maxKbps
			if route == "relay" && s.cfg.RelayMaxKbps > 0 {
				// Headroom for the packets' own headers (RTP, SRTP, UDP, TURN).
				ceiling = max(minKbps, s.cfg.RelayMaxKbps*85/100)
			}
			if kbps > ceiling {
				kbps = ceiling
				rateAt = time.Now()
				s.enc.SetBitrate(kbps)
			}
			if s.cfg.Stats != nil {
				out := st.flush(kbps)
				out.Encoder = s.enc.Name()
				if last != nil {
					out.Width, out.Height = last.Width&^1, last.Height&^1
				}
				out.Route, out.RTTMs = route, rtt
				s.cfg.Stats(out)
			}
		case <-s.kick:
			if p := pending; p != nil {
				pending, wait = nil, nil
				send(p)
			} else if last != nil {
				send(last)
			}
		case <-tick.C:
			// The first second of stillness: send the still picture again
			// so it sharpens, and a key frame when the viewer lost one.
			s.mu.Lock()
			needKey := s.needKey
			s.mu.Unlock()
			if last == nil || pending != nil || (time.Since(changedAt) > stillFor && !needKey) || time.Since(lastSent) < stillEvery ||
				(!needKey && overBudget(time.Now()) > 0) {
				continue
			}
			send(last)
		case p := <-pictures:
			changedAt = time.Now()
			pending = &p
			if wait != nil {
				continue // already due
			}
			if d := max(minInterval-time.Since(lastSent), overBudget(time.Now())); d > 0 {
				wait = time.After(d)
				continue
			}
			pending = nil
			send(&p)
		case <-wait:
			wait = nil
			if d := overBudget(time.Now()); d > 0 && pending != nil {
				wait = time.After(d)
				continue
			}
			if p := pending; p != nil {
				pending = nil
				send(p)
			}
		}
	}
}

func nalStartsKey(au []byte) bool { return len(au) > 4 && au[4]&0x1f == 7 }

// route names the path the picture takes (as clientaddr does for requests)
// and the link's round trip.
func (s *Session) route() (string, float64) {
	sctp := s.pc.SCTP()
	if sctp == nil {
		return "", 0
	}
	pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return "", 0
	}
	route := "direct"
	if pair.Local.Typ == webrtc.ICECandidateTypeRelay || pair.Remote.Typ == webrtc.ICECandidateTypeRelay {
		route = "relay"
	} else if ip := net.ParseIP(pair.Remote.Address); ip != nil {
		route = clientaddr.Kind(ip)
	}
	var rtt float64
	for _, v := range s.pc.GetStats() {
		if p, ok := v.(webrtc.ICECandidatePairStats); ok && p.Nominated && p.CurrentRoundTripTime > 0 {
			rtt = p.CurrentRoundTripTime * 1000
		}
	}
	return route, rtt
}

type statsWindow struct {
	start  time.Time
	frames int
	keys   int
	bytes  int
	encode []time.Duration
}

func (w *statsWindow) add(n int, took time.Duration, key bool) {
	w.frames++
	w.bytes += n
	if key {
		w.keys++
	}
	w.encode = append(w.encode, took)
}

func (w *statsWindow) flush(kbps int) Stats {
	out := Stats{Transport: "webrtc", Updates: w.frames, Video: w.frames, Bytes: w.bytes, Kbps: kbps, Keys: w.keys}
	if len(w.encode) > 0 {
		sort.Slice(w.encode, func(i, j int) bool { return w.encode[i] < w.encode[j] })
		out.EncodeMs = float64(w.encode[len(w.encode)/2].Microseconds()) / 1000
		out.EncodeMax = float64(w.encode[len(w.encode)-1].Microseconds()) / 1000
	}
	*w = statsWindow{start: time.Now(), encode: w.encode[:0]}
	return out
}
