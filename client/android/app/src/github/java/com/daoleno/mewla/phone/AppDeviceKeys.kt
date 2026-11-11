package com.daoleno.mewla.phone

import android.content.Context
import com.daoleno.mewla.SecureStore
import com.daoleno.mewla.phone.link.DeviceIdentity
import com.daoleno.mewla.phone.link.DeviceKeys

/**
 * The device key the Phone use link signs with: the one the app pairs with, read from the app's
 * secure store under core/auth.ts's keys. The phone module names this class in its manifest, so
 * the link can sign after a reboot or a swipe-away with no screen. Nothing is copied out.
 */
class AppDeviceKeys(context: Context) : DeviceKeys {
  private val store = SecureStore(context)

  override fun identity(): DeviceIdentity? {
    val id = store.get(DEVICE_ID)?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    val seed = store.get(DEVICE_SEED)?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    return DeviceIdentity(id, seed)
  }

  private companion object {
    const val DEVICE_ID = "mewla.device.v3.id"
    const val DEVICE_SEED = "mewla.device.v3.seed"
  }
}
