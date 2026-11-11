package com.daoleno.mewla

import android.content.Intent
import android.content.res.Configuration
import com.longbridge.nsh.NshView

/** The Play build: no Phone use and no self-update (the store updates the app). Keep in step with src/github. */
@Suppress("UNUSED_PARAMETER")
class ChannelHosts(activity: MainActivity, nsh: NshView) {
  val channel = "play"

  fun onResume() {}

  fun onNewIntent(intent: Intent) {}

  fun onUserLeaveHint() {}

  fun onPictureInPictureModeChanged(inPip: Boolean, config: Configuration) {}

  fun onDestroy() {}
}
