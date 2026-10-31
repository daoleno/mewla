package expo.modules.mewlaphone.helper

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
}
