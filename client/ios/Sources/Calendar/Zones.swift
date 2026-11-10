import Foundation

/// One stretch of a zone's offset, as core/calendar/zones.ts's `ZoneSpan`.
struct ZoneSpan: Equatable {
  let atMs: Int64
  let offsetMinutes: Int
  /// The short English name the browser's Intl (en-US) gives: "EDT", "GMT+8".
  let name: String

  var json: [String: Any] { ["atMs": atMs, "offsetMinutes": offsetMinutes, "name": name] }
}

/// `calendar.zones`: QuickJS knows no zone but UTC, so the host gives the table the Calendar shows
/// times with: per zone, its span at `fromMs` and one per offset change until `toMs`, the same
/// table `intlZoneTable` builds from Intl on the web.
enum Zones {
  static let maxSpans = 64

  /// nil for an id that is no zone.
  static func spans(_ id: String, fromMs: Int64, toMs: Int64) -> [ZoneSpan]? {
    guard let zone = TimeZone(identifier: id) else { return nil }
    let names = DateFormatter()
    names.locale = Locale(identifier: "en_US")
    names.timeZone = zone
    names.dateFormat = "zzz"
    func span(_ date: Date, _ ms: Int64) -> ZoneSpan {
      ZoneSpan(atMs: ms, offsetMinutes: zone.secondsFromGMT(for: date) / 60, name: names.string(from: date))
    }
    var at = date(fromMs)
    var spans = [span(at, fromMs)]
    while spans.count < maxSpans, let next = zone.nextDaylightSavingTimeTransition(after: at) {
      let ms = Int64((next.timeIntervalSince1970 * 1000).rounded())
      if ms > toMs { break }
      at = next
      let change = span(next, ms)
      // Intl sees offsets only: a change of name alone is no new span.
      if change.offsetMinutes != spans[spans.count - 1].offsetMinutes { spans.append(change) }
    }
    return spans
  }

  /// The whole answer, `ZoneTable` in core/calendar/zones.ts.
  static func table(_ ids: [String], fromMs: Int64, toMs: Int64) -> [String: Any] {
    var zones: [String: Any] = [:]
    for id in ids {
      if let found = spans(id, fromMs: fromMs, toMs: toMs) {
        zones[id] = found.map(\.json)
      } else {
        zones[id] = NSNull()
      }
    }
    return ["device": TimeZone.current.identifier, "hour12": hour12, "zones": zones]
  }

  /// Whether the phone shows 12-hour times (its region and the 24-Hour Time setting).
  static var hour12: Bool {
    DateFormatter.dateFormat(fromTemplate: "j", options: 0, locale: .current)?.contains("a") ?? false
  }

  static func date(_ ms: Int64) -> Date { Date(timeIntervalSince1970: Double(ms) / 1000) }
}
