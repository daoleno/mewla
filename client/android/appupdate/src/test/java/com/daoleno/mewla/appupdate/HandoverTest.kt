package com.daoleno.mewla.appupdate

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class HandoverTest {
  private class Vault(var json: String? = null) : HandoverVault {
    override fun staged() = json
    override fun stage(json: String) { this.json = json }
    override fun clear() { json = null }
  }

  @Test fun `get answers the staged bundle, and nothing when none is staged`() {
    assertEquals(mapOf("json" to """{"version":1}"""), Handover.answer(Vault("""{"version":1}"""), Handover.GET))
    assertEquals(mapOf("json" to null), Handover.answer(Vault(), Handover.GET))
  }

  @Test fun `taken makes the old app forget it`() {
    val vault = Vault("""{"version":1}""")
    assertEquals(emptyMap<String, String?>(), Handover.answer(vault, Handover.TAKEN))
    assertNull(vault.json)
    assertEquals(mapOf("json" to null), Handover.answer(vault, Handover.GET))
  }

  @Test fun `anything else, or no vault, is refused`() {
    assertNull(Handover.answer(Vault("x"), "set"))
    assertNull(Handover.answer(null, Handover.GET))
  }
}
