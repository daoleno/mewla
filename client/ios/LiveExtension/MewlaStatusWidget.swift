import SwiftUI
import UIKit
import WidgetKit

struct MewlaEntry: TimelineEntry {
  let date: Date
  let snapshot: MewlaWidgetSnapshot
}

struct MewlaProvider: TimelineProvider {
  func placeholder(in context: Context) -> MewlaEntry {
    MewlaEntry(date: Date(), snapshot: MewlaWidgetSnapshot(state: nil, needsApp: true))
  }

  func getSnapshot(in context: Context, completion: @escaping (MewlaEntry) -> Void) {
    Task {
      completion(MewlaEntry(date: Date(), snapshot: await MewlaWidgetData.load()))
    }
  }

  func getTimeline(in context: Context, completion: @escaping (Timeline<MewlaEntry>) -> Void) {
    Task {
      let entry = MewlaEntry(date: Date(), snapshot: await MewlaWidgetData.load())
      // The app and pushes reload the timeline when a state arrives; this is the periodic floor.
      completion(Timeline(entries: [entry], policy: .after(Date().addingTimeInterval(15 * 60))))
    }
  }
}

/// The home-screen and Lock Screen widgets (option A): the small one is the one thing that matters now,
/// the medium one the tray; each row opens its Session or Work. Quiet, the cat naps; a widget that knows
/// nothing (not set up, refused, or nothing heard for 12 hours) shows the cat greyed.
struct MewlaStatusWidget: Widget {
  var body: some WidgetConfiguration {
    StaticConfiguration(kind: "MewlaStatus", provider: MewlaProvider()) { entry in
      MewlaWidgetView(snapshot: entry.snapshot)
    }
    .configurationDisplayName("Mewla")
    .description("What needs you and what is running")
    .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular, .accessoryRectangular])
  }
}

private struct MewlaWidgetView: View {
  let snapshot: MewlaWidgetSnapshot
  @Environment(\.widgetFamily) private var family

  var body: some View {
    let state = snapshot.needsApp ? nil : snapshot.state
    let items = state.map(MewlaLiveView.items) ?? []
    Group {
      switch family {
      case .accessoryCircular: CircularView(focus: items.first)
      case .accessoryRectangular: RectangularView(state: state, focus: items.first)
      default:
        if let state, !items.isEmpty {
          if family == .systemMedium { TrayView(state: state, items: items) } else { FocusView(state: state, focus: items[0]) }
        } else {
          QuietView(state: state, wide: family == .systemMedium)
        }
      }
    }
    .widgetURL(items.first.map { MewlaLiveView.link($0) } ?? MewlaLiveView.link(nil))
    .containerBackground(for: .widget) {
      if family == .accessoryCircular || family == .accessoryRectangular { AccessoryWidgetBackground() } else { MewlaStyle.surface }
    }
  }
}

/// Nothing to show: the cat naps over the header's words; it is greyed with no words when the widget knows nothing.
private struct QuietView: View {
  let state: MewlaLiveState?
  let wide: Bool
  var body: some View {
    let image = Image(state == nil ? "pet-offline" : "pet-idle").resizable().scaledToFit()
    let words = state.map(MewlaLiveView.headerWords) ?? ""
    Group {
      if wide {
        HStack(spacing: 12) {
          image.frame(width: 96, height: 96)
          if !words.isEmpty { Text(words).font(.subheadline).foregroundStyle(.secondary) }
        }
      } else {
        VStack(spacing: 2) {
          image.frame(width: 88, height: 88)
          if !words.isEmpty { Text(words).font(.subheadline).foregroundStyle(.secondary) }
        }
      }
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
  }
}

/// The small widget: the one thing that matters now.
private struct FocusView: View {
  let state: MewlaLiveState
  let focus: MewlaLiveItem
  var body: some View {
    let line = focus.asks ? focus.line : MewlaLiveView.othersRunning(state, focus)
    VStack(alignment: .leading, spacing: 0) {
      HStack(alignment: .top) {
        RingLogo(item: focus, size: 48)
        Spacer(minLength: 4)
        RowEnd(item: focus)
      }
      Spacer(minLength: 8)
      Text(focus.title).font(.headline).lineLimit(2)
      if !line.isEmpty {
        Text(line).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
      }
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
  }
}

/// The medium widget: the tray. The header's words open it; each row opens its Session or Work.
private struct TrayView: View {
  let state: MewlaLiveState
  let items: [MewlaLiveItem]
  var body: some View {
    let needs = items.first?.asks ?? false
    VStack(alignment: .leading, spacing: 4) {
      Link(destination: MewlaLiveView.trayURL) {
        HStack(spacing: 6) {
          Image(needs ? "pet-attention" : "pet-working").resizable().scaledToFit().frame(width: 22, height: 22)
          if needs || MewlaLiveView.runningCount(state) > 0 {
            Circle().fill(needs ? AnyShapeStyle(MewlaStyle.seal) : AnyShapeStyle(HierarchicalShapeStyle.primary)).frame(width: 7, height: 7)
          }
          Text(MewlaLiveView.headerWords(state)).font(.footnote.weight(.semibold))
        }
      }
      ForEach(items.prefix(3), id: \.key) { item in
        Link(destination: MewlaLiveView.link(item)) {
          HStack(spacing: 10) {
            RingLogo(item: item, size: 28)
            VStack(alignment: .leading, spacing: 0) {
              Text(item.title).font(.subheadline.weight(.semibold)).lineLimit(1)
              if !item.line.isEmpty { Text(item.line).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
            }
            Spacer(minLength: 4)
            RowEnd(item: item)
          }
          .frame(height: 34)
        }
      }
      Spacer(minLength: 0)
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
  }
}

/// Lock Screen, round: the one item's logo in its ring (the system tints it).
private struct CircularView: View {
  let focus: MewlaLiveItem?
  var body: some View {
    if let focus {
      RingLogo(item: MewlaLiveItem(key: focus.key, open: focus.open, id: focus.id, title: focus.title, line: focus.line,
                                   agent: "brain", needs: focus.needs, ring: focus.ring, startedAt: focus.startedAt), size: 44, behind: .clear)
    } else {
      AgentMark(agent: "brain").frame(width: 26, height: 26).opacity(0.6)
    }
  }
}

/// Lock Screen, a line: the header's words and the one item.
private struct RectangularView: View {
  let state: MewlaLiveState?
  let focus: MewlaLiveItem?
  var body: some View {
    VStack(alignment: .leading, spacing: 1) {
      Text(state.map(MewlaLiveView.headerWords) ?? "Mewla").font(.headline).lineLimit(1)
      if let focus {
        Text(focus.title).font(.subheadline).lineLimit(1)
        if focus.asks {
          Text(focus.line).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        } else if let since = focus.startedDate {
          ElapsedView(since: since).font(.caption).foregroundStyle(.secondary).multilineTextAlignment(.leading)
        }
      }
    }
    .frame(maxWidth: .infinity, alignment: .leading)
  }
}
