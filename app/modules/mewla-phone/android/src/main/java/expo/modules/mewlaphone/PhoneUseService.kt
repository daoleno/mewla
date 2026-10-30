package expo.modules.mewlaphone

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * Keeps the App's process alive while Phone use is on, so an agent's request reaches the phone
 * with the App in the background or the screen off. Its notification is the status one.
 */
class PhoneUseService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        PhoneController.init(this)
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        ServiceCompat.startForeground(this, PhoneNotifier.ID_STATUS, PhoneNotifier.status(this), type)
        // No App screen needed: after a reboot this brings the link to the computer back by itself.
        PhoneController.ensureLink()
        return START_STICKY
    }

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, PhoneUseService::class.java))
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, PhoneUseService::class.java))
        }
    }
}
