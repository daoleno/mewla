import AVFoundation
import NativeShell
import UIKit

/// `mewla.qrScanner`: AVFoundation's QR reader. `scan` fires once with the first code read;
/// `unavailable` when the camera is refused or missing (the simulator has none).
final class QrScanner: NSObject, NshComponent, AVCaptureMetadataOutputObjectsDelegate {
  let view = PreviewView()
  private let events: NshComponentContext
  private let session = AVCaptureSession()
  private var configured = false
  private var done = false

  init(_ events: NshComponentContext) {
    self.events = events
    super.init()
    view.backgroundColor = .black
    view.preview.session = session
    view.preview.videoGravity = .resizeAspectFill
  }

  func setProp(_ name: String, _ value: Any?) {
    guard name == "active" else { return }
    if (value as? Bool) == true { start() } else { stop() }
  }

  private func start() {
    switch AVCaptureDevice.authorizationStatus(for: .video) {
    case .authorized:
      run()
    case .notDetermined:
      AVCaptureDevice.requestAccess(for: .video) { granted in
        DispatchQueue.main.async { granted ? self.run() : self.unavailable("camera access is off for Mewla") }
      }
    default:
      unavailable("camera access is off for Mewla")
    }
  }

  private func run() {
    if !configured {
      guard let camera = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: camera),
            session.canAddInput(input) else { return unavailable("no camera") }
      session.addInput(input)
      let output = AVCaptureMetadataOutput()
      session.addOutput(output)
      output.setMetadataObjectsDelegate(self, queue: .main)
      output.metadataObjectTypes = [.qr]
      configured = true
    }
    let session = self.session
    DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
  }

  private func stop() {
    let session = self.session
    DispatchQueue.global(qos: .userInitiated).async { session.stopRunning() }
  }

  private func unavailable(_ reason: String) {
    events.emit("unavailable", ["reason": reason])
  }

  func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
    guard !done, let value = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
    done = true
    events.emit("scan", ["value": value])
    stop()
  }

  deinit { session.stopRunning() }

  final class PreviewView: UIView {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
  }
}
