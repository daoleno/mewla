package expo.modules.mewlaphone.helper

import android.annotation.SuppressLint
import android.content.Context
import android.app.ActivityManager
import android.content.Intent
import android.os.PowerManager
import android.os.Build
import android.os.Looper
import android.util.Base64
import android.util.Log
import android.view.KeyEvent
import org.json.JSONArray
import org.json.JSONObject
import java.io.DataInputStream
import java.io.DataOutputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.security.MessageDigest
import java.util.concurrent.TimeUnit
import kotlin.system.exitProcess

/**
 * The part of phone use that needs the shell's privileges. Mewla starts it through the phone's own
 * Wireless debugging (`CLASSPATH=<apk> app_process … PhoneHelperMain <package> <port>`, with a
 * token on its standard input); it then serves one app on a loopback port, and keeps running after
 * the app is gone, so a task survives the app being swiped away.
 *
 * Only the app may talk to it: SELinux keeps an app from reaching a shell-owned unix socket, so the
 * channel is loopback TCP and the first message must carry the token only the app holds. The helper
 * makes a virtual display on request and acts only on that display, never on the person's own screen.
 */
internal class PhoneHelper(
    private val context: Context,
    private val packageName: String,
    private val port: Int,
    private val token: ByteArray,
) {
    private val injector by lazy { Injector() }
    private val reader by lazy { ScreenReader() }
    @Volatile private var screen: AgentScreen? = null
    @Volatile private var client: Client? = null
    private val actionLock = Any()

    private class Client(val socket: Socket, val out: DataOutputStream)

    fun serve() {
        val server = try {
            ServerSocket(port, 4, InetAddress.getByName("127.0.0.1"))
        } catch (e: Exception) {
            Log.e(TAG, "cannot listen on $port: ${e.message}")
            exitProcess(3)
        }
        Log.i(TAG, "serving 127.0.0.1:$port for $packageName")
        Thread({ watchOrphans() }, "mewla-helper-watchdog").apply { isDaemon = true }.start()
        while (true) {
            val socket = try {
                server.accept()
            } catch (e: Exception) {
                Log.w(TAG, "accept failed", e)
                exitProcess(1)
            }
            Thread({ session(socket) }, "mewla-helper-session").apply { isDaemon = true }.start()
        }
    }

    /** A display nobody has asked about for a while is the app's leftover: free it. */
    private fun watchOrphans() {
        var orphanedSince = 0L
        while (true) {
            Thread.sleep(30_000)
            val now = System.currentTimeMillis()
            if (client != null || screen == null) {
                orphanedSince = 0L
            } else if (orphanedSince == 0L) {
                orphanedSince = now
            } else if (now - orphanedSince >= ORPHAN_DISPLAY_MS) {
                synchronized(actionLock) { screen?.release(); screen = null }
                orphanedSince = 0L
            }
        }
    }

    private fun session(socket: Socket) {
        try {
            val input = DataInputStream(socket.getInputStream().buffered(64 * 1024))
            if (!admit(socket, input)) {
                socket.close()
                return
            }
            val out = DataOutputStream(socket.getOutputStream().buffered(64 * 1024))
            val me = Client(socket, out)
            // A newer session replaces the older one: the app only ever holds one.
            client?.let { runCatching { it.socket.close() } }
            client = me
            Wire.write(out, Wire.REPLY, JSONObject().put("ok", true).put("id", 0).toString())
            while (true) {
                val message = Wire.read(input) ?: break
                if (message.kind != Wire.REQUEST) continue
                val request = JSONObject(message.text())
                val reply = handle(request)
                reply.put("id", request.opt("id"))
                Wire.write(out, Wire.REPLY, reply.toString())
            }
        } catch (e: Exception) {
            Log.i(TAG, "session ended: ${e.message}")
        } finally {
            if (client?.socket === socket) {
                client = null
                // The app is gone: nobody is watching, so stop spending the phone's battery on frames.
                screen?.watch(1, 120, 20, null)
            }
            runCatching { socket.close() }
        }
    }

    /** Only a peer that opens with the token. A wrong token ends the connection after a pause, so guessing is slow. */
    private fun admit(socket: Socket, input: DataInputStream): Boolean {
        socket.soTimeout = AUTH_TIMEOUT_MS
        val first = try {
            Wire.read(input)
        } catch (_: Exception) {
            null
        }
        socket.soTimeout = 0
        if (first == null || first.kind != Wire.AUTH || !MessageDigest.isEqual(first.payload, token)) {
            Log.w(TAG, "refused a connection without the token")
            Thread.sleep(BAD_TOKEN_DELAY_MS)
            return false
        }
        return true
    }

    private fun handle(request: JSONObject): JSONObject = try {
        synchronized(actionLock) { dispatch(request.getString("op"), request) }
    } catch (e: Exception) {
        Log.w(TAG, "${request.optString("op")} failed", e)
        fail(e.message ?: e.javaClass.simpleName)
    }

    private fun dispatch(op: String, r: JSONObject): JSONObject = when (op) {
        "hello" -> ok()
            .put("protocol", PROTOCOL).put("sdk", Build.VERSION.SDK_INT).put("model", Build.MODEL)
            .put("pid", android.os.Process.myPid()).put("display", screen?.displayId ?: -1)
            .put("w", screen?.width ?: 0).put("h", screen?.height ?: 0)
        "display" -> display(r.optInt("w", 720), r.optInt("h", 1600), r.optInt("dpi", 280))
        "display_release" -> { screen?.release(); screen = null; ok() }
        "watch" -> {
            val on = r.optBoolean("on", true)
            val s = screen ?: return fail("no display")
            val c = client
            if (on && c != null) {
                s.watch(r.optInt("fps", 12), r.optInt("maxWidth", 540), r.optInt("quality", 55)) { jpeg ->
                    runCatching { Wire.write(c.out, Wire.FRAME, jpeg) }
                }
            } else {
                s.watch(1, 120, 20, null)
            }
            ok()
        }
        "screenshot" -> {
            val s = screen ?: return fail("no display")
            val jpeg = s.jpeg(r.optInt("maxWidth", 720), r.optInt("quality", 70)) ?: return fail("no frame yet")
            ok().put("jpeg", Base64.encodeToString(jpeg, Base64.NO_WRAP)).put("w", s.width).put("h", s.height)
        }
        "tree" -> {
            val s = screen ?: return fail("no display")
            // The tree is only reported while the phone's own screen is on.
            reader.read(s.displayId).put("ok", true).put("w", s.width).put("h", s.height).put("interactive", interactive())
        }
        "settle" -> {
            val s = screen ?: return fail("no display")
            ok().put("settled", s.settle(r.optLong("quiet_ms", 300), r.optLong("timeout_ms", 2_500)))
        }
        "tap" -> {
            val s = screen ?: return fail("no display")
            val hold = if (r.optBoolean("long", false)) LONG_PRESS_MS else 50L
            if (injector.tap(s.displayId, r.getDouble("x").toFloat(), r.getDouble("y").toFloat(), hold)) ok() else fail("tap was not delivered")
        }
        "touch" -> {
            val s = screen ?: return fail("no display")
            if (injector.touch(s.displayId, r.getString("phase"), r.getDouble("x").toFloat(), r.getDouble("y").toFloat())) ok() else fail("touch was not delivered")
        }
        "swipe" -> {
            val s = screen ?: return fail("no display")
            val done = injector.swipe(
                s.displayId, r.getDouble("x1").toFloat(), r.getDouble("y1").toFloat(),
                r.getDouble("x2").toFloat(), r.getDouble("y2").toFloat(), r.optLong("ms", 300),
            )
            if (done) ok() else fail("swipe was not delivered")
        }
        "key" -> {
            val s = screen ?: return fail("no display")
            val code = keyCode(r.getString("name")) ?: return fail("unknown key")
            if (injector.key(s.displayId, code)) ok() else fail("key was not delivered")
        }
        "text" -> typeText(r.getString("value"), r.optBoolean("replace", false))
        "apps" -> apps()
        "launch" -> launch(r.getString("package"))
        "shutdown" -> {
            screen?.release()
            Thread { Thread.sleep(100); exitProcess(0) }.start()
            ok()
        }
        else -> fail("unknown op $op")
    }

    private fun display(w: Int, h: Int, dpi: Int): JSONObject {
        screen?.let { existing ->
            if (existing.width == w && existing.height == h) return ok().put("display", existing.displayId).put("w", w).put("h", h)
            existing.release()
            screen = null
        }
        val created = AgentScreen(context, w, h, dpi)
        screen = created
        setImePolicy(created.displayId)
        return ok().put("display", created.displayId).put("w", w).put("h", h)
    }

    /** Keyboards for a field on the agent's display appear there, not on the person's screen. */
    private fun setImePolicy(displayId: Int) {
        runCatching {
            val wm = Class.forName("android.view.WindowManagerGlobal").getMethod("getWindowManagerService").invoke(null)!!
            wm.javaClass.getMethod("setDisplayImePolicy", Int::class.javaPrimitiveType, Int::class.javaPrimitiveType).invoke(wm, displayId, 0)
        }.onFailure { Log.w(TAG, "setDisplayImePolicy failed", it) }
    }

    private fun typeText(value: String, replace: Boolean): JSONObject {
        val s = screen ?: return fail("no display")
        val current = reader.focusedText(s.displayId) ?: return fail("no field has focus")
        val next = if (replace) value else current + value
        return if (reader.setFocusedText(s.displayId, next)) ok() else fail("the field refused the text")
    }

    private fun apps(): JSONObject {
        val pm = context.packageManager
        val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val list = JSONArray()
        for (info in pm.queryIntentActivities(intent, 0)) {
            list.put(JSONObject().put("package", info.activityInfo.packageName).put("label", info.loadLabel(pm).toString()))
        }
        return ok().put("apps", list)
    }

    private fun launch(pkg: String): JSONObject {
        val s = screen ?: return fail("no display")
        val intent = context.packageManager.getLaunchIntentForPackage(pkg) ?: return fail("not installed").put("code", "not_installed")
        // Starting an app on the agent's display moves its task there, so an app the person is
        // looking at on their own screen is never launched.
        if (visibleOnMainScreen(pkg)) return fail("the person is using this app on their own screen").put("code", "in_use_on_main")
        val component = intent.component?.flattenToShortString() ?: return fail("no launcher activity")
        val process = ProcessBuilder("am", "start", "--display", s.displayId.toString(), "-n", component, "-f", "0x10000000")
            .redirectErrorStream(true).start()
        val output = process.inputStream.bufferedReader().readText()
        if (!process.waitFor(15, TimeUnit.SECONDS)) {
            process.destroyForcibly()
            return fail("starting $pkg timed out")
        }
        return if (output.contains("Error", ignoreCase = true)) fail(output.trim().take(200)) else ok().put("component", component)
    }

    private fun interactive(): Boolean = context.getSystemService(PowerManager::class.java)?.isInteractive ?: true

    /** Whether [pkg] has a task showing on the person's own display. */
    @SuppressLint("PrivateApi")
    private fun visibleOnMainScreen(pkg: String): Boolean = try {
        val service = Class.forName("android.app.ActivityTaskManager").getMethod("getService").invoke(null)!!
        val tasks = service.javaClass.getMethod("getTasks", Int::class.javaPrimitiveType).invoke(service, 100) as List<*>
        tasks.any { task ->
            val info = task as ActivityManager.RunningTaskInfo
            val display = info.javaClass.getField("displayId").getInt(info)
            val visible = runCatching { info.javaClass.getField("isVisible").getBoolean(info) }.getOrDefault(true)
            val owner = (info.topActivity ?: info.baseActivity)?.packageName
            display == 0 && visible && owner == pkg
        }
    } catch (e: Exception) {
        // Not being able to tell is a reason not to take the person's app.
        Log.w(TAG, "cannot inspect tasks", e)
        true
    }

    private fun keyCode(name: String): Int? = when (name.lowercase()) {
        "back" -> KeyEvent.KEYCODE_BACK
        "home" -> KeyEvent.KEYCODE_HOME
        "recents" -> KeyEvent.KEYCODE_APP_SWITCH
        "enter" -> KeyEvent.KEYCODE_ENTER
        "delete" -> KeyEvent.KEYCODE_DEL
        "tab" -> KeyEvent.KEYCODE_TAB
        "search" -> KeyEvent.KEYCODE_SEARCH
        else -> null
    }

    private fun ok() = JSONObject().put("ok", true)
    private fun fail(message: String) = JSONObject().put("ok", false).put("error", message)

    companion object {
        private const val TAG = "MewlaPhoneHelper"
        private const val LONG_PRESS_MS = 700L
        /** Bumped when the app and helper stop understanding each other; the app restarts a helper that differs. */
        const val PROTOCOL = 1
        private const val AUTH_TIMEOUT_MS = 5_000
        private const val ORPHAN_DISPLAY_MS = 5 * 60 * 1000L
        private const val BAD_TOKEN_DELAY_MS = 1_000L

        fun sha256(bytes: ByteArray): String =
            MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

        @JvmStatic
        fun main(args: Array<String>) {
            if (args.size < 2) {
                Log.e(TAG, "usage: PhoneHelperMain <package> <port> (token on stdin)")
                exitProcess(2)
            }
            val port = args[1].toIntOrNull() ?: exitProcess(2)
            // The token comes on the first line of standard input, which closes right after.
            val token = runCatching { System.`in`.bufferedReader().readLine()?.trim() }.getOrNull()
            runCatching { System.`in`.close() }
            if (token.isNullOrEmpty()) {
                Log.e(TAG, "no token on stdin")
                exitProcess(2)
            }
            // The shell's own context needs a main looper to exist.
            Looper.prepareMainLooper()
            val context = ShellContext.create()
            val helper = PhoneHelper(context, args[0], port, token.toByteArray(Charsets.UTF_8))
            Thread({ helper.serve() }, "mewla-helper-accept").start()
            Looper.loop()
        }
    }
}
