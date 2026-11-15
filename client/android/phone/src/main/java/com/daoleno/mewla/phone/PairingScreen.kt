package com.daoleno.mewla.phone

/**
 * What the pairing reader and the guided walk decide from a screen's facts, kept apart from the
 * accessibility nodes so it can be tested: the six digits and the address shown, and whether the
 * walk may act. The pairing dialog lives only while it is in front: anything the walk does over it
 * (a tap, a scroll, opening Settings again) closes it and throws its code away.
 */
object PairingScreen {
    /** One node as the reader sees it: its text, its description and its view id. */
    class Node(val text: String?, val description: String? = null, val viewId: String? = null)

    /** Where the walk is: Settings, the system UI (the shade, a heads-up, the lock screen, a prompt) or anything else. */
    enum class Front { Settings, SystemUi, Other }

    enum class Move {
        /** Leave the screen alone. */
        Hold,
        /** Tap or scroll on Settings. */
        Act,
        /** Bring the right Settings screen up. */
        Open,
    }

    /** After the walk taps Pair device with pairing code, it waits this long for the dialog before tapping again. */
    const val TAP_HOLD_MS = 120_000L

    private val SIX = Regex("^\\d{6}$")
    private val ADDRESS = Regex("((?:\\d{1,3}\\.){3}\\d{1,3}):(\\d{2,5})")
    private val SPACED = Regex("^\\d(?:[ \\u00a0\\u2009\\u202f]?\\d){5}$")

    /** The six digits of a pairing code in one piece of text ("123456", "123 456", "1 2 3 4 5 6"); null for anything else. */
    fun code(text: String?): String? {
        val t = text?.trim() ?: return null
        if (!SPACED.matches(t)) return null
        return t.filter { it.isDigit() }.takeIf { SIX.matches(it) }
    }

    /** The code on screen: the node AOSP names pairing_code first, then any text or description that is six digits. */
    fun readCode(nodes: List<Node>): String? {
        nodes.firstOrNull { it.viewId?.endsWith(":id/pairing_code") == true }?.let { node ->
            (code(node.text) ?: code(node.description))?.let { return it }
        }
        return nodes.firstNotNullOfOrNull { code(it.text) ?: code(it.description) }
    }

    /** The first "host:port" on screen. */
    fun readAddress(nodes: List<Node>): Pair<String, Int>? {
        for (node in nodes) {
            for (t in listOfNotNull(node.text, node.description)) {
                val m = ADDRESS.find(t) ?: continue
                val port = m.groupValues[2].toIntOrNull() ?: continue
                return m.groupValues[1] to port
            }
        }
        return null
    }

    /**
     * The walk's move on the pair step. Over the system UI it waits (the person may be typing the
     * code into the notification, with the dialog still open underneath). On Settings it never acts
     * while a dialog is up or a code is on screen, and after tapping Pair device with pairing code
     * it waits for the dialog while that row is gone, however the dialog's text reads.
     */
    fun pairStep(
        front: Front,
        dialogUp: Boolean,
        codeOnScreen: Boolean,
        pairRowVisible: Boolean,
        msSincePairTap: Long?,
    ): Move = when (front) {
        Front.SystemUi -> Move.Hold
        Front.Other -> Move.Open
        Front.Settings -> when {
            dialogUp || codeOnScreen -> Move.Hold
            msSincePairTap != null && msSincePairTap < TAP_HOLD_MS && !pairRowVisible -> Move.Hold
            else -> Move.Act
        }
    }

    /** A window-state change from Settings opens a dialog (its class names one) or a page (anything else). */
    fun isDialog(className: String?): Boolean = className?.contains("Dialog", ignoreCase = true) == true ||
        className?.contains("BottomSheet", ignoreCase = true) == true
}
