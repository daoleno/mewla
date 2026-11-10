import UIKit

/// The system date or time picker in a small sheet with Cancel and Done. `answer` runs exactly
/// once: the picked date on Done, nil on Cancel or when the sheet is swiped away.
@MainActor
final class DatePickerSheet: UIViewController, UIAdaptivePresentationControllerDelegate {
  private let picker = UIDatePicker()
  private var answer: ((Date?) -> Void)?
  private let height: CGFloat

  private init(mode: UIDatePicker.Mode, style: UIDatePickerStyle, date: Date, height: CGFloat, answer: @escaping (Date?) -> Void) {
    self.answer = answer
    self.height = height
    super.init(nibName: nil, bundle: nil)
    picker.datePickerMode = mode
    picker.preferredDatePickerStyle = style
    picker.calendar = DatePickerSheet.calendar
    picker.date = date
  }

  @available(*, unavailable)
  required init?(coder: NSCoder) { fatalError("not from a storyboard") }

  /// Gregorian in the phone's zone: the app's dates are.
  static var calendar: Calendar {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = .current
    return calendar
  }

  static func pickDate(year: Int?, month: Int?, day: Int?, answer: @escaping ([String: Int]?) -> Void) {
    let today = calendar.dateComponents([.year, .month, .day], from: Date())
    let start = calendar.date(from: DateComponents(year: year ?? today.year, month: month ?? today.month, day: day ?? today.day, hour: 12)) ?? Date()
    present(DatePickerSheet(mode: .date, style: .inline, date: start, height: 470) { picked in
      answer(picked.map { date in
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return ["year": parts.year ?? 0, "month": parts.month ?? 0, "day": parts.day ?? 0]
      })
    }, otherwise: { answer(nil) })
  }

  static func pickTime(hour: Int?, minute: Int?, answer: @escaping ([String: Int]?) -> Void) {
    let start = calendar.date(bySettingHour: hour ?? 9, minute: minute ?? 0, second: 0, of: Date()) ?? Date()
    present(DatePickerSheet(mode: .time, style: .wheels, date: start, height: 300) { picked in
      answer(picked.map { date in
        let parts = calendar.dateComponents([.hour, .minute], from: date)
        return ["hour": parts.hour ?? 0, "minute": parts.minute ?? 0]
      })
    }, otherwise: { answer(nil) })
  }

  private static func present(_ sheet: DatePickerSheet, otherwise: () -> Void) {
    guard let top = topController() else { return otherwise() }
    let navigation = UINavigationController(rootViewController: sheet)
    navigation.modalPresentationStyle = .pageSheet
    if let presentation = navigation.sheetPresentationController {
      let height = sheet.height
      presentation.detents = [.custom { _ in height }]
      presentation.prefersGrabberVisible = false
    }
    navigation.presentationController?.delegate = sheet
    top.present(navigation, animated: true)
  }

  private static func topController() -> UIViewController? {
    let window = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
      .flatMap(\.windows).first { $0.isKeyWindow }
    var top = window?.rootViewController
    while let presented = top?.presentedViewController { top = presented }
    return top
  }

  override func viewDidLoad() {
    super.viewDidLoad()
    view.backgroundColor = .systemBackground
    navigationItem.leftBarButtonItem = UIBarButtonItem(systemItem: .cancel, primaryAction: UIAction { [weak self] _ in self?.finish(nil) })
    navigationItem.rightBarButtonItem = UIBarButtonItem(systemItem: .done, primaryAction: UIAction { [weak self] _ in
      guard let self else { return }
      self.finish(self.picker.date)
    })
    picker.translatesAutoresizingMaskIntoConstraints = false
    view.addSubview(picker)
    NSLayoutConstraint.activate([
      picker.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
      picker.leadingAnchor.constraint(equalTo: view.layoutMarginsGuide.leadingAnchor),
      picker.trailingAnchor.constraint(equalTo: view.layoutMarginsGuide.trailingAnchor),
    ])
  }

  private func finish(_ date: Date?) {
    guard let answer else { return }
    self.answer = nil
    answer(date)
    dismiss(animated: true)
  }

  /// Swiped away: a cancel.
  nonisolated func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
    MainActor.assumeIsolated {
      guard let answer else { return }
      self.answer = nil
      answer(nil)
    }
  }
}
