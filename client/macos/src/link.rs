//! Mewla Relay's pinned tunnels for the Mac, as the phone hosts open them (client/android `LinkTransportHost.kt`,
//! client/ios `LinkTransportHost.swift`): a loopback listener per key whose every connection is bridged to TLS 1.3 at
//! host:port, trusting only a leaf whose SubjectPublicKeyInfo SHA-256 is the pin the computer's code carried. The
//! app reaches a computer paired through Mewla Relay, and the LAN or Tailscale address it announces, only through one.
//! - `link.start {key, host, port, pin, mode}` → `{port, rttMs}`: mode `measure` first does one pinned handshake and
//!   reports its time, `on-demand` only listens. The same key with the same endpoint returns the running tunnel.
//! - `link.stop {key}`, `link.stopAll`.
//! - `link.events {key}` → `[]`: a failed handshake fails the connection it carried, which the app sees.

use std::collections::HashMap;
use std::io::{self, Read, Write};
use std::net::{Shutdown, SocketAddr, TcpListener, TcpStream, ToSocketAddrs};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::crypto::WebPkiSupportedAlgorithms;
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{ClientConfig, ClientConnection, DigitallySignedStruct, SignatureScheme};

const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(15);
const MAX_CONNECTIONS: usize = 64;

/// How a tunnel reaches its far end: by name (DNS) in the app; tests name the address.
pub type Dial = Arc<dyn Fn(&str, u16) -> io::Result<TcpStream> + Send + Sync>;

pub fn dial_by_name() -> Dial {
    Arc::new(|host: &str, port: u16| {
        let addresses: Vec<SocketAddr> = (host.trim_start_matches('[').trim_end_matches(']'), port).to_socket_addrs()?.collect();
        let mut last = io::Error::new(io::ErrorKind::NotFound, "no address");
        for address in addresses {
            match TcpStream::connect_timeout(&address, CONNECT_TIMEOUT) {
                Ok(stream) => return Ok(stream),
                Err(error) => last = error,
            }
        }
        Err(last)
    })
}

/// The tunnels the app has open, by key.
pub struct Tunnels {
    open: Mutex<HashMap<String, Arc<Proxy>>>,
    dial: Dial,
}

impl Tunnels {
    pub fn new(dial: Dial) -> Self {
        Tunnels { open: Mutex::new(HashMap::new()), dial }
    }

    /// `link.start`: the loopback port of the tunnel for `key`, and the measured handshake time (0 when not measured).
    pub fn start(&self, key: &str, host: &str, port: u16, pin: &str, mode: &str) -> Result<(u16, u32), String> {
        if key.trim().is_empty() {
            return Err("Mewla Relay tunnel key is required.".into());
        }
        if host.trim().is_empty() || host.contains('/') || host.contains('\0') {
            return Err("Mewla Relay host is invalid.".into());
        }
        if port == 0 {
            return Err("Mewla Relay port is invalid.".into());
        }
        let pin = decode_pin(pin).ok_or("Mewla Relay SPKI pin is invalid.")?;
        let measure = match mode {
            "measure" => true,
            "on-demand" => false,
            _ => return Err("Mewla Relay tunnel mode is invalid.".into()),
        };
        if let Some(existing) = self.open.lock().unwrap().get(key).cloned() {
            if existing.matches(host, port, &pin, measure) {
                return Ok((existing.local_port, existing.rtt_ms));
            }
            self.stop(key);
        }
        let proxy = Proxy::start(host, port, pin, measure, self.dial.clone())
            .map_err(|error| {
                log::warn!("link: {host}:{port} {mode}: {error}");
                "Mewla Relay could not be reached.".to_string()
            })?;
        let result = (proxy.local_port, proxy.rtt_ms);
        if let Some(previous) = self.open.lock().unwrap().insert(key.to_string(), proxy) {
            previous.close();
        }
        Ok(result)
    }

    /// `link.stop`: out of the map first, so a start for the same key right after never gets the closing tunnel.
    pub fn stop(&self, key: &str) {
        let proxy = self.open.lock().unwrap().remove(key);
        if let Some(proxy) = proxy {
            proxy.close();
        }
    }

    pub fn stop_all(&self) {
        let all: Vec<_> = self.open.lock().unwrap().drain().map(|(_, proxy)| proxy).collect();
        for proxy in all {
            proxy.close();
        }
    }
}

struct Proxy {
    host: String,
    port: u16,
    pin: [u8; 32],
    measure: bool,
    local_port: u16,
    rtt_ms: u32,
    running: AtomicBool,
    /// Every socket the tunnel has open, closed with it.
    sockets: Mutex<HashMap<usize, TcpStream>>,
    next: AtomicUsize,
    connections: AtomicUsize,
}

impl Proxy {
    fn start(host: &str, port: u16, pin: [u8; 32], measure: bool, dial: Dial) -> io::Result<Arc<Proxy>> {
        let config = pinned_config(pin);
        let mut rtt_ms = 0;
        if measure {
            let started = Instant::now();
            let (stream, _) = connect(&dial, &config, host, port)?;
            let _ = stream.shutdown(Shutdown::Both);
            rtt_ms = (started.elapsed().as_millis() as u32).max(1);
        }
        let listener = TcpListener::bind(("127.0.0.1", 0))?;
        let proxy = Arc::new(Proxy {
            host: host.to_string(),
            port,
            pin,
            measure,
            local_port: listener.local_addr()?.port(),
            rtt_ms,
            running: AtomicBool::new(true),
            sockets: Mutex::new(HashMap::new()),
            next: AtomicUsize::new(0),
            connections: AtomicUsize::new(0),
        });
        let owner = proxy.clone();
        std::thread::Builder::new().name("link-accept".into()).spawn(move || owner.accept(listener, dial, config))?;
        Ok(proxy)
    }

    fn matches(&self, host: &str, port: u16, pin: &[u8; 32], measure: bool) -> bool {
        self.host == host && self.port == port && &self.pin == pin && self.measure == measure && self.running.load(Ordering::SeqCst)
    }

    fn accept(self: Arc<Self>, listener: TcpListener, dial: Dial, config: Arc<ClientConfig>) {
        for local in listener.incoming() {
            if !self.running.load(Ordering::SeqCst) {
                break;
            }
            let Ok(local) = local else { continue };
            if self.connections.fetch_add(1, Ordering::SeqCst) >= MAX_CONNECTIONS {
                self.connections.fetch_sub(1, Ordering::SeqCst);
                let _ = local.shutdown(Shutdown::Both);
                continue;
            }
            let owner = self.clone();
            let (dial, config) = (dial.clone(), config.clone());
            let spawned = std::thread::Builder::new().name("link-bridge".into()).spawn(move || {
                let id = owner.track(&local);
                if let Err(error) = owner.bridge(local, &dial, &config) {
                    log::info!("link: a connection to {}:{} ended: {error}", owner.host, owner.port);
                }
                owner.untrack(id);
                owner.connections.fetch_sub(1, Ordering::SeqCst);
            });
            if spawned.is_err() {
                self.connections.fetch_sub(1, Ordering::SeqCst);
            }
        }
    }

    fn track(&self, stream: &TcpStream) -> usize {
        let id = self.next.fetch_add(1, Ordering::SeqCst);
        if let Ok(clone) = stream.try_clone() {
            self.sockets.lock().unwrap().insert(id, clone);
        }
        if !self.running.load(Ordering::SeqCst) {
            let _ = stream.shutdown(Shutdown::Both);
        }
        id
    }

    fn untrack(&self, id: usize) {
        self.sockets.lock().unwrap().remove(&id);
    }

    /// Carries one loopback connection over its own pinned TLS connection, both ways, until either side ends.
    fn bridge(&self, local: TcpStream, dial: &Dial, config: &Arc<ClientConfig>) -> io::Result<()> {
        let (remote, tls) = connect(dial, config, &self.host, self.port)?;
        let remote_id = self.track(&remote);
        let result = pump(local, remote, tls);
        self.untrack(remote_id);
        result
    }

    fn close(&self) {
        if !self.running.swap(false, Ordering::SeqCst) {
            return;
        }
        for (_, socket) in self.sockets.lock().unwrap().drain() {
            let _ = socket.shutdown(Shutdown::Both);
        }
        // Wakes the accept loop, which then sees the tunnel closed and lets its listener go.
        let _ = TcpStream::connect_timeout(&SocketAddr::from(([127, 0, 0, 1], self.local_port)), Duration::from_millis(500));
    }
}

/// A TCP connection to host:port with its pinned TLS 1.3 handshake done.
fn connect(dial: &Dial, config: &Arc<ClientConfig>, host: &str, port: u16) -> io::Result<(TcpStream, ClientConnection)> {
    let mut stream = dial(host, port)?;
    stream.set_nodelay(true)?;
    let name = ServerName::try_from(pinned_server_name(host)).map_err(|e| io::Error::new(io::ErrorKind::InvalidInput, e))?;
    let mut tls = ClientConnection::new(config.clone(), name).map_err(io::Error::other)?;
    stream.set_read_timeout(Some(HANDSHAKE_TIMEOUT))?;
    stream.set_write_timeout(Some(HANDSHAKE_TIMEOUT))?;
    while tls.is_handshaking() {
        tls.complete_io(&mut stream)?;
    }
    stream.set_read_timeout(None)?;
    stream.set_write_timeout(None)?;
    Ok((stream, tls))
}

/// Plain bytes from the app go into TLS records to the computer, and the computer's records come out as plain bytes,
/// each way on its own thread; the TLS state is shared, and a raw read of the network never holds it.
fn pump(local: TcpStream, remote: TcpStream, tls: ClientConnection) -> io::Result<()> {
    let tls = Arc::new(Mutex::new(tls));
    let (mut local_in, mut remote_out) = (local.try_clone()?, remote.try_clone()?);
    let up = {
        let tls = tls.clone();
        let (local, remote) = (local.try_clone()?, remote.try_clone()?);
        std::thread::Builder::new().name("link-up".into()).spawn(move || {
            let mut buffer = vec![0u8; 32 * 1024];
            loop {
                let n = match local_in.read(&mut buffer) {
                    Ok(0) | Err(_) => break,
                    Ok(n) => n,
                };
                let mut tls = tls.lock().unwrap();
                if tls.writer().write_all(&buffer[..n]).is_err() || flush(&mut tls, &mut remote_out).is_err() {
                    let _ = local.shutdown(Shutdown::Both);
                    let _ = remote.shutdown(Shutdown::Both);
                    return;
                }
            }
            // The app closed its side: the computer hears close_notify, then the end of the stream.
            let mut tls = tls.lock().unwrap();
            tls.send_close_notify();
            let _ = flush(&mut tls, &mut remote_out);
            let _ = remote.shutdown(Shutdown::Write);
        })?
    };
    let mut remote_in = remote.try_clone()?;
    let mut remote_back = remote.try_clone()?;
    let mut local_out = local.try_clone()?;
    let mut raw = vec![0u8; 32 * 1024];
    let mut plain = vec![0u8; 32 * 1024];
    let outcome = loop {
        let n = match remote_in.read(&mut raw) {
            Ok(0) => break Ok(()),
            Ok(n) => n,
            Err(error) => break Err(error),
        };
        let mut chunk = &raw[..n];
        let mut out = Vec::new();
        let mut ended = false;
        {
            let mut tls = tls.lock().unwrap();
            while !chunk.is_empty() {
                if let Err(error) = tls.read_tls(&mut chunk) {
                    return finish(&local, &remote, up, Err(error));
                }
                let state = match tls.process_new_packets() {
                    Ok(state) => state,
                    Err(error) => return finish(&local, &remote, up, Err(io::Error::other(error))),
                };
                loop {
                    match tls.reader().read(&mut plain) {
                        Ok(0) => {
                            ended = true;
                            break;
                        }
                        Ok(m) => out.extend_from_slice(&plain[..m]),
                        Err(error) if error.kind() == io::ErrorKind::WouldBlock => break,
                        Err(error) => return finish(&local, &remote, up, Err(error)),
                    }
                }
                if state.peer_has_closed() {
                    ended = true;
                }
            }
            // A key update or an alert to send back.
            if flush(&mut tls, &mut remote_back).is_err() {
                ended = true;
            }
        }
        if !out.is_empty() && local_out.write_all(&out).is_err() {
            break Ok(());
        }
        if ended {
            break Ok(());
        }
    };
    let _ = local.shutdown(Shutdown::Write);
    finish(&local, &remote, up, outcome)
}

fn finish(local: &TcpStream, remote: &TcpStream, up: std::thread::JoinHandle<()>, outcome: io::Result<()>) -> io::Result<()> {
    if outcome.is_err() {
        let _ = local.shutdown(Shutdown::Both);
    }
    let _ = remote.shutdown(Shutdown::Both);
    let _ = local.shutdown(Shutdown::Read);
    let _ = up.join();
    let _ = local.shutdown(Shutdown::Both);
    outcome
}

fn flush(tls: &mut ClientConnection, remote: &mut TcpStream) -> io::Result<()> {
    while tls.wants_write() {
        tls.write_tls(remote)?;
    }
    remote.flush()
}

/// An IP literal has no name to send; the computer's Link certificate answers this one (as the phones send it).
pub fn pinned_server_name(host: &str) -> String {
    let literal = host.trim().trim_start_matches('[').trim_end_matches(']');
    if literal.parse::<std::net::IpAddr>().is_ok() { "mewla-desktop.invalid".into() } else { host.trim().to_string() }
}

fn decode_pin(pin: &str) -> Option<[u8; 32]> {
    let pin = pin.trim();
    if pin.len() != 64 || !pin.bytes().all(|b| b.is_ascii_hexdigit()) {
        return None;
    }
    let mut out = [0u8; 32];
    for (index, byte) in out.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&pin[index * 2..index * 2 + 2], 16).ok()?;
    }
    Some(out)
}

fn pinned_config(pin: [u8; 32]) -> Arc<ClientConfig> {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let verifier = Arc::new(PinnedKey { pin, algorithms: provider.signature_verification_algorithms });
    let mut config = ClientConfig::builder_with_provider(provider)
        .with_protocol_versions(&[&rustls::version::TLS13])
        .expect("TLS 1.3 is supported")
        .dangerous()
        .with_custom_certificate_verifier(verifier)
        .with_no_client_auth();
    config.alpn_protocols = vec![b"http/1.1".to_vec()];
    Arc::new(config)
}

/// Trusts exactly the computer's transport key: the leaf's SubjectPublicKeyInfo (the whole DER SEQUENCE, as Go's
/// `RawSubjectPublicKeyInfo` and Android's `publicKey.encoded`) must hash to the pin; the handshake's signature is
/// still checked against that key. Names and issuers are not looked at.
#[derive(Debug)]
struct PinnedKey {
    pin: [u8; 32],
    algorithms: WebPkiSupportedAlgorithms,
}

/// The SHA-256 of a certificate's SubjectPublicKeyInfo, as the pin carries it.
pub fn spki_sha256(certificate: &CertificateDer<'_>) -> Option<[u8; 32]> {
    let leaf = webpki::EndEntityCert::try_from(certificate).ok()?;
    let digest = ring::digest::digest(&ring::digest::SHA256, leaf.subject_public_key_info().as_ref());
    digest.as_ref().try_into().ok()
}

impl ServerCertVerifier for PinnedKey {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        let actual = spki_sha256(end_entity).ok_or(rustls::Error::InvalidCertificate(rustls::CertificateError::BadEncoding))?;
        if actual != self.pin {
            return Err(rustls::Error::General("Mewla Relay transport certificate pin mismatch".into()));
        }
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(&self, _: &[u8], _: &CertificateDer<'_>, _: &DigitallySignedStruct) -> Result<HandshakeSignatureValid, rustls::Error> {
        Err(rustls::Error::PeerIncompatible(rustls::PeerIncompatible::Tls12NotOffered))
    }

    fn verify_tls13_signature(&self, message: &[u8], cert: &CertificateDer<'_>, dss: &DigitallySignedStruct) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(message, cert, dss, &self.algorithms)
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        self.algorithms.supported_schemes()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rustls::pki_types::PrivateKeyDer;
    use rustls::pki_types::pem::PemObject;
    use rustls::{ServerConfig, ServerConnection, StreamOwned};

    // A throwaway P-256 key and its self-signed certificate (CN=computer), as the daemon's Link identity is one: the
    // pin is what matters, not the name or the issuer.
    const CERT: &str = include_str!("../testdata/link-cert.pem");
    const KEY: &str = include_str!("../testdata/link-key.pem");

    fn cert() -> CertificateDer<'static> {
        CertificateDer::from_pem_slice(CERT.as_bytes()).unwrap()
    }

    fn pin_hex() -> String {
        spki_sha256(&cert()).unwrap().iter().map(|b| format!("{b:02x}")).collect()
    }

    /// A computer that answers TLS 1.3 with the test certificate and echoes every byte, upper-cased.
    fn computer() -> u16 {
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let config = Arc::new(
            ServerConfig::builder_with_provider(provider)
                .with_protocol_versions(&[&rustls::version::TLS13])
                .unwrap()
                .with_no_client_auth()
                .with_single_cert(vec![cert()], PrivateKeyDer::from_pem_slice(KEY.as_bytes()).unwrap())
                .unwrap(),
        );
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        std::thread::spawn(move || {
            for stream in listener.incoming().flatten() {
                let config = config.clone();
                std::thread::spawn(move || {
                    let mut tls = StreamOwned::new(ServerConnection::new(config).unwrap(), stream);
                    let mut buffer = [0u8; 4096];
                    loop {
                        match tls.read(&mut buffer) {
                            Ok(0) | Err(_) => break,
                            Ok(n) => {
                                if tls.write_all(&buffer[..n].to_ascii_uppercase()).is_err() {
                                    break;
                                }
                            }
                        }
                    }
                });
            }
        });
        port
    }

    fn to_loopback() -> Dial {
        Arc::new(|_host: &str, port: u16| TcpStream::connect(("127.0.0.1", port)))
    }

    #[test]
    fn a_tunnel_carries_bytes_both_ways_to_the_pinned_computer() {
        let far = computer();
        let tunnels = Tunnels::new(to_loopback());
        let (port, rtt) = tunnels.start("server:a:0", "quiet-otter.link.test", far, &pin_hex(), "measure").unwrap();
        assert!(rtt >= 1);
        for word in ["hello", "again"] {
            let mut app = TcpStream::connect(("127.0.0.1", port)).unwrap();
            app.write_all(word.as_bytes()).unwrap();
            let mut answer = vec![0u8; word.len()];
            app.read_exact(&mut answer).unwrap();
            assert_eq!(answer, word.to_uppercase().as_bytes());
        }
        // The same key and endpoint is the running tunnel.
        assert_eq!(tunnels.start("server:a:0", "quiet-otter.link.test", far, &pin_hex(), "measure").unwrap().0, port);
        tunnels.stop("server:a:0");
        let mut gone = TcpStream::connect(("127.0.0.1", port));
        if let Ok(stream) = gone.as_mut() {
            stream.set_read_timeout(Some(Duration::from_secs(2))).unwrap();
            let _ = stream.write_all(b"x");
            let mut buffer = [0u8; 1];
            assert!(matches!(stream.read(&mut buffer), Ok(0) | Err(_)));
        }
    }

    #[test]
    fn another_key_is_refused_and_nothing_reaches_it() {
        let far = computer();
        let tunnels = Tunnels::new(to_loopback());
        let wrong = "ab".repeat(32);
        assert_eq!(tunnels.start("server:a:0", "quiet-otter.link.test", far, &wrong, "measure").unwrap_err(), "Mewla Relay could not be reached.");
        // On demand the listener opens, and its connection ends without a byte from the computer.
        let (port, _) = tunnels.start("server:a:1", "10.0.0.5", far, &wrong, "on-demand").unwrap();
        let mut app = TcpStream::connect(("127.0.0.1", port)).unwrap();
        app.set_read_timeout(Some(Duration::from_secs(5))).unwrap();
        let _ = app.write_all(b"secret");
        let mut buffer = [0u8; 16];
        assert!(matches!(app.read(&mut buffer), Ok(0) | Err(_)));
    }

    #[test]
    fn checks_its_input_and_names_ip_literals_as_the_phones_do() {
        let tunnels = Tunnels::new(to_loopback());
        assert!(tunnels.start("", "h", 1, &pin_hex(), "measure").is_err());
        assert!(tunnels.start("k", "a/b", 1, &pin_hex(), "measure").is_err());
        assert!(tunnels.start("k", "h", 1, "zz", "measure").is_err());
        assert!(tunnels.start("k", "h", 1, &pin_hex(), "later").is_err());
        assert_eq!(pinned_server_name("192.168.1.20"), "mewla-desktop.invalid");
        assert_eq!(pinned_server_name("[fd7a::1]"), "mewla-desktop.invalid");
        assert_eq!(pinned_server_name("quiet-otter.m.mewla.ai"), "quiet-otter.m.mewla.ai");
    }
}
