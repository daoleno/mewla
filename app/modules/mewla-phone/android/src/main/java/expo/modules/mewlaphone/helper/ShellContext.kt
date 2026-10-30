package expo.modules.mewlaphone.helper

import android.annotation.SuppressLint
import android.app.Application
import android.app.Instrumentation
import android.content.AttributionSource
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.ApplicationInfo
import android.os.Build

/**
 * The system context a process started from the shell has to use: services check that the caller's
 * package belongs to its uid, so the helper reports itself as the shell (uid 2000), not as Mewla.
 */
internal class ShellContext private constructor(base: Context) : ContextWrapper(base) {
    override fun getPackageName(): String = SHELL_PACKAGE
    override fun getOpPackageName(): String = SHELL_PACKAGE
    override fun getAttributionSource(): AttributionSource =
        AttributionSource.Builder(SHELL_UID).setPackageName(SHELL_PACKAGE).build()
    override fun getApplicationContext(): Context = this

    companion object {
        const val SHELL_PACKAGE = "com.android.shell"
        const val SHELL_UID = 2000

        /** Needs the main looper to exist: call after `Looper.prepareMainLooper()`. */
        @SuppressLint("PrivateApi", "DiscouragedPrivateApi")
        fun create(): Context {
            val activityThread = Class.forName("android.app.ActivityThread")
            val thread = activityThread.getMethod("currentActivityThread").invoke(null) ?: newActivityThread(activityThread)
            val system = activityThread.getDeclaredMethod("getSystemContext").invoke(thread) as Context
            val context = ShellContext(system)
            val initial = activityThread.getDeclaredField("mInitialApplication").apply { isAccessible = true }
            if (initial.get(thread) == null) {
                runCatching { initial.set(thread, Instrumentation.newApplication(Application::class.java, context)) }
            }
            return context
        }

        @SuppressLint("PrivateApi", "DiscouragedPrivateApi", "SoonBlockedPrivateApi")
        private fun newActivityThread(activityThread: Class<*>): Any {
            val thread = activityThread.getDeclaredConstructor().apply { isAccessible = true }.newInstance()
            activityThread.getDeclaredField("sCurrentActivityThread").apply { isAccessible = true }.set(null, thread)
            activityThread.getDeclaredField("mSystemThread").apply { isAccessible = true }.setBoolean(thread, true)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) runCatching {
                val controllerClass = Class.forName("android.app.ConfigurationController")
                val internal = Class.forName("android.app.ActivityThreadInternal")
                val controller = controllerClass.getDeclaredConstructor(internal).apply { isAccessible = true }.newInstance(thread)
                activityThread.getDeclaredField("mConfigurationController").apply { isAccessible = true }.set(thread, controller)
            }
            runCatching {
                val bindData = Class.forName("android.app.ActivityThread\$AppBindData")
                val data = bindData.getDeclaredConstructor().apply { isAccessible = true }.newInstance()
                bindData.getDeclaredField("appInfo").apply { isAccessible = true }
                    .set(data, ApplicationInfo().apply { packageName = SHELL_PACKAGE })
                activityThread.getDeclaredField("mBoundApplication").apply { isAccessible = true }.set(thread, data)
            }
            return thread
        }
    }
}
