import CryptoKit
import Foundation
import NativeShell
import Network
import Security

/// Mewla Relay's pinned TLS tunnel. Each tunnel is a 127.0.0.1 listener whose accepted
/// connections are bridged to a TLS 1.3 connection to host:port that trusts only a leaf
/// certificate whose SubjectPublicKeyInfo SHA-256 is the pin.
/// - `link.start {key, host, port, pin, mode}` → `{port, rttMs}`: mode `measure` does one pinned
///   handshake first and reports its round trip; `on-demand` listens at once (rttMs 0). A running
///   tunnel for the key with the same host, port, pin and mode is returned as it is; any other is
///   closed and replaced.
/// - `link.stop {key}`, `link.stopAll`: close tunnels.
/// - `link.events {key}` → `[String]`: what the tunnel saw, hop by hop, for a pairing that did not finish.
enum LinkTransportHost {
  @MainActor
  static func register(_ app: NshAppView) {
    app.handle("link.start") { data -> Any? in
      let input = (data as? [String: Any]) ?? [:]
      let key = input["key"] as? String ?? ""
      let host = input["host"] as? String ?? ""
      let port = integer(input["port"]) ?? 0
      let pin = input["pin"] as? String ?? ""
      let mode = input["mode"] as? String ?? ""
      try validate(key: key, host: host, port: port, pin: pin, mode: mode)

      if let existing = LinkTunnels.shared.reuseOrEvict(key: key, host: host, port: port, pin: pin, mode: mode) {
        return ["port": existing.port, "rttMs": existing.rttMilliseconds] as [String: Any]
      }
      guard let expectedPin = linkPinBytes(pin) else { throw LinkTransportError.invalidPin }
      let tunnel = LinkTunnel(host: host, port: UInt16(port), pin: expectedPin, measureBeforeListen: mode == "measure")
      let started: LinkTunnelStart
      do {
        started = try await withCheckedThrowingContinuation { continuation in
          tunnel.start { continuation.resume(with: $0) }
        }
      } catch {
        tunnel.stop()
        throw error
      }
      LinkTunnels.shared.store(tunnel, key: key)
      return ["port": started.port, "rttMs": started.rttMilliseconds] as [String: Any]
    }
    app.handle("link.stop") { data -> Any? in
      LinkTunnels.shared.remove(key: (data as? [String: Any])?["key"] as? String ?? "")
      return nil
    }
    app.handle("link.stopAll") { _ -> Any? in
      LinkTunnels.shared.removeAll()
      return nil
    }
    app.handle("link.events") { data -> Any? in
      LinkTunnels.shared.events(key: (data as? [String: Any])?["key"] as? String ?? "")
    }
  }

  /// A JSON number that is a whole number (JS sends every number as a double).
  private static func integer(_ value: Any?) -> Int? {
    if let int = value as? Int { return int }
    if let double = value as? Double, double.rounded() == double, abs(double) < 1e9 { return Int(double) }
    return nil
  }

  private static func validate(key: String, host: String, port: Int, pin: String, mode: String) throws {
    guard !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
      throw LinkTransportError.invalidInput("Mewla Relay tunnel key is required.")
    }
    guard !host.isEmpty, !host.contains("/"), !host.contains("\0") else {
      throw LinkTransportError.invalidInput("Mewla Relay host is invalid.")
    }
    guard (1...65535).contains(port) else {
      throw LinkTransportError.invalidInput("Mewla Relay port is invalid.")
    }
    guard pin.range(of: "^[0-9a-fA-F]{64}$", options: .regularExpression) != nil else {
      throw LinkTransportError.invalidPin
    }
    guard mode == "measure" || mode == "on-demand" else {
      throw LinkTransportError.invalidInput("Mewla Relay tunnel mode is invalid.")
    }
  }
}

enum LinkTransportError: LocalizedError {
  case invalidInput(String)
  case invalidPin
  case unavailable

  var errorDescription: String? {
    switch self {
    case .invalidInput(let detail): return detail
    case .invalidPin: return "Mewla Relay SPKI pin is invalid."
    case .unavailable: return "Mewla Relay could not be reached."
    }
  }
}

/// The running tunnels by key. Locks are taken only in these synchronous methods.
private final class LinkTunnels: @unchecked Sendable {
  static let shared = LinkTunnels()
  private let lock = NSLock()
  private var tunnels: [String: LinkTunnel] = [:]

  /// The running tunnel for the key when it serves the same endpoint and mode; otherwise the
  /// key's tunnel (if any) is closed and nil returned.
  func reuseOrEvict(key: String, host: String, port: Int, pin: String, mode: String) -> LinkTunnelStart? {
    lock.lock()
    if let existing = tunnels[key], existing.matches(host: host, port: port, pin: pin, mode: mode) {
      lock.unlock()
      return existing.current()
    }
    let previous = tunnels.removeValue(forKey: key)
    lock.unlock()
    previous?.stop()
    return nil
  }

  /// Two starts for one key may finish in either order: the last one stays, the other is closed.
  func store(_ tunnel: LinkTunnel, key: String) {
    lock.lock()
    let displaced = tunnels.updateValue(tunnel, forKey: key)
    lock.unlock()
    if let displaced, displaced !== tunnel { displaced.stop() }
  }

  func remove(key: String) {
    lock.lock()
    let tunnel = tunnels.removeValue(forKey: key)
    lock.unlock()
    tunnel?.stop()
  }

  func removeAll() {
    lock.lock()
    let current = Array(tunnels.values)
    tunnels.removeAll()
    lock.unlock()
    current.forEach { $0.stop() }
  }

  func events(key: String) -> [String] {
    lock.lock()
    let tunnel = tunnels[key]
    lock.unlock()
    return tunnel?.events.snapshot() ?? []
  }
}

private func pinnedServerName(_ host: String) -> String {
  let literal = host.trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
  if literal.contains(":") || literal.range(of: #"^\d{1,3}(?:\.\d{1,3}){3}$"#, options: .regularExpression) != nil {
    return "mewla-desktop.invalid"
  }
  return host
}

/// A short, capped account of one tunnel's life: connections, TLS states and the size and time
/// of every chunk that crossed, so a request that gets no answer can be placed on the phone,
/// the relay or the computer.
private final class LinkTunnelEvents: @unchecked Sendable {
  private let lock = NSLock()
  private let started = DispatchTime.now().uptimeNanoseconds
  private var lines: [String] = []

  func add(_ text: String) {
    let elapsed = (DispatchTime.now().uptimeNanoseconds - started) / 1_000_000
    lock.lock()
    if lines.count < 120 {
      lines.append("+\(elapsed)ms \(text)")
    }
    lock.unlock()
  }

  func snapshot() -> [String] {
    lock.lock()
    defer { lock.unlock() }
    return lines
  }
}

/// True for the first caller only: one outcome for a probe that may time out, fail or connect.
private final class LinkOnce: @unchecked Sendable {
  private let lock = NSLock()
  private var claimed = false

  func claim() -> Bool {
    lock.lock()
    defer { lock.unlock() }
    if claimed { return false }
    claimed = true
    return true
  }
}

private struct LinkTunnelStart: Sendable {
  let port: Int
  let rttMilliseconds: Int
}

private final class LinkTunnel: @unchecked Sendable {
  private static let connectionLimit = 64

  private let host: String
  private let port: UInt16
  private let pin: Data
  private let measureBeforeListen: Bool
  private let queue = DispatchQueue(label: "dev.mewla.link.transport")
  private let lock = NSLock()
  let events = LinkTunnelEvents()
  private var listener: NWListener?
  private var connections: [ObjectIdentifier: LinkBridge] = [:]
  private var stopped = false
  private var startCompletion: (@Sendable (Result<LinkTunnelStart, Error>) -> Void)?
  private var localPort = 0
  private var lastRTTMilliseconds = 0

  init(host: String, port: UInt16, pin: Data, measureBeforeListen: Bool) {
    self.host = host
    self.port = port
    self.pin = pin
    self.measureBeforeListen = measureBeforeListen
  }

  func matches(host: String, port: Int, pin: String, mode: String) -> Bool {
    guard let decoded = linkPinBytes(pin) else { return false }
    lock.lock()
    defer { lock.unlock() }
    return !stopped &&
      self.host == host &&
      self.port == UInt16(port) &&
      self.pin == decoded &&
      self.measureBeforeListen == (mode == "measure")
  }

  func current() -> LinkTunnelStart {
    lock.lock()
    defer { lock.unlock() }
    return LinkTunnelStart(port: localPort, rttMilliseconds: lastRTTMilliseconds)
  }

  /// Calls `completion` exactly once: with the listening port, or with why it could not start.
  func start(completion: @escaping @Sendable (Result<LinkTunnelStart, Error>) -> Void) {
    lock.lock()
    startCompletion = completion
    lock.unlock()
    guard measureBeforeListen else {
      startListener(rttMilliseconds: 0)
      return
    }
    probe { result in
      switch result {
      case .failure(let error):
        self.finishStart(.failure(error))
      case .success(let rtt):
        if self.isStopped {
          self.finishStart(.failure(LinkTransportError.unavailable))
        } else {
          self.startListener(rttMilliseconds: rtt)
        }
      }
    }
  }

  private var isStopped: Bool {
    lock.lock()
    defer { lock.unlock() }
    return stopped
  }

  private func startListener(rttMilliseconds: Int) {
    do {
      let parameters = NWParameters.tcp
      parameters.requiredLocalEndpoint = .hostPort(host: NWEndpoint.Host("127.0.0.1"), port: .any)
      let listener = try NWListener(using: parameters, on: .any)
      lock.lock()
      self.listener = listener
      lock.unlock()
      listener.newConnectionHandler = { [weak self] local in
        self?.accept(local: local)
      }
      listener.stateUpdateHandler = { [weak self, weak listener] state in
        guard let self else { return }
        switch state {
        case .ready:
          guard let localPort = listener?.port else {
            self.finishStart(.failure(LinkTransportError.unavailable))
            return
          }
          self.lock.lock()
          guard !self.stopped else {
            self.lock.unlock()
            self.finishStart(.failure(LinkTransportError.unavailable))
            return
          }
          self.localPort = Int(localPort.rawValue)
          self.lastRTTMilliseconds = rttMilliseconds
          self.lock.unlock()
          self.finishStart(.success(LinkTunnelStart(port: Int(localPort.rawValue), rttMilliseconds: rttMilliseconds)))
        case .failed(let error):
          self.finishStart(.failure(error))
          self.stop()
        case .cancelled:
          // Stopped before it was ready.
          self.finishStart(.failure(LinkTransportError.unavailable))
        default:
          break
        }
      }
      listener.start(queue: queue)
    } catch {
      finishStart(.failure(error))
    }
  }

  private func finishStart(_ result: Result<LinkTunnelStart, Error>) {
    lock.lock()
    let completion = startCompletion
    startCompletion = nil
    lock.unlock()
    completion?(result)
  }

  func stop() {
    lock.lock()
    if stopped {
      lock.unlock()
      return
    }
    stopped = true
    let currentListener = listener
    let currentConnections = Array(connections.values)
    connections.removeAll()
    lock.unlock()
    currentListener?.cancel()
    currentConnections.forEach { $0.stop() }
  }

  private func probe(completion: @escaping @Sendable (Result<Int, Error>) -> Void) {
    let started = DispatchTime.now().uptimeNanoseconds
    let connection = makeRemoteConnection()
    let once = LinkOnce()
    connection.stateUpdateHandler = { state in
      switch state {
      case .ready:
        guard once.claim() else { return }
        let elapsed = DispatchTime.now().uptimeNanoseconds - started
        connection.cancel()
        completion(.success(max(1, Int(elapsed / 1_000_000))))
      case .failed(let error):
        guard once.claim() else { return }
        connection.cancel()
        completion(.failure(error))
      default:
        break
      }
    }
    connection.start(queue: queue)
    queue.asyncAfter(deadline: .now() + 5) {
      guard once.claim() else { return }
      connection.cancel()
      completion(.failure(LinkTransportError.unavailable))
    }
  }

  private func accept(local: NWConnection) {
    lock.lock()
    let shouldReject = stopped || connections.count >= Self.connectionLimit
    lock.unlock()
    if shouldReject {
      local.cancel()
      return
    }

    events.add("accepted a local connection")
    let bridge = LinkBridge(local: local, remote: makeRemoteConnection(), events: events, queue: queue) { [weak self] identifier in
      guard let self else { return }
      self.lock.lock()
      self.connections.removeValue(forKey: identifier)
      self.lock.unlock()
    }
    lock.lock()
    connections[ObjectIdentifier(bridge)] = bridge
    lock.unlock()
    bridge.start()
  }

  private func makeRemoteConnection() -> NWConnection {
    let tls = NWProtocolTLS.Options()
    sec_protocol_options_set_min_tls_protocol_version(tls.securityProtocolOptions, .TLSv13)
    sec_protocol_options_set_tls_server_name(tls.securityProtocolOptions, pinnedServerName(host))
    let expectedPin = pin
    sec_protocol_options_set_verify_block(
      tls.securityProtocolOptions,
      { _, secTrust, complete in
        let trust = sec_trust_copy_ref(secTrust).takeRetainedValue()
        guard
          let certificate = (SecTrustCopyCertificateChain(trust) as? [SecCertificate])?.first,
          let key = SecCertificateCopyKey(certificate),
          let rawKey = SecKeyCopyExternalRepresentation(key, nil) as Data?
        else {
          complete(false)
          return
        }
        // The daemon pins SHA-256 of the certificate's DER SubjectPublicKeyInfo.
        // Security returns only the raw key, so rebuild the SPKI header for it.
        guard let prefix = subjectPublicKeyInfoPrefix(forRawKeyCount: rawKey.count) else {
          complete(false)
          return
        }
        let actualPin = Data(SHA256.hash(data: prefix + rawKey))
        complete(actualPin == expectedPin)
      },
      queue
    )
    let parameters = NWParameters(tls: tls, tcp: NWProtocolTCP.Options())
    // The port was validated to 1...65535.
    return NWConnection(host: NWEndpoint.Host(host), port: NWEndpoint.Port(rawValue: port)!, using: parameters)
  }
}

/// SubjectPublicKeyInfo DER headers for the raw keys SecKeyCopyExternalRepresentation returns:
/// the daemon's P-256 transport key (uncompressed 65-byte point) and the RFC 8410 Ed25519 key
/// older daemons used.
private func subjectPublicKeyInfoPrefix(forRawKeyCount count: Int) -> Data? {
  switch count {
  case 65:
    return Data([
      0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
      0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
    ])
  case 32:
    return Data([0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00])
  default:
    return nil
  }
}

/// One accepted local connection and its pinned remote, copied both ways.
private final class LinkBridge: @unchecked Sendable {
  private static let halfClosedGraceSeconds: Double = 5

  private let local: NWConnection
  private let remote: NWConnection
  private let events: LinkTunnelEvents
  private let queue: DispatchQueue
  private let finished: @Sendable (ObjectIdentifier) -> Void
  private let lock = NSLock()
  private var directionsFinished = 0
  private var stopped = false

  init(
    local: NWConnection,
    remote: NWConnection,
    events: LinkTunnelEvents,
    queue: DispatchQueue,
    finished: @escaping @Sendable (ObjectIdentifier) -> Void
  ) {
    self.local = local
    self.remote = remote
    self.events = events
    self.queue = queue
    self.finished = finished
  }

  func start() {
    local.stateUpdateHandler = { [events = self.events] state in
      events.add("local \(state)")
    }
    local.start(queue: queue)
    remote.stateUpdateHandler = { [weak self] state in
      guard let self else { return }
      self.events.add("remote \(state)")
      switch state {
      case .ready:
        self.pump(from: self.local, to: self.remote, label: "up")
        self.pump(from: self.remote, to: self.local, label: "down")
      case .failed, .cancelled:
        self.stop()
      default:
        break
      }
    }
    remote.start(queue: queue)
  }

  func stop() {
    lock.lock()
    if stopped {
      lock.unlock()
      return
    }
    stopped = true
    lock.unlock()
    events.add("bridge stopped")
    local.cancel()
    remote.cancel()
    finished(ObjectIdentifier(self))
  }

  private func pump(from source: NWConnection, to destination: NWConnection, label: String) {
    source.receive(minimumIncompleteLength: 1, maximumLength: 32 * 1024) { [weak self] data, _, complete, error in
      guard let self else { return }
      self.events.add("\(label) read \(data?.count ?? 0)B complete=\(complete) error=\(String(describing: error))")
      if error != nil {
        self.stop()
        return
      }
      // The end of a side is a half-close: only the final message context tells the other
      // connection its peer is done. With the default context `isComplete` merely ends a
      // message, the far side never sees the end, and a request that got no answer waits
      // for its own 60 s timeout.
      destination.send(
        content: data,
        contentContext: complete ? .finalMessage : .defaultMessage,
        isComplete: complete,
        completion: .contentProcessed { sendError in
          self.events.add("\(label) wrote \(data?.count ?? 0)B error=\(String(describing: sendError))")
          if sendError != nil {
            self.stop()
          } else if complete {
            self.directionFinished()
          } else {
            self.pump(from: source, to: destination, label: label)
          }
        }
      )
    }
  }

  private func directionFinished() {
    lock.lock()
    directionsFinished += 1
    let done = directionsFinished == 2
    lock.unlock()
    if done {
      stop()
      return
    }
    // One side is done; the other gets a short time to finish what it is sending, then the
    // pair is closed so nothing is left half open.
    queue.asyncAfter(deadline: .now() + Self.halfClosedGraceSeconds) { [weak self] in
      self?.stop()
    }
  }
}

/// The pin's bytes; nil unless it is whole hex bytes.
private func linkPinBytes(_ hex: String) -> Data? {
  let normalized = hex.trimmingCharacters(in: .whitespacesAndNewlines)
  guard normalized.count % 2 == 0 else { return nil }
  var output = Data(capacity: normalized.count / 2)
  var index = normalized.startIndex
  while index < normalized.endIndex {
    let next = normalized.index(index, offsetBy: 2)
    guard let byte = UInt8(normalized[index..<next], radix: 16) else { return nil }
    output.append(byte)
    index = next
  }
  return output
}
