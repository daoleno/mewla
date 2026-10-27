package expo.modules.touchcancel

import android.os.SystemClock
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

private const val GESTURE_ROOT_CLASS =
  "com.swmansion.gesturehandler.react.RNGestureHandlerRootView"

/**
 * When a gesture-handler gesture takes over a touch, its root view stops
 * passing the touch down without the ACTION_CANCEL a native intercept would
 * send. Views below keep waiting for an UP that never comes: a selectable
 * TextView's long press fires and selects the word under the finger. This
 * sends them that cancel. The root itself is skipped, so the gesture that
 * took over keeps running.
 */
class TouchCancelModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("TouchCancel")

    Function("cancelTouchesBelowGestureRoot") {
      appContext.currentActivity?.window?.decorView?.let { decor ->
        decor.post { cancelBelowGestureRoots(decor) }
      }
      Unit
    }
  }

  private fun cancelBelowGestureRoots(view: View) {
    if (view !is ViewGroup) return
    if (view.javaClass.name == GESTURE_ROOT_CLASS) {
      val now = SystemClock.uptimeMillis()
      val cancel = MotionEvent.obtain(now, now, MotionEvent.ACTION_CANCEL, 0f, 0f, 0)
      for (index in 0 until view.childCount) {
        view.getChildAt(index).dispatchTouchEvent(cancel)
      }
      cancel.recycle()
      return
    }
    for (index in 0 until view.childCount) {
      cancelBelowGestureRoots(view.getChildAt(index))
    }
  }
}
