package com.daoleno.mewla

import android.content.Context
import android.database.sqlite.SQLiteDatabase

/**
 * What the Expo app kept in AsyncStorage (SQLite "RKStorage"): the computers, the current one,
 * drafts, preferences. The app copies it into its own storage once, on the first start after the
 * upgrade, and says when it has; the old database is left as it was.
 */
class LegacyStorage(private val context: Context) {
  private val prefs = context.getSharedPreferences("mewla.host", Context.MODE_PRIVATE)

  /** The Mewla entries, or null once they were copied or when there never was an Expo app. */
  fun entries(): Map<String, String>? {
    if (prefs.getBoolean(COPIED, false)) return null
    val file = context.getDatabasePath("RKStorage")
    if (!file.exists()) return null
    return SQLiteDatabase.openDatabase(file.path, null, SQLiteDatabase.OPEN_READONLY).use { db ->
      db.rawQuery("SELECT key, value FROM catalystLocalStorage WHERE key LIKE 'mewla%'", null).use { rows ->
        buildMap { while (rows.moveToNext()) put(rows.getString(0), rows.getString(1)) }
      }
    }
  }

  fun copied() {
    prefs.edit().putBoolean(COPIED, true).apply()
  }

  private companion object {
    const val COPIED = "legacyStorageCopied"
  }
}
