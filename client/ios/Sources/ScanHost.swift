import AVFoundation
import CoreImage
import NativeShell
import PhotosUI
import UIKit

/// What pairing needs from the system:
/// - `scan.permission` → `granted` | `undetermined` (the system may still ask) | `denied` (only Settings can change it);
/// - `scan.request` → the same, after the system's camera prompt (or at once when it would not show);
/// - `app.openSettings`: the app's page in Settings;
/// - `app.share {text}`: the share sheet;
/// - `scan.image` → `{value}` (the QR code in a picture the person picks; `value` null when it holds none),
///   or nil when they picked nothing.
enum ScanHost {
  @MainActor
  static func register(_ app: NshAppView) {
    app.handle("scan.permission") { _ -> Any? in permission() }
    app.handle("scan.request") { _ -> Any? in
      guard AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined else { return permission() }
      _ = await AVCaptureDevice.requestAccess(for: .video)
      return permission()
    }
    app.handle("scan.image") { [weak app] _ -> Any? in
      guard let app, let presenter = app.window?.rootViewController else { throw ScanHostError.noWindow }
      var top = presenter
      while let shown = top.presentedViewController { top = shown }
      guard let image = await QrImagePicker.pick(from: top) else { return nil }
      if let value = QrImagePicker.read(image) { return ["value": value] }
      return ["value": NSNull()]
    }
    app.handle("app.openSettings") { _ -> Any? in
      guard let url = URL(string: UIApplication.openSettingsURLString) else { return nil }
      await UIApplication.shared.open(url)
      return nil
    }
    app.handle("app.share") { [weak app] data -> Any? in
      guard let text = (data as? [String: Any])?["text"] as? String else { throw ScanHostError.missing("text") }
      guard let app, let presenter = app.window?.rootViewController else { throw ScanHostError.noWindow }
      let sheet = UIActivityViewController(activityItems: [text], applicationActivities: nil)
      // iPad shows the sheet as a popover, which needs an anchor.
      sheet.popoverPresentationController?.sourceView = app
      sheet.popoverPresentationController?.sourceRect = CGRect(x: app.bounds.midX, y: app.bounds.midY, width: 1, height: 1)
      var top = presenter
      while let shown = top.presentedViewController { top = shown }
      top.present(sheet, animated: true)
      return nil
    }
  }

  static func permission() -> String {
    switch AVCaptureDevice.authorizationStatus(for: .video) {
    case .authorized: return "granted"
    case .notDetermined: return "undetermined"
    default: return "denied"
    }
  }
}

enum ScanHostError: LocalizedError {
  case missing(String)
  case noWindow
  var errorDescription: String? {
    switch self {
    case .missing(let name): return "\(name) is required"
    case .noWindow: return "Nothing on screen to share from."
    }
  }
}

/// The system photo picker (no photo library permission: only the picture the person chooses) and
/// Core Image's QR reader.
@MainActor
final class QrImagePicker: NSObject, PHPickerViewControllerDelegate {
  private var done: CheckedContinuation<UIImage?, Never>?
  private static var current: QrImagePicker?

  static func pick(from presenter: UIViewController) async -> UIImage? {
    current?.finish(nil)
    let picker = QrImagePicker()
    current = picker
    return await withCheckedContinuation { continuation in
      picker.done = continuation
      var configuration = PHPickerConfiguration()
      configuration.filter = .images
      configuration.selectionLimit = 1
      let controller = PHPickerViewController(configuration: configuration)
      controller.delegate = picker
      presenter.present(controller, animated: true)
    }
  }

  nonisolated static func read(_ image: UIImage) -> String? {
    guard let source = image.cgImage.map({ CIImage(cgImage: $0) }) ?? CIImage(image: image),
          let detector = CIDetector(ofType: CIDetectorTypeQRCode, context: nil, options: [CIDetectorAccuracy: CIDetectorAccuracyHigh])
    else { return nil }
    return detector.features(in: source).compactMap { ($0 as? CIQRCodeFeature)?.messageString }.first { !$0.isEmpty }
  }

  nonisolated func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
    // Only the image crosses to the main actor; the provider stays where the picker handed it.
    if let provider = results.first?.itemProvider, provider.canLoadObject(ofClass: UIImage.self) {
      provider.loadObject(ofClass: UIImage.self) { object, _ in
        let image = object as? UIImage
        Task { @MainActor in self.finish(image) }
      }
    } else {
      Task { @MainActor in self.finish(nil) }
    }
    Task { @MainActor in picker.dismiss(animated: true) }
  }

  private func finish(_ image: UIImage?) {
    done?.resume(returning: image)
    done = nil
    if QrImagePicker.current === self { QrImagePicker.current = nil }
  }
}
