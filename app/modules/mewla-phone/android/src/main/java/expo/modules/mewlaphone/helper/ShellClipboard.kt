package expo.modules.mewlaphone.helper

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Handler
import android.os.Looper
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

    /** Puts [text] on the clipboard; false when the system refuses. */
    fun set(text: String): Boolean = runCatching {
        manager!!.setPrimaryClip(ClipData.newPlainText("mewla", text))
        true
    }.onFailure { Log.w(TAG, "setPrimaryClip failed", it) }.getOrDefault(false)

    private companion object {
        const val TAG = "MewlaPhoneHelper"
    }
}
