package expo.modules.mewlaphone

import android.content.Context
import org.json.JSONObject
import java.io.File
import java.security.SecureRandom

/**
 * Where the helper listens and the token that admits this app to it. Kept in the app's private
 * storage: the helper gets the token over the adb session that starts it, and nothing else on the
 * phone can read it.
 */
class HelperEndpoint(val port: Int, val token: String) {
    companion object {
        private val random = SecureRandom()

        private fun file(context: Context) = File(context.noBackupFilesDir, "phone/helper.json")

        fun load(context: Context): HelperEndpoint {
            val f = file(context)
            runCatching {
                val json = JSONObject(f.readText())
                return HelperEndpoint(json.getInt("port"), json.getString("token"))
            }
            return fresh(context)
        }

        /** A new port and token, for when the old port is taken or the helper must be told apart from an older one. */
        fun fresh(context: Context): HelperEndpoint {
            val bytes = ByteArray(24).also { random.nextBytes(it) }
            val endpoint = HelperEndpoint(PORT_MIN + random.nextInt(PORT_MAX - PORT_MIN), bytes.joinToString("") { "%02x".format(it) })
            val f = file(context)
            f.parentFile?.mkdirs()
            f.writeText(JSONObject().put("port", endpoint.port).put("token", endpoint.token).toString())
            return endpoint
        }

        private const val PORT_MIN = 40_000
        private const val PORT_MAX = 60_000
    }
}
