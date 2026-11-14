package com.daoleno.mewla.live

import org.json.JSONArray
import org.json.JSONObject

data class LiveWorker(val name: String, val phase: String)

/** A Work's progress ring as the app draws it: moving or back as far as [percent] (rounded to 5, at most 95), or the last stretch. */
data class LiveRing(val stage: String, val percent: Int) {
  val last: Boolean get() = stage == "last"
  /** How much of the circle the arc covers; the last stretch closes it but for a small gap. */
  val fraction: Float get() = if (last) 1f else percent.coerceIn(0, 95) / 100f
}

/** One row of a live surface (daemon/live/items.go). */
data class LiveItem(
  val key: String,
  /** "work", "session" or "brain": what a tap opens. */
  val open: String,
  val id: String,
  val title: String,
  val line: String,
  /** The Session's provider (claude, codex, …), "brain", or "" for a plain terminal. */
  val agent: String,
  /** "answer" or "approve" while it waits for the person; "" otherwise. */
  val needs: String,
  val ring: LiveRing?,
  val startedAt: Long,
) {
  val asks: Boolean get() = needs.isNotEmpty()
}

data class LiveQuestion(
  val id: String,
  val kind: String,
  val workId: String,
  val text: String,
  val choices: List<String>,
)

/** The daemon's live_state projection (daemon/live): the one wire shape for the notification, the widgets and the endpoint. */
data class LiveState(
  val kind: String,
  val epoch: Long,
  val seq: Long,
  val runKey: String,
  val title: String,
  val phase: String,
  val startedAt: Long,
  val done: Int,
  val total: Int,
  val workers: List<LiveWorker>,
  val workerCount: Int,
  val needsCount: Int,
  val question: LiveQuestion?,
  /** What the surfaces draw: needs first, then running; at most three. Empty from a daemon that predates them. */
  val items: List<LiveItem> = emptyList(),
  /** The app header's "N running" (a Work with several Workers is one). */
  val running: Int = 0,
  val queued: Int = 0,
  val waiting: Int = 0,
) {
  val isIdle: Boolean get() = kind == KIND_IDLE
  val needsYou: Boolean get() = kind == KIND_NEEDS_YOU

  companion object {
    const val KIND_IDLE = "idle"
    const val KIND_RUNNING = "running"
    const val KIND_NEEDS_YOU = "needs_you"

    fun parse(raw: String?): LiveState? {
      if (raw.isNullOrBlank()) return null
      return try {
        from(JSONObject(raw))
      } catch (_: Exception) {
        null
      }
    }

    /** The state inside the widget endpoint's `{"live": <State>, "generated_at": ms}`, as the JSON the widget caches. */
    fun liveFromSnapshot(raw: String?): String? {
      if (raw.isNullOrBlank()) return null
      return try {
        val live = JSONObject(raw).optJSONObject("live") ?: return null
        if (from(live) == null) null else live.toString()
      } catch (_: Exception) {
        null
      }
    }

    fun from(o: JSONObject): LiveState? {
      val kind = o.optString("kind")
      if (kind != KIND_IDLE && kind != KIND_RUNNING && kind != KIND_NEEDS_YOU) return null
      val workers = ArrayList<LiveWorker>()
      o.optJSONArray("workers")?.let { arr ->
        for (i in 0 until arr.length()) {
          val w = arr.optJSONObject(i) ?: continue
          workers.add(LiveWorker(w.optString("name"), w.optString("phase")))
        }
      }
      val question = o.optJSONObject("question")?.let { q ->
        LiveQuestion(
          id = q.optString("id"),
          kind = q.optString("kind"),
          workId = q.optString("work_id"),
          text = q.optString("text"),
          choices = strings(q.optJSONArray("choices")),
        )
      }
      val items = ArrayList<LiveItem>()
      o.optJSONArray("items")?.let { arr ->
        for (i in 0 until arr.length()) {
          val it = arr.optJSONObject(i) ?: continue
          val ring = it.optJSONObject("ring")?.let { r -> LiveRing(r.optString("stage"), r.optInt("percent")) }
            ?.takeIf { r -> r.stage == "moving" || r.stage == "back" || r.stage == "last" }
          items.add(
            LiveItem(
              key = it.optString("key"),
              open = it.optString("open"),
              id = it.optString("id"),
              title = it.optString("title"),
              line = it.optString("line"),
              agent = it.optString("agent"),
              needs = it.optString("needs"),
              ring = ring,
              startedAt = it.optLong("started_at"),
            ),
          )
        }
      }
      return LiveState(
        kind = kind,
        epoch = o.optLong("epoch"),
        seq = o.optLong("seq"),
        runKey = o.optString("run_key"),
        title = o.optString("title"),
        phase = o.optString("phase"),
        startedAt = o.optLong("started_at"),
        done = o.optInt("done"),
        total = o.optInt("total"),
        workers = workers,
        workerCount = o.optInt("worker_count", workers.size),
        needsCount = o.optInt("needs_count"),
        question = question,
        items = items,
        running = o.optInt("running"),
        queued = o.optInt("queued"),
        waiting = o.optInt("waiting"),
      )
    }

    private fun strings(arr: JSONArray?): List<String> {
      if (arr == null) return emptyList()
      val out = ArrayList<String>()
      for (i in 0 until arr.length()) out.add(arr.optString(i))
      return out
    }
  }
}
