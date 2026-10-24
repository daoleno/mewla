import SwiftUI
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

struct MewlaStatusWidget: Widget {
  var body: some WidgetConfiguration {
    StaticConfiguration(kind: "MewlaStatus", provider: MewlaProvider()) { entry in
      MewlaWidgetView(snapshot: entry.snapshot)
    }
    .configurationDisplayName("Mewla")
    .description("What is running, and when Brain needs you")
    .supportedFamilies([.systemSmall, .systemMedium])
  }
}

private struct MewlaWidgetView: View {
  let snapshot: MewlaWidgetSnapshot
  @Environment(\.widgetFamily) private var family
  @Environment(\.colorScheme) private var scheme

  private var needs: Bool { snapshot.state?.needsYou ?? false }

  private var background: Color {
    if needs {
      return scheme == .dark
        ? Color(red: 0.22, green: 0.10, blue: 0.09) : Color(red: 0.99, green: 0.91, blue: 0.90)
    }
    return scheme == .dark
      ? Color(red: 0.12, green: 0.11, blue: 0.10) : Color(red: 0.99, green: 0.98, blue: 0.97)
  }

  var body: some View {
    Group {
      if snapshot.needsApp || snapshot.state == nil {
        OpenMewlaView()
      } else if let state = snapshot.state {
        switch family {
        case .systemMedium: MediumView(state: state)
        default: SmallView(state: state)
        }
      }
    }
    .widgetURL(MewlaStyle.openURL)
    .containerBackground(for: .widget) { background }
  }
}

private struct OpenMewlaView: View {
  var body: some View {
    VStack(spacing: 8) {
      SealView(size: 30)
      Text("Open Mewla").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
  }
}

/// One glance: how many things are running; red only when you are needed.
private struct SmallView: View {
  let state: MewlaLiveState

  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      SealView(size: 22)
      Spacer(minLength: 0)
      Text("\(state.needsYou ? max(state.needsCount, 1) : state.workerCount)")
        .font(.system(size: 44, weight: .bold, design: .rounded))
        .foregroundStyle(state.needsYou ? MewlaStyle.seal : Color.primary)
      Text(state.needsYou ? "Brain needs you" : "running")
        .font(.footnote)
        .foregroundStyle(state.needsYou ? MewlaStyle.seal : Color.secondary)
        .lineLimit(1)
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
  }
}

/// The needs-you strip on top, then the running Workers.
private struct MediumView: View {
  let state: MewlaLiveState

  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      if state.needsYou, let question = state.question {
        VStack(alignment: .leading, spacing: 6) {
          Text(question.text.isEmpty ? "Brain needs you" : question.text)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
          ChoiceChips(question: question, compact: true)
        }
      }
      if state.workers.isEmpty && !state.needsYou {
        HStack(spacing: 8) {
          SealView(size: 22)
          Text("Nothing is running").font(.subheadline).foregroundStyle(.secondary)
        }
      }
      ForEach(Array(state.workers.prefix(state.needsYou ? 2 : 3).enumerated()), id: \.offset) { index, worker in
        HStack(spacing: 6) {
          Circle().fill(index == 0 ? Color.primary : Color.secondary).frame(width: 6, height: 6)
          Text(worker.name).font(.subheadline.weight(.medium)).lineLimit(1)
          Text(worker.phase).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
          Spacer(minLength: 4)
          if index == 0, let since = state.startedDate {
            Text(timerInterval: since...since.addingTimeInterval(12 * 3600), countsDown: false)
              .monospacedDigit()
              .font(.subheadline)
              .foregroundStyle(.secondary)
              .frame(width: 56, alignment: .trailing)
          }
        }
      }
      Spacer(minLength: 0)
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
  }
}
