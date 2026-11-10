import NativeShell
import UIKit

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
  func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
    // Before launch ends: the tap that started the app reaches this delegate right after.
    PushCenter.shared.start()
    return true
  }

  func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
    PushCenter.shared.registered(deviceToken)
  }

  func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
    PushCenter.shared.failed(error)
  }

  /// A content-available push: a live_state one updates the Live Activity and widgets with the app closed.
  func application(
    _ application: UIApplication,
    didReceiveRemoteNotification userInfo: [AnyHashable: Any],
    fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
  ) {
    LivePush.receive(userInfo, state: application.applicationState, done: completionHandler)
  }
}

/// The whole app is client/ui on NSH; this host gives it what only the platform has:
/// the Keychain, the camera reader, the links the system opens, notifications, the Live
/// Activity and widgets, calendar alerts, zones and pickers and, in a debug build linked
/// with NativeShellDev, hot reload from `nsh dev`.
final class SceneDelegate: UIResponder, UIWindowSceneDelegate {
  var window: UIWindow?
  private let app = NshAppView()
  private let secure = SecureStore()
  private let legacy = LegacyStorage()

  func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options: UIScene.ConnectionOptions) {
    guard let scene = scene as? UIWindowScene else { return }
    app.onError = { NSLog("Mewla: %@", $0) }
    app.register(component: "mewla.qrScanner") { QrScanner($0) }
    app.handle("secure.get") { [secure] data in try secure.get(Self.field(data, "key")) }
    app.handle("secure.set") { [secure] data in try secure.set(Self.field(data, "key"), Self.field(data, "value")); return nil }
    app.handle("secure.remove") { [secure] data in try secure.remove(Self.field(data, "key")); return nil }
    app.handle("app.legacyStorage") { [legacy] _ in legacy.entries() }
    app.handle("app.legacyStorageCopied") { [legacy] _ in legacy.copied(); return nil }
    PushHost.register(app)
    LiveHost.register(app)
    CalendarHost.register(app)
    app.params = ["deviceName": UIDevice.current.name]

    let window = UIWindow(windowScene: scene)
    window.rootViewController = HostController(app: app)
    window.makeKeyAndVisible()
    self.window = window
    load()
    PushCenter.shared.attach(app)
    options.urlContexts.forEach { open($0.url) }
  }

  func scene(_ scene: UIScene, openURLContexts contexts: Set<UIOpenURLContext>) {
    contexts.forEach { open($0.url) }
  }

  /// A `mewla://` link; the app holds the event until it listens.
  private func open(_ url: URL) {
    app.send("links.open", ["url": url.absoluteString])
  }

  private func load() {
    #if DEBUG
    // client/scripts/dev-ios.sh passes the dev server; the release framework refuses it.
    if let dev = UserDefaults.standard.string(forKey: "devServer"), let url = URL(string: dev), (try? app.connectDevServer(url)) != nil { return }
    #endif
    do {
      guard let bundle = Bundle.main.url(forResource: "bundle", withExtension: "nshc", subdirectory: "nsh") else {
        throw NSError(domain: "Mewla", code: 1, userInfo: [NSLocalizedDescriptionKey: "the app bundle is missing"])
      }
      try app.loadBundle(at: bundle)
    } catch {
      NSLog("Mewla: cannot load: %@", String(describing: error))
    }
  }

  private static func field(_ data: Any?, _ name: String) throws -> String {
    guard let value = (data as? [String: Any])?[name] as? String else {
      throw NSError(domain: "Mewla", code: 2, userInfo: [NSLocalizedDescriptionKey: "\(name) is required"])
    }
    return value
  }
}

final class HostController: UIViewController {
  let app: NshAppView
  init(app: NshAppView) {
    self.app = app
    super.init(nibName: nil, bundle: nil)
  }

  @available(*, unavailable)
  required init?(coder: NSCoder) { fatalError("not from a storyboard") }

  override func viewDidLoad() {
    super.viewDidLoad()
    app.translatesAutoresizingMaskIntoConstraints = false
    view.addSubview(app)
    NSLayoutConstraint.activate([
      app.topAnchor.constraint(equalTo: view.topAnchor),
      app.bottomAnchor.constraint(equalTo: view.bottomAnchor),
      app.leadingAnchor.constraint(equalTo: view.leadingAnchor),
      app.trailingAnchor.constraint(equalTo: view.trailingAnchor),
    ])
  }

  override var preferredStatusBarStyle: UIStatusBarStyle { NshAppView.preferredStatusBarStyle }
  override var prefersStatusBarHidden: Bool { NshAppView.prefersStatusBarHidden }
  override var preferredStatusBarUpdateAnimation: UIStatusBarAnimation { NshAppView.preferredStatusBarUpdateAnimation }
}
