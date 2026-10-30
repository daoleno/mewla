package expo.modules.mewlaphone

/** How long the phone waits before it starts another background attempt at reaching the computer. */
internal object LinkRetry {
    private val STEPS_MS = longArrayOf(10_000, 20_000, 40_000, 80_000, 160_000, 300_000)

    /** [attempt] is how many have been made since the link was last up. */
    fun delayMs(attempt: Int): Long = STEPS_MS[attempt.coerceIn(0, STEPS_MS.size - 1)]
}
