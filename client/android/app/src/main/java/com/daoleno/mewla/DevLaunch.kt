package com.daoleno.mewla

import java.util.UUID

/**
 * The one way a debug build starts on an `nsh dev` server: the start link (DevStartActivity, src/debug), after
 * the person confirmed it. It keeps the confirmed server in this process under a random token and starts
 * MainActivity with only the token, so another app cannot start the app on a server of its choosing (MainActivity
 * is exported). The token outlives a recreate (the intent keeps it), not the process or a start from the icon.
 */
object DevLaunch {
  const val EXTRA = "devLaunch"

  @Volatile private var confirmed: Pair<String, String>? = null

  /** The token for a server the person just confirmed. */
  fun confirm(server: String): String = UUID.randomUUID().toString().also { confirmed = it to server }

  /** The confirmed server, when the launch carries its token. */
  fun serverFor(token: String?): String? = confirmed?.takeIf { token != null && it.first == token }?.second
}
