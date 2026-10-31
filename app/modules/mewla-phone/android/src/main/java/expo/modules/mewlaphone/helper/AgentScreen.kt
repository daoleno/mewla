package expo.modules.mewlaphone.helper

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Bitmap
import android.graphics.PixelFormat
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.Image
import android.media.ImageReader
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import java.io.ByteArrayOutputStream
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * The agent's own screen: a virtual display whose frames land in an ImageReader. Apps launched on it
 * leave the person's screen alone. The latest frame is always kept, so a screenshot is the screen as
 * it is now even when nothing has changed since the last frame.
 */
internal class AgentScreen(context: Context, val width: Int, val height: Int, val dpi: Int) {
    private val thread = HandlerThread("mewla-agent-frames").apply { start() }
    private val handler = Handler(thread.looper)
    private val reader = ImageReader.newInstance(width, height, PixelFormat.RGBA_8888, 3)
    private val lock = Any()
    private var held: Image? = null
    private var scratch: Bitmap? = null
    private val display: VirtualDisplay
    private val manager: DisplayManager
    @Volatile private var released = false

    /** Called with a JPEG on the frame thread while someone watches. */
    @Volatile var sink: ((ByteArray) -> Unit)? = null
    @Volatile private var intervalMs = 66L
    @Volatile private var maxWidth = 540
    @Volatile private var quality = 55
    private var lastPushAt = 0L
    @Volatile private var lastFrameAt = SystemClock.uptimeMillis()
    @Volatile private var frames = 0L
    @Volatile private var actionAt = 0L
    private var pushPending = false
    // Frames go to the app from a thread of their own and are dropped while it is still busy: a
    // slow or stalled reader on the app's side must never hold up taking frames from the display.
    private val pusher: ExecutorService = Executors.newSingleThreadExecutor { Thread(it, "mewla-agent-push").apply { isDaemon = true } }
    private val pushing = AtomicBoolean(false)

    val displayId: Int

    init {
        // The context's own getSystemService would bind the manager to the system package, which
        // the shell uid does not own.
        @SuppressLint("PrivateApi")
        manager = DisplayManager::class.java.getDeclaredConstructor(Context::class.java)
            .apply { isAccessible = true }.newInstance(context)
        reader.setOnImageAvailableListener({ onImage() }, handler)
        display = manager.createVirtualDisplay("mewla-agent", width, height, dpi, reader.surface, flags())
            ?: error("createVirtualDisplay returned null")
        displayId = display.display.displayId
    }

    fun watch(fps: Int, maxWidth: Int, quality: Int, sink: ((ByteArray) -> Unit)?) {
        this.maxWidth = maxWidth.coerceIn(120, width)
        this.quality = quality.coerceIn(20, 90)
        this.intervalMs = (1000L / fps.coerceIn(1, 30))
        this.sink = sink
        if (sink != null) handler.post { push() }
    }

    private fun onImage() {
        if (!drain()) return
        if (sink == null || pushPending) return
        val wait = lastPushAt + intervalMs - SystemClock.uptimeMillis()
        if (wait > 0) {
            pushPending = true
            handler.postDelayed({ pushPending = false; push() }, wait)
        } else {
            push()
        }
    }

    /**
     * Takes the newest frame the display has drawn, if there is one, as the held frame. Any thread may
     * ask, so a screenshot or a settle sees what the display drew even when the frame thread is late.
     */
    private fun drain(): Boolean {
        if (released) return false
        val image = synchronized(lock) {
            val next = runCatching { reader.acquireLatestImage() }.getOrNull()
            if (next != null) {
                held?.close()
                held = next
            }
            next
        } ?: return false
        lastFrameAt = SystemClock.uptimeMillis()
        frames++
        return true
    }

    private fun push() {
        val target = sink ?: return
        if (!pushing.compareAndSet(false, true)) return
        lastPushAt = SystemClock.uptimeMillis()
        val width = maxWidth
        val q = quality
        pusher.execute {
            try {
                jpeg(width, q)?.let(target)
            } finally {
                pushing.set(false)
            }
        }
    }

    /** Whether the system still has this display: a removed one keeps its object but never draws again. */
    fun alive(): Boolean = !released && runCatching { manager.getDisplay(displayId) != null }.getOrDefault(true)

    /** Waits up to [timeoutMs] for a frame drawn after [since] (uptime ms); false when the display drew nothing. */
    fun drewSince(since: Long, timeoutMs: Long): Boolean {
        val deadline = SystemClock.uptimeMillis() + timeoutMs
        while (lastFrameAt < since) {
            if (SystemClock.uptimeMillis() >= deadline) return false
            Thread.sleep(40)
        }
        return true
    }

    /** How many frames this display has ever drawn, and how long ago the last one was. */
    fun frameCount(): Long = frames
    fun frameAgeMs(): Long = SystemClock.uptimeMillis() - lastFrameAt

    /** Notes that something is about to act on this display, so [settle] waits for what it causes. */
    fun markAction() {
        actionAt = SystemClock.uptimeMillis()
    }

    /**
     * Waits until the display has stopped changing for [quietMs], at most [timeoutMs], so what the
     * agent reads next is the screen its action led to, not the middle of the transition. An
     * action that draws something is seen by its first frame; one that draws nothing (a tap on a
     * dead spot) is given [minWaitMs] to do so. A screen that never settles (a video, a spinner)
     * returns when the time is up.
     */
    fun settle(quietMs: Long, timeoutMs: Long, minWaitMs: Long): Boolean {
        val deadline = SystemClock.uptimeMillis() + timeoutMs
        while (true) {
            val now = SystemClock.uptimeMillis()
            drain()
            if (SettlePolicy.settled(now, actionAt, lastFrameAt, quietMs, minWaitMs)) return true
            if (now >= deadline) return false
            Thread.sleep(SETTLE_POLL_MS)
        }
    }

    /** Whether the display drew after the last action, so a picture of it shows what the action led to. */
    fun drewSinceAction(): Boolean = SettlePolicy.drewSince(actionAt, lastFrameAt)

    /** The screen now as a JPEG no wider than [maxWidth]; null before the first frame. */
    fun jpeg(maxWidth: Int, quality: Int): ByteArray? {
        drain()
        return synchronized(lock) {
            val image = held ?: return@synchronized blank(maxWidth, quality)
            encode(image, maxWidth, quality)
        }
    }

    /** A display nothing has drawn on yet is black: say so with a picture instead of none. */
    private fun blank(maxWidth: Int, quality: Int): ByteArray {
        val target = maxWidth.coerceIn(1, width)
        val bitmap = Bitmap.createBitmap(target, maxOf(1, height * target / width), Bitmap.Config.ARGB_8888)
        bitmap.eraseColor(0xFF000000.toInt())
        val out = ByteArrayOutputStream(8 * 1024)
        bitmap.compress(Bitmap.CompressFormat.JPEG, quality, out)
        bitmap.recycle()
        return out.toByteArray()
    }

    private fun encode(image: Image, maxWidth: Int, quality: Int): ByteArray {
        val plane = image.planes[0]
        val strideWidth = plane.rowStride / plane.pixelStride
        var bitmap = scratch
        if (bitmap == null || bitmap.width != strideWidth) {
            bitmap?.recycle()
            bitmap = Bitmap.createBitmap(strideWidth, height, Bitmap.Config.ARGB_8888)
            scratch = bitmap
        }
        plane.buffer.rewind()
        bitmap.copyPixelsFromBuffer(plane.buffer)
        val cropped = if (strideWidth != width) Bitmap.createBitmap(bitmap, 0, 0, width, height) else bitmap
        val target = maxWidth.coerceIn(1, width)
        val scaled = if (target < width) Bitmap.createScaledBitmap(cropped, target, maxOf(1, height * target / width), true) else cropped
        val out = ByteArrayOutputStream(64 * 1024)
        scaled.compress(Bitmap.CompressFormat.JPEG, quality, out)
        if (scaled !== cropped) scaled.recycle()
        if (cropped !== bitmap) cropped.recycle()
        return out.toByteArray()
    }

    fun release() {
        if (released) return
        released = true
        sink = null
        pusher.shutdownNow()
        runCatching { display.release() }
        handler.post {
            synchronized(lock) {
                held?.close()
                held = null
                scratch?.recycle()
                scratch = null
            }
            reader.close()
            thread.quitSafely()
        }
    }

    private fun flags(): Int {
        var flags = FLAG_PUBLIC or FLAG_OWN_CONTENT_ONLY or FLAG_SUPPORTS_TOUCH or FLAG_ROTATES_WITH_CONTENT or FLAG_DESTROY_CONTENT_ON_REMOVAL
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            flags = flags or FLAG_TRUSTED or FLAG_OWN_DISPLAY_GROUP or FLAG_ALWAYS_UNLOCKED or FLAG_TOUCH_FEEDBACK_DISABLED
        }
        // Its own focus keeps the person's focused window where it is; STEAL_TOP_FOCUS_DISABLED
        // stops an app opening a page here from taking the top focus either.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) flags = flags or FLAG_OWN_FOCUS or FLAG_STEAL_TOP_FOCUS_DISABLED
        return flags
    }

    private companion object {
        const val SETTLE_POLL_MS = 20L
        // DisplayManager.VIRTUAL_DISPLAY_FLAG_*; several are hidden constants.
        const val FLAG_PUBLIC = 1 shl 0
        const val FLAG_OWN_CONTENT_ONLY = 1 shl 3
        const val FLAG_SUPPORTS_TOUCH = 1 shl 6
        const val FLAG_ROTATES_WITH_CONTENT = 1 shl 7
        const val FLAG_DESTROY_CONTENT_ON_REMOVAL = 1 shl 8
        const val FLAG_TRUSTED = 1 shl 10
        const val FLAG_OWN_DISPLAY_GROUP = 1 shl 11
        const val FLAG_ALWAYS_UNLOCKED = 1 shl 12
        const val FLAG_TOUCH_FEEDBACK_DISABLED = 1 shl 13
        const val FLAG_OWN_FOCUS = 1 shl 14
        const val FLAG_STEAL_TOP_FOCUS_DISABLED = 1 shl 16
    }
}
