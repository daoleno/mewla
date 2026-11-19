package com.daoleno.mewla.phone.helper

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.os.PersistableBundle
import android.util.Log

/**
 * The clipboard as the shell, for pasting text a field takes no other way. The manager is built by hand
 * with the shell's context: the one the system context hands out belongs to the system package, whose
 * name the shell's uid does not own.
 */
internal class ShellClipboard(private val context: Context) {
    private val manager: ClipboardManager? by lazy {
        runCatching {
            ClipboardManager::class.java.getDeclaredConstructor(Context::class.java, Handler::class.java)
                .apply { isAccessible = true }.newInstance(context, Handler(Looper.getMainLooper()))
        }.onFailure { Log.w(TAG, "no clipboard", it) }.getOrNull()
    }

    /**
     * Puts [text] on the clipboard for as long as [use] runs, then puts back what was there, so the
     * person's own clipboard is theirs again. False, without calling [use], when the system refuses, or
     * when there is a clip the shell cannot read: putting text over it would lose it for good.
     * Neither clip raises the system's copy overlay on the person's screen.
     */
    fun with(text: String, use: () -> Unit): Boolean {
        val cm = manager ?: return false
        val had = runCatching { cm.hasPrimaryClip() }.getOrDefault(true)
        val before = runCatching { cm.primaryClip }.getOrNull()
        if (had && before == null) {
            Log.w(TAG, "the clipboard holds a clip the shell cannot read: not pasting through it")
            return false
        }
        val set = runCatching { cm.setPrimaryClip(quiet(ClipData.newPlainText("mewla", text))) }
            .onFailure { Log.w(TAG, "setPrimaryClip failed", it) }.isSuccess
        if (!set) return false
        try {
            use()
        } finally {
            runCatching { if (before != null) cm.setPrimaryClip(quiet(before)) else cm.clearPrimaryClip() }
                .onFailure { Log.w(TAG, "could not put the clipboard back", it) }
        }
        return true
    }

    /** Marks [clip] so SystemUI shows no copy overlay for it; it honours the mark only from the shell. */
    private fun quiet(clip: ClipData): ClipData {
        val extras = clip.description.extras ?: PersistableBundle()
        extras.putBoolean(SUPPRESS_OVERLAY, true)
        clip.description.extras = extras
        return clip
    }

    private companion object {
        const val TAG = "MewlaPhoneHelper"
        /** SystemUI's ClipboardListener.EXTRA_SUPPRESS_OVERLAY. */
        const val SUPPRESS_OVERLAY = "com.android.systemui.SUPPRESS_CLIPBOARD_OVERLAY"
    }
}
