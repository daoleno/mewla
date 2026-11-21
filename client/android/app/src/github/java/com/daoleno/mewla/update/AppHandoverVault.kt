package com.daoleno.mewla.update

import android.content.Context
import com.daoleno.mewla.SecureStore
import com.daoleno.mewla.appupdate.HandoverVault

/**
 * What the app stages for the new app of a move, kept in its secure store like the device key itself (the
 * provider in the appupdate module names this class in its manifest).
 */
class AppHandoverVault(context: Context) : HandoverVault {
  private val store = SecureStore(context)

  override fun staged(): String? = store.get(KEY)
  override fun stage(json: String) = store.set(KEY, json)
  override fun clear() = store.remove(KEY)

  private companion object {
    const val KEY = "mewla.handover.v1"
  }
}
