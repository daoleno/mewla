package com.daoleno.mewla.phone

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log

/**
 * After a reboot, brings Phone use back when the person had it on: the service keeps the process,
 * the keeper starts the helper again through the already paired Wireless debugging, and the link
 * reconnects to the computer it last followed.
 */
class PhoneBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        // init starts the service, the keeper and the link itself when Phone use is on.
        runCatching { PhoneController.init(context) }.onFailure { Log.w("MewlaPhone", "boot start failed", it) }
    }
}
