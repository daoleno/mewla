package expo.modules.mewlalive

import android.content.Context
import android.util.Log

/** Applies a live_state from any source (FCM data message, the app's socket) exactly once. */
object LiveController {
  private const val TAG = "MewlaLive"
  private val lock = Any()

  /**
   * @param serverRef the opaque server id the daemon echoes from register_push; "" means the
   *   source cannot tell (a socket message from the connected server).
   * @return true when the state was newer than what the phone had and was applied.
   */
  fun apply(context: Context, raw: String, serverRef: String): Boolean {
    val state = LiveState.parse(raw) ?: return false
    val app = context.applicationContext
    var widgetRepaint = false
    synchronized(lock) {
      val prefs = LivePrefs(app)
      val current = prefs.currentServerRef
      val fromCurrent = current.isNotEmpty() && (serverRef.isEmpty() || serverRef == current)
      val decision = LiveRules.decide(state, prefs.memory(), fromCurrent)
      Log.d(TAG, "live ${state.kind} epoch=${state.epoch} seq=${state.seq} -> $decision")
      when (decision) {
        LiveDecision.Ignore -> return false
        LiveDecision.Cancel -> LiveNotifier.cancel(app)
        LiveDecision.Post -> LiveNotifier.post(app, state)
      }
      prefs.remember(state)
      // The widgets show every state that is newer than theirs, even one the notification
      // does not show (idle, or a dismissed run). A refused or unset widget stays as it is.
      val widgetsChanged = prefs.widgetProvisioned() && prefs.cacheWidgetState(raw, state)
      if (widgetsChanged) widgetRepaint = true
    }
    if (widgetRepaint) WidgetRender.renderAll(app)
    return true
  }

  /** The current server changed (or was removed): no live state of the old one may linger. */
  fun switchServer(context: Context, serverRef: String) {
    val app = context.applicationContext
    synchronized(lock) {
      val prefs = LivePrefs(app)
      if (prefs.currentServerRef == serverRef) return
      prefs.currentServerRef = serverRef
      prefs.resetForServerSwitch()
      prefs.clearWidget()
      LiveNotifier.cancel(app)
    }
    WidgetRender.renderAll(app)
  }
}
