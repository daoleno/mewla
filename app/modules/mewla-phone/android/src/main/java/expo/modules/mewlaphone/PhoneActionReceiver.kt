package expo.modules.mewlaphone

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput

/** The buttons on phone use's notifications. */
class PhoneActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        PhoneController.init(context)
        when (intent.action) {
            PhoneNotifier.ACTION_STOP -> PhoneController.stop()
            PhoneNotifier.ACTION_RESUME -> PhoneController.giveBack()
            PhoneNotifier.ACTION_DONE -> PhoneController.handoffDone()
            PhoneNotifier.ACTION_CODE -> {
                val code = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(PhoneNotifier.KEY_CODE)?.toString()?.trim()
                if (code != null && Regex("^\\d{6}$").matches(code)) {
                    val pending = goAsync()
                    Thread {
                        val ok = PhoneController.pair(code)
                        if (ok) PhoneNotifier.clearPairingCode(context)
                        runCatching { PhoneController.ensureHelper() }
                        PhoneController.publish()
                        pending.finish()
                    }.start()
                }
            }
        }
    }
}
