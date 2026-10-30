package expo.modules.mewlaphone

import android.content.Context
import android.content.Intent
import com.facebook.react.HeadlessJsTaskService
import com.facebook.react.bridge.Arguments
import com.facebook.react.jstasks.HeadlessJsTaskConfig

/**
 * Starts the App's JavaScript without a screen, so the phone's signed connection to the computer
 * comes back after a reboot, or once the person has swiped the App away, with nobody opening it.
 * The task (services/phoneHeadless.ts) holds the same one link the Phone page does.
 */
class PhoneLinkTaskService : HeadlessJsTaskService() {
    override fun getTaskConfig(intent: Intent?): HeadlessJsTaskConfig =
        HeadlessJsTaskConfig("MewlaPhoneLink", Arguments.createMap(), 60_000, true)

    companion object {
        fun start(context: Context) {
            context.startService(Intent(context, PhoneLinkTaskService::class.java))
        }
    }
}
