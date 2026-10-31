package expo.modules.mewlaphone.helper

/**
 * When the agent's display has settled after an action. Times are uptime milliseconds.
 *
 * The display only draws when something changes, so a screen that did not change draws nothing: the
 * action is then given [minWaitMs] to draw before the screen is called settled. A screen that did
 * draw is settled once it has been quiet for [quietMs]. An app that draws its own UI answers slowly
 * and in bursts (WeChat's search opens after a lull), so the caller asks for more quiet there.
 */
internal object SettlePolicy {
    fun settled(now: Long, actionAt: Long, lastFrameAt: Long, quietMs: Long, minWaitMs: Long): Boolean {
        val drew = lastFrameAt >= actionAt
        return if (drew) now - lastFrameAt >= quietMs else now - actionAt >= minWaitMs
    }

    /** Whether a frame was drawn after the action: when not, a picture of the display may be older than the screen. */
    fun drewSince(actionAt: Long, lastFrameAt: Long): Boolean = lastFrameAt >= actionAt
}
