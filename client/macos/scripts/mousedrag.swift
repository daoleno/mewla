// A person's mouse drag through the system's event stream, from whatever is under the mouse (macos-image-input.yml):
//   swift mousedrag.swift <from x> <from y> <to x> <to y> <hold seconds> [press ms]
// Global points, top left origin. The button goes down at <from>, stays still <press ms> (600; a browser starts an
// image's drag after a still press, the screenshot thumbnail opens its menu on one: 40 there), moves to <to> and holds
// there <hold seconds> with small moves, as a hand's drag gets updates, then is let go.
import CoreGraphics
import Foundation

let a = CommandLine.arguments.dropFirst().compactMap { Double($0) }
guard a.count >= 5 else {
  FileHandle.standardError.write("usage: mousedrag <from x> <from y> <to x> <to y> <hold seconds> [press ms]\n".data(using: .utf8)!)
  exit(2)
}
let from = CGPoint(x: a[0], y: a[1]), to = CGPoint(x: a[2], y: a[3]), hold = a[4]
let press = a.count > 5 ? a[5] : 600
func post(_ type: CGEventType, _ at: CGPoint) {
  CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: at, mouseButton: .left)?.post(tap: .cghidEventTap)
}
post(.mouseMoved, from); usleep(400_000)
post(.leftMouseDown, from); usleep(useconds_t(press * 1000))
for i in 1...6 { post(.leftMouseDragged, CGPoint(x: from.x + Double(i) * 2, y: from.y + Double(i) * 2)); usleep(50_000) }
let steps = 50
for s in 1...steps {
  let t = Double(s) / Double(steps)
  post(.leftMouseDragged, CGPoint(x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t)); usleep(30_000)
}
let until = Date().addingTimeInterval(hold)
var wiggle = 1.0
while Date() < until { wiggle = -wiggle; post(.leftMouseDragged, CGPoint(x: to.x + wiggle, y: to.y)); usleep(100_000) }
post(.leftMouseUp, to)
print("mousedrag: let go at \(Int(to.x)),\(Int(to.y))")
