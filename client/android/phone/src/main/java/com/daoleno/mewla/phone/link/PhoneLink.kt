package com.daoleno.mewla.phone.link

import android.util.Log
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

/**
 * The phone's signed WebSocket to its one current computer (`/phone/device`). The computer sends a
 * request at a time; [Host.execute] runs it on a thread of its own and the result goes back on the
 * same socket. It lives in the foreground service's process, not in the app's JavaScript, so it
 * stays up and reconnects (1, 2, 4, 8, 15 s) with the app in the background, swiped away, or not
 * started since a reboot. Another phone taking the computer's Phone use (4001) or a computer that
 * does not know this protocol (4002) stops it: reconnecting would only repeat that.
 */
class PhoneLink(private val target: LinkTarget, private val host: Host) {
    /** What the link needs of Phone use. Called from the link's threads. */
    interface Host {
        /** The paired device key, or null when there is none yet. */
        fun identity(): DeviceIdentity?
        /** The hello frame: what this phone is and the state it is in now. */
        fun hello(): JSONObject
        /** The live status, sent as a state frame once the link is up. */
        fun status(): JSONObject
        /** Runs one request from the computer; returns its result frame, never throws. */
        fun execute(request: JSONObject): JSONObject
        /** connecting, online, offline, replaced or unsupported. */
        fun onState(state: String)
    }

    val linkTarget: LinkTarget get() = target

    private val lock = Any()
    private var socket: WebSocket? = null
    private var stopped = false
    private var attempt = 0
    private var timer: ScheduledFuture<*>? = null
    private var lastStateKey = ""
    @Volatile var state: String = "connecting"
        private set

    /** Signing reads the secure store: never on the caller's (often the main) thread. */
    fun start() {
        synchronized(lock) { stopped = false }
        scheduler.execute { open() }
    }

    fun stop() {
        val closing = synchronized(lock) {
            stopped = true
            timer?.cancel(false)
            timer = null
            socket.also { socket = null }
        }
        closing?.close(1000, null)
    }

    /** Skips the wait before the next try (the app came to the front, the network came back). */
    fun retryNow() {
        synchronized(lock) {
            if (stopped || socket != null) return
            timer?.cancel(false)
            timer = null
            attempt = 0
        }
        scheduler.execute { open() }
    }

    private fun open() {
        val authorization = try {
            val identity = host.identity() ?: throw IllegalStateException("this phone has no device key yet")
            DeviceAuth.header(target.daemonId, identity)
        } catch (e: Exception) {
            Log.w(TAG, "cannot sign the link: ${e.message}")
            retry()
            return
        }
        val request = try {
            Request.Builder().url(target.url).header("Authorization", authorization).build()
        } catch (e: IllegalArgumentException) {
            Log.w(TAG, "bad link address ${target.url}")
            retry()
            return
        }
        synchronized(lock) {
            if (stopped || socket != null) return
            setState("connecting")
            socket = client.newWebSocket(request, Listener())
        }
    }

    private inner class Listener : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            synchronized(lock) {
                if (socket !== webSocket) return
                attempt = 0
                lastStateKey = ""
            }
            webSocket.send(host.hello().toString())
            setState("online")
            relayStatus(host.status())
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            val frame = runCatching { JSONObject(text) }.getOrNull() ?: return
            if (frame.optString("type") != "request") return
            workers.execute {
                val result = runCatching { host.execute(frame) }.getOrElse {
                    PhoneFrames.failure(frame.optString("id"), "failed", it.message ?: "The phone could not do that")
                }
                if (synchronized(lock) { socket === webSocket }) webSocket.send(result.toString())
            }
        }

        override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
            webSocket.close(code, null)
            closed(webSocket, code)
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = closed(webSocket, code)

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            Log.i(TAG, "link failed: ${t.message}${response?.let { " (${it.code})" }.orEmpty()}")
            closed(webSocket, 1006)
        }
    }

    private fun closed(webSocket: WebSocket, code: Int) {
        val outcome = synchronized(lock) {
            if (socket !== webSocket) return
            socket = null
            if (stopped) return
            PhoneFrames.closeOutcome(code).also { if (it != "retry") stopped = true }
        }
        if (outcome == "retry") retry() else setState(outcome)
    }

    private fun retry() {
        synchronized(lock) {
            if (stopped) return
            val delay = LinkBackoff.delayMs(attempt)
            attempt += 1
            timer?.cancel(false)
            timer = scheduler.schedule({ open() }, delay, TimeUnit.MILLISECONDS)
        }
        setState("offline")
    }

    /** Sends a state frame when the status says something new to the computer. */
    fun relayStatus(status: JSONObject) {
        val key = PhoneFrames.stateKey(status)
        val target = synchronized(lock) {
            if (state != "online" || key == lastStateKey) return
            lastStateKey = key
            socket
        } ?: return
        target.send(PhoneFrames.state(status).toString())
    }

    /** Something the person did (stop, take_over, give_back, handed_off). Dropped while the link is down, as the app's link did. */
    fun send(frame: JSONObject) {
        val target = synchronized(lock) { socket.takeIf { state == "online" } } ?: return
        target.send(frame.toString())
    }

    private fun setState(next: String) {
        if (state == next) return
        state = next
        host.onState(next)
    }

    companion object {
        private const val TAG = "MewlaPhoneLink"

        /** Pings keep the connection honest through NATs and tunnels and tell a dead computer from a quiet one. */
        private val client: OkHttpClient by lazy {
            OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .pingInterval(30, TimeUnit.SECONDS)
                .build()
        }
        private val scheduler = Executors.newSingleThreadScheduledExecutor { Thread(it, "mewla-phone-link").apply { isDaemon = true } }
        /** A handoff waits up to ten minutes for the person: requests never wait on each other. */
        private val workers = Executors.newCachedThreadPool { Thread(it, "mewla-phone-request").apply { isDaemon = true } }
    }
}
