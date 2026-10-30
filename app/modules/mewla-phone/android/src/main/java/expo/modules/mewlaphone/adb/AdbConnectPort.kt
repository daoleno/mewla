// Adapted from Mika (https://github.com/wangzan101/mika), Apache License 2.0. See NOTICE.
package expo.modules.mewlaphone.adb

import kotlinx.coroutines.CancellationException

interface PortCache {
    fun load(): Int?
    fun save(port: Int)
}

class AdbConnectPort(
    private val property: () -> Int?,
    private val finder: AdbServiceFinder,
    private val cache: PortCache,
    private val mdnsTimeoutMs: Long = 3_000,
) {
    suspend fun <T> connect(open: (Int) -> T): T {
        val tried = HashSet<Int>()
        var last: AdbConnectionException? = null
        val sources: List<suspend () -> List<Int>> = listOf(
            { listOfNotNull(property()) },
            { finder.findPorts(AdbServiceTypes.CONNECT, mdnsTimeoutMs) },
            { listOfNotNull(cache.load()) },
        )
        for (source in sources) {
            val ports = try {
                source()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                emptyList()
            }
            for (port in ports) {
                if (port !in 1..65535 || !tried.add(port)) continue
                try {
                    return open(port).also { cache.save(port) }
                } catch (e: AdbConnectionException) {
                    if (e.reason == AdbConnectionException.Reason.UNAUTHORIZED) throw e
                    last = e
                }
            }
        }
        throw last ?: AdbConnectionException(AdbConnectionException.Reason.CONNECT_FAILED, "no wireless debugging port found")
    }
}
