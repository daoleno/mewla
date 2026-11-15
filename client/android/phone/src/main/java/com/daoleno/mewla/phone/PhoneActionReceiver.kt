package com.daoleno.mewla.phone

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput

/** The buttons on phone use's notifications (and the floating window's Stop). */
class PhoneActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        PhoneController.init(context)
        when (intent.action) {
            PhoneNotifier.ACTION_STOP -> PhoneController.stop()
            PhoneNotifier.ACTION_RESUME -> PhoneController.giveBack()
            PhoneNotifier.ACTION_DONE -> PhoneController.handoffDone()
            PhoneNotifier.ACTION_CODE -> {
                val typed = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(PhoneNotifier.KEY_CODE)?.toString()
                val code = PairingScreen.code(typed)
                if (code == null) {
                    // Not six digits: the box again, for another go.
                    PhoneNotifier.pairingCode(context)
                } else if (PhoneController.pairingNow) {
                    // Handed to the pairing that waits. The box is put back first (it stops the reply's spinner and is
                    // ready should the code be wrong); pairing clears it when it ends.
                    PhoneNotifier.pairingCode(context, alert = false)
                    PhoneController.pair(code)
                } else {
                    // No pairing waits (the person opened the dialog after one ended): this code starts one.
                    // The foreground service keeps the process while it runs, so no broadcast is held open.
                    Thread({
                        if (PhoneController.pair(code)) runCatching { PhoneController.ensureHelper() }
                        PhoneController.publish()
                    }, "mewla-phone-pair").start()
                }
            }
            // Swiped away while pairing still waits for a code: it is the only place to type it.
            PhoneNotifier.ACTION_CODE_GONE -> if (PhoneController.pairingNow) PhoneNotifier.pairingCode(context, alert = false)
        }
    }
}
