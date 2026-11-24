#!/usr/bin/env bash
# The terminal's right-click menu, as a person gets it (macos-app.yml, the terminal capture):
#   client/macos/scripts/terminal-menu-check.sh "Mewla Dev" <out prefix>
# A real right click (a posted mouse event) on the terminal view of the app's largest window, a screenshot of the
# screen with the menu open (<out prefix>menu.png), and the open menu's items read through Accessibility. Passes with
# Copy, Paste and Select All and no AutoFill item (the view's menu takes no plug-ins: client/macos/src/terminal/view.rs
# `context_menu`). Needs Screen Recording and Accessibility for the shell that runs it (a hosted runner has both).
set -euo pipefail

owner="$1"
out="$2"

osascript -e "tell application \"System Events\" to set frontmost of process \"$owner\" to true" >/dev/null 2>&1 || true
sleep 1

# The app's largest normal window; the terminal fills its right pane under the header.
read -r x y w h < <(swift -e '
import CoreGraphics
import Foundation
let owner = CommandLine.arguments[1]
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
var best = (0, 0, 0, 0)
for win in list where (win[kCGWindowOwnerName as String] as? String) == owner && (win[kCGWindowLayer as String] as? Int) == 0 {
  let b = win[kCGWindowBounds as String] as! [String: Any]
  let r = [b["X"], b["Y"], b["Width"], b["Height"]].map { ($0 as! NSNumber).intValue }
  if r[2] * r[3] > best.2 * best.3 { best = (r[0], r[1], r[2], r[3]) }
}
print(best.0, best.1, best.2, best.3)' "$owner")
[[ "$w" -gt 0 ]] || { echo "terminal menu: no window of $owner"; exit 1; }
cx=$((x + w * 65 / 100))
cy=$((y + h * 60 / 100))

swift -e '
import CoreGraphics
import Foundation
let p = CGPoint(x: Double(CommandLine.arguments[1])!, y: Double(CommandLine.arguments[2])!)
CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .left)!.post(tap: .cghidEventTap)
usleep(200_000)
for t in [CGEventType.rightMouseDown, .rightMouseUp] {
  CGEvent(mouseEventSource: nil, mouseType: t, mouseCursorPosition: p, mouseButton: .right)!.post(tap: .cghidEventTap)
  usleep(80_000)
}' "$cx" "$cy"
sleep 1.5
screencapture -x "${out}menu.png"

# The open menu's items: the app's Accessibility tree down to its AXMenu (a context menu hangs off the application).
items=$(swift -e '
import ApplicationServices
import AppKit
let owner = CommandLine.arguments[1]
guard let app = NSWorkspace.shared.runningApplications.first(where: { $0.localizedName == owner }) else { print("no app"); exit(0) }
if !AXIsProcessTrusted() { print("not trusted for Accessibility"); exit(0) }
func attr(_ e: AXUIElement, _ name: String) -> AnyObject? {
  var v: AnyObject?
  return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}
func menus(_ e: AXUIElement, _ depth: Int) -> [AXUIElement] {
  if (attr(e, kAXRoleAttribute) as? String) == kAXMenuRole { return [e] }
  guard depth > 0, let kids = attr(e, kAXChildrenAttribute) as? [AXUIElement] else { return [] }
  return kids.flatMap { menus($0, depth - 1) }
}
// Not the menu bar menus: the one a click opened.
var open: AXUIElement? = nil
for m in menus(AXUIElementCreateApplication(app.processIdentifier), 4) {
  var parentRole: String? = nil
  if let p = attr(m, kAXParentAttribute) { parentRole = attr(p as! AXUIElement, kAXRoleAttribute) as? String }
  if parentRole != kAXMenuBarItemRole { open = m; break }
}
guard let menu = open else { print("no open menu"); exit(0) }
let titles = (attr(menu, kAXChildrenAttribute) as? [AXUIElement] ?? []).map { (attr($0, kAXTitleAttribute) as? String) ?? "" }
print(titles.map { $0.isEmpty ? "—" : $0 }.joined(separator: " | "))' "$owner" 2>&1 || true)

if ! grep -q "Copy" <<<"$items"; then
  # The same through System Events (what the workflow already reads the Dock with), should this process not see it.
  items="$items; System Events: $(osascript -e "tell application \"System Events\" to tell process \"$owner\" to get name of every menu item of menu 1" 2>&1 || true)"
fi
osascript -e 'tell application "System Events" to key code 53' >/dev/null 2>&1 || true
echo "terminal menu (right click at $cx,$cy in $w x $h at $x,$y): $items"
grep -q "Copy" <<<"$items" && grep -q "Paste" <<<"$items" && grep -q "Select All" <<<"$items" || { echo "terminal menu: Copy, Paste and Select All not all there"; exit 1; }
if grep -qi "autofill" <<<"$items"; then echo "terminal menu: macOS's AutoFill item is in it"; exit 1; fi
echo "terminal menu: Copy, Paste, Select All; no AutoFill"
