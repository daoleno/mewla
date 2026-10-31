package expo.modules.mewlaphone.helper

internal object TextCheck {
    /** Whether a field that now holds [actual] took the text that was meant to fill it. A field that cannot be read back did not. */
    fun landed(expected: String, actual: String?): Boolean = actual != null && actual == expected
}
