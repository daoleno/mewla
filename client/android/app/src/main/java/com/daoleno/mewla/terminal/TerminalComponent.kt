package com.daoleno.mewla.terminal

import android.content.Context
import android.os.Trace
import android.view.View
import com.longbridge.nsh.NshComponent
import com.longbridge.nsh.NshComponentContext

/** `mewla.terminal` (client/ui/terminal/contract.ts): the native terminal view as an NSH component. */
class TerminalComponent(context: Context, private val ctx: NshComponentContext) : NshComponent {
  private val view = TerminalView(context, object : TerminalView.Events {
    override fun size(cols: Int, rows: Int) = ctx.emit("size", mapOf("cols" to cols, "rows" to rows))
    override fun failed(message: String) = ctx.emit("failed", mapOf("message" to message))
    override fun input(data: String) = ctx.emit("input", mapOf("data" to data))
    override fun scroll(lines: Int) {
      Trace.beginSection("mewla.term.scrollReport")
      try { ctx.emit("scroll", mapOf("lines" to lines)) } finally { Trace.endSection() }
    }
    override fun viewport(top: Int, atBottom: Boolean) = ctx.emit("viewport", mapOf("top" to top, "atBottom" to atBottom))
    override fun tapCell(col: Int, row: Int, text: String, index: Int) =
      ctx.emit("tapCell", mapOf("col" to col, "row" to row, "text" to text, "index" to index))
    override fun copy(text: String) = ctx.emit("copy", mapOf("text" to text))
    override fun fontSize(size: Int) = ctx.emit("fontSize", mapOf("size" to size))
  })

  override fun view(): View = view

  override fun setProp(name: String, value: Any?) {
    when (name) {
      "fontSize" -> (value as? Number)?.let { view.setFontSize(it.toFloat()) }
      "padding" -> (value as? Map<*, *>).let { m ->
        fun side(name: String) = (m?.get(name) as? Number)?.toFloat() ?: 0f
        view.setContentPadding(side("top"), side("right"), side("bottom"), side("left"))
      }
      "theme" -> (value as? Map<*, *>)?.let { m ->
        view.setTheme(
          background = m["background"] as? String ?: return,
          foreground = m["foreground"] as? String ?: return,
          cursor = m["cursor"] as? String ?: return,
          selection = m["selection"] as? String ?: return,
          handle = m["handle"] as? String,
          palette = (m["palette"] as? List<*>)?.map { it.toString() } ?: emptyList(),
        )
      }
    }
  }

  override fun command(name: String, args: Any?) {
    // A command from the page, as one trace section (free unless perfetto records): how many per frame, and their cost.
    Trace.beginSection(name)
    try { run(name, args as? Map<*, *>) } finally { Trace.endSection() }
  }

  private fun run(name: String, m: Map<*, *>?) {
    when (name) {
      "write" -> (m?.get("data") as? String)?.let(view::write)
      "reset" -> view.reset()
      "sessionSize" -> {
        val cols = (m?.get("cols") as? Number)?.toInt()
        val rows = (m?.get("rows") as? Number)?.toInt()
        if (cols != null && rows != null) view.setSessionSize(cols, rows)
      }
      "history" -> view.applyHistory(
        reset = m?.get("reset") == true,
        removed = (m?.get("removed") as? Number)?.toInt() ?: 0,
        rows = (m?.get("rows") as? List<*>)?.map { it as? String ?: "" } ?: emptyList(),
        cols = (m?.get("cols") as? Number)?.toInt() ?: 0,
        alternate = m?.get("alternate") == true,
      )
      "scrollTo" -> (m?.get("row") as? Number)?.let { view.scrollTo(it.toInt()) }
      "scrollToBottom" -> view.scrollToBottom()
      "mark" -> view.mark((m?.get("row") as? Number)?.toInt() ?: -1)
      "focus" -> view.showKeyboard()
      "blur" -> view.hideKeyboard()
      "retry" -> view.retry()
      "paste" -> (m?.get("text") as? String)?.let(view::pasteText)
    }
  }

  override fun destroy() = view.destroy()
}
