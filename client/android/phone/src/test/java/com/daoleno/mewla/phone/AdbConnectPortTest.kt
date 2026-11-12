package com.daoleno.mewla.phone

import com.daoleno.mewla.phone.adb.AdbConnectPort
import com.daoleno.mewla.phone.adb.AdbConnectionException
import com.daoleno.mewla.phone.adb.AdbServiceFinder
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Right after Wireless debugging is switched on the system still announces the port it had before. */
class AdbConnectPortTest {
    private class Cache(var port: Int? = null) : com.daoleno.mewla.phone.adb.PortCache {
        override fun load() = port
        override fun save(port: Int) { this.port = port }
    }

    private fun refuse(port: Int): Nothing =
        throw AdbConnectionException(AdbConnectionException.Reason.CONNECT_FAILED, "cannot connect to 127.0.0.1:$port")

    @Test fun theRealPortThatFollowsAStaleOneIsFoundWhileItKeepsTrying() {
        var asked = 0
        val finder = AdbServiceFinder { _, _ -> if (++asked < 4) listOf(46251) else listOf(46251, 46109) }
        val tried = mutableListOf<Int>()
        val port = runBlocking {
            AdbConnectPort({ null }, finder, Cache(46251), keepTryingMs = 5_000, pollMs = 10).connect { p ->
                tried += p
                if (p == 46251) refuse(p) else p
            }
        }
        assertEquals(46109, port)
        assertEquals("a refused port is not tried again", listOf(46251, 46109), tried)
    }

    @Test fun withoutKeepTryingTheStalePortIsTheEnd() {
        val finder = AdbServiceFinder { _, _ -> listOf(46251) }
        val failure = runCatching {
            runBlocking { AdbConnectPort({ null }, finder, Cache(), keepTryingMs = 0).connect<Int> { refuse(it) } }
        }.exceptionOrNull()
        assertTrue(failure is AdbConnectionException)
    }

    @Test fun itGivesUpWhenTheTimeIsUp() {
        val finder = AdbServiceFinder { _, _ -> emptyList() }
        val started = System.currentTimeMillis()
        val failure = runCatching {
            runBlocking { AdbConnectPort({ null }, finder, Cache(), keepTryingMs = 200, pollMs = 20).connect<Int> { refuse(it) } }
        }.exceptionOrNull()
        assertTrue(failure is AdbConnectionException)
        assertTrue(System.currentTimeMillis() - started in 150..2_000)
    }

    @Test fun anUnauthorizedAnswerIsNotRetried() {
        val finder = AdbServiceFinder { _, _ -> listOf(1111, 2222) }
        var opens = 0
        val failure = runCatching {
            runBlocking {
                AdbConnectPort({ null }, finder, Cache(), keepTryingMs = 1_000, pollMs = 10).connect<Int> {
                    opens += 1
                    throw AdbConnectionException(AdbConnectionException.Reason.UNAUTHORIZED, "no")
                }
            }
        }.exceptionOrNull() as? AdbConnectionException
        assertEquals(AdbConnectionException.Reason.UNAUTHORIZED, failure?.reason)
        assertEquals(1, opens)
    }
}
