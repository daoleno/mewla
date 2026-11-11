import Darwin
import Foundation
import MetricKit
import NativeShell
import UIKit

/// What took the app down, kept in Application Support/crashes until the person has seen it
/// (`app.crashReports`, `app.clearCrashReports`): an uncaught NSException, a fatal signal (a Swift
/// trap, a bad access), MetricKit's crash diagnostics when the system delivers them, and the app's
/// own JS errors. At most `keep` reports, newest kept.
enum CrashLog {
  private static let keep = 8
  private static let limit = 64 * 1024

  static var folder: URL {
    let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
    let url = base.appendingPathComponent("crashes", isDirectory: true)
    try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
    return url
  }

  /// First thing at launch: turns a signal caught last time into a report, then arms the handlers.
  static func install() {
    let signalFile = folder.appendingPathComponent("signal.log")
    if let data = try? Data(contentsOf: signalFile), !data.isEmpty {
      let at = (try? FileManager.default.attributesOfItem(atPath: signalFile.path)[.modificationDate] as? Date) ?? Date()
      write("native", String(decoding: data, as: UTF8.self), at: at, header: false)
    }
    NSSetUncaughtExceptionHandler { exception in
      let body = "Uncaught \(exception.name.rawValue): \(exception.reason ?? "")\n\n" + exception.callStackSymbols.joined(separator: "\n")
      CrashLog.write("native", body)
    }
    armSignals(path: signalFile.path, header: header(kind: "native", at: Date()))
    if #available(iOS 14.0, *) { MXMetricManager.shared.add(Diagnostics.shared) }
  }

  /// An error the app's JS reported (NshAppView.onError in a release build).
  static func js(_ message: String) { write("js", message) }

  @MainActor
  static func register(_ app: NshAppView) {
    app.handle("app.crashReports") { _ -> Any? in list() }
    app.handle("app.clearCrashReports") { data -> Any? in
      let ids = ((data as? [String: Any])?["ids"] as? [Any])?.compactMap { $0 as? String } ?? []
      for id in ids where !id.contains("/") && id.hasSuffix(".txt") {
        try? FileManager.default.removeItem(at: folder.appendingPathComponent(id))
      }
      return nil
    }
  }

  private static func list() -> [[String: Any]] {
    let files = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)) ?? []
    return files.filter { $0.pathExtension == "txt" }.sorted { $0.lastPathComponent < $1.lastPathComponent }.compactMap { file in
      guard let text = try? String(contentsOf: file, encoding: .utf8) else { return nil }
      let parts = file.deletingPathExtension().lastPathComponent.split(separator: "-")
      return [
        "id": file.lastPathComponent,
        "kind": parts.count > 1 ? String(parts[1]) : "native",
        "at": Double(parts.first ?? "") ?? Date().timeIntervalSince1970 * 1000,
        "text": text,
      ]
    }
  }

  static func write(_ kind: String, _ body: String, at: Date = Date(), header withHeader: Bool = true) {
    var text = (withHeader ? header(kind: kind, at: at) : "") + body
    if text.utf8.count > limit { text = String(text.prefix(limit)) + "\n…" }
    let ms = Int64(at.timeIntervalSince1970 * 1000)
    let name = "\(ms)-\(kind)-\(Int.random(in: 0..<1_000_000)).txt"
    try? text.write(to: folder.appendingPathComponent(name), atomically: true, encoding: .utf8)
    let files = ((try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)) ?? [])
      .filter { $0.pathExtension == "txt" }.sorted { $0.lastPathComponent > $1.lastPathComponent }
    for file in files.dropFirst(keep) { try? FileManager.default.removeItem(at: file) }
  }

  private static func header(kind: String, at: Date) -> String {
    let info = Bundle.main.infoDictionary ?? [:]
    let version = info["CFBundleShortVersionString"] as? String ?? "?"
    let build = info["CFBundleVersion"] as? String ?? "?"
    var system = utsname()
    uname(&system)
    let machine = withUnsafeBytes(of: &system.machine) { String(decoding: $0.prefix { $0 != 0 }, as: UTF8.self) }
    #if DEBUG
    let flavor = "debug"
    #else
    let flavor = "release"
    #endif
    let time = ISO8601DateFormatter().string(from: at)
    return "Mewla \(version) (\(build), \(flavor)) \(Bundle.main.bundleIdentifier ?? "")\n"
      + "\(machine), iOS \(UIDevice.current.systemVersion)\n\(kind) at \(time)\n\n"
  }

  /// MetricKit hands over the system's crash reports (symbolicated frames' binary offsets), a day late at worst.
  @available(iOS 14.0, *)
  final class Diagnostics: NSObject, MXMetricManagerSubscriber {
    static let shared = Diagnostics()
    func didReceive(_ payloads: [MXDiagnosticPayload]) {
      for payload in payloads {
        for crash in payload.crashDiagnostics ?? [] {
          let json = String(decoding: crash.jsonRepresentation(), as: UTF8.self)
          CrashLog.write("native", "Crash reported by the system (MetricKit)\n\n" + json, at: payload.timeStampEnd)
        }
      }
    }
  }
}

// The signal handler may only call async-signal-safe functions: it writes into a file opened at
// launch, with the header formatted then, and the frames by backtrace_symbols_fd.
private var signalFd: Int32 = -1
/// One line per signal number ("<header>Fatal signal SIGSEGV\n\n"), built at launch.
private var signalLines: [UnsafeMutableBufferPointer<UInt8>] = []
private let frames = UnsafeMutablePointer<UnsafeMutableRawPointer?>.allocate(capacity: 64)
private let fatalSignals: [(Int32, String)] = [
  (SIGABRT, "SIGABRT"), (SIGSEGV, "SIGSEGV"), (SIGBUS, "SIGBUS"), (SIGILL, "SIGILL"), (SIGTRAP, "SIGTRAP"), (SIGFPE, "SIGFPE"),
]

private func armSignals(path: String, header: String) {
  signalFd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0o644)
  guard signalFd >= 0 else { return }
  _ = frames // a global is set up on first use; that must not be inside the handler
  signalLines = (0..<32).map { number in
    let name = fatalSignals.first { $0.0 == number }?.1 ?? "\(number)"
    let bytes = Array((header + "Fatal signal \(name)\n\n").utf8)
    let buffer = UnsafeMutableBufferPointer<UInt8>.allocate(capacity: bytes.count)
    _ = buffer.initialize(from: bytes)
    return buffer
  }
  for (sig, _) in fatalSignals {
    var action = sigaction()
    action.__sigaction_u.__sa_handler = onSignal
    sigemptyset(&action.sa_mask)
    sigaction(sig, &action, nil)
  }
}

private func onSignal(_ sig: Int32) {
  if sig >= 0 && Int(sig) < signalLines.count {
    let line = signalLines[Int(sig)]
    _ = write(signalFd, line.baseAddress, line.count)
  }
  let count = backtrace(frames, 64)
  backtrace_symbols_fd(frames, count, signalFd)
  close(signalFd)
  signal(sig, SIG_DFL)
  raise(sig)
}
