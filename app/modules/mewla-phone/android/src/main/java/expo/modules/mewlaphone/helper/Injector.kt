package expo.modules.mewlaphone.helper

import android.annotation.SuppressLint
import android.os.SystemClock
import android.view.InputDevice
import android.view.InputEvent
import android.view.KeyCharacterMap
import android.view.KeyEvent
import android.view.MotionEvent

/** Touch and key events for one display, injected the way the shell's `input` command does. */
internal class Injector {
    private val manager: Any = run {
        @SuppressLint("PrivateApi")
        val global = runCatching { Class.forName("android.hardware.input.InputManagerGlobal").getMethod("getInstance").invoke(null) }.getOrNull()
        global ?: Class.forName("android.hardware.input.InputManager").getMethod("getInstance").invoke(null)!!
    }
    private val inject = manager.javaClass.getMethod("injectInputEvent", InputEvent::class.java, Int::class.javaPrimitiveType)
    private val setDisplayId = InputEvent::class.java.getMethod("setDisplayId", Int::class.javaPrimitiveType)

    private fun send(event: InputEvent, displayId: Int): Boolean {
        setDisplayId.invoke(event, displayId)
        return inject.invoke(manager, event, INJECT_MODE_ASYNC) as Boolean
    }

    private fun touch(action: Int, downTime: Long, x: Float, y: Float, displayId: Int): Boolean {
        val event = MotionEvent.obtain(downTime, SystemClock.uptimeMillis(), action, x, y, 1f, 1f, 0, 1f, 1f, 0, 0)
        event.source = InputDevice.SOURCE_TOUCHSCREEN
        return try {
            send(event, displayId)
        } finally {
            event.recycle()
        }
    }

    private var touchDownTime = 0L

    /** One raw touch event for a person driving the agent's screen through the live view. */
    fun touch(displayId: Int, phase: String, x: Float, y: Float): Boolean {
        val action = when (phase) {
            "down" -> MotionEvent.ACTION_DOWN
            "move" -> MotionEvent.ACTION_MOVE
            "up" -> MotionEvent.ACTION_UP
            else -> MotionEvent.ACTION_CANCEL
        }
        if (action == MotionEvent.ACTION_DOWN) touchDownTime = SystemClock.uptimeMillis()
        return touch(action, touchDownTime, x, y, displayId)
    }

    fun tap(displayId: Int, x: Float, y: Float, holdMs: Long = TAP_MS): Boolean {
        val down = SystemClock.uptimeMillis()
        if (!touch(MotionEvent.ACTION_DOWN, down, x, y, displayId)) return false
        Thread.sleep(holdMs)
        return touch(MotionEvent.ACTION_UP, down, x, y, displayId)
    }

    /** One stroke from (x1, y1) to (x2, y2) over [durationMs], eased so a list flings the way a finger would. */
    fun swipe(displayId: Int, x1: Float, y1: Float, x2: Float, y2: Float, durationMs: Long): Boolean {
        val total = durationMs.coerceIn(50, 5_000)
        val down = SystemClock.uptimeMillis()
        if (!touch(MotionEvent.ACTION_DOWN, down, x1, y1, displayId)) return false
        val steps = (total / STEP_MS).toInt().coerceAtLeast(2)
        for (i in 1..steps) {
            val t = i.toFloat() / steps
            val eased = 1f - (1f - t) * (1f - t)
            touch(MotionEvent.ACTION_MOVE, down, x1 + (x2 - x1) * eased, y1 + (y2 - y1) * eased, displayId)
            Thread.sleep(total / steps)
        }
        return touch(MotionEvent.ACTION_UP, down, x2, y2, displayId)
    }

    fun key(displayId: Int, keyCode: Int, meta: Int = 0): Boolean {
        val now = SystemClock.uptimeMillis()
        val down = KeyEvent(now, now, KeyEvent.ACTION_DOWN, keyCode, 0, meta, KeyCharacterMap.VIRTUAL_KEYBOARD, 0, 0, InputDevice.SOURCE_KEYBOARD)
        if (!send(down, displayId)) return false
        val up = KeyEvent(now, SystemClock.uptimeMillis(), KeyEvent.ACTION_UP, keyCode, 0, meta, KeyCharacterMap.VIRTUAL_KEYBOARD, 0, 0, InputDevice.SOURCE_KEYBOARD)
        return send(up, displayId)
    }

    /** Select all in the focused field (Ctrl+A). */
    fun selectAll(displayId: Int): Boolean = key(displayId, KeyEvent.KEYCODE_A, KeyEvent.META_CTRL_ON or KeyEvent.META_CTRL_LEFT_ON)

    /** Paste the clipboard into the focused field. */
    fun paste(displayId: Int): Boolean = key(displayId, KeyEvent.KEYCODE_PASTE)

    /** Types [text] as key events, which only characters a keyboard has can be; false when one is not. */
    fun typeKeys(displayId: Int, text: String): Boolean {
        val events = KeyCharacterMap.load(KeyCharacterMap.VIRTUAL_KEYBOARD).getEvents(text.toCharArray()) ?: return false
        for (event in events) if (!send(event, displayId)) return false
        return true
    }

    private companion object {
        const val INJECT_MODE_ASYNC = 0
        const val TAP_MS = 50L
        const val STEP_MS = 16L
    }
}
