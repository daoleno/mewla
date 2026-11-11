import AVFoundation
import NativeShell
import UIKit

/// `mewla.qrScanner`: AVFoundation's QR reader, running while `active`. `scan {value}` for each code
/// read (a value again only after 1.5 s, so a rejected code still in view is not re-sent every frame);
/// `unavailable {reason, message}` with `permission` (the app asks, through `scan.request`) or
/// `camera` (none, as on the Simulator, or it would not start). It never asks for the camera itself.
final class QrScanner: NSObject, NshComponent, AVCaptureMetadataOutputObjectsDelegate {
  private let camera = PreviewView()
  var view: UIView { camera }
  private let events: NshComponentContext
  private let session = AVCaptureSession()
  private var configured = false
  private var active = false
  private var last = ""
  private var lastAt = Date.distantPast

  init(_ events: NshComponentContext) {
    self.events = events
    super.init()
    camera.backgroundColor = .black
    camera.preview.session = session
    camera.preview.videoGravity = .resizeAspectFill
  }

  func setProp(_ name: String, _ value: Any?) {
    guard name == "active" else { return }
    active = (value as? Bool) == true
    if active { start() } else { stop() }
  }

  private func start() {
    guard AVCaptureDevice.authorizationStatus(for: .video) == .authorized else {
      return unavailable("permission", "camera access is off for Mewla")
    }
    if !configured {
      guard let device = AVCaptureDevice.default(for: .video) else { return unavailable("camera", "no camera") }
      let input: AVCaptureDeviceInput
      do { input = try AVCaptureDeviceInput(device: device) } catch { return unavailable("camera", error.localizedDescription) }
      let output = AVCaptureMetadataOutput()
      guard session.canAddInput(input), session.canAddOutput(output) else { return unavailable("camera", "the camera is busy") }
      session.addInput(input)
      session.addOutput(output)
      output.setMetadataObjectsDelegate(self, queue: .main)
      guard output.availableMetadataObjectTypes.contains(.qr) else { return unavailable("camera", "this camera reads no QR codes") }
      output.metadataObjectTypes = [.qr]
      configured = true
    }
    let session = self.session
    DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
  }

  private func stop() {
    let session = self.session
    DispatchQueue.global(qos: .userInitiated).async { if session.isRunning { session.stopRunning() } }
  }

  private func unavailable(_ reason: String, _ message: String) {
    events.emit("unavailable", ["reason": reason, "message": message])
  }

  func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
    guard active, let value = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
    let now = Date()
    if value == last && now.timeIntervalSince(lastAt) < 1.5 { return }
    last = value
    lastAt = now
    events.emit("scan", ["value": value])
  }

  deinit { session.stopRunning() }

  final class PreviewView: UIView {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    // layerClass above makes this cast always succeed.
    var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
  }
}
