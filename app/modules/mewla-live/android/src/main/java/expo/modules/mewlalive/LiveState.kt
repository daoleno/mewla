package expo.modules.mewlalive

import org.json.JSONArray
import org.json.JSONObject

data class LiveWorker(val name: String, val phase: String)

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
