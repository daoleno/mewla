package com.daoleno.mewla.phone

import android.content.Context
import android.view.View
import com.longbridge.nsh.NshComponent

/**
 * `mewla.phoneLive` (client/ui/phone/contract.ts): the agent's screen, live. `interactive`: touches
 * drive it (Take over) instead of being ignored (watch); `fps` and `maxWidth`: what this view needs of
 * the one shared frame stream.
 */
class PhoneLiveComponent(context: Context) : NshComponent {
  private val view = PhoneLiveView(context)

  override fun view(): View = view

  override fun setProp(name: String, value: Any?) {
    when (name) {
      "interactive" -> view.interactive = value == true
      "fps" -> (value as? Number)?.let { view.fps = it.toInt() }
      "maxWidth" -> (value as? Number)?.let { view.maxWidth = it.toInt() }
    }
  }
}
