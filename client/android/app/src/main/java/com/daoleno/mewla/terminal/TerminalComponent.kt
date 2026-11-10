package com.daoleno.mewla.terminal

import android.content.Context
import android.view.View
import com.longbridge.nsh.NshComponent
import com.longbridge.nsh.NshComponentContext

/** `mewla.terminal` (client/ui/terminal/contract.ts): the native terminal view as an NSH component. */
class TerminalComponent(context: Context, private val ctx: NshComponentContext) : NshComponent {
  private val view = TerminalView(context, object : TerminalView.Events {
    override fun size(cols: Int, rows: Int) = ctx.emit("size", mapOf("cols" to cols, "rows" to rows))
    override fun input(data: String) = ctx.emit("input", mapOf("data" to data))
    override fun scroll(lines: Int) = ctx.emit("scroll", mapOf("lines" to lines))
    override fun tapCell(col: Int, row: Int) = ctx.emit("tapCell", mapOf("col" to col, "row" to row))
  })

  override fun view(): View = view

  override fun setProp(name: String, value: Any?) {
    when (name) {
      "fontSize" -> (value as? Number)?.let { view.setFontSize(it.toFloat()) }
      "theme" -> (value as? Map<*, *>)?.let { m ->
        view.setTheme(
          background = m["background"] as? String ?: return,
          foreground = m["foreground"] as? String ?: return,
          cursor = m["cursor"] as? String ?: return,
          selection = m["selection"] as? String ?: return,
          palette = (m["palette"] as? List<*>)?.map { it.toString() } ?: emptyList(),
        )
      }
    }
  }

  override fun command(name: String, args: Any?) {
    val m = args as? Map<*, *>
    when (name) {
      "write" -> (m?.get("data") as? String)?.let(view::write)
      "reset" -> view.reset()
      "focus" -> view.showKeyboard()
      "blur" -> view.hideKeyboard()
    }
  }

  override fun destroy() = view.destroy()
}
