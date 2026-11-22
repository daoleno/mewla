import Foundation
import Vision
import CoreGraphics
import ImageIO

// The scan command writes only inside the simulated phone's private directory.
let request = VNRecognizeTextRequest()
let barcode = VNDetectBarcodesRequest()
barcode.symbologies = [.qr]
let image = URL(fileURLWithPath: CommandLine.arguments[1])
try VNImageRequestHandler(url: image).perform([request, barcode])
let lines = (request.results ?? []).compactMap { $0.topCandidates(1).first?.string.lowercased() }
let title = lines.contains("pair a phone")
let qr = (barcode.results ?? []).first {
    guard let value = $0.payloadStringValue, let url = URLComponents(string: value) else { return false }
    return url.scheme == "mewla" && url.host == "settings" &&
        url.queryItems?.contains(where: { $0.name == "p" && !($0.value ?? "").isEmpty }) == true
}
let link = qr != nil
switch CommandLine.arguments[2] {
case "title": exit(title ? 0 : 1)
case "closed": exit(title || link ? 1 : 0)
case "qr": exit(title && link ? 0 : 1)
case "connections": exit(lines.contains("phones reach this mac") ? 0 : 1)
case "no-code": exit((barcode.results ?? []).isEmpty ? 0 : 1)
case "consent": exit(title && !link && lines.contains("allow trusted lan") ? 0 : 1)
case "scan":
    guard title, let qr, let value = qr.payloadStringValue,
          lines.contains(where: { $0.range(of: #"(1[0-5]|[0-9]):[0-5][0-9] left"#, options: .regularExpression) != nil }) else { exit(1) }
    try value.write(toFile: CommandLine.arguments[3], atomically: true, encoding: .utf8)
case "click":
    let target = CommandLine.arguments[3].lowercased()
    guard let found = request.results?.first(where: {
        guard let text = $0.topCandidates(1).first?.string.lowercased() else { return false }
        return text == target || text.hasSuffix(" " + target)
    }) else { exit(1) }
    let screen = CGDisplayBounds(CGMainDisplayID())
    let box = found.boundingBox
    let point = CGPoint(x: screen.minX + screen.width * box.midX, y: screen.minY + screen.height * (1 - box.midY))
    if target == "turn off lan" {
        print("off click: original OCR point=\(point), exact match=\(found.topCandidates(1).first?.string.lowercased() == target)")
    }
    for type: CGEventType in [.leftMouseDown, .leftMouseUp] {
        CGEvent(mouseEventSource: nil, mouseType: type, mouseCursorPosition: point, mouseButton: .left)?.post(tap: .cghidEventTap)
    }
case "settings-state":
    guard (barcode.results ?? []).isEmpty, !title, lines.contains("phones reach this mac") else { exit(1) }
    let states = ["turn off lan", "turning off", "trusted lan access", "saved the network setting",
                  "change trusted lan access", "not on wi", "applying its network setting"]
    print(states.filter { state in lines.contains { $0.contains(state) } }.joined(separator: ","))
case "redact":
    guard title, let qr, let source = CGImageSourceCreateWithURL(image as CFURL, nil),
          let pixels = CGImageSourceCreateImageAtIndex(source, 0, nil),
          let ctx = CGContext(data: nil, width: pixels.width, height: pixels.height, bitsPerComponent: 8, bytesPerRow: 0,
                              space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue) else { exit(1) }
    let width = CGFloat(pixels.width), height = CGFloat(pixels.height)
    ctx.draw(pixels, in: CGRect(x: 0, y: 0, width: width, height: height))
    let box = qr.boundingBox
    ctx.setFillColor(CGColor(gray: 0.5, alpha: 1))
    ctx.fill(CGRect(x: box.minX * width, y: box.minY * height, width: box.width * width, height: box.height * height).insetBy(dx: -8, dy: -8))
    guard let result = ctx.makeImage(),
          let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: CommandLine.arguments[3]) as CFURL, "public.png" as CFString, 1, nil) else { exit(1) }
    CGImageDestinationAddImage(dest, result, nil)
    exit(CGImageDestinationFinalize(dest) ? 0 : 1)
default: exit(2)
}
