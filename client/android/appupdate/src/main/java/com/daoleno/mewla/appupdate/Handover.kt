package com.daoleno.mewla.appupdate

import android.content.ContentProvider
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.database.Cursor
import android.net.Uri
import android.os.Bundle

/**
 * Where a move to another app (a release of another application id installs beside this app) leaves what pairs
 * this phone with its computers, for the new app to take once. The app implements it over its secure store and
 * names the class in this module's manifest (`com.daoleno.mewla.appupdate.VAULT`), as the phone module does for
 * the device key. The bundle is the app's own JSON (client/core/update/handover.ts); nothing here reads it.
 */
interface HandoverVault {
  fun staged(): String?
  fun stage(json: String)
  fun clear()

  companion object {
    const val META = "com.daoleno.mewla.appupdate.VAULT"

    fun load(context: Context): HandoverVault? = runCatching {
      val info = context.packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
      val name = info.metaData?.getString(META) ?: return null
      Class.forName(name).getConstructor(Context::class.java).newInstance(context.applicationContext) as HandoverVault
    }.getOrNull()
  }
}

/** What the new app asks of the old one, and what it gets: a pure function, so a JVM test covers every call. */
object Handover {
  /** `call("get")`: the staged bundle (`json`, absent when none is staged). */
  const val GET = "get"
  /** `call("taken")`: the new app has it; the old one forgets it. */
  const val TAKEN = "taken"
  const val JSON = "json"

  /** The answer's fields, or null for a call this provider does not know (or no vault to answer from). */
  fun answer(vault: HandoverVault?, method: String): Map<String, String?>? {
    vault ?: return null
    return when (method) {
      GET -> mapOf(JSON to vault.staged())
      TAKEN -> {
        vault.clear()
        emptyMap()
      }
      else -> null
    }
  }
}

/**
 * The old app's side of the move: `content://<applicationId>.handover`, protected by the signature permission
 * `<applicationId>.permission.HANDOVER` (this module's manifest), so only an app signed with the same release key
 * can call it. Calls only; it has no table.
 */
class HandoverProvider : ContentProvider() {
  override fun onCreate(): Boolean = true

  override fun call(method: String, arg: String?, extras: Bundle?): Bundle? {
    val context = context ?: return null
    val fields = Handover.answer(HandoverVault.load(context), method) ?: return null
    return Bundle().also { bundle -> fields.forEach { (key, value) -> bundle.putString(key, value) } }
  }

  override fun query(uri: Uri, projection: Array<out String>?, selection: String?, selectionArgs: Array<out String>?, sortOrder: String?): Cursor? = null
  override fun getType(uri: Uri): String? = null
  override fun insert(uri: Uri, values: ContentValues?): Uri? = null
  override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = 0
  override fun update(uri: Uri, values: ContentValues?, selection: String?, selectionArgs: Array<out String>?): Int = 0
}

/**
 * The new app's side: what the app it replaces (the id before the move) staged, taken once, and the old app's
 * removal. Everything answers "nothing" for an id that is this app's own, an app that is not installed, one that is
 * not signed with the same key, or one built before the move: the person then pairs by scanning, as on a new phone.
 */
object HandoverClient {
  fun staged(context: Context, legacyId: String): String? = call(context, legacyId, Handover.GET)?.getString(Handover.JSON)

  fun taken(context: Context, legacyId: String) {
    call(context, legacyId, Handover.TAKEN)
  }

  fun installed(context: Context, legacyId: String): Boolean = legacyId != context.packageName && runCatching {
    context.packageManager.getPackageInfo(legacyId, 0)
  }.isSuccess

  /** The system's own uninstall dialog for the old app. */
  fun removeIntent(legacyId: String): Intent =
    Intent(Intent.ACTION_DELETE, Uri.parse("package:$legacyId")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)

  private fun call(context: Context, legacyId: String, method: String): Bundle? {
    if (legacyId == context.packageName) return null
    return try {
      context.contentResolver.call(Uri.parse("content://$legacyId.handover"), method, null, null)
    } catch (_: Exception) {
      // Not installed, not visible, another signature (SecurityException), or an old build without the provider.
      null
    }
  }
}
