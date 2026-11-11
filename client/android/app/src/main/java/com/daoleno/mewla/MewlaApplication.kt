package com.daoleno.mewla

import android.app.Application

/** Starts before anything else in every process start (activity, push, alarm, boot): crash capture first. */
class MewlaApplication : Application() {
  override fun onCreate() {
    CrashLog.install(this)
    super.onCreate()
  }
}
