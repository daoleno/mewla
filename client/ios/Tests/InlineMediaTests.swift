import WebKit
import XCTest

/// Screen's video plays in the page, not in the system's fullscreen player (Sources/InlineMedia.m):
/// every web view the app makes, as NSH makes them, allows inline media without a tap.
final class InlineMediaTests: XCTestCase {
  func testWebViewsPlayMediaInline() {
    let configuration = WKWebViewConfiguration()
    XCTAssertFalse(configuration.allowsInlineMediaPlayback, "an iPhone's default changed: the workaround may be unneeded")
    let view = WKWebView(frame: .zero, configuration: configuration)
    XCTAssertTrue(view.configuration.allowsInlineMediaPlayback)
    XCTAssertEqual(view.configuration.mediaTypesRequiringUserActionForPlayback, [])
  }
}
