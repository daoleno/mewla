package expo.modules.mewlalive

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONArray
import org.json.JSONObject

/** The calendar alerts' private state: what is armed, what fired, the local setting and the actions taken natively. */
internal class CalendarAlertStore(context: Context) {
  private val prefs: SharedPreferences =
    context.applicationContext.getSharedPreferences("mewla_calendar_alerts", Context.MODE_PRIVATE)

  var strong: Boolean
    get() = prefs.getBoolean("strong", true)
    set(value) { prefs.edit().putBoolean("strong", value).apply() }

  fun armed(): List<CalendarAlert> = try {
    val array = JSONArray(prefs.getString("armed", "[]"))
    (0 until array.length()).map { alertOf(array.getJSONObject(it)) }
  } catch (_: Exception) {
    emptyList()
  }

  fun setArmed(alerts: List<CalendarAlert>) {
    val array = JSONArray()
    alerts.forEach { array.put(jsonOf(it)) }
    prefs.edit().putString("armed", array.toString()).apply()
  }

  fun fired(now: Long): Map<String, Long> = try {
    val o = JSONObject(prefs.getString("fired", "{}"))
    CalendarAlertRules.pruneFired(o.keys().asSequence().associateWith { o.getLong(it) }, now)
  } catch (_: Exception) {
    emptyMap()
  }

  fun markFired(key: String, now: Long) {
    val o = JSONObject()
    (fired(now) + (key to now)).forEach { (k, v) -> o.put(k, v) }
    prefs.edit().putString("fired", o.toString()).apply()
  }

  fun pushAction(type: String, alert: CalendarAlert, minutes: Int, now: Long) {
    val array = try { JSONArray(prefs.getString("actions", "[]")) } catch (_: Exception) { JSONArray() }
    array.put(
      JSONObject().put("type", type).put("itemId", alert.itemId).put("serverId", alert.serverId)
        .put("minutes", minutes).put("atMs", now),
    )
    prefs.edit().putString("actions", array.toString()).apply()
  }

  fun takeActions(): String {
    val raw = prefs.getString("actions", "[]") ?: "[]"
    prefs.edit().remove("actions").apply()
    return raw
  }

  companion object {
    fun jsonOf(alert: CalendarAlert): JSONObject = JSONObject().put("key", alert.key).put("itemId", alert.itemId)
      .put("serverId", alert.serverId).put("title", alert.title).put("body", alert.body)
      .put("atMs", alert.atMs).put("native", alert.native)

    fun alertOf(o: JSONObject): CalendarAlert {
      val itemId = o.getString("itemId")
      val atMs = o.getLong("atMs")
      return CalendarAlert(
        key = o.optString("key").ifEmpty { CalendarAlertRules.key(itemId, atMs) },
        itemId = itemId,
        serverId = o.optString("serverId"),
        title = o.optString("title"),
        body = o.optString("body"),
        atMs = atMs,
        native = o.optBoolean("native", false),
      )
    }
  }
}
