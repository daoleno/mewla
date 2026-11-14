import CoreText
import UIKit

/// The terminal's bundled faces (client/native/terminal/fonts, in the bundle's `fonts` folder):
/// Maple Mono NF Regular (with the Nerd Fonts icons), SemiBold for bold, and the real Italic and
/// SemiBold Italic (Latin, Greek, Cyrillic, punctuation only), made straight from the files. Anything
/// they lack (CJK, emoji) falls back to the system's faces through CoreText.
enum TerminalFonts {
  private static let regularDescriptor = descriptor("MapleMono-NF-Regular")
  private static let boldDescriptor = descriptor("MapleMono-NF-SemiBold")
  private static let italicDescriptor = descriptor("MapleMono-NF-Italic")
  private static let boldItalicDescriptor = descriptor("MapleMono-NF-SemiBoldItalic")

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

  /// The real italic, nil when the file is missing (then italic is the upright face slanted).
  static func italic(_ size: CGFloat, bold: Bool) -> CTFont? {
    guard let descriptor = bold ? boldItalicDescriptor : italicDescriptor else { return nil }
    return CTFontCreateWithFontDescriptor(descriptor, size, nil)
  }
}
