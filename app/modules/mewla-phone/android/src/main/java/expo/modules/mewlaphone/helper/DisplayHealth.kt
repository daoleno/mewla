package expo.modules.mewlaphone.helper

/** When the agent's display is stale enough that it is better made again than trusted. */
internal object DisplayHealth {
    /**
     * After an app was started on the display: a display the system dropped, or one that has never
     * drawn a single frame (an app that is merely already in front draws nothing new, so a display
     * that drew before is left alone), is replaced, once, and the app started there.
     */
    fun needsRecreate(alive: Boolean, drewSinceLaunch: Boolean, everDrew: Boolean, attempt: Int): Boolean =
        attempt == 0 && (!alive || (!drewSinceLaunch && !everDrew))
}
