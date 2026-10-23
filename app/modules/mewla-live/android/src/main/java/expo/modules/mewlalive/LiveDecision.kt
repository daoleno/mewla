package expo.modules.mewlalive

/** What the phone remembers between pushes. */
data class LiveMemory(val epoch: Long, val seq: Long, val dismissedKey: String)

sealed class LiveDecision {
  /** Not for this server, or older than what the phone already applied. */
  object Ignore : LiveDecision()

  /** Nothing runs or waits, or the user dismissed this run: take the surface down. */
  object Cancel : LiveDecision()
  object Post : LiveDecision()
}

object LiveRules {
  /**
   * One live surface, ending when nothing runs or waits. Dismissed means it stays
   * away until a different run (a new Work) or a different question arrives.
   */
  fun decide(state: LiveState, memory: LiveMemory, fromCurrentServer: Boolean): LiveDecision {
    if (!fromCurrentServer) return LiveDecision.Ignore
    if (isStale(state, memory)) return LiveDecision.Ignore
    if (state.isIdle) return LiveDecision.Cancel
    if (state.runKey.isNotEmpty() && state.runKey == memory.dismissedKey) return LiveDecision.Cancel
    return LiveDecision.Post
  }

  /** (epoch, seq) is ordered lexicographically; a restarted daemon brings a later epoch. */
  fun isStale(state: LiveState, memory: LiveMemory): Boolean {
    if (state.epoch < memory.epoch) return true
    return state.epoch == memory.epoch && state.seq <= memory.seq
  }

  /** (epoch, seq) order: whether [state] is older than [other]. */
  fun isOlder(state: LiveState, other: LiveState): Boolean =
    state.epoch < other.epoch || (state.epoch == other.epoch && state.seq < other.seq)

  /** A widget with nothing newer than this stops claiming to know what is running. */
  const val WIDGET_STALE_MS = 12L * 60 * 60 * 1000

  fun widgetFresh(fetchedAt: Long, now: Long): Boolean = fetchedAt > 0 && now - fetchedAt <= WIDGET_STALE_MS

  /** Android shows at most three actions; the daemon already caps choices at three. */
  fun actionChoices(state: LiveState): List<String> = state.question?.choices.orEmpty().take(3)
}
