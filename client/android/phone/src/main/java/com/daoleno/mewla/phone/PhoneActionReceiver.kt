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
                } else {
                    // Handed to the pairing that waits, or (the person opened the dialog after one ended) starting
                    // one. The box is put back first: it stops the reply's spinner, is ready should the code be
                    // wrong, and pairing clears it when it ends. Off the main thread either way (pairing ending
                    // just now makes this call the 3-minute one); the foreground service keeps the process.
                    val handOff = PhoneController.pairingNow
                    if (handOff) PhoneNotifier.pairingCode(context, alert = false)
                    Thread({
                        // A hand-off returns at once; the pairing it joins starts the helper when it ends.
                        if (PhoneController.pair(code) && !handOff) runCatching { PhoneController.ensureHelper() }
                        PhoneController.publish()
                    }, "mewla-phone-pair").start()
                }
            }
            // Swiped away while pairing still waits for a code: it is the only place to type it.
            PhoneNotifier.ACTION_CODE_GONE -> if (PhoneController.pairingNow) PhoneNotifier.pairingCode(context, alert = false)
        }
    }
}
