// A mouse click at a point on the main display, in points: swift click.swift <left|right> <x> <y> (macos-fidelity.yml
// opens the menu bar cat's menu with a right-click).
import CoreGraphics
import Foundation

let args = CommandLine.arguments
let right = args[1] == "right"
let at = CGPoint(x: Double(args[2])!, y: Double(args[3])!)
let (down, up, button): (CGEventType, CGEventType, CGMouseButton) = right ? (.rightMouseDown, .rightMouseUp, .right) : (.leftMouseDown, .leftMouseUp, .left)
CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: at, mouseButton: button)?.post(tap: .cghidEventTap)
usleep(100_000)
CGEvent(mouseEventSource: nil, mouseType: down, mouseCursorPosition: at, mouseButton: button)?.post(tap: .cghidEventTap)
usleep(60_000)
CGEvent(mouseEventSource: nil, mouseType: up, mouseCursorPosition: at, mouseButton: button)?.post(tap: .cghidEventTap)
