package expo.modules.mewlalive

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** The user swiped the live notification away: keep it away until a different run or question. */
class LiveDismissReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    val key = intent.getStringExtra("run_key").orEmpty()
    if (key.isNotEmpty()) LivePrefs(context).dismiss(key)
  }
}
