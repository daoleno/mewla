package expo.modules.mewlaphone.helper

import android.app.UiAutomation
import android.graphics.Rect
import android.os.Bundle
import android.os.HandlerThread
import android.os.Looper
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo
import org.json.JSONArray
import org.json.JSONObject

/**
 * Reads the accessibility tree of one display through UiAutomation, the shell's own accessibility
 * connection: nothing for the person to switch on, and other accessibility services keep running.
 */
internal class ScreenReader {
    private val automation: UiAutomation by lazy { connect() }

    private fun connect(): UiAutomation {
        val looper: Looper = HandlerThread("mewla-uiautomation").apply { start() }.looper
        val connection = Class.forName("android.app.UiAutomationConnection").getDeclaredConstructor().newInstance()
        val constructor = UiAutomation::class.java.getDeclaredConstructor(Looper::class.java, Class.forName("android.app.IUiAutomationConnection"))
            .apply { isAccessible = true }
        val automation = constructor.newInstance(looper, connection)
        UiAutomation::class.java.getDeclaredMethod("connect", Int::class.javaPrimitiveType)
            .apply { isAccessible = true }.invoke(automation, FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES)
        val info = automation.serviceInfo
        info.flags = info.flags or android.accessibilityservice.AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS or
            android.accessibilityservice.AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS or
            android.accessibilityservice.AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS
        automation.serviceInfo = info
        return automation
    }

    private fun windows(displayId: Int): List<AccessibilityWindowInfo> =
        automation.windowsOnAllDisplays.get(displayId) ?: emptyList()

    /** The windows on [displayId], top first, each with its visible, meaningful nodes in tree order. */
    fun read(displayId: Int): JSONObject {
        val out = JSONArray()
        for (window in windows(displayId)) {
            val root = rootOf(window) ?: continue
            val box = Rect().also { window.getBoundsInScreen(it) }
            val nodes = JSONArray()
            try {
                walk(root, 0, nodes, box, lenient = false)
                // An app can report none of its views as visible to the user while they are drawn (WeChat on a
                // virtual display): read the same tree again, trusting each node's bounds instead.
                if (nodes.length() == 0 && window.type == AccessibilityWindowInfo.TYPE_APPLICATION) {
                    walk(root, 0, nodes, box, lenient = true)
                }
            } finally {
                root.recycleCompat()
            }
            out.put(
                JSONObject()
                    .put("id", window.id)
                    .put("type", window.type)
                    .put("layer", window.layer)
                    .put("active", window.isActive)
                    .put("focused", window.isFocused)
                    .put("pkg", root.packageName?.toString() ?: "")
                    .put("title", window.title?.toString() ?: "")
                    .put("bounds", JSONArray(listOf(box.left, box.top, box.right, box.bottom)))
                    .put("nodes", nodes),
            )
        }
        return JSONObject().put("windows", out)
    }

    /** A window's root; an app in the middle of an animation can answer none for a moment, so ask again. */
    private fun rootOf(window: AccessibilityWindowInfo): AccessibilityNodeInfo? {
        for (attempt in 0 until ROOT_TRIES) {
            window.root?.let { return it }
            if (attempt < ROOT_TRIES - 1) Thread.sleep(ROOT_RETRY_MS)
        }
        return null
    }

    /**
     * Adds the meaningful nodes below [node]. Strictly, a node the app calls invisible hides its whole
     * subtree. Leniently only a node with nothing on [window] hides its subtree: bounds off the
     * window are scrolled away, an empty rectangle is not drawn.
     */
    private fun walk(node: AccessibilityNodeInfo, depth: Int, out: JSONArray, window: Rect, lenient: Boolean) {
        if (out.length() >= MAX_NODES || depth > MAX_DEPTH) return
        if (lenient) {
            val box = Rect().also { node.getBoundsInScreen(it) }
            if (box.isEmpty || !Rect.intersects(box, window)) return
        } else if (!node.isVisibleToUser) {
            return
        }
        if (keep(node)) out.put(describe(node, depth))
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            try {
                walk(child, depth + 1, out, window, lenient)
            } finally {
                child.recycleCompat()
            }
        }
    }

    /** Containers with nothing to say are skipped; their children keep their place through depth. */
    private fun keep(node: AccessibilityNodeInfo): Boolean =
        !node.text.isNullOrEmpty() || !node.contentDescription.isNullOrEmpty() || !node.hintText.isNullOrEmpty() ||
            !node.stateDescription.isNullOrEmpty() || !node.tooltipText.isNullOrEmpty() ||
            node.isClickable || node.isLongClickable || node.isEditable || node.isScrollable || node.isCheckable

    private fun describe(node: AccessibilityNodeInfo, depth: Int): JSONObject {
        val box = Rect().also { node.getBoundsInScreen(it) }
        var flags = 0
        if (node.isClickable) flags = flags or CLICKABLE
        if (node.isLongClickable) flags = flags or LONG_CLICKABLE
        if (node.isEditable) flags = flags or EDITABLE
        if (node.isScrollable) flags = flags or SCROLLABLE
        if (node.isCheckable) flags = flags or CHECKABLE
        if (node.isChecked) flags = flags or CHECKED
        if (node.isEnabled) flags = flags or ENABLED
        if (node.isFocused) flags = flags or FOCUSED
        if (node.isSelected) flags = flags or SELECTED
        if (node.isPassword) flags = flags or PASSWORD
        val obj = JSONObject()
            .put("d", depth)
            .put("c", (node.className?.toString() ?: "").substringAfterLast('.'))
            .put("b", JSONArray(listOf(box.left, box.top, box.right, box.bottom)))
            .put("f", flags)
        node.viewIdResourceName?.let { obj.put("r", it.substringAfter(":id/")) }
        // A password's text is never read.
        if (!node.isPassword) node.text?.let { obj.put("t", it.toString().take(MAX_TEXT)) }
        // What a view says about itself in any of its words: description, else its state, else its tooltip.
        val said = node.contentDescription?.takeIf { it.isNotEmpty() } ?: node.stateDescription?.takeIf { it.isNotEmpty() }
            ?: node.tooltipText?.takeIf { it.isNotEmpty() }
        said?.let { obj.put("x", it.toString().take(MAX_TEXT)) }
        node.hintText?.let { obj.put("h", it.toString().take(MAX_TEXT)) }
        return obj
    }

    /** The focused input field on [displayId], or null. Some apps report their field only as a focused editable node. */
    fun focusedInput(displayId: Int): AccessibilityNodeInfo? {
        val roots = windows(displayId).mapNotNull { it.root }
        for (root in roots) root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)?.let { return it }
        for (root in roots) editableFocused(root, 0)?.let { return it }
        return null
    }

    private fun editableFocused(node: AccessibilityNodeInfo, depth: Int): AccessibilityNodeInfo? {
        if (depth > MAX_DEPTH) return null
        if (node.isEditable && node.isFocused) return node
        for (i in 0 until node.childCount) {
            val child = node.getChild(i) ?: continue
            editableFocused(child, depth + 1)?.let { return it }
        }
        return null
    }

    /** Sets the focused field's text; false when no field has focus or it refuses. */
    fun setFocusedText(displayId: Int, text: String): Boolean {
        val field = focusedInput(displayId) ?: return false
        val args = Bundle().apply { putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text) }
        return field.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)
    }

    /** What is in the focused field now. */
    class Field(val text: String, val password: Boolean)

    /** The focused field's current text (empty while it shows only its hint), or null with no field focused. */
    fun focusedField(displayId: Int): Field? {
        val field = focusedInput(displayId) ?: return null
        val text = if (field.isShowingHintText) "" else field.text?.toString() ?: ""
        return Field(text, field.isPassword)
    }

    private fun AccessibilityNodeInfo.recycleCompat() {
        @Suppress("DEPRECATION")
        runCatching { recycle() }
    }

    companion object {
        const val FLAG_DONT_SUPPRESS_ACCESSIBILITY_SERVICES = 1
        const val MAX_DEPTH = 80
        const val ROOT_TRIES = 3
        const val ROOT_RETRY_MS = 60L
        const val MAX_NODES = 700
        const val MAX_TEXT = 300
        const val CLICKABLE = 1
        const val LONG_CLICKABLE = 2
        const val EDITABLE = 4
        const val SCROLLABLE = 8
        const val CHECKABLE = 16
        const val CHECKED = 32
        const val ENABLED = 64
        const val FOCUSED = 128
        const val SELECTED = 256
        const val PASSWORD = 512
    }
}
