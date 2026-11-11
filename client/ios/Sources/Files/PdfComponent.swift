import NativeShell
import PDFKit
import UIKit

/// `mewla.pdf` (client/ui/more/files/host.ts): PDFKit's scrolling pages. An http(s) `source` is
/// fetched with `headers` into a temp file first (at most 50 MiB) and deleted with the view;
/// a file:// one is read in place. `load {pageCount}` once it shows, `error {message}`.
final class PdfComponent: NshComponent {
  private static let limit: Int64 = 50 * 1024 * 1024
  private let pdf = PDFView()
  var view: UIView { pdf }
  private let events: NshComponentContext
  private var source: String?
  private var headers: [String: String] = [:]
  /// The size the file was inspected at: a stream of any other size is a changed file.
  private var expectedBytes: Int64?
  private var loaded: String?
  private var scheduled = false
  private var task: URLSessionDownloadTask?
  private var staged: URL?

  init(_ events: NshComponentContext) {
    self.events = events
    pdf.autoScales = true
    pdf.displayMode = .singlePageContinuous
    pdf.displayDirection = .vertical
    pdf.backgroundColor = .clear
    pdf.accessibilityLabel = "PDF preview"
  }

  deinit {
    task?.cancel()
    if let staged { try? FileManager.default.removeItem(at: staged) }
  }

  func setProp(_ name: String, _ value: Any?) {
    switch name {
    case "source": source = value as? String
    case "headers": headers = value as? [String: String] ?? [:]
    case "expectedBytes": expectedBytes = (value as? NSNumber).map { $0.int64Value }.flatMap { $0 > 0 ? $0 : nil }
    default: return
    }
    // The props of one render come together: open once they are all set.
    guard !scheduled else { return }
    scheduled = true
    DispatchQueue.main.async { [weak self] in
      self?.scheduled = false
      self?.open()
    }
  }

  private func open() {
    guard let target = source, target != loaded, let url = URL(string: target) else { return }
    loaded = target
    close()
    switch url.scheme {
    case "file": show(url)
    case "http", "https":
      var request = URLRequest(url: url)
      request.cachePolicy = .reloadIgnoringLocalCacheData
      headers.forEach { request.setValue($0.value, forHTTPHeaderField: $0.key) }
      let expected = expectedBytes
      let limit = min(expected ?? Self.limit, Self.limit)
      let next = URLSession.shared.downloadTask(with: request) { [weak self] file, response, error in
        // The temp file goes when this returns: keep it under a name of ours first.
        var kept: URL?
        var failure: String?
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        if status == 409 {
          failure = "The PDF changed before it could be opened."
        } else if let file, error == nil, (200..<300).contains(status) {
          let size = (try? file.resourceValues(forKeys: [.fileSizeKey]).fileSize).map { Int64($0) } ?? 0
          if size > limit {
            failure = "The PDF stream exceeded its inspected file size."
          } else if let expected, size != expected {
            failure = "The PDF changed while it was being downloaded."
          } else {
            let ours = FileManager.default.temporaryDirectory.appendingPathComponent("mewla-pdf-\(UUID().uuidString).pdf")
            if (try? FileManager.default.moveItem(at: file, to: ours)) != nil { kept = ours } else { failure = "Could not prepare the PDF preview." }
          }
        } else if (error as? URLError)?.code != .cancelled {
          failure = "Could not download the PDF into the secure preview."
        }
        DispatchQueue.main.async {
          guard let self, self.loaded == target else {
            if let kept { try? FileManager.default.removeItem(at: kept) }
            return
          }
          self.task = nil
          if let kept {
            self.staged = kept
            self.show(kept)
          } else if let failure {
            self.events.emit("error", ["message": failure])
          }
        }
      }
      task = next
      next.resume()
    default:
      events.emit("error", ["message": "This PDF could not be opened."])
    }
  }

  private func show(_ url: URL) {
    guard let document = PDFDocument(url: url) else {
      return events.emit("error", ["message": "This PDF could not be rendered on this device."])
    }
    pdf.document = document
    events.emit("load", ["pageCount": document.pageCount])
  }

  private func close() {
    task?.cancel()
    task = nil
    pdf.document = nil
    if let staged { try? FileManager.default.removeItem(at: staged) }
    staged = nil
  }
}
