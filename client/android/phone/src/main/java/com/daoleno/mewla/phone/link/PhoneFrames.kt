package com.daoleno.mewla.phone.link

import org.json.JSONObject

/**
 * The frames on the phone's WebSocket to its computer (/phone/device). Mirrors
 * daemon/phone/protocol.go: change them together.
 */
object PhoneFrames {
    const val VERSION = 1

    // The daemon's close codes.
    const val CLOSE_REPLACED = 4001
    const val CLOSE_UNSUPPORTED = 4002
    const val CLOSE_PROTOCOL = 4003

    /** The first frame, within 10 s of connecting. */
    fun hello(model: String, sdk: Int, appVersion: String, state: String): JSONObject =
        JSONObject().put("type", "hello").put("version", VERSION).put("model", model).put("sdk", sdk)
            .put("app_version", appVersion).put("state", state)

    /** The state frame a status is sent as: `w`/`h` only once the display exists, `problem` only while in setup. */
    fun state(status: JSONObject): JSONObject {
        val frame = JSONObject().put("type", "state").put("state", status.optString("state", "setup"))
        status.optInt("w").takeIf { it > 0 }?.let { frame.put("w", it) }
        status.optInt("h").takeIf { it > 0 }?.let { frame.put("h", it) }
        problem(status)?.let { frame.put("problem", it) }
        text(status, "display_info")?.let { frame.put("display", it) }
        return frame
    }

    /** What makes two statuses the same to the computer: a state frame goes out only when it changes. */
    fun stateKey(status: JSONObject): String =
        "${status.optString("state")}:${status.optInt("w")}:${status.optInt("h")}:${problem(status).orEmpty()}:${text(status, "display_info").orEmpty()}"

    fun event(kind: String, id: String? = null): JSONObject =
        JSONObject().put("type", "event").put("kind", kind).apply { id?.let { put("id", it) } }

    fun failure(id: String, code: String, error: String): JSONObject =
        JSONObject().put("type", "result").put("id", id).put("ok", false).put("code", code).put("error", error)

    /** What a close from the computer means: retry, or stop because retrying would only repeat it. */
    fun closeOutcome(code: Int): String = when (code) {
        CLOSE_REPLACED -> "replaced"
        CLOSE_UNSUPPORTED -> "unsupported"
        else -> "retry"
    }

    private fun problem(status: JSONObject): String? = if (status.optString("state") == "setup") text(status, "problem") else null

    private fun text(status: JSONObject, name: String): String? =
        if (status.isNull(name)) null else status.optString(name).takeIf { it.isNotEmpty() }
}

/** How long the link waits before it tries the computer again: 1, 2, 4, 8, then every 15 s. */
object LinkBackoff {
    private val STEPS_MS = longArrayOf(1_000, 2_000, 4_000, 8_000, 15_000)

    /** [attempt] is how many tries failed since the link was last up. */
    fun delayMs(attempt: Int): Long = STEPS_MS[attempt.coerceIn(0, STEPS_MS.size - 1)]
}
