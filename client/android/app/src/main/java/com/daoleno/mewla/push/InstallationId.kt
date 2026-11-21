package com.daoleno.mewla.push

import android.content.Context
import java.io.File
import java.util.UUID

/**
 * The id Expo's push service knows this install by (the `deviceId` of the Expo token
 * exchange): `noBackupFilesDir/expo_notifications_installation_uuid.txt`, made on first
 * use and kept. The file keeps the name installs already hold it under.
 */
internal object InstallationId {
  private const val FILE = "expo_notifications_installation_uuid.txt"

  @Synchronized
  fun get(context: Context): String {
    val dir = context.noBackupFilesDir
    read(File(dir, FILE))?.let { return it }
    val id = UUID.randomUUID().toString()
    File(dir, FILE).writeText(id)
    return id
  }

  private fun read(file: File): String? = try {
    file.takeIf { it.isFile }?.readText()?.trim()?.takeIf { it.isNotEmpty() }
  } catch (_: Exception) {
    null
  }
}
