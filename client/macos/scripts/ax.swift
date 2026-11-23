// A running app's windows through Accessibility, as a person's eyes and hands find them (macos-image-input.yml):
//   swift ax.swift find <app> <text>     the middle ("x y", global points) of the first element whose title, value,
//                                         description or file name is <text> (a Finder item, a button)
//   swift ax.swift click <app> <x> <y>   a click there through the system's event stream
//   swift ax.swift texts <app>           every text, title and description in its windows, one per line (a dialog)
//   swift ax.swift press <app> <title>   presses its button titled <title> ("Allow", "Don't Allow")
// <app> is its name or bundle id. Exits 1 when it finds nothing.
import AppKit
import ApplicationServices

let a = CommandLine.arguments
guard a.count >= 3 else { exit(2) }
let mode = a[1]
func post(_ type: CGEventType, _ at: CGPoint) {
  CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: at, mouseButton: .left)?.post(tap: .cghidEventTap)
}
if mode == "click", a.count == 5, let x = Double(a[3]), let y = Double(a[4]) {
  let at = CGPoint(x: x, y: y)
  post(.mouseMoved, at); usleep(150_000); post(.leftMouseDown, at); usleep(80_000); post(.leftMouseUp, at)
  exit(0)
}
guard let app = NSWorkspace.shared.runningApplications.first(where: { $0.localizedName == a[2] || $0.bundleIdentifier == a[2] }) else {
  print("no app \(a[2])"); exit(1)
}
let root = AXUIElementCreateApplication(app.processIdentifier)
func attr(_ e: AXUIElement, _ name: String) -> AnyObject? {
  var value: AnyObject?
  return AXUIElementCopyAttributeValue(e, name as CFString, &value) == .success ? value : nil
}
func middle(_ e: AXUIElement) -> CGPoint? {
  guard let p = attr(e, kAXPositionAttribute), let s = attr(e, kAXSizeAttribute) else { return nil }
  var position = CGPoint.zero, size = CGSize.zero
  AXValueGetValue(p as! AXValue, .cgPoint, &position)
  AXValueGetValue(s as! AXValue, .cgSize, &size)
  return size.width > 0 && size.height > 0 ? CGPoint(x: position.x + size.width / 2, y: position.y + size.height / 2) : nil
}
var all: [AXUIElement] = []
func walk(_ e: AXUIElement, _ depth: Int) {
  guard depth < 40, all.count < 20000 else { return }
  all.append(e)
  for child in (attr(e, kAXChildrenAttribute) as? [AXUIElement]) ?? [] { walk(child, depth + 1) }
}
for window in (attr(root, kAXWindowsAttribute) as? [AXUIElement]) ?? [] { walk(window, 0) }
let keys = [kAXTitleAttribute, kAXValueAttribute, kAXDescriptionAttribute, "AXFilename"]
func texts(_ e: AXUIElement) -> [String] { keys.compactMap { attr(e, $0) as? String }.filter { !$0.isEmpty } }
let role = { (e: AXUIElement) in attr(e, kAXRoleAttribute) as? String ?? "" }
switch mode {
case "find":
  guard a.count == 4, let hit = all.first(where: { texts($0).contains(a[3]) && middle($0) != nil }), let at = middle(hit) else { exit(1) }
  print(Int(at.x), Int(at.y))
case "texts":
  for e in all { for text in texts(e) { print("\(role(e)): \(text)") } }
case "press":
  guard a.count == 4, let button = all.first(where: { role($0) == kAXButtonRole && texts($0).contains(a[3]) }) else { exit(1) }
  exit(AXUIElementPerformAction(button, kAXPressAction as CFString) == .success ? 0 : 1)
default:
  exit(2)
}
