package expo.modules.mewlaphone

import org.json.JSONObject

/** What a helper found running on this phone is to the App that finds it. */
internal enum class HelperKind {
    /** Started by this install of the App: use it. */
    CURRENT,

    /** Started by an earlier install but speaking the same protocol: it still works, and is replaced when a new one can be started. */
    STALE,

    /** Answers wrongly or speaks another protocol: of no use. */
    FOREIGN,
}

internal object HelperAdoption {
    fun classify(hello: JSONObject?, protocol: Int, build: Long): HelperKind = when {
        hello == null || hello.optInt("protocol") != protocol -> HelperKind.FOREIGN
        hello.optLong("build") == build -> HelperKind.CURRENT
        else -> HelperKind.STALE
    }
}
