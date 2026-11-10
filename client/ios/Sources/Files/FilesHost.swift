import NativeShell
import UIKit
import UniformTypeIdentifiers

/// `files.*` (client/ui/more/files/host.ts): the document picker for attachments and for the
/// download folder, the streamed upload and download, and the `mewla.pdf` component.
/// Picked files and folders stay security-scoped URLs here; JS holds only their tokens.
/// The handlers keep this object alive (they capture it), as the scene keeps the app view.
@MainActor
final class FilesHost: NSObject, UIDocumentPickerDelegate {
  private let app: NshAppView
  private let transfers = Transfers()
  private var picked: [String: URL] = [:]
  private var folders: [String: URL] = [:]
  private var picking: (reply: NshHostReply, maxCount: Int)?
  private var choosingFolder: NshHostReply?
  private weak var filePicker: UIDocumentPickerViewController?

  init(_ app: NshAppView) {
    self.app = app
    super.init()
    transfers.onProgress = { [weak app] id, sent, total in
      Task { @MainActor in app?.send("files.uploadProgress", ["uploadId": id, "bytesSent": sent, "totalBytes": total]) }
    }
    app.handle("files.pick") { [self] data, reply in pick(data, reply) }
    app.handle("files.upload") { [self] data, reply in upload(data, reply) }
    app.handle("files.cancel") { [self] data, reply in reply.resolve(transfers.cancel(id: Self.string(data, "uploadId"))) }
    app.handle("files.pickDirectory") { [self] _, reply in pickDirectory(reply) }
    app.handle("files.download") { [self] data, reply in download(data, reply) }
    app.handle("files.cancelDownload") { [self] data, reply in reply.resolve(transfers.cancel(id: Self.string(data, "downloadId"))) }
    app.register(component: "mewla.pdf") { PdfComponent($0) }
  }

  // MARK: Picking

  private func pick(_ data: Any?, _ reply: NshHostReply) {
    let maxCount = Self.number(data, "maxCount").map { Int($0) } ?? 0
    guard (1...8).contains(maxCount) else { return reply.reject("Choose between one and eight files.") }
    picking?.reply.resolve([])
    picking = (reply, maxCount)
    let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.item], asCopy: false)
    picker.allowsMultipleSelection = true
    filePicker = picker
    present(picker) { reply.reject("Could not open the file picker.") }
  }

  private func pickDirectory(_ reply: NshHostReply) {
    choosingFolder?.resolve(nil)
    choosingFolder = reply
    let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.folder], asCopy: false)
    present(picker) { reply.reject("Could not open the folder picker.") }
  }

  func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
    if controller === filePicker, let current = picking {
      picking = nil
      guard urls.count <= current.maxCount else {
        return current.reply.reject("Choose up to \(current.maxCount) files. Remove an attachment to make room.")
      }
      current.reply.resolve(urls.map { describe($0) })
    } else if let reply = choosingFolder, let folder = urls.first {
      choosingFolder = nil
      let token = "folder-\(UUID().uuidString)"
      folders[token] = folder
      reply.resolve(["token": token, "name": folder.lastPathComponent])
    }
  }

  func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
    if controller === filePicker, let picking {
      self.picking = nil
      picking.reply.resolve([])
    } else if let reply = choosingFolder {
      choosingFolder = nil
      reply.resolve(nil)
    }
  }

  /// A picked file as JS sees it; one the app cannot read says why instead of uploading.
  private func describe(_ url: URL) -> [String: Any] {
    let token = "picked-\(UUID().uuidString)"
    picked[token] = url
    let scoped = url.startAccessingSecurityScopedResource()
    defer { if scoped { url.stopAccessingSecurityScopedResource() } }
    let values = try? url.resourceValues(forKeys: [.nameKey, .fileSizeKey, .contentTypeKey, .isDirectoryKey, .isReadableKey])
    var asset: [String: Any] = [
      "uri": token,
      "name": values?.name ?? url.lastPathComponent,
      "mimeType": values?.contentType?.preferredMIMEType ?? "application/octet-stream",
    ]
    // An unknown size is left out: the upload then goes chunked.
    if let size = values?.fileSize { asset["size"] = size }
    if values?.isDirectory == true {
      asset["selectionError"] = "Cannot read this file. Choose a file, not a folder."
      asset["selectionRetryable"] = false
    } else if values?.isReadable == false {
      asset["selectionError"] = "Cannot read this file. Access was denied. Select the file again."
      asset["selectionRetryable"] = true
    }
    return asset
  }

  // MARK: Transfers

  private func upload(_ data: Any?, _ reply: NshHostReply) {
    let id = Self.string(data, "uploadId")
    guard !id.isEmpty, let url = Self.httpURL(data, "url") else { return reply.reject("Upload request is invalid.") }
    guard let file = picked[Self.string(data, "fileUri")] else { return reply.reject("Select the file again.") }
    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.cachePolicy = .reloadIgnoringLocalCacheData
    Self.headers(data).forEach { request.setValue($0.value, forHTTPHeaderField: $0.key) }
    request.setValue(nil, forHTTPHeaderField: "Content-Length")
    request.setValue("ios-native-v1", forHTTPHeaderField: "X-Mewla-Upload-Transport")
    // The file stays readable for as long as its bytes are sent.
    let scoped = file.startAccessingSecurityScopedResource()
    do {
      try transfers.upload(id: id, request: request, file: file) { result in
        if scoped { file.stopAccessingSecurityScopedResource() }
        Task { @MainActor in
          switch result {
          case .success(.upload(let answer)): reply.resolve(["status": answer.status, "body": answer.body])
          case .success: reply.reject("Could not upload the attachment.")
          case .failure(let error): reply.reject(error.localizedDescription)
          }
        }
      }
    } catch {
      if scoped { file.stopAccessingSecurityScopedResource() }
      reply.reject(error.localizedDescription)
    }
  }

  private func download(_ data: Any?, _ reply: NshHostReply) {
    let id = Self.string(data, "downloadId")
    guard !id.isEmpty, let url = Self.httpURL(data, "url") else { return reply.reject("Download request is invalid.") }
    guard let folder = folders[Self.string(data, "directory")] else { return reply.reject("Choose the folder again.") }
    let names = ((data as? [String: Any])?["names"] as? [String] ?? []).filter { !$0.isEmpty }
    let maxBytes = Self.number(data, "maxBytes").map { Int64($0) } ?? 0
    let expected = Self.number(data, "expectedSize").map { Int64($0) }
    guard !names.isEmpty, maxBytes > 0 else { return reply.reject("Download request is invalid.") }
    var request = URLRequest(url: url)
    request.cachePolicy = .reloadIgnoringLocalCacheData
    Self.headers(data).forEach { request.setValue($0.value, forHTTPHeaderField: $0.key) }
    request.setValue("ios-native-v1", forHTTPHeaderField: "X-Mewla-Download-Transport")
    do {
      try transfers.download(id: id, request: request, maxBytes: maxBytes) { result in
        let answer: Result<[String: Any], Error> = result.flatMap { outcome in
          guard case .download(let bytes, let file) = outcome else { return .failure(FilesError("Session file download failed.")) }
          defer { try? FileManager.default.removeItem(at: file) }
          if let expected, bytes != expected {
            return .failure(FilesError("Session file download was truncated (expected \(expected) bytes, received \(bytes))."))
          }
          return Result { try Self.move(file, into: folder, names: names) }.map { ["bytesWritten": bytes, "name": $0] }
        }
        Task { @MainActor in
          switch answer {
          case .success(let value): reply.resolve(value)
          case .failure(let error): reply.reject(error.localizedDescription)
          }
        }
      }
    } catch {
      reply.reject(error.localizedDescription)
    }
  }

  /// Moves the finished download to the first of `names` the folder does not have.
  nonisolated private static func move(_ file: URL, into folder: URL, names: [String]) throws -> String {
    let scoped = folder.startAccessingSecurityScopedResource()
    defer { if scoped { folder.stopAccessingSecurityScopedResource() } }
    var failure: Error?
    var saved: String?
    NSFileCoordinator().coordinate(writingItemAt: folder, options: [], error: nil) { target in
      guard let name = names.first(where: { !FileManager.default.fileExists(atPath: target.appendingPathComponent($0).path) }) else {
        failure = FilesError("This folder already has files with this name. Choose another folder.")
        return
      }
      do {
        try FileManager.default.copyItem(at: file, to: target.appendingPathComponent(name))
        saved = name
      } catch {
        failure = FilesError("Could not save the file in the selected folder.")
      }
    }
    if let failure { throw failure }
    guard let saved else { throw FilesError("Could not save the file in the selected folder.") }
    return saved
  }

  // MARK: Helpers

  private func present(_ picker: UIDocumentPickerViewController, failed: () -> Void) {
    picker.delegate = self
    guard var top = app.window?.rootViewController else { return failed() }
    while let next = top.presentedViewController { top = next }
    top.present(picker, animated: true)
  }

  nonisolated private static func string(_ data: Any?, _ key: String) -> String {
    (data as? [String: Any])?[key] as? String ?? ""
  }

  nonisolated private static func number(_ data: Any?, _ key: String) -> Double? {
    ((data as? [String: Any])?[key] as? NSNumber)?.doubleValue
  }

  nonisolated private static func httpURL(_ data: Any?, _ key: String) -> URL? {
    guard let url = URL(string: string(data, key)), url.scheme == "http" || url.scheme == "https" else { return nil }
    return url
  }

  nonisolated private static func headers(_ data: Any?) -> [String: String] {
    (data as? [String: Any])?["headers"] as? [String: String] ?? [:]
  }
}
