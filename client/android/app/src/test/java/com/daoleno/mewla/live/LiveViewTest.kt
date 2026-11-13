package com.daoleno.mewla.live

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LiveViewTest {
  private val wire = """{"kind":"needs_you","epoch":3,"seq":9,"run_key":"ask:q1","title":"Pricing page","needs_count":2,
    "question":{"id":"q1","kind":"work","work_id":"w-price","text":"14-day or 7-day trial?","choices":["14-day","7-day"]},
    "running":2,"waiting":1,
    "items":[
      {"key":"work:w-price","open":"work","id":"w-price","title":"Pricing page","line":"14-day or 7-day trial?","agent":"claude","needs":"answer"},
      {"key":"session:s9","open":"session","id":"s9","title":"infra","line":"Run git push origin main","agent":"codex","needs":"approve"},
      {"key":"work:w1","open":"session","id":"s1","title":"Quickstart rewrite","line":"Editing docs","agent":"claude","ring":{"stage":"moving","percent":45},"started_at":1700000000000}
    ]}"""

  @Test fun parsesItemsRingsAndCounts() {
    val state = LiveState.parse(wire)!!
    assertEquals(3, state.items.size)
    assertEquals(2, state.running)
    assertEquals(1, state.waiting)
    val ring = state.items[2].ring
    assertNotNull(ring)
    assertEquals(0.45f, ring!!.fraction, 0.001f)
    assertTrue(state.items[0].asks)
    assertNull(state.items[0].ring)
  }

  @Test fun anUnknownRingStageDrawsNoRing() {
    val state = LiveState.parse("""{"kind":"running","items":[{"key":"k","open":"work","title":"t","ring":{"stage":"unmeasured"}}]}""")!!
    assertNull(state.items[0].ring)
  }

  @Test fun focusIsTheNewestRequestAndTheHeaderSaysTheCount() {
    val state = LiveState.parse(wire)!!
    assertEquals("work:w-price", LiveView.focus(state)!!.key)
    assertEquals("2 need you", LiveView.headerWords(state))
    assertEquals("Answer", LiveView.tag(state.items[0]))
    assertEquals("Approve", LiveView.tag(state.items[1]))
  }

  @Test fun choicesAnswerOnlyTheWorkQuestion() {
    val state = LiveState.parse(wire)!!
    assertEquals(listOf("14-day", "7-day"), LiveView.choices(state, state.items[0]))
    assertEquals(emptyList<String>(), LiveView.choices(state, state.items[1]))
    assertEquals(emptyList<String>(), LiveView.choices(state, state.items[2]))
  }

  @Test fun linksOpenTheItemOnItsComputer() {
    val state = LiveState.parse(wire)!!
    assertEquals("mewla://brain/work/w-price?serverId=srv%201", LiveView.link(state.items[0], "srv 1"))
    assertEquals("mewla://sessions/s1", LiveView.link(state.items[2], ""))
    assertEquals("mewla://brain", LiveView.link(null, ""))
  }

  @Test fun runningAndQuietWords() {
    val running = LiveState.parse("""{"kind":"running","running":3,"items":[{"key":"a","open":"session","id":"a","title":"A"}]}""")!!
    assertEquals("3 running", LiveView.headerWords(running))
    assertEquals("+2 running", LiveView.othersRunning(running, running.items[0]))
    val quiet = LiveState.parse("""{"kind":"idle","queued":1,"waiting":2,"items":[]}""")!!
    assertEquals("1 queued, 2 waiting", LiveView.headerWords(quiet))
    assertEquals("All quiet", LiveView.headerWords(LiveState.parse("""{"kind":"idle"}""")!!))
  }

  @Test fun anOlderDaemonStillShowsItsQuestionOrItsRun() {
    val asking = LiveState.parse("""{"kind":"needs_you","needs_count":1,"title":"Ship","question":{"id":"q","work_id":"w","text":"Ship it?","choices":["Ship"]}}""")!!
    val item = LiveView.focus(asking)!!
    assertEquals("Ship", item.title)
    assertEquals("Ship it?", item.line)
    assertEquals(listOf("Ship"), LiveView.choices(asking, item))
    val running = LiveState.parse("""{"kind":"running","title":"perpetuo","phase":"releasing","started_at":5}""")!!
    assertEquals("perpetuo", LiveView.focus(running)!!.title)
    assertEquals("1 running", LiveView.headerWords(running))
    assertNull(LiveView.focus(LiveState.parse("""{"kind":"idle"}""")!!))
  }
}
