package com.daoleno.mewla.phone.helper

internal object TextCheck {
    /** Whether a field that now holds [actual] took the text that was meant to fill it. A field that cannot be read back did not. */
    fun landed(expected: String, actual: String?): Boolean = actual != null && actual == expected

    /**
     * What an empty-looking field holds: nothing when it shows its hint. A field says so with the
     * showing-hint flag, but some apps (Telegram) put the hint in the field's text and leave the flag
     * off; a text equal to the hint is then the hint, not something typed.
     */
    fun existing(text: String?, hint: String?, showingHint: Boolean): String {
        val value = text ?: ""
        if (showingHint) return ""
        if (!hint.isNullOrEmpty() && value == hint) return ""
        return value
    }

    /** Whether [actual] is [before] with [value] put in at one place: what typing at the cursor leaves. */
    fun insertedAt(before: String, value: String, actual: String?): Boolean {
        if (actual == null || actual.length != before.length + value.length) return false
        var start = 0
        while (start <= before.length) {
            val at = actual.indexOf(value, start)
            if (at < 0 || at > before.length) return false
            if (actual.regionMatches(0, before, 0, at) && actual.regionMatches(at + value.length, before, at, before.length - at)) return true
            start = at + 1
        }
        return false
    }
}

/** Text to type, cut in order into runs a keyboard has keys for and runs that go in as a paste. */
internal object TypingRuns {
    data class Run(val text: String, val keys: Boolean)

    /**
     * [keyed] says whether a character can go as key events. A newline is always pasted: as Enter it would
     * submit a one-line field. A character outside the basic plane (an emoji) is pasted whole.
     */
    fun split(text: String, keyed: (Char) -> Boolean): List<Run> {
        val runs = mutableListOf<Run>()
        val run = StringBuilder()
        var runKeys = false
        var i = 0
        while (i < text.length) {
            val c = text[i]
            val width = if (Character.isHighSurrogate(c) && i + 1 < text.length) 2 else 1
            val keys = width == 1 && c != '\n' && c != '\r' && keyed(c)
            if (run.isNotEmpty() && keys != runKeys) {
                runs += Run(run.toString(), runKeys)
                run.setLength(0)
            }
            runKeys = keys
            run.append(text, i, i + width)
            i += width
        }
        if (run.isNotEmpty()) runs += Run(run.toString(), runKeys)
        return runs
    }
}
