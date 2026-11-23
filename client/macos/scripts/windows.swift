// The on-screen windows, one per line: "<owner>|<layer>|<x> <y> <width> <height>" (global points) (macos-image-input.yml).
import CoreGraphics

for w in CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] ?? [] {
  let b = w[kCGWindowBounds as String] as? [String: Any] ?? [:]
  print("\(w[kCGWindowOwnerName as String] ?? "")|\(w[kCGWindowLayer as String] ?? "")|\(b["X"] ?? 0) \(b["Y"] ?? 0) \(b["Width"] ?? 0) \(b["Height"] ?? 0)")
}
