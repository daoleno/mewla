import CoreText
import UIKit

/// The terminal's bundled faces (client/native/terminal/fonts, in the bundle's `fonts` folder):
/// Maple Mono NF Regular (with the Nerd Fonts icons) and SemiBold for bold, made straight from the
/// files. Anything they lack (CJK, emoji) falls back to the system's faces through CoreText.
enum TerminalFonts {
  private static let regularDescriptor = descriptor("MapleMono-NF-Regular")
  private static let boldDescriptor = descriptor("MapleMono-NF-SemiBold")

  private static func descriptor(_ name: String) -> CTFontDescriptor? {
    guard let url = Bundle(for: TerminalView.self).url(forResource: name, withExtension: "ttf", subdirectory: "fonts")
      ?? Bundle.main.url(forResource: name, withExtension: "ttf", subdirectory: "fonts")
      ?? Bundle.main.url(forResource: name, withExtension: "ttf"),
      let data = try? Data(contentsOf: url) else { return nil }
    return CTFontManagerCreateFontDescriptorFromData(data as CFData)
  }

  static func regular(_ size: CGFloat) -> CTFont {
    guard let regularDescriptor else { return UIFont.monospacedSystemFont(ofSize: size, weight: .regular) as CTFont }
    return CTFontCreateWithFontDescriptor(regularDescriptor, size, nil)
  }

  static func bold(_ size: CGFloat) -> CTFont {
    guard let boldDescriptor else { return UIFont.monospacedSystemFont(ofSize: size, weight: .semibold) as CTFont }
    return CTFontCreateWithFontDescriptor(boldDescriptor, size, nil)
  }
}
