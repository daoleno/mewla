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
# An Accessibility client before the menu opens: AppKit gives a menu its element only if one was listening when it
# opened (in a run's first pass the menu showed on screen and the app's tree had none: 15 elements, no menu).
osascript -e "tell application \"System Events\" to get name of every window of process \"$owner\"" >/dev/null 2>&1 || true
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

# A right click and the read of the menu it opened. Twice at most: the run that saw the menu on screen and an app tree
# of 15 elements with no menu was each job's first right click (11-24, two gate runs, both in light), so the second
# opens the menu again with an Accessibility client already there.
right_click_and_read() {
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
func attr(_ e: AXUIElement, _ name: String) -> AnyObject? {
  var v: AnyObject?
  return AXUIElementCopyAttributeValue(e, name as CFString, &v) == .success ? v : nil
}
let root = AXUIElementCreateApplication(app.processIdentifier)
// What a failed read says: trust, and the app top-level elements (did the click open anything?).
func explain(_ why: String) -> Never {
  let kids = (attr(root, kAXChildrenAttribute) as? [AXUIElement] ?? []).map { k -> String in
    "\((attr(k, kAXRoleAttribute) as? String) ?? "?"):\((attr(k, kAXTitleAttribute) as? String) ?? "")"
  }
  print("\(why) (trusted \(AXIsProcessTrusted()); top level: \(kids.joined(separator: ", ")))")
  exit(0)
}
if !AXIsProcessTrusted() { explain("not trusted for Accessibility") }
// The menu a click opened, wherever it hangs in the tree (off the application, or under the window and the view
// that showed it: in a 720 x 560 window it was deeper than four levels); the menu bar and its menus are not it.
// Level by level, so a menu off the application is found before the many elements of the window are read; three
// looks, 0.7 s apart, as the menu element may come a moment after the menu.
var seen = 0
var found: AXUIElement? = nil
for look in 0..<3 where found == nil {
  if look > 0 { usleep(700_000) }
  var level: [AXUIElement] = [root]
  seen = 0
  while found == nil && !level.isEmpty && seen < 20000 {
    var next: [AXUIElement] = []
    for e in level {
      seen += 1
      let role = attr(e, kAXRoleAttribute) as? String
      if role == kAXMenuRole { found = e; break }
      if role == kAXMenuBarRole { continue }
      next += attr(e, kAXChildrenAttribute) as? [AXUIElement] ?? []
    }
    level = next
  }
}
guard let menu = found else { explain("no open menu (\(seen) elements searched)") }
let titles = (attr(menu, kAXChildrenAttribute) as? [AXUIElement] ?? []).map { (attr($0, kAXTitleAttribute) as? String) ?? "" }
print(titles.map { $0.isEmpty ? "—" : $0 }.joined(separator: " | "))' "$owner" 2>&1 || true)

if ! grep -q "Copy" <<<"$items"; then
  # The same through System Events (what the workflow already reads the Dock with), should this process not see it.
  items="$items; System Events: $(osascript -e "tell application \"System Events\" to tell process \"$owner\" to get name of every menu item of menu 1" 2>&1 || true)"
fi
}
right_click_and_read
if ! grep -q "Copy" <<<"$items"; then
  echo "terminal menu, first right click: $items"
  osascript -e 'tell application "System Events" to key code 53' >/dev/null 2>&1 || true
  sleep 1
  right_click_and_read
fi
osascript -e 'tell application "System Events" to key code 53' >/dev/null 2>&1 || true
echo "terminal menu (right click at $cx,$cy in $w x $h at $x,$y): $items"
if grep -qi "autofill" <<<"$items"; then echo "terminal menu: macOS's AutoFill item is in it"; exit 1; fi
# Exactly the terminal's menu (the Edit menu has Copy, Paste and Select All too, with Cut, Undo and Redo).
if grep -qE "(^|; System Events: )Copy \| Paste \| — \| Select All$" <<<"$items" || grep -qE "System Events: Copy, Paste, missing value, Select All$" <<<"$items"; then
  echo "terminal menu: Copy, Paste, Select All; no AutoFill"
else
  echo "terminal menu: not exactly Copy, Paste, Select All"
  exit 1
fi
