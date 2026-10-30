package expo.modules.mewlaphone

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.view.MotionEvent
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.views.ExpoView
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * The agent's screen, live. Frames arrive from the helper as JPEGs and are drawn letterboxed;
 * when [interactive] the person's finger drives the agent's screen through the helper.
 */
class PhoneLiveView(context: Context, appContext: AppContext) : ExpoView(context, appContext) {
    private var frame: Bitmap? = null
    private val paint = Paint(Paint.FILTER_BITMAP_FLAG)
    private val box = RectF()
    private val decoding = AtomicBoolean(false)
    private val touches = Executors.newSingleThreadExecutor()

    /** Whether touches go to the agent's screen (Take over) or are ignored (Watch). */
    var interactive = false

    /** What this view needs of the shared stream; the stream runs at the highest of the mounted views. */
    var fps = PhoneLiveViews.DEFAULT_FPS
        set(value) {
            field = value
            if (isAttachedToWindow) PhoneLiveViews.refresh()
        }
    var maxWidth = PhoneLiveViews.DEFAULT_WIDTH
        set(value) {
            field = value
            if (isAttachedToWindow) PhoneLiveViews.refresh()
        }

    init {
        setWillNotDraw(false)
        setBackgroundColor(Color.TRANSPARENT)
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        PhoneLiveViews.add(this)
        PhoneLiveViews.last?.let { showFrame(it) }
    }

    override fun onDetachedFromWindow() {
        PhoneLiveViews.remove(this)
        super.onDetachedFromWindow()
    }

    /** A frame from the helper; decoding happens off the UI thread and a frame that arrives mid-decode is dropped. */
    fun showFrame(jpeg: ByteArray) {
        if (!decoding.compareAndSet(false, true)) return
        Thread {
            val bitmap = try {
                BitmapFactory.decodeByteArray(jpeg, 0, jpeg.size)
            } finally {
                decoding.set(false)
            }
            if (bitmap != null) post {
                frame = bitmap
                invalidate()
            }
        }.start()
    }

    private fun fit(bitmap: Bitmap): RectF {
        val scale = minOf(width.toFloat() / bitmap.width, height.toFloat() / bitmap.height)
        val w = bitmap.width * scale
        val h = bitmap.height * scale
        box.set((width - w) / 2f, (height - h) / 2f, (width + w) / 2f, (height + h) / 2f)
        return box
    }

    override fun onDraw(canvas: Canvas) {
        val bitmap = frame ?: return
        canvas.drawBitmap(bitmap, null, fit(bitmap), paint)
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (!interactive) return false
        val bitmap = frame ?: return true
        val rect = fit(bitmap)
        val display = PhoneController.displaySize() ?: return true
        val x = ((event.x - rect.left) / rect.width()).coerceIn(0f, 1f) * display[0]
        val y = ((event.y - rect.top) / rect.height()).coerceIn(0f, 1f) * display[1]
        val phase = when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> "down"
            MotionEvent.ACTION_MOVE -> "move"
            MotionEvent.ACTION_UP -> "up"
            MotionEvent.ACTION_CANCEL -> "cancel"
            else -> return true
        }
        touches.execute { PhoneController.touch(phase, x.toDouble(), y.toDouble()) }
        return true
    }
}

/**
 * The mounted live views, which share the one frame stream. Every change to the set or to what a
 * view asks for re-issues the one watch request, in order on a single thread, so two views never
 * race each other and the stream falls back to the rate a remaining view wants.
 */
internal object PhoneLiveViews {
    const val DEFAULT_FPS = 12
    const val DEFAULT_WIDTH = 540

    private val views = java.util.concurrent.CopyOnWriteArrayList<PhoneLiveView>()
    private val requests = Executors.newSingleThreadExecutor()

    fun add(view: PhoneLiveView) {
        views.addIfAbsent(view)
        refresh()
    }

    fun remove(view: PhoneLiveView) {
        views.remove(view)
        refresh()
    }

    /** The newest frame, so a view that appears while the agent's screen sits still has a picture at once. */
    @Volatile var last: ByteArray? = null

    fun deliver(jpeg: ByteArray) {
        last = jpeg
        views.forEach { it.showFrame(jpeg) }
    }

    /** The helper or its display just came back: views that are already mounted ask for frames again. */
    fun resume() {
        if (views.isNotEmpty()) refresh()
    }

    /** Frames flow only while a view is mounted, at the rate of the most demanding one. */
    fun refresh() {
        val fps = views.maxOfOrNull { it.fps }
        val width = views.maxOfOrNull { it.maxWidth }
        requests.execute {
            if (fps == null || width == null) PhoneController.watch(false) else PhoneController.watch(true, fps, width)
        }
    }
}
