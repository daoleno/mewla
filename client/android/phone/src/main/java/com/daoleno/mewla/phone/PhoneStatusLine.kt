package com.daoleno.mewla.phone

/**
 * What the ongoing Phone use notification says, from the phone's real state alone: the switch being
 * on says only that the service runs, so "on" is claimed only once the helper is really up.
 */
internal data class PhoneStatusLine(val title: String, val text: String? = null, val buttons: Buttons = Buttons.None) {
    enum class Buttons { None, Resume, TakeOverAndStop }

    companion object {
        /** [state] is [PhoneController.state]; [agent] is the Worker using the phone, if any. */
        fun of(linkOnline: Boolean, state: String, paired: Boolean, agent: String?): PhoneStatusLine = when {
            // The App's process is alive for the helper but its connection to the computer is not.
            !linkOnline -> PhoneStatusLine("Open Mewla to reconnect")
            state == "stopped" -> PhoneStatusLine("Stopped", buttons = Buttons.Resume)
            agent != null -> PhoneStatusLine("$agent is using your phone", buttons = Buttons.TakeOverAndStop)
            // The helper is not running: tapping opens the Phone page, where the one missing step is.
            state == "setup" && !paired -> PhoneStatusLine("Finish setting up Phone use", "Tap to continue")
            state == "setup" -> PhoneStatusLine("Phone use isn't ready", "Tap to fix")
            else -> PhoneStatusLine("Phone use is on")
        }
    }
}
