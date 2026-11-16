package com.daoleno.mewla.link

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Handler
import android.os.Looper
import com.longbridge.nsh.NshView

/**
 * Tells the app when the phone's network changes (the event `link.network {online}`): the connection to the computer
 * then reconnects as soon as a network is there again instead of waiting out its backoff, and a socket left on the
 * network that went away is found out in seconds, not after 25 s of silence (client/core/transport.ts
 * `networkChanged`). Only the default network counts, the one new sockets go out on. A change is another network
 * becoming the default, the default one becoming usable (validated), or losing it; the network as it is when the
 * watch starts is not one. The iOS host's LinkNetworkWatch tells the same.
 */
internal class NetworkWatch(context: Context, private val nsh: NshView) {
  private val connectivity = context.getSystemService(ConnectivityManager::class.java)
  private val main = Handler(Looper.getMainLooper())
  // What was last told, touched on the main thread only: "<network>" or "<network>:validated", or "offline".
  private var told: String? = null
  private var registered = false

  private val callback = object : ConnectivityManager.NetworkCallback() {
    override fun onAvailable(network: Network) = post(network.toString())

    override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
      if (capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) post("$network:validated")
    }

    override fun onLost(network: Network) = post("offline")
  }

  fun start() {
    if (registered) return
    told = connectivity.activeNetwork?.let { network ->
      val validated = connectivity.getNetworkCapabilities(network)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true
      if (validated) "$network:validated" else network.toString()
    } ?: "offline"
    registered = runCatching { connectivity.registerDefaultNetworkCallback(callback) }.isSuccess
  }

  fun stop() {
    if (!registered) return
    registered = false
    runCatching { connectivity.unregisterNetworkCallback(callback) }
  }

  private fun post(seen: String) {
    main.post {
      // The default network's own capability updates (signal strength, bandwidth) repeat what was told.
      if (seen == told || (!seen.endsWith(":validated") && told == "$seen:validated")) return@post
      told = seen
      nsh.send("link.network", mapOf("online" to (seen != "offline")))
    }
  }
}
