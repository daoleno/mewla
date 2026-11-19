import NativeShell
import Photos
import UIKit
import UniformTypeIdentifiers

/// `files.*` (client/ui/more/files/host.ts): the document picker for attachments and for the
/// download folder, the streamed upload and download, a chat's picture saved to Photos or shared
/// (`files.saveImage`, `files.shareImage`), and the `mewla.pdf` component.
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
    app.handle("files.thumbnail") { [self] data, reply in thumbnail(data, reply) }
    // This build saves and shares pictures: the UI shows Save and Share.
    app.handle("files.canSaveImage") { _, reply in reply.resolve(true) }
    app.handle("files.saveImage") { data, reply in Self.saveImage(data, reply) }
    app.handle("files.shareImage") { [self] data, reply in shareImage(data, reply) }
    app.register(component: "mewla.pdf") { PdfComponent($0) }
  }

  // MARK: Pictures

  /// A picture a chat shows (client/ui/chat/pictureActions.tsx), handed over as the app holds its original:
  /// `source` is a data URL (the computer's file read whole) or a web address, decoded or downloaded here,
  /// then staged in the app's temporary folder (one at a time) under `name`.
  nonisolated private static func stagePicture(_ data: Any?, _ done: @escaping (Result<URL, Error>) -> Void) {
    let source = string(data, "source")
    let given = string(data, "name").replacingOccurrences(of: "/", with: "_").trimmingCharacters(in: .whitespacesAndNewlines)
    let name = given.isEmpty ? "picture" : given
    let write = { (bytes: Data) in
      do {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("pictures", isDirectory: true)
        try? FileManager.default.removeItem(at: folder)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let file = folder.appendingPathComponent(name)
        try bytes.write(to: file)
        done(.success(file))
      } catch {
        done(.failure(error))
      }
    }
    if source.lowercased().hasPrefix("data:image/"), let comma = source.firstIndex(of: ","), source[..<comma].lowercased().hasSuffix(";base64") {
      guard let bytes = Data(base64Encoded: String(source[source.index(after: comma)...]), options: .ignoreUnknownCharacters) else {
        return done(.failure(PictureError("The picture is unreadable.")))
      }
      DispatchQueue.global(qos: .userInitiated).async { write(bytes) }
    } else if let url = URL(string: source), url.scheme == "https" || url.scheme == "http" {
      URLSession.shared.dataTask(with: url) { bytes, response, error in
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard let bytes, error == nil, (200..<300).contains(status) else {
          return done(.failure(PictureError("The picture did not download (HTTP \(status)).")))
        }
        write(bytes)
      }.resume()
    } else {
      done(.failure(PictureError("No picture to save.")))
    }
  }

  private struct PictureError: LocalizedError {
    let errorDescription: String?
    init(_ message: String) { errorDescription = message }
  }

  /// `files.saveImage {source, name, mimeType}` → `{name, where}`: the original's bytes added to Photos.
  /// Photos asks for add-only access the first time, at the moment of saving.
  nonisolated private static func saveImage(_ data: Any?, _ reply: NshHostReply) {
    stagePicture(data) { staged in
      guard case let .success(file) = staged else {
        if case let .failure(error) = staged { DispatchQueue.main.async { reply.reject(error.localizedDescription) } }
        return
      }
      let name = file.lastPathComponent
      PHPhotoLibrary.requestAuthorization(for: .addOnly) { status in
        guard status == .authorized || status == .limited else {
          return DispatchQueue.main.async { reply.reject("Photos permission denied.") }
        }
        PHPhotoLibrary.shared().performChanges({
          let options = PHAssetResourceCreationOptions()
          options.originalFilename = name
          PHAssetCreationRequest.forAsset().addResource(with: .photo, fileURL: file, options: options)
        }) { saved, error in
          DispatchQueue.main.async {
            if saved { reply.resolve(["name": name, "where": "Photos"]) }
            else { reply.reject(error?.localizedDescription ?? "Photos did not take the picture.") }
          }
        }
      }
    }
  }

  /// `files.shareImage {source, name, mimeType}`: the original as a file in the system share sheet.
  private func shareImage(_ data: Any?, _ reply: NshHostReply) {
    Self.stagePicture(data) { [weak self] staged in
      DispatchQueue.main.async {
        switch staged {
        case let .failure(error): reply.reject(error.localizedDescription)
        case let .success(file):
          guard let self, var top = self.app.window?.rootViewController else { return reply.reject("Could not open the share sheet.") }
          while let next = top.presentedViewController { top = next }
          let sheet = UIActivityViewController(activityItems: [file], applicationActivities: nil)
          // An iPad shows it as a popover from the middle of the window.
          sheet.popoverPresentationController?.sourceView = top.view
          sheet.popoverPresentationController?.sourceRect = CGRect(x: top.view.bounds.midX, y: top.view.bounds.midY, width: 0, height: 0)
          sheet.popoverPresentationController?.permittedArrowDirections = []
          top.present(sheet, animated: true)
          reply.resolve(true)
        }
      }
    }
  }

  // MARK: Thumbnails

  /// A picked picture scaled so its longer side is at most `maxPixels`, as a JPEG data URL;
  /// nil for a token this picker did not give or a file UIImage does not decode.
  private func thumbnail(_ data: Any?, _ reply: NshHostReply) {
    guard let file = picked[Self.string(data, "uri")] else { return reply.resolve(nil) }
    let maxPixels = CGFloat(min(max(Self.number(data, "maxPixels") ?? 256, 16), 4096))
    DispatchQueue.global(qos: .userInitiated).async {
      let scoped = file.startAccessingSecurityScopedResource()
      defer { if scoped { file.stopAccessingSecurityScopedResource() } }
      guard let image = UIImage(contentsOfFile: file.path), image.size.width > 0, image.size.height > 0 else {
        return DispatchQueue.main.async { reply.resolve(nil) }
      }
      let scale = min(1, maxPixels / max(image.size.width * image.scale, image.size.height * image.scale))
      let size = CGSize(width: max(1, image.size.width * image.scale * scale), height: max(1, image.size.height * image.scale * scale))
      let format = UIGraphicsImageRendererFormat()
      format.scale = 1
      let jpeg = UIGraphicsImageRenderer(size: size, format: format).jpegData(withCompressionQuality: 0.85) { _ in
        image.draw(in: CGRect(origin: .zero, size: size))
      }
      DispatchQueue.main.async { reply.resolve(["dataUrl": "data:image/jpeg;base64," + jpeg.base64EncodedString()]) }
    }
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
