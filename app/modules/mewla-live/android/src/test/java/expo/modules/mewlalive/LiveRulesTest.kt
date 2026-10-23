package expo.modules.mewlalive

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class LiveRulesTest {
  private fun state(kind: String = "running", epoch: Long = 1, seq: Long = 1, runKey: String = "w1") = LiveState(
    kind = kind, epoch = epoch, seq = seq, runKey = runKey, title = "perpetuo", phase = "releasing",
    startedAt = 1000, done = 2, total = 5, workers = emptyList(), workerCount = 1, needsCount = 0, question = null,
  )

  @Test fun postsANewerRunningState() {
    assertEquals(LiveDecision.Post, LiveRules.decide(state(seq = 2), LiveMemory(1, 1, ""), true))
  }

  @Test fun ignoresRepeatsOlderStatesAndOtherServers() {
    assertEquals(LiveDecision.Ignore, LiveRules.decide(state(seq = 1), LiveMemory(1, 1, ""), true))
    assertEquals(LiveDecision.Ignore, LiveRules.decide(state(epoch = 1, seq = 9), LiveMemory(2, 0, ""), true))
    assertEquals(LiveDecision.Ignore, LiveRules.decide(state(seq = 5), LiveMemory(1, 1, ""), false))
  }

  @Test fun aRestartedDaemonWithLaterEpochIsAcceptedEvenWithLowSeq() {
    assertEquals(LiveDecision.Post, LiveRules.decide(state(epoch = 2, seq = 1), LiveMemory(1, 99, ""), true))
  }

  @Test fun idleTakesTheSurfaceDown() {
    assertEquals(LiveDecision.Cancel, LiveRules.decide(state(kind = "idle", seq = 3), LiveMemory(1, 1, ""), true))
  }

  @Test fun dismissedRunStaysAwayUntilANewRunKey() {
    val memory = LiveMemory(1, 1, "w1")
    assertEquals(LiveDecision.Cancel, LiveRules.decide(state(seq = 2, runKey = "w1"), memory, true))
    assertEquals(LiveDecision.Post, LiveRules.decide(state(seq = 3, runKey = "w2"), memory, true))
  }

  @Test fun parsesTheDaemonWireShapeAndRejectsGarbage() {
    val parsed = LiveState.parse(
      """{"kind":"needs_you","epoch":5,"seq":7,"run_key":"q1","needs_count":2,
      "question":{"id":"q1","kind":"work","work_id":"w","text":"Ship?","choices":["Ship","Wait","x","y"]}}""",
    )
    assertNotNull(parsed)
    assertEquals(true, parsed!!.needsYou)
    assertEquals(listOf("Ship", "Wait", "x"), LiveRules.actionChoices(parsed))
    assertNull(LiveState.parse("{not json"))
    assertNull(LiveState.parse("""{"kind":"weird"}"""))
  }

  @Test fun theWidgetCachesTheStateInsideTheSnapshotEnvelope() {
    val body = """{"live":{"kind":"running","epoch":5,"seq":7,"workers":[{"name":"a","phase":"b"}]},"generated_at":123}"""
    val cached = LiveState.liveFromSnapshot(body)
    assertNotNull(cached)
    assertEquals("running", LiveState.parse(cached)!!.kind)
    // The envelope itself is not a state, which is why the widget must unwrap it.
    assertNull(LiveState.parse(body))
    assertNull(LiveState.liveFromSnapshot("""{"generated_at":1}"""))
    assertNull(LiveState.liveFromSnapshot("""{"live":{"kind":"weird"}}"""))
  }
}
