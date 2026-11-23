// Puts what a person copies on the general pasteboard, as the app that copies it does (macos-image-input.yml):
//   swift pasteboard.swift file <path>             a file copied in Finder: its file URL and its name as text
//   swift pasteboard.swift image <png>             "Copy Image" in Preview: PNG and TIFF data
//   swift pasteboard.swift rich <text> <png>       a spreadsheet cell or a web page selection: text, HTML and a picture of it
//   swift pasteboard.swift types                   what the pasteboard holds now, one type a line
// A screenshot to the clipboard (⌃⇧⌘4) is `screencapture -c`, the system's own.
import AppKit

let args = CommandLine.arguments
let board = NSPasteboard.general
func data(_ path: String) -> Data { FileManager.default.contents(atPath: path) ?? Data() }

switch args.count > 1 ? args[1] : "" {
case "file":
  board.clearContents()
  let url = URL(fileURLWithPath: args[2])
  let item = NSPasteboardItem()
  item.setString(url.absoluteString, forType: .fileURL)
  item.setString(url.lastPathComponent, forType: .string)
  board.writeObjects([item])
case "image":
  board.clearContents()
  let png = data(args[2])
  let item = NSPasteboardItem()
  item.setData(png, forType: .png)
  if let tiff = NSImage(data: png)?.tiffRepresentation { item.setData(tiff, forType: .tiff) }
  board.writeObjects([item])
case "rich":
  board.clearContents()
  let item = NSPasteboardItem()
  item.setString(args[2], forType: .string)
  item.setString("<table><tr><td>\(args[2])</td></tr></table>", forType: .html)
  item.setData(data(args[3]), forType: .png)
  board.writeObjects([item])
case "types":
  break
default:
  FileHandle.standardError.write("usage: pasteboard.swift file|image|rich|types …\n".data(using: .utf8)!)
  exit(2)
}
for item in board.pasteboardItems ?? [] {
  print("pasteboard item:", item.types.map(\.rawValue).joined(separator: " "))
}
