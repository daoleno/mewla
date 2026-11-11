package com.daoleno.mewla.phone

import android.app.Activity
import android.graphics.Color
import android.view.ViewGroup
import android.widget.FrameLayout
import java.lang.ref.WeakReference

/**
 * In the floating window only the agent's screen fits. The app's UI is paused while it floats, so
 * the picture is a native view laid over the page for as long as the window is floating, whatever
 * the page was showing. The activity tells it when picture-in-picture starts and ends.
 */
internal object PipOverlay {
    private var shown: WeakReference<PhoneLiveView>? = null

    fun sync(activity: Activity, floating: Boolean) {
        val content = activity.findViewById<ViewGroup>(android.R.id.content) ?: return
        val view = shown?.get()?.takeIf { it.parent === content }
        if (floating && view == null) {
            val live = PhoneLiveView(activity).apply { setBackgroundColor(Color.BLACK) }
            content.addView(live, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
            shown = WeakReference(live)
        } else if (!floating && view != null) {
            content.removeView(view)
            shown = null
        }
    }
}
