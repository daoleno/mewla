import Photos
import UIKit

/// A picture a chat shows (client/ui/chat/pictureActions.tsx), handed over as the app holds its original:
/// `source` is a data URL (the computer's file read whole) or a web address, decoded or downloaded here,
/// staged in the app's temporary folder (one at a time), then added to Photos (`files.saveImage`) or
/// shown in the share sheet (`files.shareImage`). FilesHost answers the calls; the tests drive this.
enum PictureSave {
  struct Failure: LocalizedError {
    let errorDescription: String?
    init(_ message: String) { errorDescription = message }
  }

  /// What a save added: its file name, where it went and the Photos asset (the tests read it back).
  struct Saved {
    let name: String
    let assetId: String?
  }

  /// Asks for add-only Photos access (the system's prompt the first time); the tests stand in for it.
  typealias Authorize = (@escaping (PHAuthorizationStatus) -> Void) -> Void
  static let photosAuthorize: Authorize = { done in PHPhotoLibrary.requestAuthorization(for: .addOnly, handler: done) }

  private static let extensions = [
    "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp", "image/heic": "heic",
    "image/heif": "heif", "image/bmp": "bmp", "image/tiff": "tiff", "image/svg+xml": "svg", "image/avif": "avif",
  ]

  /// The staged file's name: `name` with the extension of what the bytes are (a GIF named login.jpg is login.gif).
  static func fileName(_ given: String, type: String) -> String {
    let clean = given.replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "\\", with: "_").trimmingCharacters(in: .whitespacesAndNewlines)
    let name = clean.isEmpty ? "picture" : clean
    guard let ext = extensions[type.lowercased()] else { return name }
    let stem = (name as NSString).deletingPathExtension
    return "\(stem.isEmpty ? "picture" : stem).\(ext)"
  }

  /// Decodes (a data URL) or downloads (a web address) the original and stages it; `done` on any queue.
  static func stage(source: String, name: String, mimeType: String = "", _ done: @escaping (Result<URL, Error>) -> Void) {
    let write = { (bytes: Data, type: String) in
      do {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("pictures", isDirectory: true)
        try? FileManager.default.removeItem(at: folder)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let file = folder.appendingPathComponent(fileName(name, type: mimeType.isEmpty ? type : mimeType))
        try bytes.write(to: file)
        done(.success(file))
      } catch {
        done(.failure(error))
      }
    }
    if source.lowercased().hasPrefix("data:image/"), let comma = source.firstIndex(of: ","), source[..<comma].lowercased().hasSuffix(";base64") {
      let type = String(source[source.index(source.startIndex, offsetBy: 5)..<comma].split(separator: ";").first ?? "")
      guard let bytes = Data(base64Encoded: String(source[source.index(after: comma)...]), options: .ignoreUnknownCharacters) else {
        return done(.failure(Failure("The picture is unreadable.")))
      }
      DispatchQueue.global(qos: .userInitiated).async { write(bytes, type) }
    } else if let url = URL(string: source), url.scheme == "https" || url.scheme == "http" {
      URLSession.shared.dataTask(with: url) { bytes, response, error in
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard let bytes, error == nil, (200..<300).contains(status) else {
          return done(.failure(Failure("The picture did not download (HTTP \(status)).")))
        }
        write(bytes, (response?.mimeType ?? "").lowercased())
      }.resume()
    } else {
      done(.failure(Failure("No picture to save.")))
    }
  }

  /// Stages the original and adds it to Photos as its own bytes; `done` on the main queue.
  static func save(source: String, name: String, mimeType: String = "", authorize: @escaping Authorize = photosAuthorize, _ done: @escaping (Result<Saved, Error>) -> Void) {
    let finish = { (result: Result<Saved, Error>) in DispatchQueue.main.async { done(result) } }
    stage(source: source, name: name, mimeType: mimeType) { staged in
      let file: URL
      switch staged {
      case let .failure(error): return finish(.failure(error))
      case let .success(url): file = url
      }
      authorize { status in
        guard status == .authorized || status == .limited else { return finish(.failure(Failure("Photos permission denied."))) }
        var assetId: String?
        PHPhotoLibrary.shared().performChanges({
          let options = PHAssetResourceCreationOptions()
          options.originalFilename = file.lastPathComponent
          let request = PHAssetCreationRequest.forAsset()
          request.addResource(with: .photo, fileURL: file, options: options)
          assetId = request.placeholderForCreatedAsset?.localIdentifier
        }) { saved, error in
          finish(saved ? .success(Saved(name: file.lastPathComponent, assetId: assetId)) : .failure(error ?? Failure("Photos did not take the picture.")))
        }
      }
    }
  }

  /// The share sheet for a staged file, shown over `top` (on an iPad, a popover from the middle of it).
  @MainActor
  @discardableResult
  static func present(file: URL, over top: UIViewController, shown: (() -> Void)? = nil) -> UIActivityViewController {
    let sheet = UIActivityViewController(activityItems: [file], applicationActivities: nil)
    sheet.popoverPresentationController?.sourceView = top.view
    sheet.popoverPresentationController?.sourceRect = CGRect(x: top.view.bounds.midX, y: top.view.bounds.midY, width: 0, height: 0)
    sheet.popoverPresentationController?.permittedArrowDirections = []
    top.present(sheet, animated: true, completion: shown)
    return sheet
  }
}
