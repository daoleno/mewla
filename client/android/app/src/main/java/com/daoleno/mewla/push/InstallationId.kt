package com.daoleno.mewla.push

import android.content.Context
import java.io.File
import java.util.UUID

/**
 * The id Expo's push service knows this install by (the `deviceId` of the Expo token
 * exchange), read where expo-notifications kept it so the upgraded app keeps its Expo
 * token: `noBackupFilesDir/expo_notifications_installation_uuid.txt`, then the older
 * SharedPreferences `host.exp.exponent.SharedPreferences` key `uuid`, then
 * `expo_installation_uuid.txt`. A fresh install makes one and keeps it in the first place.
 */
internal object InstallationId {
  private const val FILE = "expo_notifications_installation_uuid.txt"
  private const val LEGACY_FILE = "expo_installation_uuid.txt"
  private const val LEGACY_PREFS = "host.exp.exponent.SharedPreferences"
  private const val LEGACY_KEY = "uuid"

  @Synchronized
  fun get(context: Context): String {
    val dir = context.noBackupFilesDir
    read(File(dir, FILE))?.let { return it }
    val legacy = context.getSharedPreferences(LEGACY_PREFS, Context.MODE_PRIVATE).getString(LEGACY_KEY, null)
      ?: read(File(dir, LEGACY_FILE))
    val id = legacy?.trim()?.takeIf { it.isNotEmpty() } ?: UUID.randomUUID().toString()
    File(dir, FILE).writeText(id)
    return id
  }

  private fun read(file: File): String? = try {
    file.takeIf { it.isFile }?.readText()?.trim()?.takeIf { it.isNotEmpty() }
  } catch (_: Exception) {
    null
  }
}
