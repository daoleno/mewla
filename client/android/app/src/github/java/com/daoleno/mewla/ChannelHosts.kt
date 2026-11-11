package com.daoleno.mewla

import android.content.Intent
import android.content.res.Configuration
import com.longbridge.nsh.NshView

/**
 * What only the GitHub APK has, behind `nsh.handle` / `nsh.send`: Phone use (`phone.*`) and the in-app
 * update (`update.*`). The Play build's ChannelHosts (src/play) registers nothing, so the app sees those
 * calls fail and hides the features. MainActivity forwards the lifecycle it needs.
 */
class ChannelHosts(private val activity: MainActivity, private val nsh: NshView) {
  val channel = "github"

  fun onResume() {}

  fun onNewIntent(intent: Intent) {}

  fun onUserLeaveHint() {}

  fun onPictureInPictureModeChanged(inPip: Boolean, config: Configuration) {}

  fun onDestroy() {}
}
