package com.daoleno.mewla.phone.helper

import java.nio.ByteBuffer

/** When the agent's display is stale enough that it is better made again than trusted. */
internal object DisplayHealth {
    /**
     * After an app was started on the display: a display the system dropped, one that has never drawn a single frame,
     * or one that drew nothing for the launch and [showsNothing] (black, and no app window on it at all), is replaced,
     * once, and the app started there. Replacing destroys what is on the display, so a display with any app window on
     * it is kept, even one that draws black with nothing to read (a video, a splash): an app that is merely already in
     * front draws nothing new. (11-19: a display that had drawn earlier stayed black with nothing on it after Chrome,
     * X and 小红书 were opened, and every launch answered success.)
     */
    fun needsRecreate(alive: Boolean, drewSinceLaunch: Boolean, everDrew: Boolean, attempt: Int, showsNothing: Boolean = false): Boolean =
        attempt == 0 && (!alive || (!drewSinceLaunch && (!everDrew || showsNothing)))

    /** What a reading with nothing to read is, from the app windows on the display, its elements and its picture. */
    enum class Empty {
        /** Something to read: not this case. */
        NONE,
        /** A launch left no app window on the display: the app did not open there. The one case that is a failure. */
        NOT_OPENED,
        /** No app is open on the display (black, no window): nothing was opened yet, or what was there closed. */
        NOTHING_OPEN,
        /** An app window is there and draws black with nothing to read: a cold start, a video, a camera. */
        BLACK_APP,
        /** No app window answers while the picture shows something: the app hung or closed and the picture may be stale. */
        NOT_ANSWERING,
        /** An app window draws a picture with nothing to read: an app that draws its own UI (WeChat). */
        OWN_UI,
    }

    fun empty(windows: Int, elements: Int, dark: Boolean, launched: Boolean): Empty = when {
        elements > 0 -> Empty.NONE
        windows == 0 && launched -> Empty.NOT_OPENED
        windows == 0 && dark -> Empty.NOTHING_OPEN
        windows == 0 -> Empty.NOT_ANSWERING
        dark -> Empty.BLACK_APP
        else -> Empty.OWN_UI
    }
}

/** Whether a frame is black: what a display shows when nothing draws on it. */
internal object FrameCheck {
    /** Pixels sampled across the frame, on a grid this many steps wide and high. */
    private const val GRID = 32
    /** The brightest channel value still taken for black (JPEG and dithering leave a little). */
    private const val DARK = 24

    /**
     * Whether nearly all of a frame's sampled pixels are black. [buffer] holds RGBA_8888 rows of [rowStride] bytes,
     * [pixelStride] bytes a pixel, as an ImageReader plane gives them.
     */
    fun dark(buffer: ByteBuffer, rowStride: Int, pixelStride: Int, width: Int, height: Int): Boolean {
        if (width <= 0 || height <= 0) return true
        var bright = 0
        var total = 0
        for (gy in 0 until GRID) {
            val y = (height - 1) * gy / (GRID - 1).coerceAtLeast(1)
            for (gx in 0 until GRID) {
                val x = (width - 1) * gx / (GRID - 1).coerceAtLeast(1)
                val at = y * rowStride + x * pixelStride
                if (at + 2 >= buffer.limit()) continue
                total++
                val r = buffer.get(at).toInt() and 0xFF
                val g = buffer.get(at + 1).toInt() and 0xFF
                val b = buffer.get(at + 2).toInt() and 0xFF
                if (maxOf(r, g, b) > DARK) bright++
            }
        }
        // A cursor, a status dot or a thin line on black is still nothing to act on.
        return total == 0 || bright * 100 < total
    }
}
