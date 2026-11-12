package com.daoleno.mewla.phone

import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * What the phone knows about itself while its helper is not up. Plain values, so every way a normal
 * day interrupts Phone use can be played through [HelperKeeper] without a phone.
 */
data class Facts(
    val enabled: Boolean,
    val paired: Boolean,
    val pairing: Boolean,
    val wifi: Boolean,
    /** The system's Wireless debugging switch. */
    val wirelessOn: Boolean,
    /** Mewla holds WRITE_SECURE_SETTINGS, so it can switch Wireless debugging on without the person. */
    val mayEnableWireless: Boolean,
    /** Until when Mewla leaves the system's network prompt alone, after the person dismissed it twice; 0 for not at all. */
    val pausedUntil: Long = 0,
    val now: Long = 0,
)

/** What to do about a helper that is not up. */
sealed class Plan {
    /** Start it now. */
    object TryNow : Plan()

    /** Nothing can be done until [reason] ends; it ends by itself, so keep looking. */
    data class Wait(val reason: String) : Plan()

    /** Nothing Mewla does will help; [reason] is what the person must do, once. */
    data class Blocked(val reason: String) : Plan()
}

/** The decision, and the sentence that says it. The sentence goes to the screen, the daemon and the agent as it is. */
object SetupRecovery {
    fun plan(f: Facts, grantFailure: String? = null): Plan = when {
        !f.enabled -> Plan.Blocked("Phone use is switched off in Mewla on the phone.")
        !f.paired -> Plan.Blocked("This phone is not paired with its own Wireless debugging; open Mewla → Phone once to set it up.")
        f.pairing -> Plan.Wait("Setting up Phone use is in progress on the phone.")
        !f.wifi -> Plan.Wait("The phone is not on Wi-Fi, which Wireless debugging needs; Phone use comes back by itself when it is.")
        f.wirelessOn -> Plan.TryNow
        !f.mayEnableWireless -> Plan.Blocked(
            "Wireless debugging is off and Mewla has not been given the one-time permission to switch it back on" +
                (grantFailure?.let { " ($it)" } ?: "") + "; switch Wireless debugging on in Developer options once and Mewla gives itself the permission.",
        )
        f.now < f.pausedUntil -> Plan.Wait(
            "Wireless debugging keeps switching itself off on this Wi-Fi network (Android asks to allow it there); Mewla tries again at ${clock(f.pausedUntil)}.",
        )
        else -> Plan.TryNow
    }

    private fun clock(at: Long): String = SimpleDateFormat("HH:mm", Locale.US).format(Date(at))
}

/**
 * The network prompt rule. Switching Wireless debugging on at a Wi-Fi network Android has not been
 * told to allow raises a prompt, and if that is dismissed the switch falls back. Falling back twice
 * within [WINDOW_MS] of being switched on means this network is the person's to approve: leave it
 * alone for [PAUSE_MS] instead of nagging, and say so (see [SetupRecovery.plan]).
 */
class PromptGuard {
    private var strikes = 0
    private var switchedOnAt = 0L
    var pausedUntil = 0L
        private set

    /** Called with the switch found off just before Mewla switches it on; false when the pause says not to. */
    fun mayRaisePrompt(now: Long): Boolean {
        if (now < pausedUntil) return false
        strikes = if (switchedOnAt > 0 && now - switchedOnAt < WINDOW_MS) strikes + 1 else 0
        if (strikes >= 2) {
            pausedUntil = now + PAUSE_MS
            strikes = 0
            return false
        }
        switchedOnAt = now
        return true
    }

    /** The switch was found on: nothing is falling back. */
    fun switchHeld() {
        strikes = 0
    }

    /** The person asked for it themselves, so the pause does not apply to them. */
    fun personAsked() {
        strikes = 0
        pausedUntil = 0
    }

    companion object {
        const val WINDOW_MS = 90_000L
        const val PAUSE_MS = 5 * 60_000L
    }
}

/**
 * The loop that keeps the helper up, one pass at a time. Each [step] looks at the facts, does the one
 * thing that moves the phone toward ready, tells [Env] why when it is not, and says how long to wait
 * before the next pass (an event from the system ends the wait early).
 */
class HelperKeeper(private val env: Env) {
    interface Env {
        fun facts(): Facts
        fun helperUp(): Boolean
        /** Starts or adopts the helper; throws with a sentence for the person when it cannot. */
        fun startHelper()
        /** Gets the secure-settings grant through the phone's own Wireless debugging; the failure as a sentence, or null. */
        fun grant(): String?
        /** Why the phone is not ready, or null when it is. */
        fun report(reason: String?)
    }

    @Volatile private var backoff = FIRST_BACKOFF_MS
    private var lastFailure: String? = null
    private var grantFailure: String? = null
    private var nextGrantAt = 0L

    fun step(): Long {
        val facts = env.facts()
        if (env.helperUp()) {
            lastFailure = null
            backoff = FIRST_BACKOFF_MS
            env.report(null)
            // A helper that was already running when this install took over was never given the grant:
            // it is gotten while Wireless debugging is still on, since only then can it be.
            if (!facts.mayEnableWireless && facts.wirelessOn && facts.wifi && facts.now >= nextGrantAt) {
                grantFailure = env.grant()
                nextGrantAt = facts.now + if (grantFailure == null) 0 else GRANT_RETRY_MS
            }
            return IDLE_MS
        }
        if (facts.mayEnableWireless) grantFailure = null
        return when (val plan = SetupRecovery.plan(facts, grantFailure)) {
            is Plan.Blocked -> { env.report(plan.reason); IDLE_MS }
            is Plan.Wait -> { env.report(plan.reason); IDLE_MS }
            Plan.TryNow -> try {
                env.startHelper()
                lastFailure = null
                backoff = FIRST_BACKOFF_MS
                env.report(null)
                IDLE_MS
            } catch (e: Exception) {
                lastFailure = e.message ?: e.javaClass.simpleName
                env.report(lastFailure)
                backoff.also { backoff = (backoff * 2).coerceAtMost(MAX_BACKOFF_MS) }
            }
        }
    }

    /** The system said something changed (Wi-Fi, the Wireless debugging switch): the next pass is now and its wait starts over. */
    fun changed() {
        backoff = FIRST_BACKOFF_MS
    }

    companion object {
        const val IDLE_MS = 5_000L
        const val FIRST_BACKOFF_MS = 4_000L
        const val MAX_BACKOFF_MS = 30_000L
        const val GRANT_RETRY_MS = 10 * 60_000L
    }
}
