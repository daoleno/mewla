import AVFoundation
import NativeShell
import UIKit

/// What pairing needs from the system:
/// - `scan.permission` → `granted` | `undetermined` (the system may still ask) | `denied` (only Settings can change it);
/// - `scan.request` → the same, after the system's camera prompt (or at once when it would not show);
/// - `app.openSettings`: the app's page in Settings;
/// - `app.share {text}`: the share sheet.
enum ScanHost {
  @MainActor
  static func register(_ app: NshAppView) {
    app.handle("scan.permission") { _ -> Any? in permission() }
    app.handle("scan.request") { _ -> Any? in
      guard AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined else { return permission() }
      _ = await AVCaptureDevice.requestAccess(for: .video)
      return permission()
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
