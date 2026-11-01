package expo.modules.keyboardlifecycle

import android.util.Log
import android.view.View
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.facebook.react.uimanager.util.ReactFindViewUtil
import expo.modules.kotlin.functions.Queues
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class KeyboardLifecycleModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("KeyboardLifecycle")

    // Opt-in diagnostics: `adb shell setprop debug.mewla.keyboard 1`, then
    // `adb logcat -s MewlaKeyboardDiag`.
    Function("diagEnabled") { diagEnabled() }

    Function("diagLog") { message: String -> Log.i("MewlaKeyboardDiag", message) }

    AsyncFunction("getForegroundSnapshot") { composerNativeId: String, revision: Int ->
      val activity = appContext.activityProvider?.currentActivity
      val root = activity?.window?.decorView
      if (activity == null || root == null || activity.isFinishing || activity.isDestroyed) {
        return@AsyncFunction closedSnapshot(revision, "activity_unavailable")
      }

      val rootInsets = ViewCompat.getRootWindowInsets(root)
      if (rootInsets == null) {
        return@AsyncFunction closedSnapshot(revision, "insets_unavailable")
      }

      val imeType = WindowInsetsCompat.Type.ime()
      val imeVisible = rootInsets.isVisible(imeType)
      val imeInsetPixels = rootInsets.getInsets(imeType).bottom
      val density = root.resources.displayMetrics.density
      val imeHeight = imeInsetPixels.coerceAtLeast(0) / density
      val composer = ReactFindViewUtil.findView(root, composerNativeId)
      val focused = activity.currentFocus
      val composerFocused = composer != null && focused != null && focused.isWithin(composer)

      mapOf(
        "revision" to revision,
        "imeVisible" to imeVisible,
        "imeHeight" to if (imeVisible) imeHeight else 0f,
        "composerFocused" to composerFocused,
        "evidence" to "window_insets_ime"
      )
    }.runOnQueue(Queues.MAIN)
  }

  private fun diagEnabled(): Boolean = try {
    val properties = Class.forName("android.os.SystemProperties")
    val get = properties.getMethod("get", String::class.java, String::class.java)
    get.invoke(null, "debug.mewla.keyboard", "0") == "1"
  } catch (_: Exception) {
    false
  }

  private fun View.isWithin(ancestor: View): Boolean {
    var candidate: View? = this
    while (candidate != null) {
      if (candidate === ancestor) return true
      candidate = candidate.parent as? View
    }
    return false
  }

  private fun closedSnapshot(revision: Int, evidence: String) = mapOf(
    "revision" to revision,
    "imeVisible" to false,
    "imeHeight" to 0,
    "composerFocused" to false,
    "evidence" to evidence
  )
}
