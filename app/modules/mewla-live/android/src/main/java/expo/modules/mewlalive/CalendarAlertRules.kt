package expo.modules.mewlalive

/** One alert the phone owes the user: armed as a local alarm, or arriving as a push. */
internal data class CalendarAlert(
  val key: String,
  val itemId: String,
  val serverId: String,
  val title: String,
  val body: String,
  val atMs: Long,
  /** Armed on this phone by a snooze, before the daemon heard of it. */
  val native: Boolean = false,
)

internal enum class BootDecision { Arm, FireNow, Drop }

/** Pure decisions behind the calendar alerts, so the clock and the fired set are testable. */
internal object CalendarAlertRules {
  /** An alert later than this past its time is news from the past, as for the daemon. */
  const val WINDOW_MS = 15 * 60 * 1000L

  /** Fired keys are remembered this long; a push or alarm for one is dropped. */
  const val FIRED_TTL_MS = 2 * 24 * 60 * 60 * 1000L

  /**
   * A snooze armed on this phone and the daemon's own snooze of the same press differ by the rounding
   * of its minutes and the time the app took to tell it; within this they are one alert.
   */
  const val SAME_ALERT_MS = 2 * 60 * 1000L

  fun key(itemId: String, atMs: Long) = "$itemId@$atMs"

  fun itemOf(key: String) = key.substringBeforeLast('@')

  fun atOf(key: String): Long? = key.substringAfterLast('@', "").toLongOrNull()

  /** A stable notification id for a key, so a race between alarm and push replaces instead of doubling. */
  fun notificationId(key: String): Int = key.hashCode() and 0x7fffffff

  fun pruneFired(fired: Map<String, Long>, now: Long): Map<String, Long> =
    fired.filterValues { now - it <= FIRED_TTL_MS }

  /** Whether [key] was already shown: fired itself, or for the same item within [SAME_ALERT_MS] of one that was. */
  fun alreadyFired(fired: Map<String, Long>, key: String): Boolean {
    if (fired.containsKey(key)) return true
    val at = atOf(key) ?: return false
    val item = itemOf(key)
    return fired.keys.any { other ->
      itemOf(other) == item && atOf(other)?.let { kotlin.math.abs(it - at) <= SAME_ALERT_MS } == true
    }
  }

  /** What to do with an armed alarm found after a reboot or an app update. */
  fun onBoot(atMs: Long, now: Long): BootDecision = when {
    atMs > now -> BootDecision.Arm
    now - atMs <= WINDOW_MS -> BootDecision.FireNow
    else -> BootDecision.Drop
  }

  /** The instant a snooze pressed at [now] for [minutes] alerts again. */
  fun snoozeAt(now: Long, minutes: Int): Long = now + minutes.coerceAtLeast(1) * 60_000L

  /**
   * The new armed set after the app synced [synced]: its alerts replace the old ones, except that a
   * snooze armed on this phone stays until the daemon's own alert for that item has arrived.
   */
  fun mergeArmed(armed: List<CalendarAlert>, synced: List<CalendarAlert>, now: Long): List<CalendarAlert> {
    val future = synced.filter { it.atMs > now }.distinctBy { it.key }
    val kept = armed.filter { old ->
      old.native && old.atMs > now && future.none {
        it.itemId == old.itemId && kotlin.math.abs(it.atMs - old.atMs) <= SAME_ALERT_MS
      }
    }
    return future + kept
  }
}
