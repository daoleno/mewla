package expo.modules.mewlaphone

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.RemoteInput

/** The buttons on phone use's notifications. */
class PhoneActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        PhoneController.init(context)
        when (intent.action) {
            PhoneNotifier.ACTION_STOP -> PhoneController.stop()
            PhoneNotifier.ACTION_TAKE_OVER -> {
                PhoneController.takeOver()
                context.startActivity(
                    Intent(Intent.ACTION_VIEW, Uri.parse("mewla://phone")).setPackage(context.packageName)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP),
                )
            }
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
