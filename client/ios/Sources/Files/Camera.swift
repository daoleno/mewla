import AVFoundation
import UIKit

/// `files.capture` (client/ui/more/files/host.ts): a photo taken now for the composer's attachments.
/// The system camera (UIImagePickerController) takes it; it is kept as a JPEG in the app's caches
/// (`camera/`), and FilesHost hands it to JS as a picked file. The camera asks for itself the first
/// time, in the system's words (NSCameraUsageDescription); a refusal is one line. Captures older
/// than a day go when the next one is taken.
@MainActor
final class Camera: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
  enum Result {
    case taken(URL)
    case cancelled
    case failed(String)
  }

  static let denied = "Allow the camera for Mewla in Settings to take a photo."

  private var done: ((Result) -> Void)?

  static var available: Bool { UIImagePickerController.isSourceTypeAvailable(.camera) }

  func capture(from top: UIViewController, _ finished: @escaping (Result) -> Void) {
    guard Self.available else { return finished(.failed("This device has no camera.")) }
    switch AVCaptureDevice.authorizationStatus(for: .video) {
    case .authorized:
      present(from: top, finished)
    case .notDetermined:
      AVCaptureDevice.requestAccess(for: .video) { granted in
        Task { @MainActor in
          if granted { self.present(from: top, finished) } else { finished(.failed(Self.denied)) }
        }
      }
    default:
      finished(.failed(Self.denied))
    }
  }

  private func present(from top: UIViewController, _ finished: @escaping (Result) -> Void) {
    done?(.cancelled)
    done = finished
    let picker = UIImagePickerController()
    picker.sourceType = .camera
    picker.cameraCaptureMode = .photo
    picker.delegate = self
    top.present(picker, animated: true)
  }

  func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
    picker.dismiss(animated: true)
    let finished = done
    done = nil
    guard let image = info[.originalImage] as? UIImage, let jpeg = image.jpegData(compressionQuality: 0.9) else {
      finished?(.failed("Could not keep the photo."))
      return
    }
    do {
      finished?(.taken(try Self.keep(jpeg)))
    } catch {
      finished?(.failed("Could not keep the photo."))
    }
  }

  func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
    picker.dismiss(animated: true)
    done?(.cancelled)
    done = nil
  }

  private static func keep(_ jpeg: Data) throws -> URL {
    let caches = try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
    let folder = caches.appendingPathComponent("camera", isDirectory: true)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    let dayAgo = Date().addingTimeInterval(-24 * 60 * 60)
    for old in (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: [.contentModificationDateKey])) ?? [] {
      let modified = (try? old.resourceValues(forKeys: [.contentModificationDateKey]))?.contentModificationDate ?? .distantPast
      if modified < dayAgo { try? FileManager.default.removeItem(at: old) }
    }
    let stamp = DateFormatter()
    stamp.locale = Locale(identifier: "en_US_POSIX")
    stamp.dateFormat = "yyyyMMdd_HHmmss"
    let file = folder.appendingPathComponent("IMG_\(stamp.string(from: Date())).jpg")
    try jpeg.write(to: file, options: .atomic)
    return file
  }
}
