package expo.modules.mewlaphone

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput

/** The three things phone use says outside the App: that it is on or working, that it needs the person, and the pairing code box. */
internal object PhoneNotifier {
    const val CHANNEL_STATUS = "mewla-phone-status"
    const val CHANNEL_HANDOFF = "mewla-phone-handoff"
    const val CHANNEL_PAIRING = "mewla-phone-pairing"
    const val ID_STATUS = 7101
    const val ID_HANDOFF = 7102
    const val ID_PAIRING = 7103

    const val ACTION_STOP = "expo.modules.mewlaphone.STOP"
    /** Carried by the intent that opens the Phone page when the person tapped Take over. */
    const val EXTRA_TAKE_OVER = "expo.modules.mewlaphone.take_over"
    const val ACTION_RESUME = "expo.modules.mewlaphone.RESUME"
    const val ACTION_DONE = "expo.modules.mewlaphone.DONE"
    const val ACTION_CODE = "expo.modules.mewlaphone.CODE"
    const val KEY_CODE = "pairing_code"

    fun canNotify(context: Context): Boolean {
        if (Build.VERSION.SDK_INT >= 33 &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return false
        return NotificationManagerCompat.from(context).areNotificationsEnabled()
    }

    private fun ensureChannels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(CHANNEL_STATUS) != null) return
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_STATUS, "Phone use", NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            },
        )
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_HANDOFF, "Needs you", NotificationManager.IMPORTANCE_HIGH),
        )
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_PAIRING, "Pairing code", NotificationManager.IMPORTANCE_HIGH).apply {
                setSound(null, null)
                enableVibration(false)
            },
        )
    }

    private fun smallIcon(context: Context): Int {
        val id = context.resources.getIdentifier("notification_icon", "drawable", context.packageName)
        return if (id != 0) id else context.applicationInfo.icon
    }

    private fun openPhone(context: Context, request: Int): PendingIntent {
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse("mewla://phone")).setPackage(context.packageName)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        return PendingIntent.getActivity(context, request, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    /**
     * Take over opens the Phone page with the person already in charge. Android blocks a
     * notification (or floating window) button from starting an activity through a broadcast, so
     * the button is the activity launch itself and the module applies the take-over on arrival.
     */
    fun takeOver(context: Context, request: Int): PendingIntent {
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse("mewla://phone")).setPackage(context.packageName)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            .putExtra(EXTRA_TAKE_OVER, true)
        return PendingIntent.getActivity(context, request, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun action(context: Context, action: String, request: Int, mutable: Boolean = false): PendingIntent {
        val intent = Intent(context, PhoneActionReceiver::class.java).setAction(action)
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (mutable) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(context, request, intent, flags)
    }

    /** The ongoing notification the foreground service holds: quiet while idle, with Take over and Stop while an agent works. */
    fun status(context: Context): Notification {
        ensureChannels(context)
        val agent = PhoneController.activeWorker
        val builder = NotificationCompat.Builder(context, CHANNEL_STATUS)
            .setSmallIcon(smallIcon(context))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(openPhone(context, 1))
        if (!PhoneController.linkOnline) {
            // The App's process is alive for the helper but its connection to the computer is not.
            builder.setContentTitle("Open Mewla to reconnect")
        } else if (PhoneController.state() == "stopped") {
            builder.setContentTitle("Stopped")
                .addAction(0, "Resume", action(context, ACTION_RESUME, 7))
        } else if (agent == null) {
            builder.setContentTitle("Phone use is on")
        } else {
            builder.setContentTitle("$agent is using your phone")
                .addAction(0, "Take over", takeOver(context, 2))
                .addAction(0, "Stop", action(context, ACTION_STOP, 3))
        }
        return builder.build()
    }

    fun refresh(context: Context) {
        if (!PhoneController.enabled || !canNotify(context)) return
        NotificationManagerCompat.from(context).notify(ID_STATUS, status(context))
    }

    /** The person must act (a login, a code, a payment): a notification that opens the live view on the agent's screen. */
    fun handoff(context: Context, reason: String, worker: String) {
        if (!canNotify(context)) return
        ensureChannels(context)
        val who = worker.ifEmpty { "Your agent" }
        val notification = NotificationCompat.Builder(context, CHANNEL_HANDOFF)
            .setSmallIcon(smallIcon(context))
            .setContentTitle(reason)
            .setContentText("$who needs you")
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(false)
            .setOngoing(true)
            .setContentIntent(openPhone(context, 4))
            .addAction(0, "Open", openPhone(context, 5))
            .addAction(0, "Done", action(context, ACTION_DONE, 6))
            .build()
        NotificationManagerCompat.from(context).notify(ID_HANDOFF, notification)
    }

    fun clearHandoff(context: Context) {
        NotificationManagerCompat.from(context).cancel(ID_HANDOFF)
    }

    /** The code box for a phone where the accessibility reading is off: type the six digits into the notification. */
    fun pairingCode(context: Context) {
        if (!canNotify(context)) return
        ensureChannels(context)
        val input = RemoteInput.Builder(KEY_CODE).setLabel("Pairing code").build()
        val reply = NotificationCompat.Action.Builder(0, "Enter code", action(context, ACTION_CODE, 7, mutable = true))
            .addRemoteInput(input).build()
        val notification = NotificationCompat.Builder(context, CHANNEL_PAIRING)
            .setSmallIcon(smallIcon(context))
            .setContentTitle("Pairing code")
            .setContentText("Type the six digits shown on the screen")
            .setOngoing(true)
            .addAction(reply)
            .build()
        NotificationManagerCompat.from(context).notify(ID_PAIRING, notification)
    }

    fun clearPairingCode(context: Context) {
        NotificationManagerCompat.from(context).cancel(ID_PAIRING)
    }
}
