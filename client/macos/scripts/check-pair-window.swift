import Foundation
import Vision

// Inspect the pixels a person saw. Never print the one-time link.
let request = VNRecognizeTextRequest()
let barcode = VNDetectBarcodesRequest()
barcode.symbologies = [.qr]
let image = URL(fileURLWithPath: CommandLine.arguments[1])
try VNImageRequestHandler(url: image).perform([request, barcode])
let lines = (request.results ?? []).compactMap { $0.topCandidates(1).first?.string.lowercased() }
let title = lines.contains("pair a phone")
let link = (barcode.results ?? []).contains {
    guard let value = $0.payloadStringValue, let url = URLComponents(string: value) else { return false }
    return url.scheme == "mewla" && url.host == "settings" &&
        url.queryItems?.contains(where: { $0.name == "p" && !($0.value ?? "").isEmpty }) == true
}
print("Pairing dialog visible: \(title). Pairing QR decoded: \(link).")
switch CommandLine.arguments[2] {
case "title": exit(title ? 0 : 1)
case "closed": exit(title || link ? 1 : 0)
case "qr": exit(title && link ? 0 : 1)
default: exit(2)
}
