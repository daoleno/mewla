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

/**
 * Text to type, cut in order into runs that go as key events and runs that go in as one paste each. Every paste
 * makes the phone tell its owner "pasted from your clipboard", so text is pasted as rarely as it allows: keys are
 * needed only where an app listens for keystrokes, a `#` or `@` that starts a word up to the first character the
 * keyboard lacks (the picker opens on the key, and its search sees what follows); everything between such runs
 * goes as keys when the keyboard has all of it, and otherwise as one paste.
 */
internal object TypingRuns {
    data class Run(val text: String, val keys: Boolean)

    /**
     * [keyed] says whether a character can go as key events. A line break is never a key: as Enter it would submit
     * a one-line field. A character outside the basic plane (an emoji) is never a key either.
     */
    fun split(text: String, keyed: (Char) -> Boolean): List<Run> {
        fun width(i: Int) = if (Character.isHighSurrogate(text[i]) && i + 1 < text.length) 2 else 1
        fun key(i: Int) = width(i) == 1 && text[i] != '\n' && text[i] != '\r' && keyed(text[i])
        fun startsWord(i: Int) = i == 0 || text[i - 1].isWhitespace() || !key(i - 1)
        val runs = mutableListOf<Run>()
        fun add(part: String, keys: Boolean) {
            if (part.isEmpty()) return
            val last = runs.lastOrNull()
            if (last != null && last.keys && keys) runs[runs.size - 1] = Run(last.text + part, true) else runs += Run(part, keys)
        }
        var spanStart = 0
        fun closeSpan(end: Int) {
            if (end <= spanStart) return
            var allKeys = true
            var k = spanStart
            while (k < end) { if (!key(k)) { allKeys = false; break }; k += width(k) }
            add(text.substring(spanStart, end), allKeys)
        }
        var i = 0
        while (i < text.length) {
            if ((text[i] == '#' || text[i] == '@') && key(i) && startsWord(i)) {
                closeSpan(i)
                var j = i
                while (j < text.length && key(j)) j++
                add(text.substring(i, j), true)
                spanStart = j
                i = j
            } else {
                i += width(i)
            }
        }
        closeSpan(text.length)
        return runs
    }
}
