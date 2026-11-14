import ActivityKit
import SwiftUI
import WidgetKit

/// The Live Activity (option A): the one thing that matters now (what needs you, else what started last)
/// with its agent's logo and progress ring, its clock or its tag, and one tap into it.
struct MewlaLiveActivity: Widget {
  var body: some WidgetConfiguration {
    ActivityConfiguration(for: MewlaLiveAttributes.self) { context in
      FocusCard(state: context.state)
        .padding(16)
        .activityBackgroundTint(MewlaStyle.surface)
        .widgetURL(MewlaLiveView.link(MewlaLiveView.focus(context.state)))
    } dynamicIsland: { context in
      let state = context.state
      let focus = MewlaLiveView.focus(state)
      return DynamicIsland {
        DynamicIslandExpandedRegion(.leading) {
          if let focus { RingLogo(item: focus, size: 44, behind: .black).padding(.leading, 4) }
        }
        DynamicIslandExpandedRegion(.center) {
          if let focus {
            VStack(alignment: .leading, spacing: 1) {
              Text(focus.title).font(.headline).lineLimit(1)
              if !focus.line.isEmpty {
                Text(focus.line).font(.subheadline).foregroundStyle(focus.asks ? .primary : .secondary).lineLimit(focus.asks ? 2 : 1)
              }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
          }
        }
        DynamicIslandExpandedRegion(.trailing) {
          if let focus, !focus.asks, let since = focus.startedDate {
            ElapsedView(since: since).font(.headline).frame(width: 64, alignment: .trailing)
          }
        }
        DynamicIslandExpandedRegion(.bottom) {
          if let focus {
            let choices = MewlaLiveView.choices(state, focus)
            if let question = state.question, !choices.isEmpty {
              ChoiceButtons(question: question, choices: choices, fill: Color.white.opacity(0.16))
            } else {
              let others = Array(MewlaLiveView.items(state).dropFirst().prefix(2))
              if !others.isEmpty { OthersLine(items: others, behind: .black).frame(maxWidth: .infinity, alignment: .leading) }
            }
          }
        }
      } compactLeading: {
        if let focus { RingLogo(item: focus, size: 24, behind: .black) }
      } compactTrailing: {
        if let focus, focus.asks {
          NeedsTag(item: focus, compact: true)
        } else if let since = focus?.startedDate {
          ElapsedView(since: since).font(.caption.weight(.semibold)).frame(width: 44)
        }
      } minimal: {
        if let focus { RingLogo(item: focus, size: 24, behind: .black) }
      }
      .widgetURL(MewlaLiveView.link(focus))
    }
  }
}
