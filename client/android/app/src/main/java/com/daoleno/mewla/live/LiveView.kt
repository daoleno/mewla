package com.daoleno.mewla.live

import java.net.URLEncoder

/**
 * What the live surfaces show of a live state (the owner's pick 2026-11-13, option A of
 * worklog/mewla-live-widgets-1113): the live update shows the one thing that matters now, the widgets
 * show the tray. Pure, so it is tested without a device.
 */
internal object LiveView {
  /**
   * The rows to draw: the daemon's items, or, from a daemon that predates them, what its older fields
   * say (the question, else the running Work).
   */
  fun items(state: LiveState): List<LiveItem> {
    if (state.items.isNotEmpty()) return state.items
    val question = state.question
    if (state.needsYou && question != null) {
      return listOf(LiveItem("work:${question.workId}", "work", question.workId, state.title.ifBlank { "Brain" }, question.text, "brain", "answer", null, 0))
    }
    if (state.kind == LiveState.KIND_RUNNING) {
      return listOf(LiveItem("legacy", "brain", "", state.title.ifBlank { "Brain" }, state.phase, "brain", "", null, state.startedAt))
    }
    return emptyList()
  }

  /** The one item a live update shows: what needs you (newest), else what started last. */
  fun focus(state: LiveState): LiveItem? = items(state).firstOrNull()

  /** The app header's words: "1 needs you", "3 need you", "2 running", "1 queued, 1 waiting", "All quiet". */
  fun headerWords(state: LiveState): String {
    val needs = if (state.needsYou) state.needsCount.coerceAtLeast(1) else state.needsCount
    if (needs > 0) return if (needs == 1) "1 needs you" else "$needs need you"
    val running = runningCount(state)
    if (running > 0) return "$running running"
    val quiet = listOfNotNull(
      state.queued.takeIf { it > 0 }?.let { "$it queued" },
      state.waiting.takeIf { it > 0 }?.let { "$it waiting" },
    )
    return quiet.joinToString(", ").ifEmpty { "All quiet" }
  }

  fun runningCount(state: LiveState): Int =
    if (state.items.isNotEmpty() || state.running > 0) state.running else if (state.kind == LiveState.KIND_RUNNING) 1 else 0

  /** The row tag a request carries, as the tray's: "Approve" or "Answer". */
  fun tag(item: LiveItem): String = if (item.needs == "approve") "Approve" else "Answer"

  /** The choices that answer from the surface: only the Work question the daemon can take an answer for. */
  fun choices(state: LiveState, item: LiveItem): List<String> {
    val question = state.question ?: return emptyList()
    if (!item.asks || item.key != "work:${question.workId}") return emptyList()
    return LiveRules.actionChoices(state)
  }

  /** "+2 running": the others the small widget does not show. */
  fun othersRunning(state: LiveState, focus: LiveItem): String {
    val others = runningCount(state) - if (focus.asks) 0 else 1
    return if (others > 0) "+$others running" else ""
  }

  /** Where a tap goes: the item's Session or Work, Brain, else home. Names the computer so another one's link switches first. */
  fun link(item: LiveItem?, serverRef: String): String {
    val base = when {
      item == null -> "mewla://brain"
      item.open == "session" && item.id.isNotEmpty() -> "mewla://sessions/${enc(item.id)}"
      item.open == "work" && item.id.isNotEmpty() -> "mewla://brain/work/${enc(item.id)}"
      else -> "mewla://brain"
    }
    return if (serverRef.isEmpty()) base else "$base?serverId=${enc(serverRef)}"
  }

  private fun enc(value: String): String = URLEncoder.encode(value, "UTF-8").replace("+", "%20")
}
