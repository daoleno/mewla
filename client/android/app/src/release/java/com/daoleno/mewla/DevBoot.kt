package com.daoleno.mewla

import android.app.Activity
import android.content.Intent
import com.longbridge.nsh.NshView

/** A release build runs its own bundle only; the Dev app's live start is src/debug/DevBoot.kt. */
object DevBoot {
  @Suppress("UNUSED_PARAMETER")
  fun start(activity: Activity, nsh: NshView, intent: Intent?): Boolean = false

  @Suppress("UNUSED_PARAMETER")
  fun resumed(activity: Activity) = Unit
}
