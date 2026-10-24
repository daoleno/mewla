import ActivityKit
import SwiftUI
import WidgetKit

struct MewlaLiveActivity: Widget {
  var body: some WidgetConfiguration {
    ActivityConfiguration(for: MewlaLiveAttributes.self) { context in
      RunningCard(state: context.state)
        .widgetURL(MewlaStyle.openURL)
    } dynamicIsland: { context in
      let state = context.state
      return DynamicIsland {
        DynamicIslandExpandedRegion(.leading) {
          SealView(size: 32)
        }
        DynamicIslandExpandedRegion(.center) {
          VStack(spacing: 1) {
            Text(MewlaStyle.headline(state)).font(.subheadline.weight(.semibold)).lineLimit(1)
            if !state.needsYou, !state.phase.isEmpty {
              Text(state.phase).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
          }
        }
        DynamicIslandExpandedRegion(.trailing) {
          if state.needsYou {
            Circle().fill(MewlaStyle.seal).frame(width: 8, height: 8)
          } else if let since = state.startedDate {
            ElapsedView(since: since).font(.title3.weight(.semibold))
          }
        }
        DynamicIslandExpandedRegion(.bottom) {
          VStack(alignment: .leading, spacing: 10) {
            if state.needsYou, let question = state.question {
              if !question.text.isEmpty { Text(question.text).font(.body).lineLimit(2) }
              ChoiceChips(question: question)
            } else {
              SegmentsView(done: state.done, total: state.total)
              if state.workerCount > 1 { WorkerNamesView(workers: state.workers) }
            }
          }
        }
      } compactLeading: {
        SealView(size: 22)
      } compactTrailing: {
        if state.needsYou {
          Text("\(max(state.needsCount, 1))")
            .font(.caption.weight(.bold))
            .foregroundStyle(.white)
            .padding(.horizontal, 6)
            .background(Capsule().fill(MewlaStyle.seal))
        } else if let since = state.startedDate {
          Text(timerInterval: since...since.addingTimeInterval(12 * 3600), countsDown: false)
            .monospacedDigit()
            .font(.caption)
            .frame(width: 44)
        }
      } minimal: {
        SealView(size: 22)
      }
      .widgetURL(MewlaStyle.openURL)
    }
  }
}
