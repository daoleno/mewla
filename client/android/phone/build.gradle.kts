plugins { id("com.android.library") }

// Phone use: an agent drives this phone (wireless-ADB self-pairing, a shell helper, an accessibility
// service, a foreground service that also owns the link to the computer, notifications, PiP).
// GitHub APK only: the Play build never links it.
android {
  namespace = "com.daoleno.mewla.phone"
  compileSdk = 36
  defaultConfig {
    minSdk = 28
    // The helper is started by class name from a shell command, and the device key by the class name in the manifest.
    consumerProguardFiles("consumer-rules.pro")
  }
  testOptions { unitTests.isReturnDefaultValues = true }
}

dependencies {
  implementation("androidx.core:core-ktx:1.17.0")
  implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
  // The /phone/device WebSocket: it lives in the foreground service, so it survives the app being swiped away.
  implementation("com.squareup.okhttp3:okhttp:4.12.0")
  testImplementation("junit:junit:4.13.2")
  testImplementation("org.json:json:20240303")
}
