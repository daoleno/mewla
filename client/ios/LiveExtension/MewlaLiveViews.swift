import SwiftUI
import UIKit
import WidgetKit

/// The Soft tokens (soft-final SPEC §10.1, §10.4; client/ui/theme.json). Red is only "your move".
enum MewlaStyle {
  private static func dynamic(_ light: UInt32, _ dark: UInt32) -> Color {
    Color(uiColor: UIColor { traits in
      let hex = traits.userInterfaceStyle == .dark ? dark : light
      return UIColor(red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    })
  }

  static let surface = dynamic(0xFFFFFF, 0x1C1D20)
  static let canvas = dynamic(0xF2F3F5, 0x060708)
  static let track = dynamic(0xE7E8E8, 0x3C3D3F)
  static let seal = dynamic(0xD63C2F, 0xD23E31)
  static let meter = dynamic(0x2A78D6, 0x3987E5)
  static let warn = dynamic(0xA65C00, 0xF0B03C)
  static let ok = dynamic(0x1B7F45, 0x5FCB88)
}

/// An agent's mark from the app's own logos (client/ui/icons/agents); one-colour marks follow the text colour.
struct AgentMark: View {
  let agent: String
  private static let kinds: Set<String> = ["claude", "codex", "cursor", "grok", "pi", "opencode", "dsh", "brain"]

  var body: some View {
    Image("agent-" + (Self.kinds.contains(agent) ? agent : "terminal"))
      .resizable()
      .scaledToFit()
      .foregroundStyle(.primary)
  }
}

/// An item's logo: inside the app's progress ring when it has one (client ui/kit/ProgressHalo.tsx: the track,
/// from twelve o'clock an arc in meter moving, warn sent back, ok closed but for a small gap in the last
/// stretch; never a number), or with the red dot when it waits for you.
struct RingLogo: View {
  let item: MewlaLiveItem
  var size: CGFloat
  /// What the red dot's cut-out shows: the surface under it.
  var behind: Color = MewlaStyle.surface

  var body: some View {
    let ring = item.asks ? nil : item.ring
    let stroke: CGFloat = size >= 44 ? 3.5 : 2.5
    ZStack {
      if let ring {
        Circle().inset(by: stroke / 2).stroke(MewlaStyle.track, lineWidth: stroke)
        Circle().inset(by: stroke / 2)
          .trim(from: 0, to: sweep(ring, stroke: stroke))
          .stroke(tone(ring), style: StrokeStyle(lineWidth: stroke, lineCap: .round))
          .rotationEffect(.degrees(-90))
      }
      AgentMark(agent: item.agent)
        .frame(width: size * (ring == nil ? 0.86 : 0.62), height: size * (ring == nil ? 0.86 : 0.62))
    }
    .frame(width: size, height: size)
    .overlay(alignment: .topTrailing) {
      if item.asks {
        let dot = min(10, size * 0.24)
        Circle().fill(MewlaStyle.seal).frame(width: dot, height: dot)
          .background(Circle().fill(behind).padding(-2))
      }
    }
    .accessibilityLabel(label(ring))
  }

  private func sweep(_ ring: MewlaLiveRing, stroke: CGFloat) -> CGFloat {
    let radius = (size - stroke) / 2
    return ring.isLast ? 1 - (stroke * 1.6 / radius) / (2 * .pi) : ring.fraction
  }

  private func tone(_ ring: MewlaLiveRing) -> Color {
    ring.isLast ? MewlaStyle.ok : ring.stage == "back" ? MewlaStyle.warn : MewlaStyle.meter
  }

  private func label(_ ring: MewlaLiveRing?) -> String {
    guard let ring else { return item.title }
    if ring.isLast { return "\(item.title), last stretch" }
    return "\(item.title), \(ring.percent) percent\(ring.stage == "back" ? ", sent back" : "")"
  }
}

/// A running row's clock: it ticks by itself, the daemon never pushes a state for it.
struct ElapsedView: View {
  let since: Date
  var body: some View {
    Text(timerInterval: since...since.addingTimeInterval(12 * 3600), countsDown: false)
      .monospacedDigit()
      .multilineTextAlignment(.trailing)
  }
}

/// The red tag a request carries, as the tray's: "Answer" or "Approve".
struct NeedsTag: View {
  let item: MewlaLiveItem
  var compact = false
  var body: some View {
    Text(MewlaLiveView.tag(item))
      .font(compact ? .caption2.weight(.semibold) : .footnote.weight(.semibold))
      .foregroundStyle(.white)
      .padding(.horizontal, compact ? 7 : 10)
      .padding(.vertical, compact ? 3 : 5)
      .background(Capsule().fill(MewlaStyle.seal))
  }
}

/// A row's end: the red tag when it asks, else a live dot and its clock.
struct RowEnd: View {
  let item: MewlaLiveItem
  var body: some View {
    if item.asks {
      NeedsTag(item: item, compact: true)
    } else if let since = item.startedDate {
      HStack(spacing: 4) {
        Circle().fill(.primary).frame(width: 6, height: 6)
        ElapsedView(since: since).font(.footnote).foregroundStyle(.secondary).frame(width: 44, alignment: .trailing)
      }
    }
  }
}

/// A Work question's choices: each opens the app on that answer, which sends it once. Brain does not rank them.
struct ChoiceButtons: View {
  let question: MewlaLiveQuestion
  let choices: [String]
  var fill: Color = Color.primary.opacity(0.12)
  var body: some View {
    HStack(spacing: 8) {
      ForEach(Array(choices.enumerated()), id: \.offset) { index, choice in
        Link(destination: MewlaLiveView.answerURL(question, choice: index)) {
          Text(choice)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .background(Capsule().fill(fill))
        }
        .buttonStyle(.plain)
      }
    }
  }
}

/// The others beside the one shown: their logos and titles, one line.
struct OthersLine: View {
  let items: [MewlaLiveItem]
  var behind: Color = MewlaStyle.surface
  var body: some View {
    HStack(spacing: 14) {
      ForEach(items, id: \.key) { item in
        HStack(spacing: 6) {
          RingLogo(item: item, size: 22, behind: behind)
          Text(item.title).font(.footnote.weight(.semibold)).lineLimit(1)
        }
      }
    }
  }
}

/// The one thing that matters now (option A): the Lock Screen card, and the Dynamic Island's expanded body.
struct FocusCard: View {
  let state: MewlaLiveState
  var behind: Color = MewlaStyle.surface

  var body: some View {
    let items = MewlaLiveView.items(state)
    if let focus = items.first {
      VStack(alignment: .leading, spacing: 12) {
        HStack(spacing: 12) {
          RingLogo(item: focus, size: 44, behind: behind)
          VStack(alignment: .leading, spacing: 1) {
            Text(focus.title).font(.headline).lineLimit(1)
            if !focus.line.isEmpty {
              Text(focus.line).font(.subheadline).foregroundStyle(focus.asks ? .primary : .secondary).lineLimit(focus.asks ? 2 : 1)
            }
          }
          Spacer(minLength: 4)
          if !focus.asks, let since = focus.startedDate {
            ElapsedView(since: since).font(.headline).frame(width: 64, alignment: .trailing)
          }
        }
        let choices = MewlaLiveView.choices(state, focus)
        if let question = state.question, !choices.isEmpty {
          ChoiceButtons(question: question, choices: choices)
        } else if items.count > 1 {
          OthersLine(items: Array(items.dropFirst().prefix(2)), behind: behind)
        }
      }
    }
  }
}
