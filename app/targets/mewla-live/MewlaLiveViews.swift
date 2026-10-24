import SwiftUI
import WidgetKit

enum MewlaStyle {
  /// The seal vermilion. Red marks only "you are needed"; everything else is neutral.
  static let seal = Color(red: 200 / 255, green: 55 / 255, blue: 43 / 255)

  static func headline(_ state: MewlaLiveState) -> String {
    if state.needsYou { return "Brain needs you" }
    if state.workerCount > 1 { return "\(state.workerCount) workers" }
    return state.title.isEmpty ? "Mewla" : state.title
  }

  /// mewla://live/answer?q=<question>&c=<choice index>: the app opens on the question and sends the answer once.
  static func answerURL(_ question: MewlaLiveQuestion, choice: Int) -> URL {
    var components = URLComponents()
    components.scheme = "mewla"
    components.host = "live"
    components.path = "/answer"
    components.queryItems = [
      URLQueryItem(name: "q", value: question.id),
      URLQueryItem(name: "c", value: String(choice)),
    ]
    return components.url ?? URL(string: "mewla://live")!
  }

  static let openURL = URL(string: "mewla://live")!
}

struct SealView: View {
  var size: CGFloat = 26
  var body: some View {
    Image("seal")
      .resizable()
      .scaledToFit()
      .frame(width: size, height: size)
      .clipShape(Circle())
  }
}

/// Elapsed time that ticks by itself: the daemon never pushes a state for the clock.
struct ElapsedView: View {
  let since: Date
  var body: some View {
    Text(timerInterval: since...since.addingTimeInterval(12 * 3600), countsDown: false)
      .monospacedDigit()
      .multilineTextAlignment(.trailing)
      .frame(width: 62, alignment: .trailing)
  }
}

/// The objective's steps, Done of Total.
struct SegmentsView: View {
  let done: Int
  let total: Int
  var body: some View {
    if total > 0 {
      HStack(spacing: 4) {
        ForEach(0..<min(total, 12), id: \.self) { index in
          Capsule()
            .fill(index < done ? Color.primary : Color.primary.opacity(0.22))
            .frame(height: 4)
        }
      }
    }
  }
}

struct WorkerNamesView: View {
  let workers: [MewlaLiveWorker]
  var body: some View {
    HStack(spacing: 12) {
      ForEach(Array(workers.prefix(3).enumerated()), id: \.offset) { index, worker in
        HStack(spacing: 4) {
          Circle()
            .fill(index == 0 ? Color.primary : Color.secondary)
            .frame(width: 6, height: 6)
          Text(worker.name).lineLimit(1)
        }
      }
    }
    .font(.caption)
    .foregroundStyle(.secondary)
  }
}

/// Neutral quick-reply chips: Brain does not rank them. Each one opens the app on that answer.
struct ChoiceChips: View {
  let question: MewlaLiveQuestion
  var compact = false
  var body: some View {
    HStack(spacing: 8) {
      ForEach(Array(question.choices.prefix(3).enumerated()), id: \.offset) { index, choice in
        Link(destination: MewlaStyle.answerURL(question, choice: index)) {
          Text(choice)
            .font(compact ? .caption.weight(.semibold) : .subheadline.weight(.semibold))
            .lineLimit(1)
            .frame(maxWidth: .infinity)
            .padding(.vertical, compact ? 5 : 8)
            .background(Capsule().fill(Color.primary.opacity(0.14)))
        }
        .buttonStyle(.plain)
      }
    }
  }
}

/// The Lock Screen card, and the Dynamic Island's expanded body.
struct RunningCard: View {
  let state: MewlaLiveState

  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      if state.needsYou, let question = state.question {
        HStack(spacing: 8) {
          SealView()
          Text("Brain needs you").font(.subheadline.weight(.semibold))
          Spacer()
          Circle().fill(MewlaStyle.seal).frame(width: 8, height: 8)
        }
        if !question.text.isEmpty {
          Text(question.text).font(.body).lineLimit(2)
        }
        ChoiceChips(question: question)
      } else {
        HStack(spacing: 10) {
          SealView(size: 32)
          VStack(alignment: .leading, spacing: 1) {
            Text(MewlaStyle.headline(state)).font(.subheadline.weight(.semibold)).lineLimit(1)
            if !state.phase.isEmpty {
              Text(state.phase).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }
          }
          Spacer()
          if let since = state.startedDate {
            ElapsedView(since: since).font(.title3.weight(.semibold))
          }
        }
        SegmentsView(done: state.done, total: state.total)
        if state.workerCount > 1 { WorkerNamesView(workers: state.workers) }
      }
    }
    .padding(14)
  }
}
