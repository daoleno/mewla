// Lists every display and its modes (HiDPI duplicates too): which sizes a runner can show (macos-fidelity.yml).
import CoreGraphics
var ids = [CGDirectDisplayID](repeating: 0, count: 8); var n: UInt32 = 0
CGGetOnlineDisplayList(8, &ids, &n)
for id in ids.prefix(Int(n)) {
  let cur = CGDisplayCopyDisplayMode(id)!
  print("display \(id) main=\(CGDisplayIsMain(id)) bounds=\(CGDisplayBounds(id)) now \(cur.width)x\(cur.height) pt \(cur.pixelWidth)x\(cur.pixelHeight) px")
  let opts = [kCGDisplayShowDuplicateLowResolutionModes: true] as CFDictionary
  for m in (CGDisplayCopyAllDisplayModes(id, opts) as! [CGDisplayMode]) {
    print("  mode \(m.width)x\(m.height) pt \(m.pixelWidth)x\(m.pixelHeight) px usable=\(m.isUsableForDesktopGUI())")
  }
}
