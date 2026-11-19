import Photos
import UIKit
import XCTest
@testable import Mewla

/// `files.saveImage` / `files.shareImage` (PictureSave) at run time on the Simulator: a chat's picture handed
/// over as a data URL goes into Photos as its own bytes, a refusal says so, and Share shows the system sheet
/// with the file. The Client (NSH) workflow grants Photos access (`simctl privacy … grant photos-add` and
/// `photos`, to read the asset back) before the tests run. With MEWLA_TEST_OUT set the sheet is saved as
/// share-sheet-ios.png.
final class PictureSaveTests: XCTestCase {
  // A 1×1 GIF, the bytes a computer answers for a file called login.jpg.
  private let gif = Data(base64Encoded: "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")!

  /// A PNG of its own each run (Photos may set aside an exact copy of a picture it already has).
  private func png() -> Data {
    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    let hue = CGFloat.random(in: 0...1)
    return UIGraphicsImageRenderer(size: CGSize(width: 64, height: 48), format: format).pngData { context in
      UIColor(hue: hue, saturation: 0.8, brightness: 0.9, alpha: 1).setFill()
      context.fill(CGRect(x: 0, y: 0, width: 64, height: 48))
      UIColor.white.setFill()
      context.fill(CGRect(x: 8, y: 8, width: 20, height: 12))
    }
  }

  private func dataURL(_ type: String, _ bytes: Data) -> String { "data:\(type);base64,\(bytes.base64EncodedString())" }

  private func save(_ source: String, name: String, authorize: @escaping PictureSave.Authorize = PictureSave.photosAuthorize) -> Result<PictureSave.Saved, Error> {
    let done = expectation(description: "saved")
    var result: Result<PictureSave.Saved, Error> = .failure(PictureSave.Failure("no answer"))
    PictureSave.save(source: source, name: name, authorize: authorize) {
      result = $0
      done.fulfill()
    }
    // A Photos prompt nobody answers (access not granted to the test host) runs this out.
    wait(for: [done], timeout: 30)
    return result
  }

  /// The asset's original resource as Photos keeps it: its bytes and file name.
  private func original(_ assetId: String) throws -> (bytes: Data, name: String) {
    XCTAssertEqual(PHPhotoLibrary.authorizationStatus(for: .readWrite), .authorized, "reading the asset back needs `simctl privacy grant photos`")
    let asset = try XCTUnwrap(PHAsset.fetchAssets(withLocalIdentifiers: [assetId], options: nil).firstObject, "the picture is not in Photos")
    let resource = try XCTUnwrap(PHAssetResource.assetResources(for: asset).first { $0.type == .photo }, "the asset has no photo resource")
    var bytes = Data()
    var failure: Error?
    let done = expectation(description: "read")
    PHAssetResourceManager.default().requestData(for: resource, options: nil, dataReceivedHandler: { bytes.append($0) }) {
      failure = $0
      done.fulfill()
    }
    wait(for: [done], timeout: 30)
    if let failure { throw failure }
    return (bytes, resource.originalFilename)
  }

  func testAPngSavesToPhotosAsItsOwnBytes() throws {
    let bytes = png()
    let saved = try save(dataURL("image/png", bytes), name: "screen.png").get()
    XCTAssertEqual(saved.name, "screen.png")
    let kept = try original(try XCTUnwrap(saved.assetId))
    XCTAssertEqual(kept.bytes, bytes, "Photos holds other bytes than the original")
    XCTAssertEqual(kept.name, "screen.png")
  }

  func testAGifNamedJpgSavesAsAGifWithItsOwnBytes() throws {
    let saved = try save(dataURL("image/gif", gif), name: "login.jpg").get()
    XCTAssertEqual(saved.name, "login.gif")
    let kept = try original(try XCTUnwrap(saved.assetId))
    XCTAssertEqual(kept.bytes, gif, "Photos holds other bytes than the original")
    XCTAssertEqual(kept.name, "login.gif")
  }

  /// The person said no to Photos: the save fails with what the app shows ("Couldn't save the picture:
  /// Mewla isn't allowed to save pictures", core/files/pictureSave.ts) and adds nothing.
  func testRefusedAccessSaysSoAndAddsNothing() {
    var asked = 0
    let result = save(dataURL("image/png", png()), name: "screen.png", authorize: { done in
      asked += 1
      done(.denied)
    })
    XCTAssertEqual(asked, 1)
    guard case let .failure(error) = result else { return XCTFail("a refused save succeeded") }
    XCTAssertEqual(error.localizedDescription, "Photos permission denied.")
  }

  func testWhatIsNoPictureIsRefusedBeforePhotos() {
    var asked = false
    let result = save("file:///etc/hosts", name: "hosts", authorize: { done in
      asked = true
      done(.authorized)
    })
    XCTAssertFalse(asked)
    guard case let .failure(error) = result else { return XCTFail("a non-picture was saved") }
    XCTAssertEqual(error.localizedDescription, "No picture to save.")
  }

  /// Share stages the original under its name and shows the system sheet with that file.
  @MainActor
  func testShareShowsTheSystemSheetWithTheFile() throws {
    let bytes = png()
    let staged = expectation(description: "staged")
    var file: URL?
    PictureSave.stage(source: dataURL("image/png", bytes), name: "screen.png") {
      file = try? $0.get()
      staged.fulfill()
    }
    wait(for: [staged], timeout: 10)
    let url = try XCTUnwrap(file)
    XCTAssertEqual(url.lastPathComponent, "screen.png")
    XCTAssertEqual(try Data(contentsOf: url), bytes)

    // The app's own window, as FilesHost finds it (`app.window`): the one the scene delegate made.
    let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
    let made: UIWindow? = (scene.delegate as? SceneDelegate)?.window
    let window = try XCTUnwrap(made ?? scene.windows.first { $0.isKeyWindow } ?? scene.windows.first)
    var top = try XCTUnwrap(window.rootViewController)
    while let next = top.presentedViewController { top = next }
    let state = { "scene \(scene.activationState.rawValue), windows \(scene.windows.map { "\(type(of: $0)) key=\($0.isKeyWindow) hidden=\($0.isHidden) root=\(String(describing: $0.rootViewController.map { type(of: $0) }))" }), top \(type(of: top)) in window=\(top.view.window != nil), presented \(String(describing: top.presentedViewController))" }
    let shown = expectation(description: "shown")
    let sheet = PictureSave.present(file: url, over: top) { shown.fulfill() }
    // A cold Simulator starts the share service the first time: the presentation finishes when it is up.
    wait(for: [shown], timeout: 30)
    XCTAssertTrue(top.presentedViewController === sheet, "the share sheet is not on screen: \(state())")
    XCTAssertTrue(sheet.view.window != nil, "the share sheet has no window: \(state())")
    RunLoop.main.run(until: Date().addingTimeInterval(1))

    let format = UIGraphicsImageRendererFormat()
    format.scale = window.screen.scale
    let image = UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
      window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
      if let shown = sheet.view.window, shown !== window { shown.drawHierarchy(in: shown.bounds, afterScreenUpdates: true) }
    }
    if let dir = ProcessInfo.processInfo.environment["MEWLA_TEST_OUT"], let shot = image.pngData() {
      try shot.write(to: URL(fileURLWithPath: dir).appendingPathComponent("share-sheet-ios.png"))
    }
    let closed = expectation(description: "closed")
    sheet.dismiss(animated: false) { closed.fulfill() }
    wait(for: [closed], timeout: 10)
  }
}
