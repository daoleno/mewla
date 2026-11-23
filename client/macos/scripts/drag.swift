// A real drag of files into another app's window, as Finder starts one (macos-image-input.yml):
//   swiftc -o drag drag.swift && ./drag <to x> <to y> <hold seconds> <marker file> <file> [<file> ...]
// A small window of its own (bottom left of the main screen) shows the files; the mouse is pressed on it through the
// system's event stream (CGEvent, as a person's mouse arrives), the press starts an AppKit dragging session carrying the
// files' URLs, the mouse moves to <to x>, <to y> (global points, top left origin, as CGWindowList gives windows), stays
// there <hold seconds> (the marker file is written once it is there, for a capture of the drop target), and is let go.
// It prints how the drag ended: "drag ended: copy" when the window under the mouse took the file.
import AppKit

let args = CommandLine.arguments
guard args.count >= 6, let toX = Double(args[1]), let toY = Double(args[2]), let hold = Double(args[3]) else {
  FileHandle.standardError.write("usage: drag <to x> <to y> <hold seconds> <marker file> <file> [<file> ...]\n".data(using: .utf8)!)
  exit(2)
}
let marker = args[4]
let files = args[5...].map { URL(fileURLWithPath: $0) }

final class Source: NSView, NSDraggingSource {
  override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
  override func draw(_ dirtyRect: NSRect) {
    NSColor.systemBlue.setFill()
    bounds.fill()
  }
  override func mouseDown(with event: NSEvent) {
    print("drag: pressed on \(files.count) file(s)")
    let items = files.enumerated().map { index, file in
      let item = NSDraggingItem(pasteboardWriter: file as NSURL)
      item.setDraggingFrame(NSRect(x: index * 4, y: index * 4, width: 64, height: 64), contents: NSWorkspace.shared.icon(forFile: file.path))
      return item
    }
    beginDraggingSession(with: items, event: event, source: self)
  }
  func draggingSession(_ session: NSDraggingSession, sourceOperationMaskFor context: NSDraggingContext) -> NSDragOperation {
    context == .outsideApplication ? .copy : []
  }
  func draggingSession(_ session: NSDraggingSession, endedAt point: NSPoint, operation: NSDragOperation) {
    print("drag ended: \(operation.contains(.copy) ? "copy" : "none")")
    exit(operation.contains(.copy) ? 0 : 1)
  }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let screen = NSScreen.screens[0].frame
let window = NSWindow(contentRect: NSRect(x: 20, y: 20, width: 80, height: 80), styleMask: [.borderless], backing: .buffered, defer: false)
window.level = .floating
window.contentView = Source(frame: NSRect(x: 0, y: 0, width: 80, height: 80))
window.makeKeyAndOrderFront(nil)
app.activate(ignoringOtherApps: true)

// The window's middle in global points (top left origin).
let from = CGPoint(x: 60, y: screen.height - 60)
let to = CGPoint(x: toX, y: toY)
func post(_ type: CGEventType, _ at: CGPoint) {
  CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: at, mouseButton: .left)?.post(tap: .cghidEventTap)
}
Thread.detachNewThread {
  Thread.sleep(forTimeInterval: 1.5)
  post(.mouseMoved, from)
  Thread.sleep(forTimeInterval: 0.3)
  post(.leftMouseDown, from)
  Thread.sleep(forTimeInterval: 0.3)
  let steps = 40
  for step in 1...steps {
    let t = Double(step) / Double(steps)
    post(.leftMouseDragged, CGPoint(x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t))
    Thread.sleep(forTimeInterval: 0.03)
  }
  print("drag: over \(Int(to.x)),\(Int(to.y)), holding \(hold) s")
  FileManager.default.createFile(atPath: marker, contents: Data())
  // Small moves while it holds: a drag that stops still gets updates, as a hand's does.
  let until = Date().addingTimeInterval(hold)
  var wiggle = 1.0
  while Date() < until {
    wiggle = -wiggle
    post(.leftMouseDragged, CGPoint(x: to.x + wiggle, y: to.y))
    Thread.sleep(forTimeInterval: 0.1)
  }
  post(.leftMouseUp, to)
  Thread.sleep(forTimeInterval: 5)
  print("drag ended: no answer from the drag")
  exit(3)
}
app.run()
