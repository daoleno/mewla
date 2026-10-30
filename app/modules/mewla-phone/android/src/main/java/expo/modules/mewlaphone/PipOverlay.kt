package expo.modules.mewlaphone

import android.app.Activity
import android.graphics.Color
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import expo.modules.kotlin.AppContext
import java.lang.ref.WeakReference

/**
 * In the floating window only the agent's screen fits. The Activity is paused while it floats and
 * React does not mount UI changes then, so the picture is a native view laid over the page for as
 * long as the window is floating, whatever the page was showing.
 */
internal object PipOverlay {
    private var activity = WeakReference<Activity>(null)
    private var view: PhoneLiveView? = null

    /** Starts following the window of [target]; safe to call again. */
    fun follow(target: Activity, appContext: AppContext) {
        target.runOnUiThread {
            if (activity.get() === target) return@runOnUiThread
            activity = WeakReference(target)
            view = null
            target.window.decorView.addOnLayoutChangeListener { decor, _, _, _, _, _, _, _, _ ->
                decor.post { sync(target, appContext) }
            }
        }
    }

    private fun sync(target: Activity, appContext: AppContext) {
        val content = target.findViewById<ViewGroup>(android.R.id.content) ?: return
        val floating = target.isInPictureInPictureMode
        val shown = view
        if (floating && shown == null) {
            val live = PhoneLiveView(target, appContext).apply { setBackgroundColor(Color.BLACK) }
            content.addView(live, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
            view = live
        } else if (!floating && shown != null) {
            content.removeView(shown)
            view = null
        }
    }
}
