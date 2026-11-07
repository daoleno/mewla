// A small AppKit app for the Mac probe of background pop-up and drag handling (see probe_popup_darwin_test.go).
// It has what two of the Cua Driver Bench tasks need and nothing else: a pop-up button, and a square that can be
// dragged. Every event it receives goes to a log file, one JSON object a line, so the probe knows what an app behind
// other windows really got.
//
//   axprobe LOGFILE
//
// It never activates itself: started from a program, it stays behind whatever is in front.
import AppKit

let logPath = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "/tmp/axprobe.log"

func log(_ fields: String) {
    let line = "{\"t\":\(Int(Date().timeIntervalSince1970 * 1000)),\(fields)}\n"
    guard let data = line.data(using: .utf8) else { return }
    if let handle = FileHandle(forWritingAtPath: logPath) {
        handle.seekToEndOfFile()
        handle.write(data)
        handle.closeFile()
    } else {
        FileManager.default.createFile(atPath: logPath, contents: data)
    }
}

final class DragView: NSView {
    var handle = NSRect(x: 20, y: 20, width: 40, height: 40)
    var dragging = false
    var onMoved: ((NSRect) -> Void)?
    override var isFlipped: Bool { true }
    override func draw(_ dirtyRect: NSRect) {
        NSColor.white.setFill()
        bounds.fill()
        NSColor.systemBlue.setFill()
        handle.fill()
    }
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }
    func point(_ event: NSEvent) -> NSPoint { convert(event.locationInWindow, from: nil) }
    override func mouseDown(with event: NSEvent) {
        let p = point(event)
        dragging = handle.contains(p)
        log("\"ev\":\"down\",\"x\":\(Int(p.x)),\"y\":\(Int(p.y)),\"hit\":\(dragging)")
    }
    override func mouseDragged(with event: NSEvent) {
        let p = point(event)
        if dragging {
            handle.origin = NSPoint(x: p.x - handle.width / 2, y: p.y - handle.height / 2)
            needsDisplay = true
            onMoved?(handle)
        }
        log("\"ev\":\"drag\",\"x\":\(Int(p.x)),\"y\":\(Int(p.y))")
    }
    override func mouseUp(with event: NSEvent) {
        let p = point(event)
        log("\"ev\":\"up\",\"x\":\(Int(p.x)),\"y\":\(Int(p.y)),\"handle\":\"\(Int(handle.midX)),\(Int(handle.midY))\"")
        dragging = false
    }
}

final class Delegate: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    let selected = NSTextField(labelWithString: "Selected: none")
    let moved = NSTextField(labelWithString: "Handle: 40,40")
    let popup = NSPopUpButton(frame: NSRect(x: 20, y: 300, width: 200, height: 26), pullsDown: false)
    let drag = DragView(frame: NSRect(x: 20, y: 20, width: 480, height: 200))

    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 240, y: 240, width: 520, height: 360),
                          styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = "Probe Lab"
        let content = NSView(frame: NSRect(x: 0, y: 0, width: 520, height: 360))
        window.contentView = content

        popup.addItems(withTitles: ["Choose", "Alpha", "Beta", "Gamma"])
        popup.target = self
        popup.action = #selector(picked(_:))
        selected.frame = NSRect(x: 240, y: 303, width: 250, height: 20)
        moved.frame = NSRect(x: 20, y: 262, width: 250, height: 20)
        content.addSubview(drag)
        content.addSubview(moved)
        content.addSubview(popup)
        content.addSubview(selected)
        drag.onMoved = { [weak self] rect in
            self?.moved.stringValue = "Handle: \(Int(rect.midX)),\(Int(rect.midY))"
        }
        window.makeKeyAndOrderFront(nil)
        // Like an app the person used a moment ago, then left for another: it has been active and key once, then the
        // probe puts the person's TextEdit in front of it. (An app that was never active refuses a pop-up press.)
        NSApp.activate(ignoringOtherApps: true)

        let center = NotificationCenter.default
        for name in [NSApplication.didBecomeActiveNotification, NSApplication.didResignActiveNotification] {
            center.addObserver(forName: name, object: nil, queue: nil) { note in
                log("\"ev\":\"app\",\"name\":\"\(note.name.rawValue)\"")
            }
        }
        for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification] {
            center.addObserver(forName: name, object: window, queue: nil) { note in
                log("\"ev\":\"window\",\"name\":\"\(note.name.rawValue)\"")
            }
        }
        // Where the drag handle is on the screen, top-left origin, for the probe to aim at.
        let inWindow = drag.convert(drag.handle, to: nil)
        let onScreen = window.convertToScreen(inWindow)
        let screenHeight = NSScreen.screens.first?.frame.height ?? 0
        log("\"ev\":\"layout\",\"handleX\":\(Int(onScreen.midX)),\"handleY\":\(Int(screenHeight - onScreen.midY)),\"pid\":\(ProcessInfo.processInfo.processIdentifier)")
    }

    @objc func picked(_ sender: NSPopUpButton) {
        let title = sender.titleOfSelectedItem ?? "?"
        selected.stringValue = "Selected: \(title)"
        log("\"ev\":\"picked\",\"title\":\"\(title)\"")
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = Delegate()
app.delegate = delegate
app.run()
