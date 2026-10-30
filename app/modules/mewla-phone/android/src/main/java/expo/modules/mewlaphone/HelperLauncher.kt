package expo.modules.mewlaphone

import android.content.Context
import expo.modules.mewlaphone.adb.AdbConnection

/** Starts the helper through the phone's own Wireless debugging, as the shell. */
object HelperLauncher {
    const val HELPER_CLASS = "expo.modules.mewlaphone.helper.PhoneHelperMain"
    const val LOG_FILE = "/data/local/tmp/mewla-phone-helper.log"

    /**
     * The shell command that starts the helper in its own session, then waits for a line on its
     * input. The first line (the token) goes to the helper. adbd ends everything an exec session
     * started when the session closes, and a child that has not yet left the session by then goes
     * with it, so the session stays open until the helper answers.
     */
    fun command(context: Context, endpoint: HelperEndpoint): String {
        val apk = context.applicationInfo.sourceDir
        val build = installStamp(context)
        return "read t; (echo \"\$t\" | CLASSPATH=$apk setsid app_process /system/bin --nice-name=mewla-phone-helper " +
            "$HELPER_CLASS ${context.packageName} ${endpoint.port} $build >$LOG_FILE 2>&1 &); read _"
    }

    /** Changes with every install of the App, so a helper started by an older install is known by it. */
    fun installStamp(context: Context): Long =
        runCatching { context.packageManager.getPackageInfo(context.packageName, 0).lastUpdateTime }.getOrDefault(0L)

    /** Starts the helper and returns once it answers; throws when it does not come up. */
    fun start(context: Context, connection: AdbConnection, waitMs: Long = 10_000): HelperClient {
        var endpoint = HelperEndpoint.load(context)
        val stream = connection.exec(command(context, endpoint))
        try {
            stream.write((endpoint.token + "\n").toByteArray())
            val deadline = System.currentTimeMillis() + waitMs
            while (System.currentTimeMillis() < deadline) {
                HelperClient.connect(endpoint)?.let { return it }
                Thread.sleep(150)
            }
            throw java.io.IOException("the helper did not start; see $LOG_FILE")
        } finally {
            // The helper is up and detached: let the session end.
            runCatching { stream.write("\n".toByteArray()) }
            stream.close()
        }
    }
}
